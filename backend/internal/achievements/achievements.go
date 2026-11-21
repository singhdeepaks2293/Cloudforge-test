// Package achievements evaluates curriculum achievement triggers
// against a user's Stats. Only triggers the control plane can
// observe are evaluated; a trigger type this package does not know
// stays locked (Evaluate never awards it). Awarding itself (the
// user_achievements row + the XP transaction) is the store's job.
package achievements

import (
	"github.com/cloudforge/cloudforge/backend/internal/curriculum"
)

// compare applies the trigger operator (default ">=").
func compare(op string, got, want int) bool {
	switch op {
	case ">=":
		return got >= want
	case ">":
		return got > want
	case "<=", "":
		if op == "" {
			return got >= want
		}
		return got <= want
	case "<":
		return got < want
	case "==", "=":
		return got == want
	default:
		return got >= want
	}
}

// TriggerMet reports whether one achievement's trigger is satisfied
// by the stats. It returns false for unknown trigger types and for
// triggers with no observable source — those stay locked.
func TriggerMet(a *curriculum.Achievement, s *curriculum.Stats) bool {
	if a == nil || s == nil {
		return false
	}
	t := a.Trigger
	switch t.Type {
	case "lessons_completed":
		return compare(t.Operator, s.LessonsCompleted, t.Value)
	case "labs_completed":
		return compare(t.Operator, s.LabsCompleted, t.Value)
	case "challenges_completed":
		return compare(t.Operator, s.ChallengesCompleted, t.Value)
	case "break_fix_solved":
		return compare(t.Operator, s.BreakFixSolved, t.Value)
	case "track_completed":
		return s.HasCompletedTrack(t.Track)
	case "tracks_completed":
		n := 0
		if t.Difficulty == "" {
			n = len(s.CompletedTracks)
		} else if s.CompletedByDifficulty != nil {
			n = s.CompletedByDifficulty[t.Difficulty]
		}
		return compare(t.Operator, n, t.Value)
	case "assessment_score":
		return compare(t.Operator, s.BestAssessmentScore, t.Value)
	case "xp_total":
		return compare(t.Operator, s.TotalXP, t.Value)
	case "streak_days":
		return compare(t.Operator, s.StreakDays, t.Value)
	default:
		return false
	}
}

// Evaluate returns the ids of achievements whose trigger is met and
// which the user has not already earned. defs is the full seeded
// list; stats.Earned marks already-awarded ids.
func Evaluate(defs []*curriculum.Achievement, s *curriculum.Stats) []string {
	var out []string
	for _, a := range defs {
		if s.Earned != nil && s.Earned[a.ID] {
			continue
		}
		if TriggerMet(a, s) {
			out = append(out, a.ID)
		}
	}
	return out
}

// LockedReason explains why an achievement is still locked: either
// its trigger is not (yet) met, or the trigger cannot be observed
// by this evaluator at all.
func LockedReason(a *curriculum.Achievement, s *curriculum.Stats) string {
	if a == nil {
		return "unknown achievement"
	}
	switch a.Trigger.Type {
	case "lessons_completed", "labs_completed", "challenges_completed",
		"break_fix_solved", "track_completed", "tracks_completed",
		"assessment_score", "xp_total", "streak_days":
		return "trigger not yet met"
	default:
		if a.Trigger.Type == "" {
			return "no trigger defined for this achievement; it is never auto-awarded"
		}
		return "trigger type " + a.Trigger.Type + " is not observable by the control plane; it is never auto-awarded"
	}
}
