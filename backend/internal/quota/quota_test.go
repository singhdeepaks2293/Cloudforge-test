package quota

import "testing"

func TestCheck(t *testing.T) {
	if err := Check(0, DefaultMaxActiveLabs); err != nil {
		t.Fatalf("0 active should pass: %v", err)
	}
	if err := Check(2, DefaultMaxActiveLabs); err != nil {
		t.Fatalf("2 active should pass: %v", err)
	}
	if err := Check(3, DefaultMaxActiveLabs); err != ErrQuotaExceeded {
		t.Fatalf("3 active at limit 3: got %v, want ErrQuotaExceeded", err)
	}
	if err := Check(5, DefaultMaxActiveLabs); err != ErrQuotaExceeded {
		t.Fatalf("5 active: got %v, want ErrQuotaExceeded", err)
	}
}

func TestIsActive(t *testing.T) {
	active := []string{"CREATED", "PROVISIONING", "READY", "RUNNING", "VALIDATING", "STOPPING"}
	for _, s := range active {
		if !IsActive(s) {
			t.Errorf("%s should count as active", s)
		}
	}
	inactive := []string{"COMPLETED", "STOPPED", "FAILED", "EXPIRED", "DESTROYING", "DESTROYED"}
	for _, s := range inactive {
		if IsActive(s) {
			t.Errorf("%s should not count as active", s)
		}
	}
}

func TestDefaultLimits(t *testing.T) {
	if l := DefaultLimits("learner"); l.MaxLabs != 3 || l.MaxCPU != 4 || l.MaxLabHours != 8 {
		t.Fatalf("learner defaults: %+v", l)
	}
	if l := DefaultLimits("admin"); l.MaxLabs != 10 || l.MaxCPU != 16 || l.MaxLabHours != 24 {
		t.Fatalf("admin defaults: %+v", l)
	}
	if l := DefaultLimits("nobody"); l.MaxLabs != DefaultMaxActiveLabs {
		t.Fatalf("unknown role must fall back to learner defaults: %+v", l)
	}
	if l := (Limits{MaxLabs: 0}).MaxLabsOrDefault(); l != DefaultMaxActiveLabs {
		t.Fatalf("MaxLabs 0 must mean the built-in default, got %d", l)
	}
}

func TestLimitsCheckProfile(t *testing.T) {
	learner := DefaultLimits("learner")
	if err := learner.CheckProfile(1, 1024, 8); err != nil {
		t.Fatalf("micro profile must fit the learner limits: %v", err)
	}
	if err := learner.CheckProfile(8, 1024, 8); err == nil {
		t.Fatal("8 vCPU must exceed the learner 4 vCPU limit")
	}
	if err := learner.CheckProfile(1, 16384, 8); err == nil {
		t.Fatal("16 GiB must exceed the learner 8 GiB limit")
	}
	if err := learner.CheckProfile(1, 1024, 100); err == nil {
		t.Fatal("100 GiB disk must exceed the learner 50 GiB limit")
	}
	unlimited := Limits{MaxCPU: 0, MaxMemoryMB: 0, MaxDiskGB: 0}
	if err := unlimited.CheckProfile(64, 1<<20, 1000); err != nil {
		t.Fatalf("zero limits must mean unlimited: %v", err)
	}
}

func TestLimitsCheckLabDuration(t *testing.T) {
	learner := DefaultLimits("learner")
	if err := learner.CheckLabDuration(60); err != nil {
		t.Fatalf("60 min must fit the 8 h learner cap: %v", err)
	}
	if err := learner.CheckLabDuration(481); err == nil {
		t.Fatal("481 min must exceed the 8 h learner cap")
	}
	if err := (Limits{MaxLabHours: 0}).CheckLabDuration(100000); err != nil {
		t.Fatalf("zero MaxLabHours must mean unlimited: %v", err)
	}
}
