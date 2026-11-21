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
	if len(b.Tracks) != 14 {
		t.Fatalf("tracks: got %d, want 14", len(b.Tracks))
	}
	lessons, questions := 0, 0
	for _, tr := range b.Tracks {
		lessons += tr.LessonCount()
		questions += len(tr.Assessment)
	}
	if lessons != 70 {
		t.Fatalf("lessons: got %d, want 70", lessons)
	}
	if questions != 84 {
		t.Fatalf("questions: got %d, want 84", questions)
	}
	if len(b.Scenarios) != 6 {
		t.Fatalf("scenarios: got %d, want 6", len(b.Scenarios))
	}
	if len(b.Achievements) != 15 {
		t.Fatalf("achievements: got %d, want 15", len(b.Achievements))
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
		if mag := s.Fault.EngineMagnitude(); mag != want.mag {
			t.Fatalf("scenario %s: magnitude=%v want %v", s.ID, mag, want.mag)
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
