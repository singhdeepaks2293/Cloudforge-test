package advanced

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GeneratePortfolio builds the portfolio artefact for one completed
// Advanced project: an architecture summary plus deployment
// evidence, as Markdown. It returns ErrProjectNotCompleted when the
// user has not completed the track, and ErrUnknownProject for an
// unknown project id. The artefact records work the user actually
// did — it is not a certificate and makes no claim of AWS
// certification (see the footer it always appends).
func (s *Service) GeneratePortfolio(ctx context.Context, userID, projectID string) (filename, markdown string, err error) {
	proj, ok := FindProject(projectID)
	if !ok {
		return "", "", ErrUnknownProject
	}
	if s.Store == nil {
		return "", "", fmt.Errorf("progress store is not configured")
	}
	stats, err := s.Store.UserStats(ctx, userID)
	if err != nil {
		return "", "", fmt.Errorf("user stats: %w", err)
	}
	if !stats.HasCompletedTrack(projectID) {
		return "", "", fmt.Errorf("%w: %s", ErrProjectNotCompleted, projectID)
	}
	track, err := s.Store.GetTrack(ctx, projectID)
	if err != nil {
		return "", "", fmt.Errorf("track %s: %w", projectID, err)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Portfolio: %s\n\n", track.Title)
	fmt.Fprintf(&b, "CloudForge Advanced project — completed by user `%s` on %s.\n\n",
		userID, time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintf(&b, "%s\n\n", track.Description)
	fmt.Fprintf(&b, "## Architecture summary\n\n")
	fmt.Fprintf(&b, "- Kind: %s\n", proj.Kind)
	fmt.Fprintf(&b, "- Estimated effort: %.0f hours\n", track.EstimatedHours)
	fmt.Fprintf(&b, "- Guided lab: %s (lab definition `%s`, %d minutes, lite-launchable: %t)\n",
		proj.Title, proj.LabDefinitionID, proj.DurationMin, proj.LiteLaunchable)
	fmt.Fprintf(&b, "- Modules (%d) and lessons (%d):\n", len(track.Modules), track.LessonCount())
	for _, m := range track.Modules {
		fmt.Fprintf(&b, "  - %s (%d lessons)\n", m.Title, len(m.Lessons))
	}
	fmt.Fprintf(&b, "\n## Learning objectives demonstrated\n\n")
	for _, o := range track.LearningObjectives {
		fmt.Fprintf(&b, "- %s\n", o)
	}
	fmt.Fprintf(&b, "\n## Deployment evidence (recorded by CloudForge)\n\n")
	fmt.Fprintf(&b, "- Track completed: %s (xp reward %d)\n", track.Title, track.XPReward)
	fmt.Fprintf(&b, "- Labs completed: %d\n", stats.LabsCompleted)
	fmt.Fprintf(&b, "- Challenges completed without guided steps: %d\n", stats.ChallengesCompleted)
	fmt.Fprintf(&b, "- Break-and-fix scenarios solved: %d\n", stats.BreakFixSolved)
	fmt.Fprintf(&b, "- Best assessment score: %d%%\n", stats.BestAssessmentScore)
	fmt.Fprintf(&b, "- Total XP: %d\n", stats.TotalXP)
	fmt.Fprintf(&b, "- Validation rules the lab was graded against: %s\n", strings.Join(proj.GradingRules, ", "))
	if len(track.Prerequisites) > 0 {
		fmt.Fprintf(&b, "- Prerequisites completed for this project: %s\n", strings.Join(track.Prerequisites, ", "))
	}
	fmt.Fprintf(&b, "\n---\n\n")
	fmt.Fprintf(&b, "This artefact records work completed inside CloudForge. It is a portfolio record, not a ")
	fmt.Fprintf(&b, "certificate, and it makes no claim of AWS certification or of employment readiness. ")
	fmt.Fprintf(&b, "The evidence above is what CloudForge itself recorded; the architecture decisions and ")
	fmt.Fprintf(&b, "tradeoffs are the author's own, as demonstrated in the project.\n")
	return fmt.Sprintf("cloudforge-portfolio-%s.md", projectID), b.String(), nil
}
