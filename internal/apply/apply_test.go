package apply

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/planner"
	"github.com/samuelfaj/sam-harness/internal/scan"
)

func TestRunRequiresApprovalPreservesExistingFilesAndIsIdempotent(t *testing.T) {
	root := newGitRepository(t)
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.27.0\n")
	write(t, filepath.Join(root, "AGENTS.md"), "# Team rules\n\nKeep this.\n")
	commitAll(t, root)

	firstScan, err := scan.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Create(firstScan, model.ProfileBaseline, approvedAnswers())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(plan, "wrong"); err == nil {
		t.Fatal("Run() applied a plan without the matching approval ID")
	}
	changed, err := Run(plan, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) == 0 {
		t.Fatal("Run() changed no files")
	}
	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agents), "Keep this.") || !strings.Contains(string(agents), "sam-harness:start") {
		t.Fatalf("AGENTS.md was not merged safely:\n%s", agents)
	}

	secondScan, err := scan.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := planner.Create(secondScan, model.ProfileBaseline, approvedAnswers())
	if err != nil {
		t.Fatal(err)
	}
	secondChanged, err := Run(secondPlan, secondPlan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondChanged) != 0 {
		t.Fatalf("second apply changed %v, want idempotent no-op", secondChanged)
	}
}

func TestRunRejectsRepositoryChangedAfterPlanning(t *testing.T) {
	root := newGitRepository(t)
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.27.0\n")
	commitAll(t, root)
	result, err := scan.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Create(result, model.ProfileBaseline, approvedAnswers())
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(root, "README.md"), "changed after plan\n")
	if _, err := Run(plan, plan.ID); err == nil || !strings.Contains(err.Error(), "changed after planning") {
		t.Fatalf("Run() error = %v, want stale-plan rejection", err)
	}
}

func TestSafeTargetRejectsSymbolicLinkParents(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".github")); err != nil {
		t.Fatal(err)
	}
	if _, err := safeTarget(root, ".github/copilot-instructions.md"); err == nil {
		t.Fatal("safeTarget() accepted a symbolic-link parent")
	}
}

func TestRunRejectsExpiredPlans(t *testing.T) {
	root := newGitRepository(t)
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.27.0\n")
	commitAll(t, root)
	result, err := scan.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Create(result, model.ProfileBaseline, approvedAnswers())
	if err != nil {
		t.Fatal(err)
	}
	plan.ExpiresAt = time.Now().Add(-time.Minute)
	plan.ID = planner.CalculateID(plan)
	if _, err := Run(plan, plan.ID); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("Run() error = %v, want expired-plan rejection", err)
	}
}

func TestRunDefendsAgainstReadOnlyPlanContent(t *testing.T) {
	root := newGitRepository(t)
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.27.0\n")
	commitAll(t, root)
	result, err := scan.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.Create(result, model.ProfileBaseline, approvedAnswers())
	if err != nil {
		t.Fatal(err)
	}
	actions := []string{}
	plan.Answers.AllowedActions = &actions
	plan.Unresolved = []string{}
	plan.ID = planner.CalculateID(plan)
	if _, err := Run(plan, plan.ID); err == nil || !strings.Contains(err.Error(), "write_repository") {
		t.Fatalf("Run() error = %v, want read-only rejection", err)
	}
}

func TestProductionApplyRemainsIdempotentWithGeneratedMigrationRunbook(t *testing.T) {
	root := newGitRepository(t)
	write(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.27.0\n")
	commitAll(t, root)
	answers := productionAnswers()
	firstScan, err := scan.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	firstPlan, err := planner.Create(firstScan, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(firstPlan, firstPlan.ID); err != nil {
		t.Fatal(err)
	}
	secondScan, err := scan.Run(root)
	if err != nil {
		t.Fatal(err)
	}
	secondPlan, err := planner.Create(secondScan, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range secondPlan.Operations {
		if operation.Action != "noop" {
			t.Fatalf("second production plan contains %s for %s", operation.Action, operation.Path)
		}
	}
}

func approvedAnswers() model.Answers {
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

func productionAnswers() model.Answers {
	answers := approvedAnswers()
	truth := true
	answers.DeploysToProduction = &truth
	answers.ObservationWindow = "until release verification completes"
	answers.RollbackOwner = "owner"
	answers.ProductionEnvironment = "release"
	answers.Workflow = productionWorkflow()
	return answers
}

func productionWorkflow() *model.WorkflowConfig {
	reviewers := make([]model.ReviewerConfig, 0, len(model.ReviewerRoles))
	for _, role := range model.ReviewerRoles {
		reviewers = append(reviewers, model.ReviewerConfig{Role: role, Command: []string{"review-agent"}, TimeoutSeconds: 60, FilesystemReadOnly: true})
	}
	command := func(name string) model.CommandSpec {
		return model.CommandSpec{Name: name, Workdir: ".", Command: []string{"fixture", name}, Required: true, TimeoutSeconds: 60}
	}
	guardWaivers := func(categories []string) model.GuardSet {
		waivers := make(map[string]string, len(categories))
		for _, category := range categories {
			waivers[category] = "not applicable to the idempotent renderer fixture"
		}
		return model.GuardSet{Commands: map[string]model.CommandSpec{}, Waivers: waivers}
	}
	return &model.WorkflowConfig{
		Enabled:      true,
		StaticGuards: guardWaivers(model.StaticGuardCategories),
		TestGuards:   guardWaivers(model.TestGuardCategories),
		Reviewers:    reviewers,
		Correction: model.CorrectionConfig{
			Enabled:             true,
			Command:             []string{"repair-agent"},
			MaxAttempts:         1,
			MaxChangedFiles:     1,
			MaxChangedLines:     1,
			BranchPrefix:        "sam-harness/fix-",
			FilesystemSandboxed: true,
		},
		Artifact: model.ArtifactWorkflow{
			Build:          command("build"),
			ArtifactPath:   "dist/app",
			SBOM:           command("sbom"),
			SBOMPath:       "dist/sbom.json",
			Provenance:     command("provenance"),
			ProvenancePath: "dist/provenance.json",
		},
		Deployment: model.DeploymentWorkflow{
			Staging:           command("staging"),
			Production:        command("production"),
			Rollback:          command("rollback"),
			HealthChecks:      []model.CommandSpec{command("health")},
			ObservationChecks: []model.CommandSpec{command("observe")},
			CanaryPercentages: []int{10, 100},
		},
		Migration:       []model.CommandSpec{command("migration")},
		ReleaseSchedule: model.ReleaseSchedule{Cron: "0 12 * * 3", Timezone: "UTC"},
	}
}

func newGitRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "test@example.com")
	run(t, root, "git", "config", "user.name", "Test")
	return root
}

func commitAll(t *testing.T, root string) {
	t.Helper()
	run(t, root, "git", "add", ".")
	run(t, root, "git", "commit", "-m", "fixture")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, root, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
}
