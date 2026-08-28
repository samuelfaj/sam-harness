package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestAnswersFromConfigPreservesProductionUpgradeDecisions(t *testing.T) {
	t.Parallel()
	cfg := model.Config{
		Profile: model.ProfileProduction,
		Release: model.ReleaseConfig{
			RollbackOwner:         "release-owner",
			ObservationWindow:     "30 minutes",
			ProductionEnvironment: "production-us",
		},
		Governance: model.GovernanceConfig{
			Criticality:     "medium",
			DataSensitivity: "internal",
			Approvers:       []string{"release-owner"},
		},
	}
	answers := answersFromConfig(cfg)
	if answers.ProductionEnvironment != "production-us" || answers.RollbackOwner != "release-owner" || answers.ObservationWindow != "30 minutes" {
		t.Fatalf("answersFromConfig() lost production decisions: %#v", answers)
	}
}

func TestPipelineAndRepairCommandsArePubliclyRouted(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "static.sh"), []byte("#!/bin/sh\ntest \"$SAM_HARNESS_PIPELINE_PHASE\" = static\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := baselineCLIConfig()
	cfg.Gates = []model.Gate{{Name: "static", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./static.sh"}, Required: true}}
	writeCLIConfig(t, root, cfg)

	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"pipeline", root, "--phase", "static", "--receipt", "false"}); err != nil {
		t.Fatalf("pipeline CLI failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "PASSED static") || strings.Contains(stdout.String(), "Receipt:") {
		t.Fatalf("pipeline output = %q", stdout.String())
	}
	if err := command.Run([]string{"pipeline", root, "--phase", "unknown"}); err == nil || !strings.Contains(err.Error(), "--phase") {
		t.Fatalf("invalid pipeline phase error = %v", err)
	}
	if err := command.Run([]string{"repair", root}); err == nil || !strings.Contains(err.Error(), "--receipt is required") {
		t.Fatalf("repair routing error = %v", err)
	}
}

func TestPipelineCLIUsesTrustedConfigOverrideAndHelpDocumentsIt(t *testing.T) {
	root := t.TempDir()
	trustedMarker := filepath.Join(t.TempDir(), "trusted-ran")
	exfilMarker := filepath.Join(t.TempDir(), "pr-config-ran")
	if err := os.WriteFile(filepath.Join(root, "trusted.sh"), []byte("#!/bin/sh\nprintf trusted > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "exfil.sh"), []byte("#!/bin/sh\nprintf exfiltrated > \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prConfig := baselineCLIConfig()
	prConfig.Gates = []model.Gate{{Name: "pr-controlled", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./exfil.sh", exfilMarker}, Required: true}}
	writeCLIConfig(t, root, prConfig)
	trustedConfig := baselineCLIConfig()
	trustedConfig.Gates = []model.Gate{{Name: "trusted", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./trusted.sh", trustedMarker}, Required: true}}
	trustedPath := filepath.Join(t.TempDir(), "trusted.yaml")
	writeCLIConfigAt(t, trustedPath, trustedConfig)

	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"pipeline", root, "--config", trustedPath, "--phase", "static", "--receipt", "false"}); err != nil {
		t.Fatalf("pipeline --config failed: %v", err)
	}
	if got, err := os.ReadFile(trustedMarker); err != nil || string(got) != "trusted" {
		t.Fatalf("trusted CLI config did not govern execution: %q err=%v", got, err)
	}
	if _, err := os.Stat(exfilMarker); !os.IsNotExist(err) {
		t.Fatalf("PR config governed CLI execution: %v", err)
	}
	if err := command.Run([]string{"repair", root, "--config", trustedPath}); err == nil || !strings.Contains(err.Error(), "--receipt is required") {
		t.Fatalf("repair --config was not publicly parsed: %v", err)
	}
	stdout.Reset()
	if err := command.Run([]string{"help"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "pipeline [path] [--config absolute-or-contained-file]") ||
		!strings.Contains(stdout.String(), "--review-base absolute-directory") ||
		!strings.Contains(stdout.String(), "--review-base-sha hex --review-head-sha hex") ||
		!strings.Contains(stdout.String(), "repair [path] [--config absolute-or-contained-file]") ||
		!strings.Contains(stdout.String(), "--config defaults to <path>/.sam-harness/config.yaml") {
		t.Fatalf("CLI help omits trusted config syntax:\n%s", stdout.String())
	}
}

func TestUpgradeAcceptsCompleteAnswersForLegacyProductionConfig(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/legacy\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := baselineCLIConfig()
	legacy.HarnessVersion = "0.1.0"
	legacy.Profile = model.ProfileProduction
	legacy.Stacks = []model.Stack{{
		Kind: "go", Path: ".", PackageManager: "go", Commands: map[string][]string{
			"build": {"go", "build", "./..."}, "test": {"go", "test", "./..."}, "typecheck": {"go", "vet", "./..."},
		},
	}}
	legacy.Authority = model.Authority{WriteRepository: true, Network: true, Release: true, Deploy: true}
	legacy.CI = model.CIConfig{Providers: []string{"github"}, Managed: true, BranchProtectionRequired: true}
	legacy.Release = model.ReleaseConfig{
		ImmutableArtifact: true, SBOM: true, Provenance: true, PromotionRequired: true,
		RollbackOwner: "release-owner", ObservationWindow: "30 minutes", ProductionEnvironment: "production",
	}
	legacy.Governance.Criticality = "medium"
	writeCLIConfig(t, root, legacy)

	answers := answersFromConfig(legacy)
	answers.Workflow = cliWorkflow(true)
	answers.CISecretWaivers = map[string]string{"github": "fixture uses no external reviewer credentials"}
	answersPath := filepath.Join(root, "answers.json")
	writeCLIJSON(t, answersPath, answers)
	outputPath := filepath.Join(t.TempDir(), "upgrade-plan.json")
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"upgrade", root, "--to", "0.2.0", "--answers", answersPath, "--output", outputPath, "--format", "json"}); err != nil {
		t.Fatalf("upgrade failed: %v\n%s", err, stdout.String())
	}
	var response struct {
		Plan model.Plan `json:"plan"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		t.Fatalf("upgrade output is not JSON: %v\n%s", err, stdout.String())
	}
	if len(response.Plan.Unresolved) != 0 || len(response.Plan.Operations) == 0 || response.Plan.Answers.Workflow == nil {
		t.Fatalf("legacy upgrade remained inapplicable: %#v", response.Plan)
	}
	if response.Plan.Answers.Workflow.StaticGuards.Waivers[model.GuardFormat] == "" {
		t.Fatalf("upgrade lost supplied workflow guards: %#v", response.Plan.Answers.Workflow)
	}
}

func TestUpgradeHumanOutputNamesUnresolvedDecisions(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/legacy\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy := baselineCLIConfig()
	legacy.HarnessVersion = "0.1.0"
	legacy.Profile = model.ProfileProduction
	legacy.Stacks = []model.Stack{{
		Kind: "go", Path: ".", PackageManager: "go", Commands: map[string][]string{
			"build": {"go", "build", "./..."}, "test": {"go", "test", "./..."}, "typecheck": {"go", "vet", "./..."},
		},
	}}
	legacy.Authority = model.Authority{WriteRepository: true, Network: true, Release: true, Deploy: true}
	legacy.CI = model.CIConfig{Providers: []string{"github"}, Managed: true, BranchProtectionRequired: true}
	legacy.Release = model.ReleaseConfig{
		ImmutableArtifact: true, SBOM: true, Provenance: true, PromotionRequired: true,
		RollbackOwner: "release-owner", ObservationWindow: "30 minutes", ProductionEnvironment: "production",
	}
	legacy.Governance.Criticality = "medium"
	writeCLIConfig(t, root, legacy)

	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"upgrade", root, "--to", "0.2.0", "--output", filepath.Join(t.TempDir(), "upgrade-plan.json")}); err != nil {
		t.Fatalf("upgrade failed: %v\n%s", err, stdout.String())
	}
	for _, expected := range []string{"Unresolved decisions:", "workflow", "No repository files were planned. Collect answers and run upgrade again."} {
		if !strings.Contains(stdout.String(), expected) {
			t.Fatalf("human upgrade output omitted %q:\n%s", expected, stdout.String())
		}
	}
}

func TestAnswersFromConfigDeepCopiesWorkflow(t *testing.T) {
	cfg := baselineCLIConfig()
	cfg.Workflow = cliWorkflow(false)
	cfg.CI.AgentSecretEnvironments = map[string]string{"github": "agent-review"}
	cfg.CI.AgentControlPlanes = map[string]model.AgentControlPlane{
		"github": {
			Mode:                model.AgentControlPlaneModeGitHubApp,
			RequiredCheck:       "sam-harness/trusted-review",
			AppIDSecret:         "SAM_HARNESS_GITHUB_APP_ID",
			AppPrivateKeySecret: "SAM_HARNESS_GITHUB_APP_PRIVATE_KEY",
		},
	}
	answers := answersFromConfig(cfg)
	cfg.Workflow.StaticGuards.Waivers[model.GuardFormat] = "changed"
	cfg.Workflow.Artifact.Build.Command[0] = "changed"
	cfg.CI.AgentSecretEnvironments["github"] = "changed"
	cfg.CI.AgentControlPlanes["github"] = model.AgentControlPlane{Mode: model.AgentControlPlaneModeExternal, RequiredCheck: "changed"}
	if answers.Workflow.StaticGuards.Waivers[model.GuardFormat] == "changed" ||
		answers.Workflow.Artifact.Build.Command[0] == "changed" ||
		answers.AgentSecretEnvironments["github"] == "changed" ||
		answers.AgentControlPlanes["github"].RequiredCheck != "sam-harness/trusted-review" {
		t.Fatalf("answers share workflow storage with config: %#v", answers.Workflow)
	}
}

func baselineCLIConfig() model.Config {
	return model.Config{
		SchemaVersion:  model.SchemaVersion,
		HarnessVersion: model.HarnessVersion,
		Profile:        model.ProfileBaseline,
		Repository:     "fixture",
		Authority:      model.Authority{WriteRepository: true},
		Evidence:       model.Evidence{ReceiptDirectory: ".sam-harness/evidence", RequiredStates: []string{"source"}},
		Governance: model.GovernanceConfig{
			Approvers: []string{"owner"}, Criticality: "low", DataSensitivity: "public",
		},
	}
}

func cliWorkflow(correctionEnabled bool) *model.WorkflowConfig {
	command := func(name string) model.CommandSpec {
		return model.CommandSpec{Name: name, Workdir: ".", Command: []string{"go", "version"}, Required: true, TimeoutSeconds: 5}
	}
	waivers := func(categories []string) model.GuardSet {
		values := make(map[string]string, len(categories))
		for _, category := range categories {
			values[category] = "explicit legacy upgrade waiver"
		}
		return model.GuardSet{Commands: map[string]model.CommandSpec{}, Waivers: values}
	}
	reviewers := make([]model.ReviewerConfig, 0, len(model.ReviewerRoles))
	for _, role := range model.ReviewerRoles {
		reviewers = append(reviewers, model.ReviewerConfig{Role: role, Command: []string{"reviewer", string(role)}, TimeoutSeconds: 60, FilesystemReadOnly: true})
	}
	workflow := &model.WorkflowConfig{
		Enabled:      true,
		StaticGuards: waivers(model.StaticGuardCategories),
		TestGuards:   waivers(model.TestGuardCategories),
		Reviewers:    reviewers,
		Artifact: model.ArtifactWorkflow{
			Build: command("build"), ArtifactPath: "out/app.bin",
			SBOM: command("sbom"), SBOMPath: "out/sbom.json",
			Provenance: command("provenance"), ProvenancePath: "out/provenance.json",
		},
		Deployment: model.DeploymentWorkflow{
			Staging: command("staging"), Production: command("production"), Rollback: command("rollback"),
			HealthChecks: []model.CommandSpec{command("health")}, ObservationChecks: []model.CommandSpec{command("observe")}, CanaryPercentages: []int{10, 100},
		},
		Migration: []model.CommandSpec{command("migration")}, ReleaseSchedule: model.ReleaseSchedule{Cron: "0 9 * * 1", Timezone: "UTC"},
	}
	if correctionEnabled {
		workflow.Correction = model.CorrectionConfig{
			Enabled: true, FilesystemSandboxed: true, Command: []string{"repairer"}, MaxAttempts: 2, MaxChangedFiles: 5, MaxChangedLines: 100, BranchPrefix: "sam-repair/",
		}
	}
	return workflow
}

func writeCLIConfig(t *testing.T, root string, cfg model.Config) {
	t.Helper()
	writeCLIConfigAt(t, filepath.Join(root, ".sam-harness", "config.yaml"), cfg)
}

func writeCLIConfigAt(t *testing.T, path string, cfg model.Config) {
	t.Helper()
	data, err := config.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeCLIJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}
