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

func TestLiteLimits(t *testing.T) {
	learner := LiteLimits("learner")
	if learner.MaxLabs != 1 || learner.MaxCPU != 1 || learner.MaxMemoryMB != 1024 ||
		learner.MaxDiskGB != 8 || learner.MaxLabMinutes != LiteMaxLabMinutes {
		t.Fatalf("lite learner limits: %+v", learner)
	}
	if LiteMaxLabMinutes != 30 {
		t.Fatalf("lite lab duration must be 30 minutes, got %d", LiteMaxLabMinutes)
	}
	// The Micro profile (1 vCPU / 1 GiB / 8 GiB) is the only profile
	// that fits the lite learner limits; Small (2 vCPU) must not.
	if err := learner.CheckProfile(1, 1024, 8); err != nil {
		t.Fatalf("micro profile must fit lite learner limits: %v", err)
	}
	if err := learner.CheckProfile(2, 2048, 16); err == nil {
		t.Fatal("small profile must exceed lite learner limits")
	}
	// 30-minute labs: a 30 min lab fits, a 31 min lab does not.
	if err := learner.CheckLabDuration(30); err != nil {
		t.Fatalf("30 min must fit the lite cap: %v", err)
	}
	if err := learner.CheckLabDuration(31); err == nil {
		t.Fatal("31 min must exceed the lite 30 min cap")
	}
	if got := LimitsFor("learner", true); got != learner {
		t.Fatalf("LimitsFor(lite) must return lite limits: %+v", got)
	}
	if got := LimitsFor("learner", false); got != DefaultLimits("learner") {
		t.Fatalf("LimitsFor(full) must return full defaults: %+v", got)
	}
}

func TestLimitsCapAt(t *testing.T) {
	full := DefaultLimits("learner") // 3 labs / 4 CPU / 8 GiB / 50 GB / 8 h
	capped := full.CapAt(LiteLimits("learner"))
	if capped.MaxLabs != 1 || capped.MaxCPU != 1 || capped.MaxMemoryMB != 1024 ||
		capped.MaxDiskGB != 8 || capped.DurationMinutes() != 30 {
		t.Fatalf("capping full learner limits at lite must give lite limits: %+v", capped)
	}
	// Capping must never raise a limit that is already below the cap.
	small := Limits{MaxLabs: 1, MaxCPU: 1, MaxMemoryMB: 512, MaxDiskGB: 4, MaxLabMinutes: 15}
	if got := small.CapAt(LiteLimits("learner")); got.MaxMemoryMB != 512 || got.DurationMinutes() != 15 {
		t.Fatalf("cap must not raise already-smaller limits: %+v", got)
	}
	// An unlimited duration capped at lite becomes 30 minutes.
	if got := (Limits{MaxLabHours: 0}).CapAt(LiteLimits("learner")); got.DurationMinutes() != 30 {
		t.Fatalf("unlimited duration capped at lite must be 30 min: %+v", got)
	}
}
