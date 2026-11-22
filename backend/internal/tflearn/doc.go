// Package tflearn implements the Terraform Learning section
// (CloudForge v2, Phase 5, master prompt §9).
//
// Content model:
//   - Course structure lives in lessons.go: 4 levels, 55 lessons and
//     7 projects. Level 1 reuses the existing curriculum Terraform
//     track (curriculum/terraform/terraform.yaml) where it overlaps —
//     each Level 1 lesson names the matching curriculum lesson in
//     CurriculumLesson instead of duplicating that track's content.
//     Levels 2–4 are new content that lives only here.
//   - The editor's local checks live in hcl.go: real HCL validation
//     and formatting that run without Docker and without the Terraform
//     binary. They are labelled "local" everywhere they surface —
//     they are not a terraform binary run.
//   - Container execution is NOT reimplemented here: it delegates to
//     the existing Milestone 7 manager (internal/terraform +
//     internal/jobs) through the lab a project is linked to. Without
//     Docker the API answers 503 terraform_unavailable, never a
//     faked plan or apply.
//   - State (state.go) is append-only versioned snapshots. State is
//     never edited by string replacement; a restore creates a new
//     version copied from the restored one.
package tflearn
