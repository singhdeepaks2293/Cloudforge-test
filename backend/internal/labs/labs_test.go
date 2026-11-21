package labs

import (
	"errors"
	"testing"
	"time"
)

func TestValidTransitions(t *testing.T) {
	legal := [][2]string{
		{StateCreated, StateProvisioning},
		{StateCreated, StateFailed},
		{StateProvisioning, StateReady},
		{StateProvisioning, StateFailed},
		{StateReady, StateRunning},
		{StateReady, StateStopping},
		{StateRunning, StateValidating},
		{StateRunning, StateStopping},
		{StateValidating, StateCompleted},
		{StateValidating, StateFailed},
		{StateValidating, StateRunning},
		{StateCompleted, StateDestroying},
		{StateStopping, StateStopped},
		{StateStopped, StateProvisioning},
		{StateStopped, StateDestroying},
		{StateFailed, StateProvisioning},
		{StateFailed, StateDestroying},
		{StateExpired, StateDestroying},
		{StateDestroying, StateDestroyed},
	}
	for _, tc := range legal {
		if err := ValidateTransition(tc[0], tc[1]); err != nil {
			t.Errorf("transition %s -> %s should be legal: %v", tc[0], tc[1], err)
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	illegal := [][2]string{
		{StateCreated, StateReady},        // must provision first
		{StateCreated, StateCompleted},    // cannot skip the lifecycle
		{StateDestroyed, StateRunning},    // terminal state
		{StateDestroyed, StateCreated},    // terminal state
		{StateReady, StateCompleted},      // must validate first
		{StateReady, StateDestroyed},      // must go through DESTROYING
		{StateStopped, StateReady},        // restart goes via PROVISIONING
		{StateCompleted, StateRunning},    // completed labs do not resume
		{StateFailed, StateReady},         // retry goes via PROVISIONING
		{StateProvisioning, StateStopped}, // must stop via STOPPING
	}
	for _, tc := range illegal {
		if err := ValidateTransition(tc[0], tc[1]); !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("transition %s -> %s should be illegal, got %v", tc[0], tc[1], err)
		}
	}
}

func waitState(t *testing.T, s *Store, id, want string) *Lab {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		l, err := s.Get(id)
		if err != nil {
			t.Fatalf("Get(%s): %v", id, err)
		}
		if l.State == want {
			return l
		}
		time.Sleep(10 * time.Millisecond)
	}
	l, _ := s.Get(id)
	t.Fatalf("lab %s did not reach %s (last state %s, error %q)", id, want, l.State, l.Error)
	return nil
}

func TestProvisioningSucceedsWhenEngineReachable(t *testing.T) {
	s := NewStore()
	s.ProvisionDelay = 5 * time.Millisecond
	s.CheckNetwork = func() bool { return true }
	l, replayed, err := s.Create("user-1", "vpc-fundamentals", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if replayed {
		t.Fatal("first create must not be a replay")
	}
	if l.State != StateCreated {
		t.Fatalf("new lab should be CREATED, got %s", l.State)
	}
	got := waitState(t, s, l.ID, StateReady)
	if got.Error != "" {
		t.Fatalf("ready lab should have no error, got %q", got.Error)
	}
}

func TestProvisioningFailsHonestlyWhenEngineDown(t *testing.T) {
	s := NewStore()
	s.ProvisionDelay = 5 * time.Millisecond
	s.CheckNetwork = func() bool { return false }
	l, _, err := s.Create("user-1", "vpc-fundamentals", "", "")
	if err != nil {
		t.Fatal(err)
	}
	got := waitState(t, s, l.ID, StateFailed)
	if got.Error == "" {
		t.Fatal("failed lab must record an error explaining why")
	}
}

func TestProvisioningFailsWhenCheckerUnset(t *testing.T) {
	s := NewStore() // CheckNetwork nil: must fail, never fake success
	s.ProvisionDelay = 5 * time.Millisecond
	l, _, err := s.Create("user-1", "vpc-fundamentals", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, s, l.ID, StateFailed)
}

func TestCreateValidationAndQuota(t *testing.T) {
	s := NewStore()
	s.ProvisionDelay = time.Hour // keep labs in CREATED/PROVISIONING so they count as active
	s.CheckNetwork = func() bool { return true }
	if _, _, err := s.Create("user-1", "no-such-definition", "", ""); !errors.Is(err, ErrUnknownDefinition) {
		t.Fatalf("unknown definition: got %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, _, err := s.Create("user-1", "ec2-basics", "", ""); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if _, _, err := s.Create("user-1", "ec2-basics", "", ""); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("4th active lab: got %v, want ErrQuotaExceeded", err)
	}
	// Another user is unaffected.
	if _, _, err := s.Create("user-2", "ec2-basics", "", ""); err != nil {
		t.Fatalf("other user's create: %v", err)
	}
}

func TestIdempotencyReplay(t *testing.T) {
	s := NewStore()
	s.ProvisionDelay = 5 * time.Millisecond
	s.CheckNetwork = func() bool { return true }
	first, replayed, err := s.Create("user-1", "ec2-basics", "my lab", "key-123")
	if err != nil {
		t.Fatal(err)
	}
	if replayed {
		t.Fatal("first create must not be a replay")
	}
	second, replayed, err := s.Create("user-1", "ec2-basics", "different name", "key-123")
	if err != nil {
		t.Fatal(err)
	}
	if !replayed {
		t.Fatal("duplicate idempotency key must replay the original result")
	}
	if second.ID != first.ID || second.Name != "my lab" {
		t.Fatalf("replay returned a different lab: %+v vs %+v", second, first)
	}
	if n := s.ActiveCount("user-1"); n != 1 {
		t.Fatalf("idempotent replay must not create a second lab (active=%d)", n)
	}
}

func TestStopAndDestroy(t *testing.T) {
	s := NewStore()
	s.ProvisionDelay = 5 * time.Millisecond
	s.CheckNetwork = func() bool { return true }
	l, _, err := s.Create("user-1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	waitState(t, s, l.ID, StateReady)
	stopped, err := s.Stop(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != StateStopping {
		t.Fatalf("Stop should return STOPPING, got %s", stopped.State)
	}
	waitState(t, s, l.ID, StateStopped)
	destroyed, err := s.Destroy(l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if destroyed.State != StateDestroying {
		t.Fatalf("Destroy should return DESTROYING, got %s", destroyed.State)
	}
	waitState(t, s, l.ID, StateDestroyed)
}

func TestStopInvalidState(t *testing.T) {
	s := NewStore()
	s.ProvisionDelay = time.Hour
	s.CheckNetwork = func() bool { return true }
	l, _, err := s.Create("user-1", "ec2-basics", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// Lab is still CREATED: stopping it is an illegal transition.
	if _, err := s.Stop(l.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("Stop from CREATED: got %v, want ErrInvalidTransition", err)
	}
	if _, err := s.Get("missing"); !errors.Is(err, ErrLabNotFound) {
		t.Fatalf("Get missing: got %v, want ErrLabNotFound", err)
	}
}
