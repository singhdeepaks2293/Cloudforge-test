// Package store defines the repository layer for Milestone 2.
//
// Handlers depend on the interfaces here, never on concrete stores.
// Two implementations exist:
//   - Memory* in memory.go: wraps the M1 in-memory stores; used by unit
//     tests and as the fallback when PostgreSQL is unavailable.
//   - Postgres* in postgres*.go: pgx implementations; the production path.
//
// Lab and user records reuse the auth.User / labs.Lab types so the M1
// API contract (JSON shapes, state machine) is unchanged.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/auth"
	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
	"github.com/cloudforge/cloudforge/backend/internal/labs"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrNotEnrolled  = errors.New("user is not enrolled in this course")
	ErrAlreadyDone  = errors.New("lesson already completed")
	ErrInvalidInput = errors.New("invalid input")
)

// --- Users ---------------------------------------------------------------

// UserStore is satisfied by auth.Store (memory) and PostgresUserStore.
type UserStore interface {
	Register(email, name, password string) (*auth.User, error)
	Authenticate(email, password string) (*auth.User, error)
	GetByID(id string) (*auth.User, error)
	List() ([]*auth.User, error)
	SetRole(id, role string) (*auth.User, error)
	IssueToken(userID string) (string, time.Time)
	ValidateToken(token string) (string, error)
}

// --- Courses -------------------------------------------------------------

// Lesson is one lesson inside a module. Slug is the stable
// curriculum lesson id (e.g. "cloud-networking"); Theory/KeyPoints/
// Diagram carry the full M8 lesson content.
type Lesson struct {
	ID        string   `json:"id"`
	ModuleID  string   `json:"module_id"`
	Position  int      `json:"position"`
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Slug      string   `json:"slug,omitempty"`
	Theory    string   `json:"theory,omitempty"`
	KeyPoints []string `json:"key_points,omitempty"`
	Diagram   string   `json:"diagram,omitempty"`
}

// Module groups lessons inside a course.
type Module struct {
	ID       string   `json:"id"`
	CourseID string   `json:"course_id"`
	Position int      `json:"position"`
	Title    string   `json:"title"`
	Lessons  []Lesson `json:"lessons"`
}

// Course is a course with its modules and lessons. Difficulty /
// EstimatedHours / XPReward are the M8 curriculum track metadata
// (0 XPReward means "no track-completion bonus", e.g. M1 courses).
type Course struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Track          string   `json:"track"`
	Description    string   `json:"description"`
	Difficulty     string   `json:"difficulty,omitempty"`
	EstimatedHours float64  `json:"estimated_hours,omitempty"`
	XPReward       int      `json:"xp_reward,omitempty"`
	Modules        []Module `json:"modules"`
}

// LessonTitles flattens all lesson titles in module/position order.
func (c *Course) LessonTitles() []string {
	var out []string
	for _, m := range c.Modules {
		for _, l := range m.Lessons {
			out = append(out, l.Title)
		}
	}
	return out
}

// LessonCount is the total number of lessons.
func (c *Course) LessonCount() int { return len(c.LessonTitles()) }

// FindLesson returns a lesson of this course by ID (UUID) or slug.
func (c *Course) FindLesson(id string) (Lesson, bool) {
	for _, m := range c.Modules {
		for _, l := range m.Lessons {
			if l.ID == id || (l.Slug != "" && l.Slug == id) {
				return l, true
			}
		}
	}
	return Lesson{}, false
}

// Progress is a user's progress in one course.
type Progress struct {
	CourseID           string     `json:"course_id"`
	Enrolled           bool       `json:"enrolled"`
	TotalLessons       int        `json:"total_lessons"`
	CompletedLessons   int        `json:"completed_lessons"`
	CompletedLessonIDs []string   `json:"completed_lesson_ids"`
	XP                 int        `json:"xp"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
}

// CourseStore covers courses, modules, lessons, enrollments and progress.
type CourseStore interface {
	List(ctx context.Context) ([]*Course, error)
	Get(ctx context.Context, id string) (*Course, error)
	Create(ctx context.Context, c *Course) (*Course, error)
	Update(ctx context.Context, id, title, track, description string) (*Course, error)
	Delete(ctx context.Context, id string) error
	Enroll(ctx context.Context, userID, courseID string) error
	GetProgress(ctx context.Context, userID, courseID string) (*Progress, error)
	// CompleteLesson records a lesson completion and awards XP.
	// It returns the updated progress and the XP awarded by this call
	// (0 when the lesson was already completed).
	CompleteLesson(ctx context.Context, userID, courseID, lessonID string) (*Progress, int, error)
}

// CurriculumStore is the M8 curriculum persistence surface; see
// internal/curriculum for the interface definition. Both the
// Postgres and the in-memory implementations satisfy it.
type CurriculumStore = curriculum.Store

// --- Labs ----------------------------------------------------------------

// LabStore is satisfied by the memory adapter (wrapping labs.Store) and
// by PostgresLabStore.
type LabStore interface {
	Create(ctx context.Context, ownerID, definitionID, name, idempotencyKey string) (*labs.Lab, bool, error)
	Get(ctx context.Context, id string) (*labs.Lab, error)
	ListByOwner(ctx context.Context, ownerID string) ([]*labs.Lab, error)
	Stop(ctx context.Context, id string) (*labs.Lab, error)
	Destroy(ctx context.Context, id string) (*labs.Lab, error)
	// Transition validates via labs.ValidateTransition and persists the
	// new state (and error message) atomically.
	Transition(ctx context.Context, id, to, errMsg string) (*labs.Lab, error)
	// ExpireDue returns labs past expires_at in states that may expire.
	ExpireDue(ctx context.Context) ([]*labs.Lab, error)
	// CleanupResources marks a lab's resources as cleaned.
	CleanupResources(ctx context.Context, labID string) error
	ActiveCount(ctx context.Context, ownerID string) (int, error)
	// SetMaxLabs sets the store-level lab-count ceiling (a safety
	// backstop; the API enforces the per-role limit on top).
	SetMaxLabs(n int)
	// ListAll returns every lab (admin/reconcile use); newest last.
	ListAll(ctx context.Context) ([]*labs.Lab, error)
}

// --- Quotas (Milestone 9, Part 3) ----------------------------------------

// QuotaSet is one role's configured limits (quotas table). A zero
// value means "no limit" except MaxLabs, where 0 means the built-in
// default — see quota.Limits.
type QuotaSet struct {
	Role          string    `json:"role"`
	MaxLabs       int       `json:"max_labs"`
	MaxCPU        int       `json:"max_cpu"`
	MaxMemoryMB   int       `json:"max_memory_mb"`
	MaxDiskGB     int       `json:"max_disk_gb"`
	MaxLabHours   int       `json:"max_lab_hours"`
	MaxLabMinutes int       `json:"max_lab_minutes"` // > 0 wins over MaxLabHours (migration 007; lite mode)
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// QuotaStore persists per-role quota sets. GetQuota returns
// ErrNotFound when no row is configured for the role (callers then
// use quota.DefaultLimits).
type QuotaStore interface {
	GetQuotas(ctx context.Context) ([]*QuotaSet, error)
	GetQuota(ctx context.Context, role string) (*QuotaSet, error)
	UpsertQuota(ctx context.Context, q *QuotaSet) error
}

// --- Audit ---------------------------------------------------------------

// AuditEntry is one audit log row. Metadata is stored as JSONB details.
type AuditEntry struct {
	ID           string         `json:"id"`
	ActorID      string         `json:"actor_id"`
	Action       string         `json:"action"`
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	RequestID    string         `json:"request_id"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// AuditStore records and lists audit entries.
type AuditStore interface {
	Record(ctx context.Context, e AuditEntry) error
	List(ctx context.Context, limit, offset int) ([]*AuditEntry, int, error)
	// CleanupOlderThan deletes entries older than days; returns rows removed.
	CleanupOlderThan(ctx context.Context, days int) (int64, error)
}

// --- Jobs ----------------------------------------------------------------

// Job is one durable background job.
type Job struct {
	ID             string         `json:"id"`
	Type           string         `json:"type"`
	Payload        map[string]any `json:"payload"`
	State          string         `json:"state"`
	Attempts       int            `json:"attempts"`
	MaxAttempts    int            `json:"max_attempts"`
	RunAt          time.Time      `json:"run_at"`
	LastError      string         `json:"last_error,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

// Job types handled by the worker.
const (
	JobLabProvision = "lab_provision"
	JobLabDestroy   = "lab_destroy"
	JobLabExpire    = "lab_expire"
	JobAuditCleanup = "audit_cleanup"
	// Terraform jobs (Milestone 7), driven by the worker via the
	// terraform Runner. Payload: {lab_id, run_id, action, user_id,
	// configuration?}.
	JobTerraformInit     = "terraform_init"
	JobTerraformValidate = "terraform_validate"
	JobTerraformPlan     = "terraform_plan"
	JobTerraformApply    = "terraform_apply"
	JobTerraformDestroy  = "terraform_destroy"
	// CleanupReconcile (Milestone 9, Part 3) detects orphaned lab
	// resources in both directions (DB rows with no live counterpart
	// and live engine/host-agent resources with no DB row) and, in
	// non-dry-run mode, removes the orphaned side. Payload:
	// {lab_id? (empty = all labs), dry_run? (absent = true)}.
	JobCleanupReconcile = "cleanup_reconcile"
)

// --- Lab resources (Milestone 6) ----------------------------------------

// LabResource is one provisioned resource inside a lab. Kind is the
// API-facing type (vpc|subnet|instance|gateway|loadbalancer|
// route-table|security-group); it is stored in lab_resources.
// resource_type. Config carries kind-specific detail (CIDR, routes,
// security-group rules, VM profile) as JSONB.
type LabResource struct {
	ID                 string            `json:"id"`
	LabID              string            `json:"lab_id"`
	OwnerID            string            `json:"owner_id"`
	Kind               string            `json:"kind"`
	Name               string            `json:"name"`
	State              string            `json:"state"`
	Provider           string            `json:"provider"`
	ProviderResourceID string            `json:"provider_resource_id,omitempty"`
	ParentID           string            `json:"parent_id,omitempty"`
	PrivateIP          string            `json:"private_ip,omitempty"`
	PublicIP           string            `json:"public_ip,omitempty"`
	CIDR               string            `json:"cidr,omitempty"`
	Labels             map[string]string `json:"labels,omitempty"`
	Config             map[string]any    `json:"config,omitempty"`
	CreatedAt          time.Time         `json:"created_at"`
}

// LabResourceStore persists lab resources.
type LabResourceStore interface {
	ListResources(ctx context.Context, labID string) ([]*LabResource, error)
	GetResource(ctx context.Context, labID, id string) (*LabResource, error)
	// CreateResource inserts r (ID assigned when empty) and returns it.
	CreateResource(ctx context.Context, r *LabResource) (*LabResource, error)
	// DeleteResource removes the row; returning false means the row
	// was already gone (delete is idempotent).
	DeleteResource(ctx context.Context, labID, id string) (bool, error)
}

// --- Terraform runs (Milestone 7) ---------------------------------------

// TerraformRun is one terraform action for a lab: the API row for an
// async job. PlanID/PlanJSON are set only for a succeeded plan;
// ApprovedAt/ApprovedBy record the explicit approval an apply
// requires; PlanExpiresAt bounds that approval (1h after the plan).
type TerraformRun struct {
	ID            string         `json:"id"`
	UserID        string         `json:"user_id"`
	LabID         string         `json:"lab_id"`
	Action        string         `json:"action"` // init|validate|plan|apply|destroy
	State         string         `json:"state"`  // QUEUED|RUNNING|SUCCEEDED|FAILED
	PlanID        string         `json:"plan_id,omitempty"`
	PlanJSON      map[string]any `json:"plan_json,omitempty"`
	PlanOutput    string         `json:"plan_output,omitempty"`
	ApplyOutput   string         `json:"apply_output,omitempty"`
	ApprovedAt    *time.Time     `json:"approved_at,omitempty"`
	ApprovedBy    string         `json:"approved_by,omitempty"`
	PlanExpiresAt *time.Time     `json:"plan_expires_at,omitempty"`
	JobID         string         `json:"job_id,omitempty"`
	Error         string         `json:"error,omitempty"`
	Configuration string         `json:"configuration,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// TerraformRunStore persists terraform runs.
type TerraformRunStore interface {
	CreateRun(ctx context.Context, r *TerraformRun) (*TerraformRun, error)
	GetRun(ctx context.Context, id string) (*TerraformRun, error)
	GetRunByPlanID(ctx context.Context, planID string) (*TerraformRun, error)
	ListRunsByLab(ctx context.Context, labID string, limit int) ([]*TerraformRun, error)
	UpdateRun(ctx context.Context, r *TerraformRun) (*TerraformRun, error)
}

// --- Terminal sessions (Milestone 6) ------------------------------------

// TerminalSession records one terminal token/session: CreatedAt is
// the session start, EndedAt is set when the WebSocket closes
// (nil while the session is open or the token unused), Used marks a
// single-use token as consumed, Mode is "simulated" or the
// host-agent mode the shell ran through.
type TerminalSession struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	LabID     string     `json:"lab_id"`
	TokenHash string     `json:"-"`
	ExpiresAt time.Time  `json:"expires_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Used      bool       `json:"used"`
	Mode      string     `json:"mode"`
	CreatedAt time.Time  `json:"created_at"`
}

// TerminalSessionStore persists terminal sessions.
type TerminalSessionStore interface {
	CreateSession(ctx context.Context, s *TerminalSession) (*TerminalSession, error)
	MarkSessionUsed(ctx context.Context, id string) error
	EndSession(ctx context.Context, id string, endedAt time.Time) error
	GetSession(ctx context.Context, id string) (*TerminalSession, error)
}

// JobStore is the durable job queue.
type JobStore interface {
	// Enqueue inserts a job; a duplicate idempotency key replays the
	// existing job (second return value true).
	Enqueue(ctx context.Context, typ string, payload map[string]any, idempotencyKey string) (*Job, bool, error)
	// Claim locks up to limit due jobs for workerID (FOR UPDATE SKIP LOCKED
	// in Postgres) and returns them in running state.
	Claim(ctx context.Context, workerID string, limit int) ([]*Job, error)
	Complete(ctx context.Context, id string) error
	// Fail records an error; dead=true moves the job to state 'dead',
	// otherwise it is rescheduled at retryAt.
	Fail(ctx context.Context, id, errMsg string, retryAt time.Time, dead bool) error
	Counts(ctx context.Context) (map[string]int, error)
}
