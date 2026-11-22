// Package labs implements the lab lifecycle state machine, the in-memory
// lab store, idempotency handling and asynchronous provisioning.
//
// M1 runs in-memory; M2 persists lab_sessions / lab_resources to
// PostgreSQL (migrations/001_init.sql) and drives provisioning through
// the Rust network engine, Docker and libvirt instead of the simulated
// steps used here. Provisioning is NOT faked as success: if the network
// engine is unreachable the lab transitions to FAILED with the error
// recorded on the lab.
package labs

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/ids"
	"github.com/cloudforge/cloudforge/backend/internal/quota"
)

// Lab states.
const (
	StateCreated      = "CREATED"
	StateProvisioning = "PROVISIONING"
	StateReady        = "READY"
	StateRunning      = "RUNNING"
	StateValidating   = "VALIDATING"
	StateCompleted    = "COMPLETED"
	StateStopping     = "STOPPING"
	StateStopped      = "STOPPED"
	StateFailed       = "FAILED"
	StateExpired      = "EXPIRED"
	StateDestroying   = "DESTROYING"
	StateDestroyed    = "DESTROYED"
)

var (
	ErrInvalidTransition = errors.New("invalid lab state transition")
	ErrLabNotFound       = errors.New("lab not found")
	ErrUnknownDefinition = errors.New("unknown lab definition")
	ErrQuotaExceeded     = quota.ErrQuotaExceeded
)

// transitions lists every legal state transition.
var transitions = map[string][]string{
	StateCreated:      {StateProvisioning, StateFailed, StateExpired, StateDestroying},
	StateProvisioning: {StateReady, StateFailed, StateExpired, StateDestroying},
	StateReady:        {StateRunning, StateStopping, StateExpired, StateDestroying},
	StateRunning:      {StateValidating, StateStopping, StateExpired, StateDestroying},
	StateValidating:   {StateCompleted, StateFailed, StateRunning},
	StateCompleted:    {StateDestroying},
	StateStopping:     {StateStopped, StateFailed, StateDestroying},
	StateStopped:      {StateProvisioning, StateExpired, StateDestroying},
	StateFailed:       {StateProvisioning, StateDestroying},
	StateExpired:      {StateDestroying},
	StateDestroying:   {StateDestroyed, StateFailed},
	StateDestroyed:    {},
}

// CanTransition reports whether from -> to is a legal transition.
func CanTransition(from, to string) bool {
	for _, t := range transitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// ValidateTransition returns nil for a legal transition, otherwise an
// error naming both states.
func ValidateTransition(from, to string) error {
	if CanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
}

// Definition is a lab a user can start.
type Definition struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	CourseID    string `json:"course_id"`
	Description string `json:"description"`
	DurationMin int    `json:"duration_minutes"`
}

// Definitions are the labs available in M1.
var Definitions = []Definition{
	{ID: "vpc-fundamentals", Title: "Build a VPC with public and private subnets", CourseID: "vpc-fundamentals", Description: "Create an isolated VPC, two subnets, route tables and a NAT gateway.", DurationMin: 60},
	{ID: "iam-security", Title: "Fix a broken IAM policy", CourseID: "iam-security", Description: "Diagnose an access-denied error and apply least privilege.", DurationMin: 30},
	{ID: "ec2-basics", Title: "Launch a Linux web server", CourseID: "ec2-basics", Description: "Launch a KVM-backed Linux instance and serve a web page.", DurationMin: 30},
	{ID: "three-tier-app", Title: "Build a three-tier application", CourseID: "vpc-fundamentals", Description: "Web, application and database tiers across public and private subnets.", DurationMin: 120},
	{ID: "saa-ha-web", Title: "Build a highly available web tier", CourseID: "aws-saa", Description: "Two Availability Zones, public and private subnets, resilient egress and a healthy load balancer.", DurationMin: 30},
	{ID: "sap-hybrid-network", Title: "Build a segmented hybrid network", CourseID: "aws-sap", Description: "Segmented VPC, resilient NAT egress, private application tier and a healthy load balancer.", DurationMin: 30},
}

// GetDefinition returns a definition by ID.
func GetDefinition(id string) (Definition, bool) {
	for _, d := range Definitions {
		if d.ID == id {
			return d, true
		}
	}
	return Definition{}, false
}

// Lab is one user's lab session.
type Lab struct {
	ID             string    `json:"id"`
	OwnerID        string    `json:"owner_id"`
	DefinitionID   string    `json:"definition_id"`
	Name           string    `json:"name"`
	State          string    `json:"state"`
	Error          string    `json:"error,omitempty"`
	IdempotencyKey string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	// ValidationRules (M8) is the lab definition's validation rule
	// set, filled in by the API when the lab was created from a
	// curriculum lab definition. Omitted otherwise, so existing
	// clients are unaffected. Generic maps (not validation.Rule)
	// to avoid an import cycle: validation depends on the store,
	// which depends on this package.
	ValidationRules []map[string]any `json:"validation_rules,omitempty"`
	// ScenarioID / FaultStatus (M8) describe an active break-and-fix
	// attempt on this lab, filled in by the API when one exists.
	ScenarioID  string `json:"scenario_id,omitempty"`
	FaultStatus string `json:"fault_status,omitempty"`
}

// Store is a mutex-guarded in-memory lab store with an idempotency index.
type Store struct {
	mu sync.Mutex
	// CheckNetwork is called during provisioning to verify the Rust
	// network engine is reachable. Nil means "unreachable" — provisioning
	// must fail rather than pretend to succeed.
	CheckNetwork func() bool
	// ProvisionDelay pauses between simulated provisioning steps; it
	// exists so tests can observe PROVISIONING and so the async flow is
	// real (state changes happen over time, not instantly).
	ProvisionDelay time.Duration

	labs    map[string]*Lab
	byKey   map[string]string // idempotency key -> lab ID
	order   []string
	maxLabs int
	now     func() time.Time
}

// NewStore returns a Store with the default quota limit.
func NewStore() *Store {
	return &Store{
		labs:           make(map[string]*Lab),
		byKey:          make(map[string]string),
		ProvisionDelay: 100 * time.Millisecond,
		maxLabs:        quota.DefaultMaxActiveLabs,
		now:            time.Now,
	}
}

// SetMaxLabs raises or lowers the store-level lab-count ceiling.
// This is a safety backstop, not the per-role limit: the API layer
// enforces the configured per-role limit (quota.Limits) before
// calling Create, and the ceiling must be at least the highest role
// limit or it would veto legitimate admin labs. Startup wires it
// from the quota store (see cmd/api); tests use the default (3).
func (s *Store) SetMaxLabs(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n > 0 {
		s.maxLabs = n
	}
}

func newID() string { return ids.New() }

func (s *Store) transitionLocked(l *Lab, to string) error {
	if err := ValidateTransition(l.State, to); err != nil {
		return err
	}
	l.State = to
	l.UpdatedAt = s.now().UTC()
	return nil
}

// Create validates the definition and quota, honours idempotency keys,
// creates the lab in CREATED and starts async provisioning.
// The second return value is true when an existing lab was replayed.
func (s *Store) Create(ownerID, definitionID, name, idempotencyKey string) (*Lab, bool, error) {
	def, ok := GetDefinition(definitionID)
	if !ok {
		return nil, false, ErrUnknownDefinition
	}
	if name == "" {
		name = def.Title
	}
	s.mu.Lock()
	if idempotencyKey != "" {
		if id, exists := s.byKey[idempotencyKey]; exists {
			if l, ok := s.labs[id]; ok {
				cp := *l
				s.mu.Unlock()
				return &cp, true, nil
			}
		}
	}
	active := 0
	for _, l := range s.labs {
		if l.OwnerID == ownerID && quota.IsActive(l.State) {
			active++
		}
	}
	if err := quota.Check(active, s.maxLabs); err != nil {
		s.mu.Unlock()
		return nil, false, err
	}
	now := s.now().UTC()
	l := &Lab{
		ID:             newID(),
		OwnerID:        ownerID,
		DefinitionID:   definitionID,
		Name:           name,
		State:          StateCreated,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
		ExpiresAt:      now.Add(time.Duration(def.DurationMin) * time.Minute),
	}
	s.labs[l.ID] = l
	s.order = append(s.order, l.ID)
	if idempotencyKey != "" {
		s.byKey[idempotencyKey] = l.ID
	}
	cp := *l
	s.mu.Unlock()

	go s.provision(l.ID)
	return &cp, false, nil
}

// provision runs the async provisioning steps. It is deliberately not
// instant: CREATED -> PROVISIONING -> READY, and FAILED (with the error
// recorded) when the network engine cannot be reached.
func (s *Store) provision(id string) {
	time.Sleep(s.ProvisionDelay)
	s.mu.Lock()
	l, ok := s.labs[id]
	if !ok || l.State != StateCreated {
		s.mu.Unlock()
		return
	}
	if err := s.transitionLocked(l, StateProvisioning); err != nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	// Step 1: network engine must be reachable, otherwise fail honestly.
	time.Sleep(s.ProvisionDelay)
	reachable := false
	if s.CheckNetwork != nil {
		reachable = s.CheckNetwork()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok = s.labs[id]
	if !ok || l.State != StateProvisioning {
		return // stopped / destroyed while provisioning
	}
	if !reachable {
		l.Error = "network engine unreachable: cannot provision isolated network"
		_ = s.transitionLocked(l, StateFailed)
		return
	}
	// Steps 2+: in M1 the remaining steps (network, VM/container
	// creation, cloud-init, health checks) are simulated by the delay
	// above; M2 performs them against the Rust engine / host agent.
	_ = s.transitionLocked(l, StateReady)
}

// Get returns a copy of a lab by ID.
func (s *Store) Get(id string) (*Lab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.labs[id]
	if !ok {
		return nil, ErrLabNotFound
	}
	cp := *l
	return &cp, nil
}

// ListByOwner returns the caller's labs, newest first.
func (s *Store) ListByOwner(ownerID string) []*Lab {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Lab
	for i := len(s.order) - 1; i >= 0; i-- {
		if l := s.labs[s.order[i]]; l.OwnerID == ownerID {
			cp := *l
			out = append(out, &cp)
		}
	}
	return out
}

// ListAll returns every lab, oldest first (admin/reconcile use).
func (s *Store) ListAll() []*Lab {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Lab, 0, len(s.order))
	for _, id := range s.order {
		cp := *s.labs[id]
		out = append(out, &cp)
	}
	return out
}

// ActiveCount returns the number of active labs for an owner.
func (s *Store) ActiveCount(ownerID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.labs {
		if l.OwnerID == ownerID && quota.IsActive(l.State) {
			n++
		}
	}
	return n
}

// Stop moves a READY/RUNNING lab to STOPPING and then STOPPED.
func (s *Store) Stop(id string) (*Lab, error) {
	s.mu.Lock()
	l, ok := s.labs[id]
	if !ok {
		s.mu.Unlock()
		return nil, ErrLabNotFound
	}
	if err := s.transitionLocked(l, StateStopping); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cp := *l
	s.mu.Unlock()
	go func() {
		time.Sleep(s.ProvisionDelay)
		s.mu.Lock()
		defer s.mu.Unlock()
		if l, ok := s.labs[id]; ok && l.State == StateStopping {
			_ = s.transitionLocked(l, StateStopped)
		}
	}()
	return &cp, nil
}

// Destroy moves a lab to DESTROYING and then DESTROYED (cleanup).
func (s *Store) Destroy(id string) (*Lab, error) {
	s.mu.Lock()
	l, ok := s.labs[id]
	if !ok {
		s.mu.Unlock()
		return nil, ErrLabNotFound
	}
	if err := s.transitionLocked(l, StateDestroying); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cp := *l
	s.mu.Unlock()
	go func() {
		// M2: delete network, VMs, containers and emulator resources here,
		// retrying failed cleanup; M1 has no real resources yet, so the
		// transition itself is the cleanup record.
		time.Sleep(s.ProvisionDelay)
		s.mu.Lock()
		defer s.mu.Unlock()
		if l, ok := s.labs[id]; ok && l.State == StateDestroying {
			_ = s.transitionLocked(l, StateDestroyed)
		}
	}()
	return &cp, nil
}

// Transition validates and applies a state change directly (no async
// side effects). It exists so the worker service and tests can drive a
// lab through the state machine without going through Stop/Destroy;
// the async provisioning goroutines still own their own transitions.
func (s *Store) Transition(id, to, errMsg string) (*Lab, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.labs[id]
	if !ok {
		return nil, ErrLabNotFound
	}
	if errMsg != "" {
		l.Error = errMsg
	} else if to != StateFailed {
		l.Error = ""
	}
	if err := s.transitionLocked(l, to); err != nil {
		return nil, err
	}
	cp := *l
	return &cp, nil
}

// ExpireDue returns labs past their expiry in states that may expire.
func (s *Store) ExpireDue() []*Lab {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var out []*Lab
	for _, l := range s.labs {
		if !l.ExpiresAt.After(now) {
			switch l.State {
			case StateCreated, StateProvisioning, StateReady, StateRunning, StateStopped:
				cp := *l
				out = append(out, &cp)
			}
		}
	}
	return out
}

// Retry re-provisions a FAILED lab (FAILED -> PROVISIONING -> READY/FAILED).
func (s *Store) Retry(id string) (*Lab, error) {
	s.mu.Lock()
	l, ok := s.labs[id]
	if !ok {
		s.mu.Unlock()
		return nil, ErrLabNotFound
	}
	l.Error = ""
	if err := s.transitionLocked(l, StateProvisioning); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	cp := *l
	s.mu.Unlock()
	go s.provisionFromProvisioning(id)
	return &cp, nil
}

func (s *Store) provisionFromProvisioning(id string) {
	time.Sleep(s.ProvisionDelay)
	reachable := false
	if s.CheckNetwork != nil {
		reachable = s.CheckNetwork()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.labs[id]
	if !ok || l.State != StateProvisioning {
		return
	}
	if !reachable {
		l.Error = "network engine unreachable: cannot provision isolated network"
		_ = s.transitionLocked(l, StateFailed)
		return
	}
	_ = s.transitionLocked(l, StateReady)
}
