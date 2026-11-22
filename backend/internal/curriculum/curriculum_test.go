package curriculum_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
)

func curriculumDir(t *testing.T) string {
	t.Helper()
	// internal/curriculum -> repo root curriculum/.
	return filepath.Join("..", "..", "..", "curriculum")
}

func TestLoadCounts(t *testing.T) {
	b, err := curriculum.Load(curriculumDir(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(b.Tracks) != 23 {
		t.Fatalf("tracks: got %d, want 23", len(b.Tracks))
	}
	lessons, questions := 0, 0
	for _, tr := range b.Tracks {
		lessons += tr.LessonCount()
		questions += len(tr.Assessment)
	}
	if lessons != 122 {
		t.Fatalf("lessons: got %d, want 122", lessons)
	}
	if questions != 142 {
		t.Fatalf("questions: got %d, want 142", questions)
	}
	if len(b.Scenarios) != 16 {
		t.Fatalf("scenarios: got %d, want 16", len(b.Scenarios))
	}
	if len(b.Achievements) != 30 {
		t.Fatalf("achievements: got %d, want 30", len(b.Achievements))
	}
	// v3 exam services: the SAA/SAP tracks gained one 3-lesson module
	// each, taking each from 9 lessons / 5 questions to 12 / 8.
	for _, want := range []struct{ id string }{{"aws-saa"}, {"aws-sap"}} {
		var tr *curriculum.Track
		for i := range b.Tracks {
			if b.Tracks[i].ID == want.id {
				tr = b.Tracks[i]
			}
		}
		if tr == nil {
			t.Fatalf("track %q missing", want.id)
		}
		if tr.LessonCount() != 12 || len(tr.Assessment) != 8 {
			t.Fatalf("track %q: %d lessons / %d questions, want 12 / 8", want.id, tr.LessonCount(), len(tr.Assessment))
		}
	}
	net := b.Tracks[0]
	for _, tr := range b.Tracks {
		if tr.ID == "networking" {
			net = tr
		}
	}
	if net.LessonCount() != 6 || net.GuidedLab.ID != "lab-networking-vpc" {
		t.Fatalf("networking track: %d lessons, guided lab %q", net.LessonCount(), net.GuidedLab.ID)
	}
	if net.Challenge.ID != "challenge-networking" {
		t.Fatalf("networking challenge id: %q", net.Challenge.ID)
	}
	// M1 alignment: the networking track must carry the same six
	// lesson topics, in the same order, as the M1 VPC Fundamentals
	// catalogue (titles compared case-insensitively by topic).
	want := []string{"cloud-networking", "vpc-cidr", "public-private-subnets", "route-tables-igw", "nat-gateways", "security-groups-nacls"}
	got := []string{}
	for _, m := range net.Modules {
		for _, l := range m.Lessons {
			got = append(got, l.Slug)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("networking lesson slugs: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("networking lesson %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestLoadMissingDir(t *testing.T) {
	if _, err := curriculum.Load("/nonexistent/curriculum"); err == nil {
		t.Fatal("Load of a missing directory should fail")
	}
	if _, err := curriculum.ResolveDir("/nonexistent/curriculum"); err == nil {
		t.Fatal("ResolveDir of a missing explicit directory should fail")
	}
}

func TestFaultMapping(t *testing.T) {
	cases := map[string]struct {
		kind int32
		mag  float64
	}{
		"route-blackhole": {5, 1},
		"latency":         {1, 5000},
		"loss":            {2, 100},
		"dns":             {4, 100},
	}
	// Per-scenario magnitudes differ from the per-kind default:
	// adv-db-replica-lag deliberately injects 8s of replica lag
	// (the 5s figure in its fix steps is the post-fix target, not
	// the injected magnitude).
	magOverrides := map[string]float64{
		"adv-db-replica-lag":           8000,
		"adv-intermittent-packet-loss": 25,
	}
	b, err := curriculum.Load(curriculumDir(t))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, s := range b.Scenarios {
		want, ok := cases[s.Fault.Kind]
		if !ok {
			t.Fatalf("scenario %s: unknown fault kind %q", s.ID, s.Fault.Kind)
		}
		kind, err := s.Fault.EngineKind()
		if err != nil || kind != want.kind {
			t.Fatalf("scenario %s: EngineKind=%v err=%v want %d", s.ID, kind, err, want.kind)
		}
		wantMag := want.mag
		if ov, ok := magOverrides[s.ID]; ok {
			wantMag = ov
		}
		if mag := s.Fault.EngineMagnitude(); mag != wantMag {
			t.Fatalf("scenario %s: magnitude=%v want %v", s.ID, mag, wantMag)
		}
	}
	if _, err := (curriculum.Fault{Kind: "explode"}).EngineKind(); err == nil {
		t.Fatal("unknown fault kind should error")
	}
}

func TestComputeStreak(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return now.AddDate(0, 0, -n) }
	if got := curriculum.ComputeStreak([]time.Time{day(0), day(1), day(2)}, now); got != 3 {
		t.Fatalf("3-day streak: got %d", got)
	}
	// No activity today: streak counts back from yesterday.
	if got := curriculum.ComputeStreak([]time.Time{day(1), day(2)}, now); got != 2 {
		t.Fatalf("streak ending yesterday: got %d", got)
	}
	// A gap breaks the streak.
	if got := curriculum.ComputeStreak([]time.Time{day(0), day(2), day(3)}, now); got != 1 {
		t.Fatalf("gapped streak: got %d", got)
	}
	if got := curriculum.ComputeStreak(nil, now); got != 0 {
		t.Fatalf("no activity: got %d", got)
	}
	// Future dates are ignored.
	if got := curriculum.ComputeStreak([]time.Time{day(-1), day(0)}, now); got != 1 {
		t.Fatalf("future date: got %d", got)
	}
}
