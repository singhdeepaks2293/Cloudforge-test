package tflearn

import (
	"strings"
	"testing"
)

func TestCourseStructureCounts(t *testing.T) {
	levels := Levels()
	if len(levels) != 3 {
		t.Fatalf("want 3 levels, got %d", len(levels))
	}
	want := []int{15, 20, 20}
	for i, l := range levels {
		if len(l.Lessons) != want[i] {
			t.Errorf("level %d: want %d lessons, got %d", l.Level, want[i], len(l.Lessons))
		}
	}
	if TotalLessons() != 55 {
		t.Errorf("want 55 lessons total, got %d", TotalLessons())
	}
	if len(Projects()) != 7 {
		t.Errorf("want 7 projects, got %d", len(Projects()))
	}
	seen := map[string]bool{}
	for _, l := range levels {
		for _, lesson := range l.Lessons {
			if lesson.ID == "" || lesson.Title == "" || lesson.Explanation == "" ||
				lesson.UseCase == "" || lesson.CodeExample == "" || lesson.Exercise == "" ||
				lesson.ExpectedOutput == "" || len(lesson.Mistakes) == 0 {
				t.Errorf("lesson %s is missing required content", lesson.ID)
			}
			if len(lesson.Quiz.Options) < 2 {
				t.Errorf("lesson %s: quiz needs >= 2 options", lesson.ID)
			}
			if lesson.Quiz.Correct < 0 || lesson.Quiz.Correct >= len(lesson.Quiz.Options) {
				t.Errorf("lesson %s: quiz correct answer out of range", lesson.ID)
			}
			if lesson.Quiz.Explanation == "" {
				t.Errorf("lesson %s: quiz needs an explanation", lesson.ID)
			}
			if seen[lesson.ID] {
				t.Errorf("duplicate lesson id %s", lesson.ID)
			}
			seen[lesson.ID] = true
		}
	}
	for _, p := range Projects() {
		if len(p.Objectives) == 0 || len(p.Tasks) == 0 || len(p.Criteria) == 0 || len(p.StarterFiles) == 0 {
			t.Errorf("project %s is missing content", p.ID)
		}
	}
	// Level 1 reuses the existing curriculum track: every L1 lesson
	// names the matching curriculum lesson.
	for _, lesson := range levels[0].Lessons {
		if lesson.CurriculumLesson == "" {
			t.Errorf("L1 lesson %s does not name its curriculum counterpart", lesson.ID)
		}
	}
	if _, ok := FindLesson("l1-13-plan"); !ok {
		t.Error("FindLesson(l1-13-plan) failed")
	}
	if _, ok := FindLesson("nope"); ok {
		t.Error("FindLesson(nope) should fail")
	}
	if SkillLevel(0, 55) != "Novice" || SkillLevel(55, 55) != "Expert" || SkillLevel(40, 55) != "Advanced" {
		t.Errorf("SkillLevel thresholds wrong: %q %q %q", SkillLevel(0, 55), SkillLevel(55, 55), SkillLevel(40, 55))
	}
}

func TestValidateHCL(t *testing.T) {
	valid := "resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}\n"
	if issues := ValidateHCL(valid); HasErrors(issues) {
		t.Errorf("valid HCL reported errors: %+v", issues)
	}
	if issues := ValidateHCL("resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n"); HasErrors(issues) == false {
		t.Error("unbalanced braces should be an error")
	}
	if issues := ValidateHCL("resource \"aws_vpc\" \"main\" {\n  cidr_block = var.missing\n}\n"); HasErrors(issues) == false {
		t.Error("undeclared variable should be an error")
	}
	if issues := ValidateHCL("resource \"aws_subnet\" \"s\" {\n  vpc_id = aws_vpc.nope.id\n}\n"); HasErrors(issues) == false {
		t.Error("undeclared resource reference should be an error")
	}
	if issues := ValidateHCL("resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n  # see https://example.amazonaws.com\n}\n"); HasErrors(issues) == false {
		t.Error("amazonaws.com reference should be an error")
	}
	if issues := ValidateHCL("variable \"x\" {\n  default = \"ok\"\n}\nresource \"aws_vpc\" \"main\" {\n  cidr_block = var.x\n}\n"); HasErrors(issues) {
		t.Errorf("declared variable reference should be valid: %+v", issues)
	}
	// Unsupported resource type is a warning, not an error.
	issues := ValidateHCL("resource \"aws_flying_carpet\" \"x\" {\n  size = 1\n}\n")
	if HasErrors(issues) {
		t.Errorf("unsupported type should warn, not error: %+v", issues)
	}
	if len(issues) == 0 {
		t.Error("unsupported type should produce a warning")
	}
}

func TestFormatHCL(t *testing.T) {
	in := "resource \"aws_vpc\" \"main\" {\ncidr_block=\"10.0.0.0/16\"\n}"
	out := FormatHCL(in)
	if !strings.Contains(out, "  cidr_block = \"10.0.0.0/16\"") {
		t.Errorf("format did not indent/normalise: %q", out)
	}
	if issues := ValidateHCL(out); HasErrors(issues) {
		t.Errorf("formatted output should still validate: %+v", issues)
	}
	// Heredoc bodies pass through untouched.
	heredoc := "resource \"aws_iam_policy\" \"p\" {\n  policy = <<EOT\n{\n  \"Version\": \"2012-10-17\"\n}\nEOT\n}\n"
	if got := FormatHCL(heredoc); !strings.Contains(got, "  \"Version\": \"2012-10-17\"") {
		t.Errorf("heredoc body was reformatted: %q", got)
	}
}

func TestExtractAndPlan(t *testing.T) {
	src := "resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.0.0.0/16\"\n}\n\nresource \"aws_subnet\" \"s\" {\n  vpc_id = aws_vpc.main.id\n  cidr_block = \"10.0.1.0/24\"\n}\n"
	res := ExtractResources(src)
	if len(res) != 2 {
		t.Fatalf("want 2 resources, got %d: %+v", len(res), res)
	}
	if res[0].Attributes["cidr_block"] != "10.0.0.0/16" {
		t.Errorf("attribute not captured: %+v", res[0].Attributes)
	}
	plan := ComputePlan(res, nil)
	if len(plan) != 2 {
		t.Fatalf("empty state: want 2 creates, got %+v", plan)
	}
	for _, c := range plan {
		if c.Action != "create" {
			t.Errorf("want create, got %s", c.Action)
		}
	}
	plan = ComputePlan(res, res)
	for _, c := range plan {
		if c.Action != "no-op" {
			t.Errorf("identical state: want no-op, got %s", c.Action)
		}
	}
	changed := ExtractResources("resource \"aws_vpc\" \"main\" {\n  cidr_block = \"10.1.0.0/16\"\n}\n")
	plan = ComputePlan(changed, res)
	actions := map[string]string{}
	for _, c := range plan {
		actions[c.Address] = c.Action
	}
	if actions["aws_vpc.main"] != "update" || actions["aws_subnet.s"] != "delete" {
		t.Errorf("want update+delete, got %+v", actions)
	}
	drift := DetectDrift(res, res, []string{"aws_vpc.main", "aws_subnet.s", "aws_s3_bucket.extra"})
	if len(drift) != 1 || drift[0].Kind != "unmanaged" {
		t.Errorf("want 1 unmanaged drift entry, got %+v", drift)
	}
	if drift := DetectDrift(res, res, []string{"aws_vpc.main", "aws_subnet.s"}); len(drift) != 0 {
		t.Errorf("no drift expected, got %+v", drift)
	}
}

func TestProviderResources(t *testing.T) {
	if len(ProviderResources) < 5 {
		t.Fatalf("want >= 5 provider resources, got %d", len(ProviderResources))
	}
	for _, r := range ProviderResources {
		if r.Type == "" || r.Description == "" || r.CloudForgeAPI == "" || r.Fidelity == "" || len(r.Arguments) == 0 {
			t.Errorf("provider resource %q is incomplete", r.Type)
		}
		if !SupportedResourceTypes[r.Type] {
			t.Errorf("provider resource %q is not in SupportedResourceTypes", r.Type)
		}
	}
	if ProviderInstallDocs == "" {
		t.Error("provider install docs are empty")
	}
}
