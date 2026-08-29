package pipeline

import (
	"testing"

	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestRequiredReviewerRolesSkipsLowRiskRoles(t *testing.T) {
	t.Parallel()
	got := RequiredReviewerRoles(model.ChangeRiskLow)
	if len(got) == len(model.ReviewerRoles) {
		t.Fatal("low risk required every reviewer role")
	}
	want := map[model.ReviewerRole]bool{model.ReviewerCorrectness: true, model.ReviewerSimplicity: true}
	for _, role := range got {
		if !want[role] {
			t.Fatalf("low risk included %s", role)
		}
	}
	all := RequiredReviewerRoles("")
	if len(all) != len(model.ReviewerRoles) {
		t.Fatalf("empty risk = %v, want fail-closed full set", all)
	}
}

func TestArbitrateKeepsAttributionAndDetectsConflicts(t *testing.T) {
	t.Parallel()
	identical := []Finding{
		{Role: model.ReviewerCorrectness, Severity: "P2", Summary: "dup", Evidence: "a", Path: "file.go", Line: 4},
		{Role: model.ReviewerSimplicity, Severity: "P2", Summary: "dup", Evidence: "a", Path: "file.go", Line: 4},
	}
	if conflicts := Arbitrate(identical); len(conflicts) != 0 {
		t.Fatalf("identical findings conflicted: %v", conflicts)
	}
	if identical[0].Role != model.ReviewerCorrectness || identical[1].Role != model.ReviewerSimplicity {
		t.Fatalf("attribution was rewritten: %#v", identical)
	}
	conflicted := []Finding{
		{Role: model.ReviewerCorrectness, Severity: "P0", Summary: "bug", Evidence: "a", Path: "file.go", Line: 4},
		{Role: model.ReviewerSimplicity, Severity: "P3", Summary: "not a bug", Evidence: "b", Path: "file.go", Line: 4},
	}
	if conflicts := Arbitrate(conflicted); len(conflicts) == 0 {
		t.Fatal("contradictory findings were not blocked")
	}
}
