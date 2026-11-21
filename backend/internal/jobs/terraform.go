// Terraform job handling (Milestone 7).
//
// The API enqueues terraform_init / validate / plan / apply / destroy
// jobs; this file executes them in the worker via the configured
// terraform Runner (ContainerRunner in production, TestStubRunner in
// tests). A terraform failure is a terminal run outcome (the run row
// is marked FAILED and the job completes) — except a Docker
// Unavailable error, which is retryable like any other transient
// backend outage. An apply syncs the approved plan's resources back
// into lab_resources; a destroy removes the emulator-managed
// resources it created.
package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/store"
	"github.com/cloudforge/cloudforge/backend/internal/terraform"
)

// TerraformConfig wires the terraform worker path.
type TerraformConfig struct {
	EmulatorEndpoint string
	MinIOEndpoint    string
	WorkdirRoot      string // per-lab workdirs: <root>/<lab-id>/terraform
}

func payloadString(j *store.Job, key string) string {
	if s, ok := j.Payload[key].(string); ok {
		return s
	}
	return ""
}

// handleTerraform executes one terraform job.
func (s *Service) handleTerraform(ctx context.Context, j *store.Job) error {
	if s.TerraformRuns == nil {
		return fmt.Errorf("terraform run store is not configured")
	}
	runID := payloadString(j, "run_id")
	labID, err := labIDOf(j)
	if err != nil {
		return err
	}
	run, err := s.TerraformRuns.GetRun(ctx, runID)
	if err != nil {
		return fmt.Errorf("terraform run %s not found: %w", runID, err)
	}
	fail := func(msg string) error {
		run.State = "FAILED"
		run.Error = msg
		if _, uerr := s.TerraformRuns.UpdateRun(ctx, run); uerr != nil {
			s.log().Error("could not record terraform failure", "run_id", runID, "error", uerr)
		}
		return nil // terminal outcome: the job completes
	}
	if s.TerraformRunner == nil {
		return fail("terraform runner is not configured")
	}
	if err := terraform.ValidateProviderConfig(s.terraformConfig().EmulatorEndpoint, s.terraformConfig().MinIOEndpoint); err != nil {
		return fail("provider config rejected: " + err.Error())
	}
	workdir := filepath.Join(s.terraformConfig().WorkdirRoot, labID, "terraform")
	if err := terraform.WriteProviderConfig(workdir, s.terraformConfig().EmulatorEndpoint, s.terraformConfig().MinIOEndpoint); err != nil {
		return fail("write provider config: " + err.Error())
	}
	configuration := payloadString(j, "configuration")
	if configuration == "" {
		configuration = run.Configuration
	}
	if configuration != "" {
		if err := terraform.ValidateConfiguration(configuration); err != nil {
			return fail("configuration rejected: " + err.Error())
		}
		if err := os.WriteFile(filepath.Join(workdir, "main.tf"), []byte(configuration), 0o640); err != nil {
			return fail("write main.tf: " + err.Error())
		}
	}
	run.State = "RUNNING"
	if _, err := s.TerraformRuns.UpdateRun(ctx, run); err != nil {
		return err
	}
	op := terraform.Operation(run.Action)
	res, err := s.TerraformRunner.Run(ctx, terraform.RunRequest{
		Operation:        op,
		Workdir:          workdir,
		Configuration:    configuration,
		EmulatorEndpoint: s.terraformConfig().EmulatorEndpoint,
		MinIOEndpoint:    s.terraformConfig().MinIOEndpoint,
	})
	if err != nil {
		if terraform.IsUnavailable(err) {
			return err // retryable: Docker may come back
		}
		return fail(fmt.Sprintf("terraform %s failed: %v", run.Action, err))
	}
	if !res.Success {
		return fail(fmt.Sprintf("terraform %s exited non-zero: %s", run.Action, res.Output))
	}
	run.State = "SUCCEEDED"
	run.Error = ""
	switch op {
	case terraform.OpInit, terraform.OpValidate:
		run.PlanOutput = res.Output
	case terraform.OpPlan:
		run.PlanOutput = res.Output
		if res.Plan != nil {
			run.PlanID = ids.New()
			run.PlanJSON = res.PlanJSON
			exp := time.Now().UTC().Add(terraform.PlanTTL)
			run.PlanExpiresAt = &exp
		}
	case terraform.OpApply:
		run.ApplyOutput = res.Output
		if _, err := s.TerraformRuns.UpdateRun(ctx, run); err != nil {
			return err
		}
		return s.syncApplyResources(ctx, run, payloadString(j, "plan_id"))
	case terraform.OpDestroy:
		run.ApplyOutput = res.Output
		if _, err := s.TerraformRuns.UpdateRun(ctx, run); err != nil {
			return err
		}
		return s.removeEmulatorResources(ctx, labID)
	}
	if _, err := s.TerraformRuns.UpdateRun(ctx, run); err != nil {
		return err
	}
	return nil
}

func (s *Service) terraformConfig() TerraformConfig {
	return s.TerraformCfg
}

// syncApplyResources creates lab_resources rows for the resources in
// the approved plan the apply just realised. The plan is the one the
// apply was approved against (planID from the job payload); if that
// lookup fails, the most recent succeeded plan for the lab is used.
func (s *Service) syncApplyResources(ctx context.Context, run *store.TerraformRun, planID string) error {
	if s.Resources == nil || s.TerraformRuns == nil {
		return nil
	}
	var planRun *store.TerraformRun
	if planID != "" {
		if r, err := s.TerraformRuns.GetRunByPlanID(ctx, planID); err == nil {
			planRun = r
		}
	}
	if planRun == nil {
		runs, err := s.TerraformRuns.ListRunsByLab(ctx, run.LabID, 20)
		if err != nil {
			return err
		}
		for _, r := range runs {
			if r.Action == "plan" && r.State == "SUCCEEDED" && r.PlanID != "" && len(r.PlanJSON) > 0 {
				planRun = r
				break
			}
		}
	}
	if planRun == nil {
		s.log().Warn("apply succeeded but no succeeded plan found to sync resources from", "lab_id", run.LabID, "run_id", run.ID)
		return nil
	}
	raw, err := json.Marshal(planRun.PlanJSON)
	if err != nil {
		return fmt.Errorf("re-encode stored plan JSON: %w", err)
	}
	plan, _, err := terraform.ParsePlanJSON(raw)
	if err != nil {
		return fmt.Errorf("re-parse stored plan JSON: %w", err)
	}
	for _, pr := range plan.Resources {
		kind, ok := terraformTypeToKind(pr.Type)
		if !ok {
			continue
		}
		state := "available"
		if kind == "instance" {
			state = "running"
		}
		cfg := map[string]any{"terraform_address": pr.Address, "terraform_type": pr.Type}
		if kind == "gateway" && strings.Contains(pr.Type, "nat") {
			cfg["gateway_type"] = "nat"
		}
		if _, err := s.Resources.CreateResource(ctx, &store.LabResource{
			LabID: run.LabID, OwnerID: run.UserID, Kind: kind, Name: pr.Name,
			State: state, Provider: "aws-emulator", ProviderResourceID: pr.Address,
			Config: cfg,
		}); err != nil {
			s.log().Warn("could not sync terraform resource", "address", pr.Address, "error", err)
		}
	}
	return nil
}

func terraformTypeToKind(tfType string) (string, bool) {
	switch tfType {
	case "aws_vpc":
		return "vpc", true
	case "aws_subnet":
		return "subnet", true
	case "aws_instance":
		return "instance", true
	case "aws_internet_gateway", "aws_nat_gateway":
		return "gateway", true
	case "aws_lb", "aws_alb":
		return "loadbalancer", true
	case "aws_route_table":
		return "route-table", true
	case "aws_security_group":
		return "security-group", true
	default:
		return "", false
	}
}

// removeEmulatorResources deletes the aws-emulator-managed resource
// rows for a lab after a successful destroy.
func (s *Service) removeEmulatorResources(ctx context.Context, labID string) error {
	if s.Resources == nil {
		return nil
	}
	list, err := s.Resources.ListResources(ctx, labID)
	if err != nil {
		return err
	}
	for _, r := range list {
		if r.Provider == "aws-emulator" {
			if _, err := s.Resources.DeleteResource(ctx, labID, r.ID); err != nil {
				s.log().Warn("could not remove destroyed terraform resource", "resource_id", r.ID, "error", err)
			}
		}
	}
	return nil
}
