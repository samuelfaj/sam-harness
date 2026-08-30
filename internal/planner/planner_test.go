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

func TestBaselinePlanDoesNotRequireRemoteWorkflowControls(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	answers.Workflow = nil
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unresolved) != 0 {
		t.Fatalf("Unresolved = %v, want baseline to omit workflow controls", plan.Unresolved)
	}
}

func TestProductionPlanRequiresEveryExecutableWorkflowDecision(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"workflow.enabled",
		"workflow.reviewers.architecture",
		"workflow.reviewers.security",
		"workflow.reviewers.correctness",
		"workflow.reviewers.test_quality",
		"workflow.reviewers.business_rules",
		"workflow.reviewers.simplicity",
		"workflow.correction",
		"workflow.artifact.build",
		"workflow.artifact.path",
		"workflow.artifact.sbom",
		"workflow.artifact.sbom_path",
		"workflow.artifact.provenance",
		"workflow.artifact.provenance_path",
		"workflow.deployment.staging",
		"workflow.deployment.production",
		"workflow.deployment.rollback",
		"workflow.deployment.health_checks",
		"workflow.deployment.observation_checks",
		"workflow.deployment.canary_percentages",
		"workflow.migration",
		"workflow.release_schedule.cron",
		"workflow.release_schedule.timezone",
	} {
		if !contains(plan.Unresolved, required) {
			t.Fatalf("Unresolved = %v, want %s", plan.Unresolved, required)
		}
	}
	for _, category := range model.StaticGuardCategories {
		if !contains(plan.Unresolved, "workflow.static_guards."+category) {
			t.Fatalf("Unresolved = %v, want static guard %s", plan.Unresolved, category)
		}
	}
	for _, category := range model.TestGuardCategories {
		if !contains(plan.Unresolved, "workflow.test_guards."+category) {
			t.Fatalf("Unresolved = %v, want test guard %s", plan.Unresolved, category)
		}
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("Operations = %d, want no production install before workflow decisions", len(plan.Operations))
	}
}

func TestProductionGuardRequiresCommandOrExplicitWaiver(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	delete(answers.Workflow.TestGuards.Commands, model.GuardPerformance)
	blocked, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(blocked.Unresolved, "workflow.test_guards.performance") {
		t.Fatalf("Unresolved = %v, want missing performance guard", blocked.Unresolved)
	}

	answers.Workflow.TestGuards.Waivers[model.GuardPerformance] = "performance is measured by the mandatory production observation check"
	resolved, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Unresolved) != 0 {
		t.Fatalf("Unresolved = %v, want explicit guard waiver accepted", resolved.Unresolved)
	}
}

func TestProductionAndRegulatedPlansAcceptCompleteWorkflow(t *testing.T) {
	t.Parallel()
	for _, profile := range []model.Profile{model.ProfileProduction, model.ProfileRegulated} {
		profile := profile
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			answers := productionAnswers()
			answers.Workflow = completeWorkflow()
			if profile == model.ProfileRegulated {
				answers.Approvers = []string{"release-owner", "security-owner"}
			}
			plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, profile, answers)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Unresolved) != 0 {
				t.Fatalf("Unresolved = %v, want complete %s workflow", plan.Unresolved, profile)
			}
			if len(plan.Operations) == 0 {
				t.Fatal("Operations is empty after every executable decision was supplied")
			}
		})
	}
}

func TestProductionWorkflowRequiresEnabledCorrection(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	answers.Workflow.Correction = model.CorrectionConfig{}
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "workflow.correction") {
		t.Fatalf("Unresolved = %v, want workflow.correction", plan.Unresolved)
	}
}

func TestProductionWorkflowRequiresFilesystemSandboxAttestation(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	answers.Workflow.Correction.FilesystemSandboxed = false
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "workflow.correction.filesystem_sandboxed") {
		t.Fatalf("Unresolved = %v, want correction filesystem sandbox attestation", plan.Unresolved)
	}
}

func TestBaselineEnabledCorrectionRequiresFilesystemSandboxAttestation(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	answers.Workflow = completeWorkflow()
	answers.Workflow.Correction.FilesystemSandboxed = false
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "workflow.correction.filesystem_sandboxed") {
		t.Fatalf("Unresolved = %v, want baseline correction filesystem sandbox attestation", plan.Unresolved)
	}
}

func TestWorkflowReviewerRequiresFilesystemReadOnlyAttestation(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	answers.Workflow.Reviewers[0].FilesystemReadOnly = false
	role := answers.Workflow.Reviewers[0].Role
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	want := "workflow.reviewers." + string(role) + ".filesystem_read_only"
	if !contains(plan.Unresolved, want) {
		t.Fatalf("Unresolved = %v, want %s", plan.Unresolved, want)
	}
}

func TestWorkflowOpenChangeRequestRequiresPublisherAuthority(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	answers.Workflow.Correction.OpenChangeRequest = true
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"network", "commit", "push"} {
		if !contains(plan.Unresolved, "authority:"+action) {
			t.Fatalf("Unresolved = %v, want authority:%s", plan.Unresolved, action)
		}
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("Operations = %d, want no publisher rendered without authority", len(plan.Operations))
	}
}

func TestProductionManagedAgenticWorkflowRequiresCISecrets(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	managed := true
	answers.AllowCIChanges = &managed
	answers.CIProviders = []string{"github", "gitlab"}
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range answers.CIProviders {
		if !contains(plan.Unresolved, "ci_secrets:"+provider) {
			t.Fatalf("Unresolved = %v, want ci_secrets:%s", plan.Unresolved, provider)
		}
		if contains(plan.Unresolved, "ci_agent_secret_environment:"+provider) {
			t.Fatalf("Unresolved = %v, no binding exists for %s", plan.Unresolved, provider)
		}
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("Operations = %d, want no managed production pipeline before secret decisions", len(plan.Operations))
	}
}

func TestProductionOnlySecretBindingDoesNotResolveAgenticWorkflow(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	managed := true
	answers.AllowCIChanges = &managed
	answers.CIProviders = []string{"github"}
	answers.CISecretBindings = map[string][]model.CISecretBinding{
		"github": {{Scope: model.CISecretScopeProduction, Environment: "DEPLOY_TOKEN", Secret: "PRODUCTION_DEPLOY_TOKEN"}},
	}
	answers.AgentSecretEnvironments = map[string]string{"github": "agent-secrets"}
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "ci_secrets:github") {
		t.Fatalf("Unresolved = %v, want review and repair secret decision", plan.Unresolved)
	}
}

func TestProductionManagedBindingsRequireProtectedAgentSecretEnvironment(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	trustWorkflowCommands(answers.Workflow)
	managed := true
	answers.AllowCIChanges = &managed
	answers.CIProviders = []string{"github"}
	answers.CISecretBindings = map[string][]model.CISecretBinding{
		"github": {
			{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"},
			{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "CORRECTION_API_KEY"},
		},
	}
	answers.CISecretWaivers = map[string]string{"github": "workload identity covers fallback agent authentication"}
	blocked, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(blocked.Unresolved, "ci_agent_secret_environment:github") {
		t.Fatalf("Unresolved = %v, want protected agent secret environment decision", blocked.Unresolved)
	}
	if contains(blocked.Unresolved, "ci_secrets:github") {
		t.Fatalf("Unresolved = %v, review and repair bindings should already resolve secret names", blocked.Unresolved)
	}

	answers.AgentSecretEnvironments = map[string]string{"github": "agent-secrets"}
	answers.AgentControlPlanes = map[string]model.AgentControlPlane{"github": githubAgentControlPlane()}
	resolved, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Unresolved) != 0 || len(resolved.Operations) == 0 {
		t.Fatalf("resolved plan = %#v, protected environment decision was ignored", resolved)
	}
}

func TestPlanRoundTripPreservesAgentSecretEnvironmentForUpgrade(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := model.Plan{
		Root: root,
		Answers: model.Answers{
			AgentSecretEnvironments: map[string]string{"github": "agent-secrets"},
		},
	}
	plan.ID = CalculateID(plan)
	path := filepath.Join(t.TempDir(), "plan.json")
	if _, err := Save(plan, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Answers.AgentSecretEnvironments["github"]; got != "agent-secrets" {
		t.Fatalf("agent secret environment = %q, want agent-secrets after plan round trip", got)
	}
}

func TestPlanRoundTripPreservesAgentControlPlaneForUpgrade(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	plan := model.Plan{
		Root: root,
		Answers: model.Answers{
			AgentControlPlanes: map[string]model.AgentControlPlane{
				"github": githubAgentControlPlane(),
			},
		},
	}
	plan.ID = CalculateID(plan)
	path := filepath.Join(t.TempDir(), "plan.json")
	if _, err := Save(plan, path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := loaded.Answers.AgentControlPlanes["github"]; got != githubAgentControlPlane() {
		t.Fatalf("agent control plane = %#v, want %#v", got, githubAgentControlPlane())
	}
}

func TestProductionProviderBoundAgentsRequireControlPlane(t *testing.T) {
	t.Parallel()
	tests := []struct {
		provider string
		profile  model.Profile
		control  model.AgentControlPlane
	}{
		{provider: "github", profile: model.ProfileProduction, control: githubAgentControlPlane()},
		{provider: "gitlab", profile: model.ProfileRegulated, control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeExternal, RequiredCheck: "sam-harness/review", ExternalProject: "trusted/review-control"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.provider, func(t *testing.T) {
			t.Parallel()
			answers := productionAnswers()
			if test.profile == model.ProfileRegulated {
				answers.Approvers = []string{"release-owner", "security-owner"}
			}
			answers.Workflow = completeWorkflow()
			trustWorkflowCommands(answers.Workflow)
			managed := true
			answers.AllowCIChanges = &managed
			answers.CIProviders = []string{test.provider}
			answers.CISecretBindings = map[string][]model.CISecretBinding{
				test.provider: {
					{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "REVIEW_API_KEY"},
					{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "REPAIR_API_KEY"},
				},
			}
			answers.AgentSecretEnvironments = map[string]string{test.provider: "agent-secrets"}
			blocked, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, test.profile, answers)
			if err != nil {
				t.Fatal(err)
			}
			want := "ci_agent_control_plane:" + test.provider
			if !contains(blocked.Unresolved, want) || len(blocked.Operations) != 0 {
				t.Fatalf("plan = %#v, want unresolved %s and no rendered pipeline", blocked, want)
			}

			answers.AgentControlPlanes = map[string]model.AgentControlPlane{test.provider: test.control}
			resolved, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, test.profile, answers)
			if err != nil {
				t.Fatal(err)
			}
			if contains(resolved.Unresolved, want) || len(resolved.Unresolved) != 0 || len(resolved.Operations) == 0 {
				t.Fatalf("resolved plan = %#v, trusted provider control plane was ignored", resolved)
			}
		})
	}
}

func TestCreateRejectsInvalidAgentControlPlanes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		control  model.AgentControlPlane
	}{
		{name: "unknown provider", provider: "circleci", control: githubAgentControlPlane()},
		{name: "unsafe required check", provider: "github", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeGitHubApp, RequiredCheck: "review;publish", AppIDSecret: "SAM_HARNESS_APP_ID", AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY"}},
		{name: "unsafe external project", provider: "gitlab", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeExternal, RequiredCheck: "sam-harness/review", ExternalProject: "trusted/../review"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			answers := productionAnswers()
			answers.AgentControlPlanes = map[string]model.AgentControlPlane{test.provider: test.control}
			if _, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers); err == nil {
				t.Fatalf("Create() accepted %s", test.name)
			}
		})
	}
}

func TestProductionManagedAgenticWorkflowAcceptsScopedSecretBindingsOrWaiver(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	trustWorkflowCommands(answers.Workflow)
	managed := true
	answers.AllowCIChanges = &managed
	answers.CIProviders = []string{"github", "gitlab"}
	answers.CISecretBindings = map[string][]model.CISecretBinding{
		"github": {
			{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"},
			{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "CORRECTION_API_KEY"},
		},
	}
	answers.AgentSecretEnvironments = map[string]string{"github": "agent-secrets"}
	answers.AgentControlPlanes = map[string]model.AgentControlPlane{"github": githubAgentControlPlane()}
	answers.CISecretWaivers = map[string]string{
		"gitlab": "all configured commands are credential-free",
	}
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	for _, provider := range answers.CIProviders {
		if contains(plan.Unresolved, "ci_secrets:"+provider) {
			t.Fatalf("Unresolved = %v, explicit secret decision for %s was ignored", plan.Unresolved, provider)
		}
		if contains(plan.Unresolved, "ci_agent_secret_environment:"+provider) {
			t.Fatalf("Unresolved = %v, protected environment decision for %s is wrong", plan.Unresolved, provider)
		}
	}
	if len(plan.Unresolved) != 0 || len(plan.Operations) == 0 {
		t.Fatalf("plan = %#v, binding environment plus waiver-only provider should be fully resolved", plan)
	}
}

func TestProductionSecretBindingsRequireTrustedExternalCommands(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	managed := true
	answers.AllowCIChanges = &managed
	answers.CIProviders = []string{"github"}
	answers.CISecretBindings = map[string][]model.CISecretBinding{
		"github": {
			{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "REVIEW_API_KEY"},
			{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "REPAIR_API_KEY"},
		},
	}
	answers.AgentSecretEnvironments = map[string]string{"github": "agent-secrets"}
	answers.AgentControlPlanes = map[string]model.AgentControlPlane{"github": githubAgentControlPlane()}
	blocked, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range model.ReviewerRoles {
		want := "workflow.reviewers." + string(role) + ".trusted_external_command"
		if !contains(blocked.Unresolved, want) {
			t.Fatalf("Unresolved = %v, want %s", blocked.Unresolved, want)
		}
	}
	if !contains(blocked.Unresolved, "workflow.correction.trusted_external_command") {
		t.Fatalf("Unresolved = %v, want correction trusted command attestation", blocked.Unresolved)
	}
	if len(blocked.Operations) != 0 {
		t.Fatalf("Operations = %d, want secret-bearing pipeline blocked", len(blocked.Operations))
	}

	trustWorkflowCommands(answers.Workflow)
	resolved, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Unresolved) != 0 || len(resolved.Operations) == 0 {
		t.Fatalf("resolved plan = %#v", resolved)
	}
}

func TestPlannerRejectsTargetControlledReviewerHelperWithSecrets(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	trustWorkflowCommands(answers.Workflow)
	managed := true
	answers.AllowCIChanges = &managed
	answers.CIProviders = []string{"github"}
	answers.CISecretBindings = map[string][]model.CISecretBinding{
		"github": {{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "REVIEW_API_KEY"}},
	}
	answers.CISecretWaivers = map[string]string{"github": "repair uses workload identity without a provider secret"}
	answers.AgentSecretEnvironments = map[string]string{"github": "agent-secrets"}
	answers.AgentControlPlanes = map[string]model.AgentControlPlane{"github": githubAgentControlPlane()}
	answers.Workflow.Reviewers[0].Command = []string{"node", "./reviewer.js"}
	if _, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers); err == nil {
		t.Fatal("Create() accepted a target-controlled reviewer script with provider secrets")
	}

	answers.Workflow.Reviewers[0].TrustedConfigArguments = []int{1}
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatalf("Create() rejected explicitly trusted-config-relative reviewer script: %v", err)
	}
	if len(plan.Unresolved) != 0 || len(plan.Operations) == 0 {
		t.Fatalf("plan = %#v, trusted helper should resolve from config root", plan)
	}
}

func TestCreateRejectsInvalidAgentSecretEnvironments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		environments map[string]string
	}{
		{name: "unknown provider", environments: map[string]string{"circleci": "agent-secrets"}},
		{name: "unsafe name", environments: map[string]string{"github": "agent secrets"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			answers := productionAnswers()
			answers.AgentSecretEnvironments = test.environments
			if _, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers); err == nil {
				t.Fatalf("Create() accepted %s", test.name)
			}
		})
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

func TestManagedProductionPlanRequiresCIAgentHostAndLogin(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	managed := true
	answers.AllowCIChanges = &managed
	answers.CIProviders = []string{"github"}
	answers.CISecretWaivers = map[string]string{"github": "local reviewers"}
	answers.CIAgentRuntime = nil
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "ci_agent_host") || !contains(plan.Unresolved, "ci_agent_login") {
		t.Fatalf("Unresolved = %v, want ci_agent_host and ci_agent_login", plan.Unresolved)
	}
	answers.CIAgentRuntime = &model.CIAgentRuntime{
		Host:             model.AgentHostClaudeCode,
		LoginMethod:      model.AgentLoginAPIKey,
		LoginEnvironment: "ANTHROPIC_API_KEY",
		LoginSecret:      "ANTHROPIC_API_KEY",
	}
	resolved, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if contains(resolved.Unresolved, "ci_agent_host") || contains(resolved.Unresolved, "ci_agent_login") {
		t.Fatalf("Unresolved = %v, agent runtime answers were ignored", resolved.Unresolved)
	}
}

func TestCreateRejectsSecretLikeAgentLoginIdentifiers(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	answers.CIAgentRuntime = &model.CIAgentRuntime{
		Host:             model.AgentHostCodex,
		LoginMethod:      model.AgentLoginAPIKey,
		LoginEnvironment: "OPENAI_API_KEY",
		LoginSecret:      "sk-not-an-identifier",
	}
	if _, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileBaseline, answers); err == nil {
		t.Fatal("Create() accepted a secret-like login identifier")
	}
}

func TestMissingStandardizeCommitsStaysUnresolved(t *testing.T) {
	t.Parallel()
	answers := completeAnswers()
	answers.StandardizeCommits = nil
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileBaseline, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(plan.Unresolved, "standardize_commits") {
		t.Fatalf("Unresolved = %v, want standardize_commits", plan.Unresolved)
	}
}

func completeAnswers() model.Answers {
	falsehood := false
	allowCI := false
	standardize := false
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
		StandardizeCommits:  &standardize,
		CIAgentRuntime: &model.CIAgentRuntime{
			Host:        model.AgentHostOther,
			HostOther:   "test",
			LoginMethod: model.AgentLoginManual,
			LoginReason: "tests use local reviewer argv",
		},
	}
}

func productionAnswers() model.Answers {
	answers := completeAnswers()
	truth := true
	answers.DeploysToProduction = &truth
	answers.ObservationWindow = "30m"
	answers.RollbackOwner = "release-owner"
	answers.ProductionEnvironment = "production"
	return answers
}

func completeWorkflow() *model.WorkflowConfig {
	reviewers := make([]model.ReviewerConfig, 0, len(model.ReviewerRoles))
	for _, role := range model.ReviewerRoles {
		reviewers = append(reviewers, model.ReviewerConfig{
			Role:               role,
			Command:            []string{"review-agent", "--json"},
			TimeoutSeconds:     60,
			FilesystemReadOnly: true,
		})
	}
	command := func(name string, argv ...string) model.CommandSpec {
		return model.CommandSpec{Name: name, Workdir: ".", Command: argv, Required: true, TimeoutSeconds: 60}
	}
	return &model.WorkflowConfig{
		Enabled:      true,
		StaticGuards: guardSet(model.StaticGuardCategories, command),
		TestGuards:   guardSet(model.TestGuardCategories, command),
		Reviewers:    reviewers,
		Correction: model.CorrectionConfig{
			Enabled:             true,
			FilesystemSandboxed: true,
			Command:             []string{"repair-agent", "--receipt", "-"},
			MaxAttempts:         2,
			MaxChangedFiles:     5,
			MaxChangedLines:     200,
			BranchPrefix:        "sam-harness/repair/",
			OpenChangeRequest:   false,
		},
		Artifact: model.ArtifactWorkflow{
			Build:          command("build", "go", "build", "-o", "dist/sam-harness", "./cmd/sam-harness"),
			ArtifactPath:   "dist/sam-harness",
			SBOM:           command("sbom", "syft", "dist/sam-harness", "-o", "spdx-json=dist/sbom.json"),
			SBOMPath:       "dist/sbom.json",
			Provenance:     command("provenance", "provenance", "dist/sam-harness"),
			ProvenancePath: "dist/provenance.json",
		},
		Deployment: model.DeploymentWorkflow{
			Staging:           command("staging", "deploy", "staging"),
			Production:        command("production", "deploy", "production"),
			Rollback:          command("rollback", "deploy", "rollback"),
			HealthChecks:      []model.CommandSpec{command("health", "check", "health")},
			ObservationChecks: []model.CommandSpec{command("observe", "check", "metrics")},
			CanaryPercentages: []int{10, 50, 100},
		},
		Migration:       []model.CommandSpec{command("migration", "migrate", "up")},
		ReleaseSchedule: model.ReleaseSchedule{Cron: "0 9 * * 1", Timezone: "UTC"},
	}
}

func trustWorkflowCommands(workflow *model.WorkflowConfig) {
	for index := range workflow.Reviewers {
		workflow.Reviewers[index].TrustedExternalCommand = true
	}
	workflow.Correction.TrustedExternalCommand = true
}

func githubAgentControlPlane() model.AgentControlPlane {
	return model.AgentControlPlane{
		Mode:                model.AgentControlPlaneModeGitHubApp,
		RequiredCheck:       "sam-harness/review",
		AppIDSecret:         "SAM_HARNESS_APP_ID",
		AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY",
	}
}

func guardSet(categories []string, command func(string, ...string) model.CommandSpec) model.GuardSet {
	guards := model.GuardSet{
		Commands: make(map[string]model.CommandSpec, len(categories)),
		Waivers:  map[string]string{},
	}
	for _, category := range categories {
		guards.Commands[category] = command(category, "guard", category)
	}
	return guards
}

func TestPlanExposesScanDetectedGuardDefaultsWithoutInventingArgv(t *testing.T) {
	t.Parallel()
	scan := model.ScanResult{
		Root:        t.TempDir(),
		Fingerprint: "fingerprint",
		Stacks: []model.Stack{{
			Kind:           "typescript",
			Path:           ".",
			PackageManager: "npm",
			Commands: map[string][]string{
				"lint":      {"npm", "run", "lint"},
				"typecheck": {"npm", "run", "typecheck"},
				"test":      {"npm", "run", "test"},
			},
		}},
	}
	answers := productionAnswers()
	plan, err := Create(scan, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("Operations = %d, want 0 until confirmation", len(plan.Operations))
	}
	for _, category := range []string{model.GuardLint, model.GuardTypecheck, model.GuardUnit} {
		spec, ok := plan.ProposedGuardDefaults[category]
		if !ok || len(spec.Command) == 0 {
			t.Fatalf("ProposedGuardDefaults missing %s: %#v", category, plan.ProposedGuardDefaults)
		}
	}
	if _, ok := plan.ProposedGuardDefaults[model.GuardIntegration]; ok {
		t.Fatalf("invented argv for integration: %#v", plan.ProposedGuardDefaults)
	}
	if _, ok := plan.ProposedGuardDefaults[model.GuardE2E]; ok {
		t.Fatalf("invented argv for e2e: %#v", plan.ProposedGuardDefaults)
	}
	if !contains(plan.Unresolved, "workflow.test_guards.unit") || !contains(plan.Unresolved, "workflow.static_guards.lint") {
		t.Fatalf("Unresolved = %v, want detected categories to stay unresolved until confirmation", plan.Unresolved)
	}
}

func TestConfirmedGuardDefaultsDecideMatchingCategoriesOnly(t *testing.T) {
	t.Parallel()
	scan := model.ScanResult{
		Root:        t.TempDir(),
		Fingerprint: "fingerprint",
		Stacks: []model.Stack{{
			Kind: "typescript", Path: ".", PackageManager: "npm",
			Commands: map[string][]string{
				"lint":      {"npm", "run", "lint"},
				"typecheck": {"npm", "run", "typecheck"},
				"test":      {"npm", "run", "test"},
			},
		}},
	}
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	delete(answers.Workflow.StaticGuards.Commands, model.GuardLint)
	delete(answers.Workflow.StaticGuards.Commands, model.GuardTypecheck)
	delete(answers.Workflow.TestGuards.Commands, model.GuardUnit)
	answers.ConfirmGuardDefaults = []string{model.GuardLint, model.GuardTypecheck, model.GuardUnit}
	plan, err := Create(scan, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if contains(plan.Unresolved, "workflow.static_guards.lint") || contains(plan.Unresolved, "workflow.test_guards.unit") {
		t.Fatalf("confirmed defaults stayed unresolved: %v", plan.Unresolved)
	}
	if len(plan.Operations) == 0 {
		t.Fatal("confirmed defaults did not produce a plan")
	}
	if plan.Answers.Workflow.TestGuards.Commands[model.GuardUnit].Command[0] != "npm" {
		t.Fatalf("confirmed unit command = %#v", plan.Answers.Workflow.TestGuards.Commands[model.GuardUnit])
	}
}

func TestCoreAdoptionPhaseProducesPlanWhileLaterSlotsStayDeferred(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.AdoptionPhase = model.AdoptionPhaseCore
	answers.Workflow = coreWorkflow()
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) == 0 {
		t.Fatalf("core phase produced no operations; unresolved=%v deferred=%v", plan.Unresolved, plan.Deferred)
	}
	if len(plan.Unresolved) != 0 {
		t.Fatalf("Unresolved = %v, want core phase to be complete", plan.Unresolved)
	}
	for _, item := range []string{"workflow.artifact.build", "workflow.deployment.staging", "workflow.migration"} {
		if !contains(plan.Deferred, item) {
			t.Fatalf("Deferred = %v, want %s", plan.Deferred, item)
		}
		if contains(plan.Unresolved, item) {
			t.Fatalf("later-phase %s blocked the core plan", item)
		}
	}
}

func TestEmptyCorePhaseStillBlocks(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.AdoptionPhase = model.AdoptionPhaseCore
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Operations) != 0 {
		t.Fatalf("Operations = %d, want empty first phase to block", len(plan.Operations))
	}
	if !contains(plan.Unresolved, "workflow.enabled") && !contains(plan.Unresolved, "workflow.static_guards.unit") && !contains(plan.Unresolved, "workflow.static_guards.format") {
		t.Fatalf("Unresolved = %v, want core slots to block", plan.Unresolved)
	}
	if !contains(plan.Deferred, "workflow.artifact.build") {
		t.Fatalf("Deferred = %v, want later-phase artifact to stay deferred", plan.Deferred)
	}
}

func TestConfirmRuntimeReviewersFillsHostRecipe(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	answers.Workflow.Reviewers = nil
	answers.CIAgentRuntime = &model.CIAgentRuntime{Host: model.AgentHostCodex, LoginMethod: model.AgentLoginManual, LoginReason: "test"}
	truth := true
	answers.ConfirmRuntimeReviewers = &truth
	answers.ReviewTimeoutSeconds = 120
	plan, err := Create(model.ScanResult{Root: t.TempDir(), Fingerprint: "fingerprint"}, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if contains(plan.Unresolved, "workflow.reviewers.architecture") {
		t.Fatalf("runtime reviewers stayed unresolved: %v", plan.Unresolved)
	}
	if len(plan.Answers.Workflow.Reviewers) != len(model.ReviewerRoles) {
		t.Fatalf("reviewers = %#v", plan.Answers.Workflow.Reviewers)
	}
	if plan.Answers.Workflow.Reviewers[0].TimeoutSeconds != 120 || plan.Answers.Workflow.Reviewers[0].Command[0] != "npx" {
		t.Fatalf("recipe = %#v", plan.Answers.Workflow.Reviewers[0])
	}
}

func TestProductionUIRequiresBrowserCommandOrWaiver(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	answers.Workflow = completeWorkflow()
	answers.DesignSourceOfTruth = "repository"
	scan := model.ScanResult{
		Root: t.TempDir(), Fingerprint: "fingerprint", HasUI: true,
		Stacks: []model.Stack{{Kind: "typescript", Path: ".", UI: true, Commands: map[string][]string{
			"browser": {"npx", "playwright", "test"},
		}}},
	}
	blocked, err := Create(scan, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(blocked.Unresolved, "design.browser") || !contains(blocked.Unresolved, "design.accessibility") {
		t.Fatalf("Unresolved = %v, want UI proof decisions", blocked.Unresolved)
	}
	if len(blocked.ProposedBrowserCommand) == 0 {
		t.Fatal("missing proposed browser command")
	}
	answers.ConfirmGuardDefaults = []string{"browser"}
	answers.AccessibilityWaiver = "no axe command in this fixture"
	resolved, err := Create(scan, model.ProfileProduction, answers)
	if err != nil {
		t.Fatal(err)
	}
	if contains(resolved.Unresolved, "design.browser") || contains(resolved.Unresolved, "design.accessibility") {
		t.Fatalf("confirmed UI proof stayed unresolved: %v", resolved.Unresolved)
	}
}

func coreWorkflow() *model.WorkflowConfig {
	workflow := completeWorkflow()
	workflow.AdoptionPhase = model.AdoptionPhaseCore
	workflow.Artifact = model.ArtifactWorkflow{}
	workflow.Deployment = model.DeploymentWorkflow{}
	workflow.Migration = nil
	workflow.ReleaseSchedule = model.ReleaseSchedule{}
	return workflow
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
