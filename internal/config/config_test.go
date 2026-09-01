package config

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/model"
	"gopkg.in/yaml.v3"
)

func TestInstalledProductionConfigUsesArchitectureCommandAndCurrentHarness(t *testing.T) {
	t.Parallel()
	cfg, err := Load(filepath.Join("..", "..", ".sam-harness", "config.yaml"))
	if err != nil {
		t.Fatalf("load installed config: %v", err)
	}
	if cfg.HarnessVersion != model.HarnessVersion {
		t.Fatalf("installed harness_version = %q, want %q", cfg.HarnessVersion, model.HarnessVersion)
	}
	if cfg.Workflow == nil {
		t.Fatal("installed production config has no workflow")
	}
	command, ok := cfg.Workflow.StaticGuards.Commands[model.GuardArchitecture]
	if !ok || len(command.Command) == 0 || !command.Required {
		t.Fatalf("architecture is not a required command: %#v", cfg.Workflow.StaticGuards)
	}
	if reason := cfg.Workflow.StaticGuards.Waivers[model.GuardArchitecture]; reason != "" {
		t.Fatalf("architecture is still a waiver: %q", reason)
	}
}

func TestMarshalAndParseValidateThePublicSchema(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Profile != model.ProfileBaseline {
		t.Fatalf("Profile = %q, want baseline", parsed.Profile)
	}
}

func TestParseBackwardCompatibleConfigurationDefaultsGatePhase(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.HarnessVersion = "0.1.0"
	cfg.Gates = []model.Gate{{
		Name:     "go:.:test",
		Stage:    "local",
		Workdir:  ".",
		Command:  []string{"go", "test", "./..."},
		Required: true,
	}}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() rejected a v0.1 configuration: %v", err)
	}
	if parsed.Workflow != nil {
		t.Fatalf("Workflow = %#v, want omitted v0.1 workflow", parsed.Workflow)
	}
	if got := parsed.Gates[0].Phase; got != model.PhaseStatic {
		t.Fatalf("Gate phase = %q, want %q", got, model.PhaseStatic)
	}
}

func TestMarshalAndParseWorkflowPreservesCommandArgv(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Workflow = validWorkflow()
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	got := parsed.Workflow.Artifact.Build.Command
	want := []string{"go", "build", "-o", "dist/sam-harness", "./cmd/sam-harness"}
	if len(got) != len(want) {
		t.Fatalf("build argv = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("build argv = %#v, want %#v", got, want)
		}
	}
}

func TestParseRejectsUnknownProfile(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "profile: baseline", "profile: imaginary", 1))
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() accepted a profile outside the JSON Schema")
	}
}

func TestParseRejectsProductionWithoutOperationalOwnership(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Profile = model.ProfileProduction
	cfg.Release = model.ReleaseConfig{ImmutableArtifact: true, SBOM: true, Provenance: true}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() accepted production without rollback, observation, and environment ownership")
	}
}

func TestParseRejectsPathsOutsideTheRepository(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Evidence.ReceiptDirectory = "../../outside"
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() accepted a receipt directory outside the repository")
	}
}

func TestParseRejectsInvalidWorkflowControls(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*model.Config)
	}{
		{
			name: "phase",
			mutate: func(cfg *model.Config) {
				cfg.Gates = []model.Gate{{Name: "bad", Stage: "local", Phase: "imaginary", Workdir: ".", Command: []string{"true"}, Required: true}}
			},
		},
		{
			name: "reviewer role",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].Role = "style"
			},
		},
		{
			name: "correction budget",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Correction.MaxChangedFiles = 0
			},
		},
		{
			name: "canary order",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Deployment.CanaryPercentages = []int{50, 10, 100}
			},
		},
		{
			name: "absolute command workdir",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Artifact.Build.Workdir = "/tmp"
			},
		},
		{
			name: "escaping command workdir",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Deployment.HealthChecks[0].Workdir = "../outside"
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.Workflow = validWorkflow()
			test.mutate(&cfg)
			data, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Parse(data); err == nil {
				t.Fatalf("Parse() accepted invalid %s", test.name)
			}
		})
	}
}

func TestGuardSetAcceptsEitherCommandOrAuditableWaiver(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Workflow = validWorkflow()
	delete(cfg.Workflow.StaticGuards.Commands, model.GuardSecurity)
	cfg.Workflow.StaticGuards.Waivers[model.GuardSecurity] = "a separate mandatory security scanner covers this repository"
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected an auditable guard waiver: %v", err)
	}
}

func TestParseRejectsInvalidGuardDecisions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*model.Config)
	}{
		{
			name: "missing command and waiver",
			mutate: func(cfg *model.Config) {
				delete(cfg.Workflow.StaticGuards.Commands, model.GuardFormat)
			},
		},
		{
			name: "duplicate command and waiver",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.TestGuards.Waivers[model.GuardUnit] = "duplicate decision"
			},
		},
		{
			name: "empty waiver",
			mutate: func(cfg *model.Config) {
				delete(cfg.Workflow.TestGuards.Commands, model.GuardE2E)
				cfg.Workflow.TestGuards.Waivers[model.GuardE2E] = "  "
			},
		},
		{
			name: "wrong phase category",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.StaticGuards.Commands[model.GuardUnit] = cfg.Workflow.StaticGuards.Commands[model.GuardFormat]
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.Workflow = validWorkflow()
			test.mutate(&cfg)
			if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
				t.Fatalf("Parse() accepted guard set with %s", test.name)
			}
		})
	}
}

func TestParseRejectsProductionWithoutExecutableWorkflow(t *testing.T) {
	t.Parallel()
	cfg := validProductionConfig()
	cfg.Workflow = nil
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() accepted v0.2 production without an executable workflow")
	}

	cfg.HarnessVersion = "0.1.0"
	data, err = yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("Parse() rejected the equivalent legacy production configuration: %v", err)
	}
}

func TestParseRejectsProductionWithDisabledCorrection(t *testing.T) {
	t.Parallel()
	cfg := validProductionConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() accepted production with correction disabled")
	}

	cfg.Profile = model.ProfileBaseline
	cfg.Release = model.ReleaseConfig{}
	cfg.CI.BranchProtectionRequired = false
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected baseline with correction disabled: %v", err)
	}
}

func TestParseRejectsUnsandboxedCorrection(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Workflow = validWorkflow()
	cfg.Workflow.Correction.FilesystemSandboxed = false
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted enabled correction without filesystem sandbox attestation")
	}
}

func TestParseRejectsReviewerWithoutReadOnlyAttestation(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Workflow = validWorkflow()
	cfg.Workflow.Reviewers[0].FilesystemReadOnly = false
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted reviewer without filesystem read-only attestation")
	}
}

func TestParseRejectsOpenChangeRequestWithoutPublisherAuthority(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Workflow = validWorkflow()
	cfg.Workflow.Correction.OpenChangeRequest = true
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted change-request publishing without network, commit, and push authority")
	}
	cfg.Authority.Network = true
	cfg.Authority.Commit = true
	cfg.Authority.Push = true
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected explicitly authorized change-request publishing: %v", err)
	}
}

func TestCISecretBindingNamesRoundTripWithoutValues(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
		"github": {
			{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"},
			{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "CORRECTION_API_KEY"},
		},
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	bindings := parsed.CI.SecretBindings["github"]
	if len(bindings) != 2 || bindings[0].Scope != model.CISecretScopeReview || bindings[1].Scope != model.CISecretScopeRepair {
		t.Fatalf("bindings = %#v, want separate review and repair scopes", bindings)
	}
}

func TestMarshalAndParsePreservesAgentSecretEnvironmentForUpgrade(t *testing.T) {
	t.Parallel()
	cfg := validProductionConfig()
	trustWorkflowCommands(cfg.Workflow)
	cfg.CI.Managed = true
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
		"github": {
			{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"},
			{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "CORRECTION_API_KEY"},
		},
	}
	cfg.CI.AgentSecretEnvironments = map[string]string{"github": "agent-secrets"}
	cfg.CI.AgentControlPlanes = map[string]model.AgentControlPlane{"github": githubAgentControlPlane()}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.CI.AgentSecretEnvironments["github"]; got != "agent-secrets" {
		t.Fatalf("agent secret environment = %q, want agent-secrets", got)
	}
}

func TestMarshalAndParsePreservesAgentControlPlaneForUpgrade(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.AgentControlPlanes = map[string]model.AgentControlPlane{
		"github": githubAgentControlPlane(),
	}
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.CI.AgentControlPlanes["github"]; got != githubAgentControlPlane() {
		t.Fatalf("agent control plane = %#v, want %#v", got, githubAgentControlPlane())
	}
}

func TestGitLabExternalPipelinePolicyRequiresAndPreservesExternalControl(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.CI.Providers = []string{"gitlab"}
	cfg.CI.AgentControlPlanes = map[string]model.AgentControlPlane{
		"gitlab": {
			Mode:            model.AgentControlPlaneModeExternal,
			RequiredCheck:   "sam-harness/trusted-gates",
			ExternalProject: "trusted/review-control",
		},
	}
	cfg.CI.GitLabExternalPipelinePolicy = true

	parsed, err := Parse(mustMarshalYAML(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if !parsed.CI.GitLabExternalPipelinePolicy {
		t.Fatal("GitLab external pipeline policy was not preserved")
	}

	cfg.CI.AgentControlPlanes = nil
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("GitLab external pipeline policy was accepted without external control")
	}
}

func TestParseRejectsInvalidAgentControlPlanes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider string
		control  model.AgentControlPlane
	}{
		{name: "unknown provider", provider: "circleci", control: githubAgentControlPlane()},
		{name: "github wrong mode", provider: "github", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeExternal, RequiredCheck: "sam-harness/review", ExternalProject: "trusted/review"}},
		{name: "github unsafe check", provider: "github", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeGitHubApp, RequiredCheck: "review; publish", AppIDSecret: "SAM_HARNESS_APP_ID", AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY"}},
		{name: "github unsafe app id secret", provider: "github", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeGitHubApp, RequiredCheck: "sam-harness/review", AppIDSecret: "123_APP_ID", AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY"}},
		{name: "github missing private key secret", provider: "github", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeGitHubApp, RequiredCheck: "sam-harness/review", AppIDSecret: "SAM_HARNESS_APP_ID"}},
		{name: "github external project conflict", provider: "github", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeGitHubApp, RequiredCheck: "sam-harness/review", AppIDSecret: "SAM_HARNESS_APP_ID", AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY", ExternalProject: "trusted/review"}},
		{name: "gitlab wrong mode", provider: "gitlab", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeGitHubApp, RequiredCheck: "sam-harness/review", AppIDSecret: "SAM_HARNESS_APP_ID", AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY"}},
		{name: "gitlab unsafe external project", provider: "gitlab", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeExternal, RequiredCheck: "sam-harness/review", ExternalProject: "trusted/../review"}},
		{name: "gitlab app secret conflict", provider: "gitlab", control: model.AgentControlPlane{Mode: model.AgentControlPlaneModeExternal, RequiredCheck: "sam-harness/review", AppIDSecret: "SAM_HARNESS_APP_ID", ExternalProject: "trusted/review"}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controls := map[string]model.AgentControlPlane{test.provider: test.control}
			if err := ValidateCIAgentControlPlanes(controls); err == nil {
				t.Fatalf("ValidateCIAgentControlPlanes() accepted %s", test.name)
			}
			cfg := validConfig()
			cfg.CI.Providers = []string{"github", "gitlab"}
			cfg.CI.AgentControlPlanes = controls
			if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
				t.Fatalf("Parse() accepted %s", test.name)
			}
		})
	}
}

func TestParseRequiresProviderControlPlaneForAgentSecretBindings(t *testing.T) {
	t.Parallel()
	cfg := validSecretBoundProductionConfig()
	cfg.CI.AgentControlPlanes = nil
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted GitHub review and repair secrets without a dedicated GitHub App control plane")
	}
	cfg.CI.AgentControlPlanes = map[string]model.AgentControlPlane{"github": githubAgentControlPlane()}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected a dedicated GitHub App control plane: %v", err)
	}

	cfg.CI.Providers = []string{"gitlab"}
	cfg.Profile = model.ProfileRegulated
	cfg.Governance.Approvers = []string{"release-owner", "security-owner"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
		"gitlab": {
			{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "REVIEW_API_KEY"},
			{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "REPAIR_API_KEY"},
		},
	}
	cfg.CI.AgentSecretEnvironments = map[string]string{"gitlab": "agent-secrets"}
	cfg.CI.AgentControlPlanes = map[string]model.AgentControlPlane{
		"gitlab": {
			Mode:            model.AgentControlPlaneModeExternal,
			RequiredCheck:   "sam-harness/review",
			ExternalProject: "trusted/review-control",
		},
	}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected a GitLab external control plane: %v", err)
	}
}

func TestCIProviderAgentSecretsBoundIncludesReviewAndRepairOnly(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		bindings []model.CISecretBinding
		want     bool
	}{
		{name: "review", bindings: []model.CISecretBinding{{Scope: model.CISecretScopeReview}}, want: true},
		{name: "repair", bindings: []model.CISecretBinding{{Scope: model.CISecretScopeRepair}}, want: true},
		{name: "production", bindings: []model.CISecretBinding{{Scope: model.CISecretScopeProduction}}, want: false},
		{name: "none", want: false},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := CIProviderAgentSecretsBound(test.bindings); got != test.want {
				t.Fatalf("CIProviderAgentSecretsBound() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestParseRejectsInvalidCISecretBindings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*model.Config)
	}{
		{
			name: "unknown provider",
			mutate: func(cfg *model.Config) {
				cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"circleci": {{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "API_KEY"}}}
			},
		},
		{
			name: "all scope",
			mutate: func(cfg *model.Config) {
				cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: "all", Environment: "REVIEW_ENV", Secret: "API_KEY"}}}
			},
		},
		{
			name: "invalid scope",
			mutate: func(cfg *model.Config) {
				cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: "publish", Environment: "REVIEW_ENV", Secret: "API_KEY"}}}
			},
		},
		{
			name: "unsafe environment name",
			mutate: func(cfg *model.Config) {
				cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: model.CISecretScopeReview, Environment: "review-env", Secret: "API_KEY"}}}
			},
		},
		{
			name: "unsafe secret name",
			mutate: func(cfg *model.Config) {
				cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "api.key"}}}
			},
		},
		{
			name: "duplicate scope environment",
			mutate: func(cfg *model.Config) {
				cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
					"github": {
						{Scope: model.CISecretScopeReview, Environment: "AGENT_API_KEY", Secret: "OPENAI_API_KEY"},
						{Scope: model.CISecretScopeReview, Environment: "AGENT_API_KEY", Secret: "ANTHROPIC_API_KEY"},
					},
				}
			},
		},
		{
			name: "empty waiver",
			mutate: func(cfg *model.Config) {
				cfg.CI.SecretWaivers = map[string]string{"github": "  "}
			},
		},
		{
			name: "unknown agent secret environment provider",
			mutate: func(cfg *model.Config) {
				cfg.CI.AgentSecretEnvironments = map[string]string{"circleci": "agent-secrets"}
			},
		},
		{
			name: "unselected agent secret environment provider",
			mutate: func(cfg *model.Config) {
				cfg.CI.AgentSecretEnvironments = map[string]string{"gitlab": "agent-secrets"}
			},
		},
		{
			name: "unsafe agent secret environment",
			mutate: func(cfg *model.Config) {
				cfg.CI.AgentSecretEnvironments = map[string]string{"github": "agent secrets"}
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.CI.Providers = []string{"github"}
			test.mutate(&cfg)
			if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
				t.Fatalf("Parse() accepted %s", test.name)
			}
		})
	}
}

func TestCISecretBindingsMayCoexistWithProviderWaiver(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
		"github": {{Scope: model.CISecretScopeProduction, Environment: "DEPLOY_TOKEN", Secret: "PRODUCTION_DEPLOY_TOKEN"}},
	}
	cfg.CI.SecretWaivers = map[string]string{"github": "agent commands use provider workload identity"}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected bindings plus an agent-authentication waiver: %v", err)
	}
}

func TestParseRejectsProductionManagedAgenticWorkflowWithoutCISecrets(t *testing.T) {
	t.Parallel()
	cfg := validProductionConfig()
	trustWorkflowCommands(cfg.Workflow)
	cfg.CI.Managed = true
	cfg.CI.Providers = []string{"github"}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted managed production agentic CI without secret bindings or waiver")
	}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
		"github": {{Scope: model.CISecretScopeProduction, Environment: "DEPLOY_TOKEN", Secret: "PRODUCTION_DEPLOY_TOKEN"}},
	}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted a production-only binding for agentic review and repair")
	}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
		"github": {{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"}},
	}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted a review binding without the enabled correction repair binding")
	}
	cfg.CI.SecretBindings["github"] = append(cfg.CI.SecretBindings["github"], model.CISecretBinding{
		Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "CORRECTION_API_KEY",
	})
	cfg.CI.SecretWaivers = map[string]string{"github": "workload identity covers fallback agent authentication"}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted agent secret bindings plus waiver without a protected agent environment")
	}
	cfg.CI.AgentSecretEnvironments = map[string]string{"github": "agent-secrets"}
	cfg.CI.AgentControlPlanes = map[string]model.AgentControlPlane{"github": githubAgentControlPlane()}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected separate review and repair bindings with their protected environment: %v", err)
	}
	cfg.CI.SecretBindings = nil
	cfg.CI.AgentSecretEnvironments = nil
	cfg.CI.AgentControlPlanes = nil
	cfg.CI.SecretWaivers = map[string]string{"github": "all configured commands are credential-free"}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() required an agent secret environment for a provider waiver without bindings: %v", err)
	}
}

func TestParseRejectsTargetControlledCommandsWhenAgentSecretsAreBound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*model.Config)
	}{
		{
			name: "reviewer attestation missing",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].TrustedExternalCommand = false
			},
		},
		{
			name: "correction attestation missing",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Correction.TrustedExternalCommand = false
			},
		},
		{
			name: "relative reviewer executable",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].Command = []string{"./reviewer"}
			},
		},
		{
			name: "interpreter target helper without trusted index",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].Command = []string{"node", "reviewer"}
			},
		},
		{
			name: "correction target helper without trusted index",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Correction.Command = []string{"node", "./repair.js"}
			},
		},
		{
			name: "relative schema without trusted index",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].Command = []string{"review-agent", "--schema", "reviewer.schema.json"}
			},
		},
		{
			name: "inline relative schema",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].Command = []string{"review-agent", "--schema=reviewer.schema.json"}
			},
		},
		{
			name: "executable trusted index",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].TrustedConfigArguments = []int{0}
			},
		},
		{
			name: "out of range trusted index",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].TrustedConfigArguments = []int{2}
			},
		},
		{
			name: "duplicate trusted index",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].Command = []string{"review-agent", "reviewer.schema.json"}
				cfg.Workflow.Reviewers[0].TrustedConfigArguments = []int{1, 1}
			},
		},
		{
			name: "escaping trusted helper",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].Command = []string{"review-agent", "../reviewer.schema.json"}
				cfg.Workflow.Reviewers[0].TrustedConfigArguments = []int{1}
			},
		},
		{
			name: "absolute trusted helper",
			mutate: func(cfg *model.Config) {
				cfg.Workflow.Reviewers[0].Command = []string{"review-agent", "/tmp/reviewer.schema.json"}
				cfg.Workflow.Reviewers[0].TrustedConfigArguments = []int{1}
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := validSecretBoundProductionConfig()
			test.mutate(&cfg)
			if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
				t.Fatalf("Parse() accepted %s", test.name)
			}
		})
	}
}

func TestParseAcceptsTrustedConfigRelativeReviewerHelper(t *testing.T) {
	t.Parallel()
	cfg := validSecretBoundProductionConfig()
	cfg.Workflow.Reviewers[0].Command = []string{"review-agent", "--schema", "reviewer.schema.json"}
	cfg.Workflow.Reviewers[0].TrustedConfigArguments = []int{2}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected trusted-config-relative reviewer schema: %v", err)
	}
}

func TestParseAcceptsExactVersionNPXPackageRunner(t *testing.T) {
	t.Parallel()
	tests := [][]string{
		{"npx", "--yes", "@openai/codex@0.150.1", "exec", "--sandbox", "read-only", "--ephemeral", "--output-schema", "reviewer.schema.json", "-"},
		{"npx", "-y", "codex@0.150.1", "exec", "-"},
	}
	for _, command := range tests {
		command := command
		t.Run(command[2], func(t *testing.T) {
			t.Parallel()
			cfg := validSecretBoundProductionConfig()
			cfg.Workflow.Reviewers[0].Command = command
			if len(command) > 5 {
				cfg.Workflow.Reviewers[0].TrustedConfigArguments = []int{8}
			}
			if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
				t.Fatalf("Parse() rejected exact-version npx package: %v", err)
			}
		})
	}
}

func TestParseRejectsUnpinnedOrTargetControlledNPXPackage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                   string
		command                []string
		trustedConfigArguments []int
	}{
		{name: "unversioned scoped package", command: []string{"npx", "--yes", "@openai/codex", "exec"}},
		{name: "latest", command: []string{"npx", "--yes", "@openai/codex@latest", "exec"}},
		{name: "range", command: []string{"npx", "--yes", "@openai/codex@^0.150.1", "exec"}},
		{name: "unsupported pre-package option", command: []string{"npx", "--package", "@openai/codex@0.150.1", "exec"}},
		{name: "additional package", command: []string{"npx", "--yes", "@openai/codex@0.150.1", "--package", "target-package", "exec"}},
		{name: "target helper", command: []string{"npx", "--yes", "@openai/codex@0.150.1", "exec", "./reviewer.js"}},
		{name: "package marked as helper", command: []string{"npx", "--yes", "@openai/codex@0.150.1", "exec"}, trustedConfigArguments: []int{2}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			cfg := validSecretBoundProductionConfig()
			cfg.Workflow.Reviewers[0].Command = test.command
			cfg.Workflow.Reviewers[0].TrustedConfigArguments = test.trustedConfigArguments
			if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
				t.Fatalf("Parse() accepted %s", test.name)
			}
		})
	}
}

func TestParseAllowsTargetHelpersWithoutProviderBoundSecrets(t *testing.T) {
	t.Parallel()
	cfg := validProductionConfig()
	cfg.CI.Managed = true
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretWaivers = map[string]string{"github": "review and repair commands receive no provider secrets"}
	cfg.Workflow.Reviewers[0].Command = []string{"node", "./reviewer.js"}
	cfg.Workflow.Correction.Command = []string{"node", "./repair.js"}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err != nil {
		t.Fatalf("Parse() rejected an explicit no-secret workflow: %v", err)
	}
}

func validConfig() model.Config {
	return model.Config{
		SchemaVersion:  model.SchemaVersion,
		HarnessVersion: model.HarnessVersion,
		Profile:        model.ProfileBaseline,
		Repository:     "fixture",
		Stacks:         []model.Stack{},
		Gates:          []model.Gate{},
		Authority:      model.Authority{},
		Evidence:       model.Evidence{ReceiptDirectory: ".sam-harness/evidence", RequiredStates: []string{"source"}},
		CI:             model.CIConfig{},
		Release:        model.ReleaseConfig{},
		Migration:      model.MigrationConfig{},
		Design:         model.DesignConfig{},
		Governance: model.GovernanceConfig{
			Approvers:       []string{"owner"},
			Criticality:     "low",
			DataSensitivity: "public",
		},
	}
}

func validProductionConfig() model.Config {
	cfg := validConfig()
	cfg.Profile = model.ProfileProduction
	cfg.Release = model.ReleaseConfig{
		ImmutableArtifact:     true,
		SBOM:                  true,
		Provenance:            true,
		PromotionRequired:     true,
		RollbackOwner:         "owner",
		ObservationWindow:     "30m",
		ProductionEnvironment: "production",
	}
	cfg.CI.BranchProtectionRequired = true
	cfg.Workflow = validWorkflow()
	return cfg
}

func validSecretBoundProductionConfig() model.Config {
	cfg := validProductionConfig()
	trustWorkflowCommands(cfg.Workflow)
	cfg.CI.Managed = true
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
		"github": {
			{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "REVIEW_API_KEY"},
			{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "REPAIR_API_KEY"},
		},
	}
	cfg.CI.AgentSecretEnvironments = map[string]string{"github": "agent-secrets"}
	cfg.CI.AgentControlPlanes = map[string]model.AgentControlPlane{"github": githubAgentControlPlane()}
	return cfg
}

func TestParseRejectsIncompleteAgentRuntime(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.CI.AgentRuntime = &model.CIAgentRuntime{Host: model.AgentHostOther, LoginMethod: model.AgentLoginManual}
	if _, err := Parse(mustMarshalYAML(t, cfg)); err == nil {
		t.Fatal("Parse() accepted incomplete agent runtime")
	}
}

func TestMarshalPreservesAgentRuntimeAndCommitConvention(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	enabled := true
	cfg.Governance.StandardizeCommits = &enabled
	cfg.CI.AgentRuntime = &model.CIAgentRuntime{
		Host:             model.AgentHostGrok,
		LoginMethod:      model.AgentLoginAPIKey,
		LoginEnvironment: "XAI_API_KEY",
		LoginSecret:      "XAI_API_KEY",
	}
	parsed, err := Parse(mustMarshalYAML(t, cfg))
	if err != nil {
		t.Fatal(err)
	}
	if parsed.CI.AgentRuntime == nil || parsed.CI.AgentRuntime.Host != model.AgentHostGrok || parsed.CI.AgentRuntime.LoginSecret != "XAI_API_KEY" {
		t.Fatalf("agent runtime = %#v", parsed.CI.AgentRuntime)
	}
	if parsed.Governance.StandardizeCommits == nil || !*parsed.Governance.StandardizeCommits {
		t.Fatal("standardize_commits was dropped")
	}
}

func githubAgentControlPlane() model.AgentControlPlane {
	return model.AgentControlPlane{
		Mode:                model.AgentControlPlaneModeGitHubApp,
		RequiredCheck:       "sam-harness/review",
		AppIDSecret:         "SAM_HARNESS_APP_ID",
		AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY",
	}
}

func mustMarshalYAML(t *testing.T, value any) []byte {
	t.Helper()
	data, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func validWorkflow() *model.WorkflowConfig {
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
