package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/render"
)

func TestRunAcceptsCompleteProductionInstallation(t *testing.T) {
	t.Parallel()
	root := installProductionHarness(t)
	report, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("complete production installation failed doctor: %v", report.Errors)
	}
}

func TestRunAcceptsCompleteBaselineInstallationWithoutRemoteWorkflow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	falsehood := false
	actions := []string{"write_repository"}
	operations, err := render.Build(model.ScanResult{
		Root:   root,
		Stacks: []model.Stack{{Kind: "go", Path: ".", Commands: map[string][]string{"test": {"go", "test", "./..."}}}},
	}, model.ProfileBaseline, model.Answers{
		Criticality:         "low",
		DataSensitivity:     "public",
		DeploysToProduction: &falsehood,
		PersistentData:      &falsehood,
		IrreversibleActions: &falsehood,
		Approvers:           []string{"owner"},
		AllowCIChanges:      &falsehood,
		AllowedActions:      &actions,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeOperations(t, root, operations)
	report, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("complete baseline installation failed doctor: %v", report.Errors)
	}
}

func TestRunRejectsMissingProductionInstallationFiles(t *testing.T) {
	paths := []string{
		"CLAUDE.md",
		"GEMINI.md",
		".github/copilot-instructions.md",
		".gitignore",
		".sam-harness/WORKFLOW.md",
		".sam-harness/REVIEWERS.md",
		".sam-harness/CHANGE_BUDGET.md",
		".sam-harness/runbooks/observability.md",
		".sam-harness/runbooks/retirement.md",
		".agents/skills/sam-harness-classify/SKILL.md",
		".agents/skills/sam-harness-context/SKILL.md",
		".agents/skills/sam-harness-plan/SKILL.md",
		".agents/skills/sam-harness-implement/SKILL.md",
		".agents/skills/sam-harness-review/SKILL.md",
		".agents/skills/sam-harness-repair/SKILL.md",
		".agents/skills/sam-harness-release/SKILL.md",
		".github/pull_request_template.md",
		".gitlab/merge_request_templates/sam-harness.md",
		"services/api/AGENTS.md",
		".github/workflows/sam-harness.yml",
		".sam-harness/ci/gitlab.yml",
		".gitlab-ci.yml",
	}
	for _, path := range paths {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			root := installProductionHarness(t)
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
				t.Fatal(err)
			}
			report, err := Run(root)
			if err != nil {
				t.Fatal(err)
			}
			if report.Passed || !hasError(report, "missing "+path) {
				t.Fatalf("doctor did not report missing %s: %+v", path, report)
			}
		})
	}
}

func TestRunRejectsIncompleteLifecycleDeclarations(t *testing.T) {
	t.Parallel()
	root := installProductionHarness(t)
	workflowPath := filepath.Join(root, ".sam-harness", "WORKFLOW.md")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	workflow = []byte(strings.Replace(string(workflow), "- security command", "- omitted security command", 1))
	if err := os.WriteFile(workflowPath, workflow, 0o644); err != nil {
		t.Fatal(err)
	}
	githubPath := filepath.Join(root, ".github", "workflows", "sam-harness.yml")
	github, err := os.ReadFile(githubPath)
	if err != nil {
		t.Fatal(err)
	}
	github = []byte(strings.ReplaceAll(string(github), "name: sam-harness-receipts-artifact", "name: missing-artifact-receipt"))
	if err := os.WriteFile(githubPath, github, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || !hasError(report, "- security") || !hasError(report, "sam-harness-receipts-artifact") {
		t.Fatalf("doctor accepted incomplete lifecycle declarations: %+v", report)
	}
}

func TestRunAcceptsSafelyInactiveOrNonPublishingProductionCI(t *testing.T) {
	t.Parallel()
	for _, denied := range []string{"network", "deploy", "release"} {
		denied := denied
		t.Run(denied, func(t *testing.T) {
			t.Parallel()
			answers := productionAnswers()
			if denied == "network" {
				answers.Workflow.Correction.OpenChangeRequest = false
			}
			allowed := withoutAction(*answers.AllowedActions, denied)
			answers.AllowedActions = &allowed
			root := installProductionHarnessWithAnswers(t, answers)
			report, err := Run(root)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Passed {
				t.Fatalf("production installation with %s=false failed doctor: %v", denied, report.Errors)
			}
		})
	}
}

func TestRunAcceptsMixedReviewAndRepairSecretBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		scope string
	}{
		{name: "review secret with credential-free repair", scope: model.CISecretScopeReview},
		{name: "credential-free review with repair secret", scope: model.CISecretScopeRepair},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			answers := productionAnswers()
			answers.CISecretBindings["github"] = bindingsForScope(answers.CISecretBindings["github"], test.scope)
			answers.CISecretBindings["gitlab"] = bindingsForScope(answers.CISecretBindings["gitlab"], test.scope)
			answers.CISecretWaivers = map[string]string{"github": "the other agent scope uses no provider secret", "gitlab": "the other agent scope uses no provider secret"}
			root := installProductionHarnessWithAnswers(t, answers)
			report, err := Run(root)
			if err != nil {
				t.Fatal(err)
			}
			if !report.Passed {
				t.Fatalf("doctor rejected valid mixed agent boundary: %v", report.Errors)
			}
		})
	}
}

func TestRunRequiresFailClosedSecretFreePullRequestPhaseJobs(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.CISecretBindings["github"] = append(answers.CISecretBindings["github"], model.CISecretBinding{Scope: model.CISecretScopeStatic, Environment: "STATIC_ENV", Secret: "STATIC_API_KEY"})
	answers.CISecretBindings["gitlab"] = append(answers.CISecretBindings["gitlab"], model.CISecretBinding{Scope: model.CISecretScopeStatic, Environment: "STATIC_ENV", Secret: "STATIC_API_KEY"})
	root := installProductionHarnessWithAnswers(t, answers)
	report, err := Run(root)
	if err != nil || !report.Passed {
		t.Fatalf("fail-closed secret-free phase installation failed doctor: err=%v report=%+v", err, report)
	}
	path := filepath.Join(root, ".github", "workflows", "sam-harness.yml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content = []byte(strings.Replace(string(content), "agent secrets cannot be injected into pull-request-controlled phase jobs", "silently skipped secret", 1))
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || !hasError(report, "agent secrets cannot be injected") {
		t.Fatalf("doctor accepted silently skipped pull-request agent secret: %+v", report)
	}
}

func TestRunRejectsUnsafeCIControlMutations(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		old      string
		replace  string
		expected string
	}{
		{name: "GitHub archive extraction", path: ".github/workflows/sam-harness.yml", old: "tar -xf", replace: "missing-tar-extract", expected: "tar -xf"},
		{name: "GitHub checkout credential isolation", path: ".github/workflows/sam-harness.yml", old: "persist-credentials: false", replace: "persist-credentials: true", expected: "persist-credentials: false"},
		{name: "GitHub duplicate rollback condition", path: ".github/workflows/sam-harness.yml", old: "    if: github.event_name == 'workflow_dispatch' && inputs.phase == 'rollback'", replace: "    if: github.event_name == 'workflow_dispatch' && inputs.phase == 'rollback'\n    if: github.event_name == 'workflow_dispatch' && inputs.phase == 'rollback'", expected: "exactly one job-level if condition"},
		{name: "GitHub explicit rollback", path: ".github/workflows/sam-harness.yml", old: "github.event_name == 'workflow_dispatch' && inputs.phase == 'rollback'", replace: "failure()", expected: "github.event_name == 'workflow_dispatch'"},
		{name: "GitHub trusted trigger", path: ".github/workflows/sam-harness-agents.yml", old: "pull_request_target:", replace: "pull_request_untrusted:", expected: "pull_request_target:"},
		{name: "GitHub rejects direct secret-bearing merge-group trigger", path: ".github/workflows/sam-harness-agents.yml", old: "repository_dispatch:\n    types: [sam_harness_merge_group_review]", replace: "merge_group:\n    types: [checks_requested]", expected: "repository_dispatch:"},
		{name: "GitHub base-owned merge-group dispatch", path: ".github/workflows/sam-harness-agents.yml", old: "github.event.client_payload.head_sha", replace: "github.event.client_payload.missing_sha", expected: "github.event.client_payload.head_sha"},
		{name: "GitHub provider-owned merge-queue ref", path: ".github/workflows/sam-harness-agents.yml", old: "refs/heads/gh-readonly-queue/*", replace: "refs/heads/*", expected: "refs/heads/gh-readonly-queue/*"},
		{name: "GitHub protected App and agent environment", path: ".github/workflows/sam-harness-agents.yml", old: "environment:\n      name: 'agent-review'", replace: "environment:\n      name: 'unprotected-agent'", expected: "environment:"},
		{name: "GitHub missing secret fails closed", path: ".github/workflows/sam-harness-agents.yml", old: `test -n "${REVIEW_ENV:-}"`, replace: "true # unsafe silent secret omission", expected: "REVIEW_ENV"},
		{name: "GitHub trusted base config", path: ".github/workflows/sam-harness-agents.yml", old: "trusted-control/.sam-harness/config.yaml", replace: "target/.sam-harness/config.yaml", expected: "trusted-control/.sam-harness/config.yaml"},
		{name: "GitHub pinned sensitive runtime", path: ".github/workflows/sam-harness-agents.yml", old: "go run github.com/samuelfaj/sam-harness/cmd/sam-harness@v" + model.HarnessVersion, replace: "go run ./cmd/sam-harness", expected: "go run github.com/samuelfaj/sam-harness/cmd/sam-harness@v"},
		{name: "GitHub no PR setup before secret", path: ".github/workflows/sam-harness-agents.yml", old: "      - name: Run six-role review from trusted base control plane", replace: "      - name: Prepare repository\n        run: npm ci\n      - name: Run six-role review from trusted base control plane", expected: "Prepare repository"},
		{name: "GitHub review patch lineage", path: ".github/workflows/sam-harness-agents.yml", old: "review_patch_sha256", replace: "missing_review_patch_sha", expected: "review_patch_sha256"},
		{name: "GitHub patch lineage", path: ".github/workflows/sam-harness-agents.yml", old: "repair_patch_sha256", replace: "missing_patch_sha", expected: "repair_patch_sha256"},
		{name: "GitHub App check provenance", path: ".github/workflows/sam-harness-agents.yml", old: "actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349", replace: "unsafe/local-token", expected: "actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349"},
		{name: "GitHub exact SHA arguments", path: ".github/workflows/sam-harness-agents.yml", old: "--review-base-sha", replace: "--missing-base-sha", expected: "--review-base-sha"},
		{name: "GitLab archive extraction", path: ".sam-harness/ci/gitlab.yml", old: "tar -xf", replace: "missing-tar-extract", expected: "tar -xf"},
		{name: "GitLab rejects local secret review job", path: ".sam-harness/ci/gitlab.yml", old: "sam-harness-artifact:\n", replace: "sam-harness-review:\n  script: echo unsafe\n\nsam-harness-artifact:\n", expected: "sam-harness-review:"},
		{name: "GitLab external project declaration", path: ".sam-harness/WORKFLOW.md", old: "trusted/review-control", replace: "missing/external-control", expected: "trusted/review-control"},
		{name: "GitLab external status declaration", path: ".sam-harness/WORKFLOW.md", old: "sam-harness/review", replace: "missing-status", expected: "sam-harness/review"},
		{name: "reviewer attestation documentation", path: ".sam-harness/REVIEWERS.md", old: "filesystem_read_only: true", replace: "filesystem_read_only: false", expected: "filesystem_read_only: true"},
		{name: "correction attestation documentation", path: ".sam-harness/CHANGE_BUDGET.md", old: "filesystem_sandboxed: true", replace: "filesystem_sandboxed: false", expected: "filesystem_sandboxed: true"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root := installProductionHarness(t)
			path := filepath.Join(root, filepath.FromSlash(test.path))
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			mutated := strings.ReplaceAll(string(content), test.old, test.replace)
			if mutated == string(content) {
				t.Fatalf("fixture did not contain %q", test.old)
			}
			if err := os.WriteFile(path, []byte(mutated), 0o644); err != nil {
				t.Fatal(err)
			}
			report, err := Run(root)
			if err != nil {
				t.Fatal(err)
			}
			if report.Passed || !hasError(report, test.expected) {
				t.Fatalf("doctor accepted %s mutation: %+v", test.name, report)
			}
		})
	}
}

func installProductionHarness(t *testing.T) string {
	t.Helper()
	return installProductionHarnessWithAnswers(t, productionAnswers())
}

func installProductionHarnessWithAnswers(t *testing.T, answers model.Answers) string {
	t.Helper()
	root := t.TempDir()
	operations, err := render.Build(model.ScanResult{
		Root:           root,
		CIProviders:    []string{"github", "gitlab"},
		HasPersistence: true,
		HasDeployment:  true,
		Stacks: []model.Stack{
			{Kind: "go", Path: ".", Commands: map[string][]string{"build": {"go", "build", "./..."}, "test": {"go", "test", "./..."}}},
			{Kind: "go", Path: "services/api", Commands: map[string][]string{"test": {"go", "test", "./..."}}},
		},
	}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	writeOperations(t, root, operations)
	return root
}

func withoutAction(actions []string, denied string) []string {
	filtered := make([]string, 0, len(actions))
	for _, action := range actions {
		if action != denied {
			filtered = append(filtered, action)
		}
	}
	return filtered
}

func bindingsForScope(bindings []model.CISecretBinding, scope string) []model.CISecretBinding {
	filtered := make([]model.CISecretBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Scope == scope {
			filtered = append(filtered, binding)
		}
	}
	return filtered
}

func writeOperations(t *testing.T, root string, operations []model.Operation) {
	t.Helper()
	for _, operation := range operations {
		target := filepath.Join(root, filepath.FromSlash(operation.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(operation.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func productionAnswers() model.Answers {
	truth := true
	actions := []string{"write_repository", "network", "commit", "push", "release", "deploy"}
	workflow := &model.WorkflowConfig{
		Enabled:      true,
		StaticGuards: allCommands(model.StaticGuardCategories),
		TestGuards:   allCommands(model.TestGuardCategories),
		Correction: model.CorrectionConfig{
			Enabled:                true,
			FilesystemSandboxed:    true,
			TrustedExternalCommand: true,
			Command:                []string{"repair-agent"},
			MaxAttempts:            2,
			MaxChangedFiles:        4,
			MaxChangedLines:        80,
			BranchPrefix:           "sam-harness/repair-",
			OpenChangeRequest:      true,
		},
		Artifact: model.ArtifactWorkflow{
			Build:          spec("artifact", "tools/build-artifact"),
			ArtifactPath:   "dist/application.tar",
			SBOM:           spec("SBOM", "tools/build-sbom"),
			SBOMPath:       "dist/application.sbom.json",
			Provenance:     spec("provenance", "tools/build-provenance"),
			ProvenancePath: "dist/application.provenance.json",
		},
		Deployment: model.DeploymentWorkflow{
			Staging:           spec("staging", "tools/deploy-staging"),
			Production:        spec("production", "tools/deploy-production"),
			Rollback:          spec("rollback", "tools/rollback"),
			HealthChecks:      []model.CommandSpec{spec("health", "tools/health")},
			ObservationChecks: []model.CommandSpec{spec("observation", "tools/observe")},
			CanaryPercentages: []int{10, 100},
		},
		Migration:       []model.CommandSpec{spec("migration", "tools/migrate")},
		ReleaseSchedule: model.ReleaseSchedule{Cron: "17 4 * * 1", Timezone: "America/Asuncion"},
	}
	for _, role := range model.ReviewerRoles {
		workflow.Reviewers = append(workflow.Reviewers, model.ReviewerConfig{Role: role, Command: []string{"review-agent", string(role)}, TimeoutSeconds: 120, FilesystemReadOnly: true, TrustedExternalCommand: true})
	}
	return model.Answers{
		Criticality:         "high",
		DataSensitivity:     "internal",
		DeploysToProduction: &truth,
		PersistentData:      &truth,
		IrreversibleActions: &truth,
		Approvers:           []string{"owner"},
		AllowCIChanges:      &truth,
		AllowedActions:      &actions,
		CISecretBindings: map[string][]model.CISecretBinding{
			"github": {
				{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"},
				{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "REPAIR_API_KEY"},
			},
			"gitlab": {
				{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"},
				{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "REPAIR_API_KEY"},
			},
		},
		AgentSecretEnvironments: map[string]string{"github": "agent-review", "gitlab": "agent-review"},
		AgentControlPlanes: map[string]model.AgentControlPlane{
			"github": {Mode: model.AgentControlPlaneModeGitHubApp, RequiredCheck: "sam-harness/review", AppIDSecret: "SAM_HARNESS_APP_ID", AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY"},
			"gitlab": {Mode: model.AgentControlPlaneModeExternal, RequiredCheck: "sam-harness/review", ExternalProject: "trusted/review-control"},
		},
		ObservationWindow:     "24 hours",
		RollbackOwner:         "release owner",
		ProductionEnvironment: "production",
		Workflow:              workflow,
	}
}

func allCommands(categories []string) model.GuardSet {
	guards := model.GuardSet{Commands: map[string]model.CommandSpec{}, Waivers: map[string]string{}}
	for _, category := range categories {
		guards.Commands[category] = spec(category, "tools/guard-"+category)
	}
	return guards
}

func spec(name, executable string) model.CommandSpec {
	return model.CommandSpec{Name: name, Workdir: ".", Command: []string{executable}, Required: true, TimeoutSeconds: 120}
}

func hasError(report Report, fragment string) bool {
	for _, item := range report.Errors {
		if strings.Contains(item, fragment) {
			return true
		}
	}
	return false
}
