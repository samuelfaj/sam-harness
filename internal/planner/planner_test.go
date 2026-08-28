package planner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestCreateStopsBeforeWritingPlanOperationsWhenAnswersAreMissing(t *testing.T) {
	t.Parallel()
	scan := model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint", HasUI: true}
	plan, err := Create(scan, model.ProfileAuto, model.Answers{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unresolved) == 0 {
		t.Fatal("Unresolved is empty, want questions")
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("Operations = %d, want 0 before decisions", len(plan.Operations))
	}
}

func TestRecommendUsesRiskInsteadOfRepositoryLanguage(t *testing.T) {
	t.Parallel()
	truth := true
	falsehood := false
	base := completeAnswers()
	base.DeploysToProduction = &falsehood
	base.PersistentData = &falsehood
	base.IrreversibleActions = &falsehood
	if got := Recommend(model.ScanResult{}, base); got != model.ProfileBaseline {
		t.Fatalf("Recommend() = %s, want baseline", got)
	}
	base.PersistentData = &truth
	if got := Recommend(model.ScanResult{}, base); got != model.ProfileProduction {
		t.Fatalf("Recommend() = %s, want production", got)
	}
	base.DataSensitivity = "regulated"
	if got := Recommend(model.ScanResult{}, base); got != model.ProfileRegulated {
		t.Fatalf("Recommend() = %s, want regulated", got)
	}
}

func TestLowerProfileRequiresRecordedRiskAcceptance(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	answers := completeAnswers()
	truth := true
	answers.DeploysToProduction = &truth
	plan, err := Create(model.ScanResult{Root: root, Fingerprint: "fingerprint"}, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "risk_acceptance") {
		t.Fatalf("Unresolved = %v, want risk_acceptance", plan.Unresolved)
	}
}

func TestCompletePlanUsesAnEmptyUnresolvedArray(t *testing.T) {
	t.Parallel()
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileBaseline, completeAnswers())
	if err != nil {
		t.Fatal(err)
	}
	if plan.Unresolved == nil || len(plan.Unresolved) != 0 {
		t.Fatalf("Unresolved = %#v, want a non-nil empty array", plan.Unresolved)
	}
}

func TestCreateStopsForUnresolvedRepositoryCommands(t *testing.T) {
	t.Parallel()
	scan := model.ScanResult{
		Root:        t.TempDir(),
		Fingerprint: "fingerprint",
		Questions:   []string{"commands:typescript:."},
		Stacks:      []model.Stack{{Kind: "typescript", Path: ".", Commands: map[string][]string{}}},
	}
	plan, err := Create(scan, model.ProfileBaseline, completeAnswers())
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "commands:typescript:.") || len(plan.Operations) != 0 {
		t.Fatalf("plan = %#v, want a blocked command decision", plan)
	}
}

func TestCommandAnswerTurnsQuestionIntoConfiguredGate(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	answers.CommandOverrides = map[string]map[string][]string{
		"typescript:.": {"test": {"npm", "test"}},
	}
	scan := model.ScanResult{
		Root:        t.TempDir(),
		Fingerprint: "fingerprint",
		Stacks:      []model.Stack{{Kind: "typescript", Path: ".", PackageManager: "npm", Commands: map[string][]string{}}},
		Questions:   []string{"commands:typescript:."},
	}
	plan, err := Create(scan, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unresolved) != 0 || len(plan.Operations) == 0 {
		t.Fatalf("plan = %#v, want an applicable command answer", plan)
	}
}

func TestCommandWaiverRecordsAnExplicitNoGateDecision(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	answers.CommandWaivers = map[string]string{"python:.": "this documentation-only package has no executable checks"}
	scan := model.ScanResult{
		Root:        t.TempDir(),
		Fingerprint: "fingerprint",
		Stacks:      []model.Stack{{Kind: "python", Path: ".", PackageManager: "python", Commands: map[string][]string{}}},
	}
	plan, err := Create(scan, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unresolved) != 0 || len(plan.Operations) == 0 {
		t.Fatalf("plan = %#v, want an applicable recorded waiver", plan)
	}
}

func TestCIProviderAnswerEnablesAnUndetectedProvider(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	allowCI := true
	answers.AllowCIChanges = &allowCI
	answers.CIProviders = []string{"github"}
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unresolved) != 0 {
		t.Fatalf("Unresolved = %v, want resolved CI provider", plan.Unresolved)
	}
	found := false
	for _, operation := range plan.Operations {
		if operation.Path == ".github/workflows/sam-harness.yml" {
			found = true
		}
	}
	if !found {
		t.Fatal("plan did not create the selected GitHub Actions adapter")
	}
}

func TestNonGoManagedCIRequiresResolvedSetup(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	allowCI := true
	answers.AllowCIChanges = &allowCI
	scan := model.ScanResult{
		Root:        t.TempDir(),
		Fingerprint: "fingerprint",
		CIProviders: []string{"github"},
		Stacks: []model.Stack{{
			Kind: "typescript", Path: ".", Commands: map[string][]string{"test": {"npm", "test"}},
		}},
	}
	blocked, err := Create(scan, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(blocked.Unresolved, "ci_setup:github") {
		t.Fatalf("Unresolved = %v, want CI setup decision", blocked.Unresolved)
	}
	answers.CISetupWaivers = map[string]string{"github": "the managed runner image already contains project dependencies"}
	resolved, err := Create(scan, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Unresolved) != 0 || len(resolved.Operations) == 0 {
		t.Fatalf("resolved plan = %#v", resolved)
	}
}

func TestCreateRequiresWriteAuthority(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	actions := []string{}
	answers.AllowedActions = &actions
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "authority:write_repository") || len(plan.Operations) != 0 {
		t.Fatalf("plan = %#v, want read-only application blocked", plan)
	}
}

func TestAppliedRegulatedProfileRequiresItsOperationalControls(t *testing.T) {
	t.Parallel()
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileRegulated, completeAnswers())
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"observation_window", "production_environment", "rollback_owner", "separated_approvers"} {
		if !contains(plan.Unresolved, required) {
			t.Fatalf("Unresolved = %v, want %s", plan.Unresolved, required)
		}
	}
}

func TestExplicitHumanAnswersOverrideDeploymentFilenameHints(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	if got := Recommend(model.ScanResult{HasDeployment: true, HasPersistence: true}, answers); got != model.ProfileBaseline {
		t.Fatalf("Recommend() = %s, want baseline from explicit human answers", got)
	}
}

func TestSaveRefusesExistingAndRepositoryPaths(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := model.Plan{Root: root}
	existing := filepath.Join(root, "README.md")
	if err := os.WriteFile(existing, []byte("preserve me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Save(plan, existing); err == nil {
		t.Fatal("Save() overwrote an existing repository file")
	}
	if _, err := Save(plan, filepath.Join(root, "plan.json")); err == nil {
		t.Fatal("Save() created a plan inside the repository")
	}
	outside := filepath.Join(t.TempDir(), "plan.json")
	if _, err := Save(plan, outside); err != nil {
		t.Fatalf("Save() rejected a new path outside the repository: %v", err)
	}
}

func completeAnswers() model.Answers {
	falsehood := false
	allowCI := false
	actions := []string{"write_repository"}
	return model.Answers{
		Criticality:         "low",
		DataSensitivity:     "public",
		DeploysToProduction: &falsehood,
		PersistentData:      &falsehood,
		IrreversibleActions: &falsehood,
		Approvers:           []string{"owner"},
		AllowCIChanges:      &allowCI,
		AllowedActions:      &actions,
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
