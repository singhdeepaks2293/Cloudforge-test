package achievements_test

import (
	"testing"

	"github.com/cloudforge/cloudforge/backend/internal/achievements"
	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
)

func defs() []*curriculum.Achievement {
	return []*curriculum.Achievement{
		{ID: "first-steps", Trigger: curriculum.Trigger{Type: "lessons_completed", Operator: ">=", Value: 1}, XP: 25},
		{ID: "first-lab", Trigger: curriculum.Trigger{Type: "labs_completed", Operator: ">=", Value: 1}, XP: 50},
		{ID: "breaker", Trigger: curriculum.Trigger{Type: "break_fix_solved", Operator: ">=", Value: 1}, XP: 100},
		{ID: "exam-ace", Trigger: curriculum.Trigger{Type: "assessment_score", Operator: ">=", Value: 100}, XP: 100},
		{ID: "streak-7", Trigger: curriculum.Trigger{Type: "streak_days", Operator: ">=", Value: 7}, XP: 150},
		{ID: "network-builder", Trigger: curriculum.Trigger{Type: "track_completed", Track: "networking"}, XP: 150},
		{ID: "full-stack", Trigger: curriculum.Trigger{Type: "tracks_completed", Difficulty: "beginner", Operator: ">=", Value: 5}, XP: 200},
		{ID: "xp-1000", Trigger: curriculum.Trigger{Type: "xp_total", Operator: ">=", Value: 1000}, XP: 100},
		{ID: "challenge-master", Trigger: curriculum.Trigger{Type: "challenges_completed", Operator: ">=", Value: 5}, XP: 200},
		{ID: "mystery", Trigger: curriculum.Trigger{Type: "teleport_completed", Operator: ">=", Value: 1}, XP: 1},
		{ID: "legacy", Trigger: curriculum.Trigger{}, XP: 10},
	}
}

func TestEvaluate(t *testing.T) {
	stats := &curriculum.Stats{
		LessonsCompleted: 1, LabsCompleted: 0, BreakFixSolved: 1,
		BestAssessmentScore: 100, StreakDays: 3, TotalXP: 1200,
		CompletedTracks:       []string{"networking"},
		CompletedByDifficulty: map[string]int{"beginner": 2},
		Earned:                map[string]bool{"first-steps": true},
	}
	got := achievements.Evaluate(defs(), stats)
	want := map[string]bool{"breaker": true, "exam-ace": true, "network-builder": true, "xp-1000": true}
	if len(got) != len(want) {
		t.Fatalf("Evaluate: got %v, want %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("Evaluate awarded unexpected %q (got %v)", id, got)
		}
	}
	// Unknown trigger types and trigger-less achievements are never
	// awarded, even with generous stats.
	for _, id := range got {
		if id == "mystery" || id == "legacy" {
			t.Fatalf("unobservable trigger %q must never be awarded", id)
		}
	}
}

func TestLockedReason(t *testing.T) {
	stats := &curriculum.Stats{Earned: map[string]bool{}}
	if r := achievements.LockedReason(&curriculum.Achievement{Trigger: curriculum.Trigger{Type: "teleport_completed"}}, stats); r == "" {
		t.Fatal("unknown trigger should have a locked reason")
	}
	if r := achievements.LockedReason(&curriculum.Achievement{Trigger: curriculum.Trigger{}}, stats); r == "" {
		t.Fatal("trigger-less achievement should have a locked reason")
	}
	if r := achievements.LockedReason(&curriculum.Achievement{Trigger: curriculum.Trigger{Type: "lessons_completed", Value: 1}}, stats); r != "trigger not yet met" {
		t.Fatalf("unmet trigger reason: %q", r)
	}
}
