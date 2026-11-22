// Package jobs is the background-job service used by cmd/worker.
//
// It claims jobs from the durable jobs table (SELECT ... FOR UPDATE SKIP
// LOCKED in Postgres) and handles lab_provision, lab_destroy, lab_expire
// and audit_cleanup. Failures retry with bounded exponential backoff
// (2^attempts seconds, capped at 60s); after MaxAttempts the job is
// dead-lettered (state 'dead'). A lab that reaches FAILED because the
// network engine is unreachable is a terminal lab outcome, NOT a job
// retry — the job completes successfully having recorded the failure.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/hostagent"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
	"github.com/cloudforge/cloudforge/backend/internal/metrics"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
)

// Service drives jobs.
type Service struct {
	Jobs         store.JobStore
	Labs         store.LabStore
	Audit        store.AuditStore
	CheckNetwork func() bool // nil = unreachable; provisioning must fail honestly
	// HostAgent is the typed host-agent client (M3). Nil = host agent
	// not configured: provisioning uses the network-engine gRPC Health check.
	HostAgent   hostagent.Provisioner
	Logger      *slog.Logger
	MaxAttempts int
	WorkerID    string
	// Terraform (Milestone 7). Nil Runner/Runs means terraform jobs
	// fail with a recorded run failure, never a faked success.
	TerraformRunner terraform.Runner
	TerraformRuns   store.TerraformRunStore
	Resources       store.LabResourceStore
	TerraformCfg    TerraformConfig
	// Engine is the network-engine topology client (Milestone 9,
	// Part 3) used by cleanup reconciliation. Nil = engine not
	// configured: reconcile reports it and skips engine checks
	// rather than deleting anything.
	Engine EngineTopology
	// AuditExportDir is where audit_cleanup archives rows older
	// than 90 days before deleting them (Phase 7, R10). Empty =
	// no archive is written (the delete still happens, and the
	// missing archive is logged).
	AuditExportDir string
}

func (s *Service) log() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Service) maxAttempts() int {
	if s.MaxAttempts < 1 {
		return 5
	}
	return s.MaxAttempts
}

func backoff(attempts int) time.Duration {
	secs := math.Pow(2, float64(attempts))
	if secs > 60 {
		secs = 60
	}
	return time.Duration(secs) * time.Second
}

// RunOnce claims up to 5 due jobs and handles each.
func (s *Service) RunOnce(ctx context.Context) error {
	jobs, err := s.Jobs.Claim(ctx, s.WorkerID, 5)
	if err != nil {
		return fmt.Errorf("claim jobs: %w", err)
	}
	for _, j := range jobs {
		if err := s.handle(ctx, j); err != nil {
			// Attempts was already incremented when the job was
			// claimed, so j.Attempts is the count including this try.
			dead := j.Attempts >= s.maxAttempts()
			retryAt := time.Now().Add(backoff(j.Attempts))
			result := "failed"
			if dead {
				result = "dead"
			}
			metrics.JobsTotal.WithLabelValues(j.Type, result).Inc()
			s.log().Error("job failed", "job_id", j.ID, "type", j.Type, "attempts", j.Attempts, "dead", dead, "error", err)
			if ferr := s.Jobs.Fail(ctx, j.ID, err.Error(), retryAt, dead); ferr != nil {
				s.log().Error("could not record job failure", "job_id", j.ID, "error", ferr)
			}
			continue
		}
		metrics.JobsTotal.WithLabelValues(j.Type, "completed").Inc()
		if err := s.Jobs.Complete(ctx, j.ID); err != nil {
			s.log().Error("could not complete job", "job_id", j.ID, "error", err)
		}
	}
	return nil
}

func (s *Service) handle(ctx context.Context, j *store.Job) error {
	switch j.Type {
	case store.JobLabProvision:
		return s.provision(ctx, j)
	case store.JobLabDestroy:
		return s.destroy(ctx, j)
	case store.JobLabExpire:
		return s.expireOne(ctx, j)
	case store.JobAuditCleanup:
		return s.auditCleanup(ctx, j)
	case store.JobTerraformInit, store.JobTerraformValidate, store.JobTerraformPlan,
		store.JobTerraformApply, store.JobTerraformDestroy:
		return s.handleTerraform(ctx, j)
	case store.JobCleanupReconcile:
		return s.cleanupReconcile(ctx, j)
	default:
		return fmt.Errorf("unknown job type %q", j.Type)
	}
}

func labIDOf(j *store.Job) (string, error) {
	raw, ok := j.Payload["lab_id"]
	if !ok {
		return "", fmt.Errorf("job %s has no lab_id in payload", j.ID)
	}
	id, ok := raw.(string)
	if !ok || id == "" {
		return "", fmt.Errorf("job %s has an invalid lab_id", j.ID)
	}
	return id, nil
}

func (s *Service) provision(ctx context.Context, j *store.Job) error {
	id, err := labIDOf(j)
	if err != nil {
		return err
	}
	lab, err := s.Labs.Get(ctx, id)
	if err != nil {
		return err
	}
	switch lab.State {
	case labs.StateReady, labs.StateFailed, labs.StateDestroyed:
		return nil // already terminal for this job
	case labs.StateCreated:
		if _, err := s.Labs.Transition(ctx, id, labs.StateProvisioning, ""); err != nil {
			return err
		}
	}
	// M3: when a host agent is configured, provision the lab VM through
	// the typed host-agent client. An unavailable host agent falls back
	// to the network-engine gRPC Health check below with a logged reason.
	if s.HostAgent != nil {
		handled, herr := s.provisionViaHostAgent(ctx, lab)
		if handled {
			if herr != nil {
				_, err := s.Labs.Transition(ctx, id, labs.StateFailed, herr.Error())
				return err
			}
			_, err := s.Labs.Transition(ctx, id, labs.StateReady, "")
			return err
		}
		s.log().Warn("host agent unavailable; falling back to network-engine gRPC Health check", "lab_id", id, "error", herr)
	}
	// The network engine must be reachable or the lab fails honestly.
	reachable := false
	if s.CheckNetwork != nil {
		reachable = s.CheckNetwork()
	}
	if !reachable {
		_, err := s.Labs.Transition(ctx, id, labs.StateFailed, "network engine unreachable: cannot provision isolated network")
		return err
	}
	_, err = s.Labs.Transition(ctx, id, labs.StateReady, "")
	return err
}

// profileForDefinition maps a lab definition to a VM profile for host
// agent provisioning.
func profileForDefinition(definitionID string) string {
	switch definitionID {
	case "ec2-basics":
		return "small"
	case "three-tier-app":
		return "medium"
	case "vpc-fundamentals", "iam-security":
		return "micro"
	default:
		return "micro"
	}
}

// provisionViaHostAgent provisions the lab's primary VM through the
// typed host-agent client. It returns handled=true when the host agent
// decided the outcome (success or a terminal failure). It returns
// handled=false with an Unavailable error when the host agent is not
// reachable/available, so the caller falls back to the network-engine
// gRPC Health check with a logged reason.
func (s *Service) provisionViaHostAgent(ctx context.Context, lab *labs.Lab) (handled bool, err error) {
	profile := profileForDefinition(lab.DefinitionID)
	name := "lab-" + lab.ID
	if len(name) > 40 {
		name = name[:40]
	}
	create, err := s.HostAgent.Do(ctx, hostagent.OpRequest{
		Op: hostagent.OpCreateVM, LabID: lab.ID, OwnerID: lab.OwnerID,
		Name: name, Profile: profile, Hostname: name,
		Username: "cloudforge", IdempotencyKey: "provision:" + lab.ID,
	})
	if err != nil {
		if hostagent.IsUnavailable(err) {
			return false, err
		}
		return true, fmt.Errorf("host agent CreateVM failed: %w", err)
	}
	if create.ID == "" {
		return true, fmt.Errorf("host agent CreateVM returned no VM id")
	}
	if _, err := s.HostAgent.Do(ctx, hostagent.OpRequest{
		Op: hostagent.OpStartVM, LabID: lab.ID, OwnerID: lab.OwnerID,
		ID: create.ID, IdempotencyKey: "provision-start:" + lab.ID,
	}); err != nil {
		if hostagent.IsUnavailable(err) {
			return false, err
		}
		return true, fmt.Errorf("host agent StartVM failed: %w", err)
	}
	s.log().Info("lab provisioned via host agent", "lab_id", lab.ID, "vm_id", create.ID, "profile", profile)
	return true, nil
}

func (s *Service) destroy(ctx context.Context, j *store.Job) error {
	id, err := labIDOf(j)
	if err != nil {
		return err
	}
	lab, err := s.Labs.Get(ctx, id)
	if err != nil {
		return err
	}
	if lab.State == labs.StateDestroyed {
		return nil
	}
	if lab.State != labs.StateDestroying {
		if _, err := s.Labs.Transition(ctx, id, labs.StateDestroying, ""); err != nil {
			// A lab in a state that cannot be destroyed (e.g. already
			// DESTROYED, raced) is not a retryable job error.
			if errors.Is(err, labs.ErrInvalidTransition) {
				s.log().Warn("destroy job skipped: invalid transition", "lab_id", id, "state", lab.State)
				return nil
			}
			return err
		}
	}
	s.cleanupHostAgent(ctx, lab)
	if err := s.Labs.CleanupResources(ctx, id); err != nil {
		return err
	}
	_, err = s.Labs.Transition(ctx, id, labs.StateDestroyed, "")
	return err
}

// cleanupHostAgent removes the lab's containers/VMs via the host agent
// when one is configured. An unavailable host agent is logged and does
// not fail the destroy: the lab's DB resources are still cleaned up by
// the caller.
func (s *Service) cleanupHostAgent(ctx context.Context, lab *labs.Lab) {
	if s.HostAgent == nil {
		return
	}
	if err := s.HostAgent.CleanupLab(ctx, lab.ID, lab.OwnerID); err != nil {
		if hostagent.IsUnavailable(err) {
			s.log().Warn("host agent unavailable during lab destroy; host resources may remain", "lab_id", lab.ID, "error", err)
		} else {
			s.log().Warn("host agent cleanup failed during lab destroy", "lab_id", lab.ID, "error", err)
		}
	}
}

func (s *Service) expireOne(ctx context.Context, j *store.Job) error {
	id, err := labIDOf(j)
	if err != nil {
		return err
	}
	return s.expireLab(ctx, id)
}

func (s *Service) expireLab(ctx context.Context, id string) error {
	lab, err := s.Labs.Get(ctx, id)
	if err != nil {
		return err
	}
	if lab.State == labs.StateDestroyed || lab.State == labs.StateDestroying {
		return nil
	}
	if _, err := s.Labs.Transition(ctx, id, labs.StateExpired, "lab expired"); err != nil {
		if errors.Is(err, labs.ErrInvalidTransition) {
			return nil
		}
		return err
	}
	if _, err := s.Labs.Transition(ctx, id, labs.StateDestroying, ""); err != nil {
		return err
	}
	s.cleanupHostAgent(ctx, lab)
	if err := s.Labs.CleanupResources(ctx, id); err != nil {
		return err
	}
	_, err = s.Labs.Transition(ctx, id, labs.StateDestroyed, "")
	return err
}

// SweepExpired finds labs past expires_at and expires+destroys them.
// Called periodically by the worker loop (not only via lab_expire jobs,
// so expiry does not depend on a job having been enqueued).
func (s *Service) SweepExpired(ctx context.Context) error {
	due, err := s.Labs.ExpireDue(ctx)
	if err != nil {
		return fmt.Errorf("list expired labs: %w", err)
	}
	for _, l := range due {
		if err := s.expireLab(ctx, l.ID); err != nil {
			s.log().Error("expiry sweep failed for lab", "lab_id", l.ID, "error", err)
		} else {
			s.log().Info("expired lab destroyed", "lab_id", l.ID)
		}
	}
	return nil
}

func (s *Service) auditCleanup(ctx context.Context, j *store.Job) error {
	if s.Audit == nil {
		return nil
	}
	// Phase 7 (R10): audit retention is no longer delete-only.
	// Rows older than 90 days are exported to a CSV archive file
	// first; only then are they deleted. A failed export blocks
	// the delete, so history is never silently lost.
	if s.AuditExportDir != "" {
		if err := exportAuditArchive(ctx, s.Audit, s.AuditExportDir); err != nil {
			s.log().Error("audit cleanup archive failed; old entries kept", "error", err)
			return err
		}
	} else {
		s.log().Warn("audit cleanup has no export directory configured; old entries will be deleted without an archive")
	}
	n, err := s.Audit.CleanupOlderThan(ctx, 90)
	if err != nil {
		return err
	}
	s.log().Info("audit cleanup removed old entries", "removed", n)
	return nil
}
