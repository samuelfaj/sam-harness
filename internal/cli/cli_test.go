package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/bootstrap"
	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

func TestVersionPrintsHarnessVersion(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"version"}); err != nil {
		t.Fatal(err)
	}
	want := "sam-harness " + model.HarnessVersion + "\n"
	if stdout.String() != want {
		t.Fatalf("version = %q, want %q", stdout.String(), want)
	}
	if model.HarnessVersion != "0.6.0" {
		t.Fatalf("HarnessVersion = %q, want 0.6.0 for this release", model.HarnessVersion)
	}
}

func TestBootstrapScriptsDefaultToHarnessVersion(t *testing.T) {
	t.Parallel()
	scripts := filepath.Join("..", "..", "skills", "sam-harness", "scripts")
	sh, err := os.ReadFile(filepath.Join(scripts, "bootstrap.sh"))
	if err != nil {
		t.Fatal(err)
	}
	ps1, err := os.ReadFile(filepath.Join(scripts, "bootstrap.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	shDefault := "SAM_HARNESS_VERSION:-" + model.HarnessVersion
	if !strings.Contains(string(sh), shDefault) {
		t.Fatalf("bootstrap.sh default missing %q", shDefault)
	}
	psDefault := `else { "` + model.HarnessVersion + `" }`
	if !strings.Contains(string(ps1), psDefault) {
		t.Fatalf("bootstrap.ps1 default missing %q", psDefault)
	}
}

func TestUsageDocumentsV03Commands(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"help"}); err != nil {
		t.Fatal(err)
	}
	output := stdout.String()
	for _, want := range []string{
		"sam-harness onboard",
		"sam-harness adopt --auto",
		"sam-harness adopt --guided",
		"sam-harness bootstrap github",
		"sam-harness bootstrap gitlab",
		"sam-harness stage classifier|context|planning|implementation|review|repair",
		"sam-harness freeze check",
		"sam-harness status",
		"sam-harness publish",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("help omitted %q:\n%s", want, output)
		}
	}
}

func TestStatusCLIDoesNotPromoteLaterStates(t *testing.T) {
	root := t.TempDir()
	writeCLIConfig(t, root, baselineCLIConfig())
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"status", root}); err != nil {
		t.Fatalf("status failed: %v\n%s", err, stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "source: proven") {
		t.Fatalf("status omitted proven source:\n%s", output)
	}
	for _, name := range []string{"review: unproven", "ci: unproven", "artifact: unproven", "deployment: unproven", "live_proof: unproven"} {
		if !strings.Contains(output, name) {
			t.Fatalf("status omitted %q:\n%s", name, output)
		}
	}
}

func TestStageCLIRunsClassifierFromInputFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := repo.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(t.TempDir(), "stage-input.json")
	writeCLIJSON(t, inputPath, map[string]any{
		"stage":       "classifier",
		"plan_id":     "cli-stage-plan",
		"fingerprint": fingerprint,
		"root":        root,
		"authority": map[string]bool{
			"write_repository": true, "network": false, "commit": false, "push": false, "release": false, "deploy": false,
		},
		"input": map[string]any{"paths": []string{"AGENTS.md"}},
	})
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"stage", "classifier", "--input", inputPath, "--format", "json"}); err != nil {
		t.Fatalf("stage CLI failed: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"plan_id": "cli-stage-plan"`) || !strings.Contains(stdout.String(), `"proof": false`) {
		t.Fatalf("stage output = %s", stdout.String())
	}
}

func TestFreezeCLIBlocksOrdinaryFeatureInsideWindow(t *testing.T) {
	policyPath := filepath.Join(t.TempDir(), "freeze.json")
	writeCLIJSON(t, policyPath, map[string]any{
		"timezone":   "UTC",
		"start":      "2026-12-20T00:00:00Z",
		"end":        "2026-12-27T00:00:00Z",
		"branches":   []string{"main"},
		"owner":      "release-owner",
		"kind":       "production",
		"exceptions": []string{"P0", "release-fix"},
	})
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	err := command.Run([]string{
		"freeze", "check",
		"--policy", policyPath,
		"--now", "2026-12-22T12:00:00Z",
		"--head", "abc123",
		"--kind", "feature",
	})
	if err == nil || !strings.Contains(err.Error(), "freeze") {
		t.Fatalf("freeze CLI error = %v", err)
	}
	exceptionPath := filepath.Join(t.TempDir(), "exception.json")
	writeCLIJSON(t, exceptionPath, map[string]any{
		"class":         "P0",
		"severity":      "critical",
		"reference":     "INC-1",
		"scope":         []string{"cmd/sam-harness"},
		"rollback_plan": "revert abc123",
		"approvers":     []string{"sre-oncall"},
		"expires_at":    "2026-12-22T14:00:00Z",
		"head_sha":      "abc123",
		"approved_at":   "2026-12-22T11:00:00Z",
	})
	if err := command.Run([]string{
		"freeze", "check",
		"--policy", policyPath,
		"--now", "2026-12-22T12:00:00Z",
		"--head", "abc123",
		"--branch", "main",
		"--kind", "feature",
		"--exception", exceptionPath,
	}); err != nil {
		t.Fatalf("freeze CLI rejected a complete exception: %v", err)
	}
	if err := command.Run([]string{
		"freeze", "check",
		"--policy", policyPath,
		"--now", "2026-12-22T12:00:00Z",
		"--head", "stale-head",
		"--branch", "main",
		"--exception", exceptionPath,
	}); err == nil || !strings.Contains(err.Error(), "stale head") {
		t.Fatalf("freeze CLI stale head error = %v", err)
	}
}

func TestOnboardAdoptAutoAndGuidedPrintPlanBeforeWrite(t *testing.T) {
	stacks := []string{"go", "python", "rust", "typescript", "full-flow"}
	commands := [][]string{
		{"onboard"},
		{"adopt", "--auto"},
		{"adopt", "--guided"},
	}
	for _, stack := range stacks {
		stack := stack
		for _, argv := range commands {
			argv := argv
			t.Run(stack+"/"+strings.Join(argv, "_"), func(t *testing.T) {
				root := copyCLIFixture(t, stack)
				answersPath := filepath.Join(t.TempDir(), "answers.json")
				if stack == "full-flow" {
					copyCLIFile(t, filepath.Join("..", "..", "testdata", "fixtures", "full-flow", "answers.production.json"), answersPath)
				} else {
					writeCLIJSON(t, answersPath, baselineCLIAnswers(stack))
				}
				planOut := filepath.Join(t.TempDir(), "plan.json")
				var stdout bytes.Buffer
				command := New(&stdout, &bytes.Buffer{})
				args := append(append([]string{}, argv...), root, "--answers", answersPath, "--output", planOut, "--answers-output", filepath.Join(t.TempDir(), "answers-out.json"))
				if err := command.Run(args); err != nil {
					t.Fatalf("%v failed: %v\n%s", argv, err, stdout.String())
				}
				if _, err := os.Stat(filepath.Join(root, ".sam-harness", "config.yaml")); !os.IsNotExist(err) {
					t.Fatalf("repository changed before --accept: %v", err)
				}
				out := stdout.String()
				for _, want := range []string{"Plan ID:", "Operations:", "Authority:", "Gates:"} {
					if !strings.Contains(out, want) {
						t.Fatalf("missing plan-before-write %q:\n%s", want, out)
					}
				}
			})
		}
	}
}

func TestAdoptApplyThenIdempotentSecondApply(t *testing.T) {
	root := copyCLIFixture(t, "go")
	unrelated := filepath.Join(root, "UNRELATED.txt")
	if err := os.WriteFile(unrelated, []byte("keep-me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	answersPath := filepath.Join(t.TempDir(), "answers.json")
	writeCLIJSON(t, answersPath, baselineCLIAnswers("go"))
	planOut := filepath.Join(t.TempDir(), "plan.json")
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", planOut}); err != nil {
		t.Fatalf("guided plan failed: %v\n%s", err, stdout.String())
	}
	planID := planIDFromOutput(t, stdout.String())
	stdout.Reset()
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", planOut, "--accept", planID}); err != nil {
		t.Fatalf("guided apply failed: %v\n%s", err, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(root, ".sam-harness", "config.yaml")); err != nil {
		t.Fatalf("apply did not write config: %v", err)
	}
	got, err := os.ReadFile(unrelated)
	if err != nil || string(got) != "keep-me\n" {
		t.Fatalf("unrelated file mutated: %q err=%v", got, err)
	}
	stdout.Reset()
	secondPlan := filepath.Join(t.TempDir(), "plan2.json")
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", secondPlan}); err != nil {
		t.Fatalf("second plan failed: %v\n%s", err, stdout.String())
	}
	secondID := planIDFromOutput(t, stdout.String())
	if secondID == planID {
		t.Fatal("second plan reused the stale plan ID")
	}
	stdout.Reset()
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", secondPlan, "--accept", secondID}); err != nil {
		t.Fatalf("second apply failed: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), "No files changed") {
		t.Fatalf("second apply was not a no-op:\n%s", stdout.String())
	}
}

func TestAdoptImplementSecurityGuardViaCLI(t *testing.T) {
	root := copyCLIFixture(t, "go")
	unrelated := filepath.Join(root, "UNRELATED.txt")
	if err := os.WriteFile(unrelated, []byte("keep-me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	answersPath := filepath.Join(t.TempDir(), "answers.json")
	writeCLIJSON(t, answersPath, baselineCLIAnswers("go"))
	harnessPlan := filepath.Join(t.TempDir(), "harness.json")
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", harnessPlan}); err != nil {
		t.Fatal(err)
	}
	harnessID := planIDFromOutput(t, stdout.String())
	stdout.Reset()
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", harnessPlan, "--accept", harnessID}); err != nil {
		t.Fatal(err)
	}
	taskPlan := filepath.Join(t.TempDir(), "security.json")
	stdout.Reset()
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", taskPlan, "--implement", "guard:security"}); err != nil {
		t.Fatal(err)
	}
	taskID := planIDFromOutput(t, stdout.String())
	if taskID == harnessID {
		t.Fatal("bounded task reused the harness plan ID")
	}
	stdout.Reset()
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", taskPlan, "--implement", "guard:security", "--accept", harnessID}); err == nil {
		t.Fatal("stale harness plan ID was accepted for the bounded task")
	}
	stdout.Reset()
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", taskPlan, "--implement", "guard:security", "--accept", taskID, "--format", "json"}); err != nil {
		t.Fatalf("implement apply failed: %v\n%s", err, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"state": "existing-and-validated"`) || !strings.Contains(stdout.String(), "guard:security") {
		t.Fatalf("coverage did not validate security guard:\n%s", stdout.String())
	}
	script := filepath.Join(root, ".sam-harness", "guards", "security.sh")
	if info, err := os.Stat(script); err != nil || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("security script missing or not executable: %v", err)
	}
	got, err := os.ReadFile(unrelated)
	if err != nil || string(got) != "keep-me\n" {
		t.Fatalf("unrelated file mutated: %q err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(root, "leaked.env"), []byte("TOKEN=ghp_exampletokenvalue0001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	checkPlan := filepath.Join(t.TempDir(), "after-secret.json")
	if err := command.Run([]string{"adopt", "--guided", root, "--answers", answersPath, "--output", checkPlan}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "guard:security existing-and-validated") {
		t.Fatalf("CLI coverage treated a secret-bearing tree as validated:\n%s", stdout.String())
	}
}

func TestBootstrapAcceptUsesDefaultTransportWithoutInjection(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/cli-bootstrap-default\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if command.BootstrapTransport != nil {
		t.Fatal("New() injected a test transport")
	}
	if err := command.Run([]string{"bootstrap", "github", root, "--format", "json"}); err != nil {
		t.Fatal(err)
	}
	var plan bootstrap.Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	err := command.Run([]string{"bootstrap", "github", root, "--accept", plan.ID})
	if err == nil {
		t.Fatal("default transport applied provider policy without live readback")
	}
	if strings.Contains(err.Error(), "injected provider transport") {
		t.Fatalf("CLI.New still requires test-only injection: %v", err)
	}
}

func TestBootstrapReadbackMismatchFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/cli-bootstrap-drift\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"bootstrap", "github", root, "--format", "json"}); err != nil {
		t.Fatal(err)
	}
	var plan bootstrap.Plan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	command.BootstrapTransport = &scriptedTransport{reads: []bootstrap.RemoteState{{DefaultBranch: "main"}, {DefaultBranch: "main"}}}
	stdout.Reset()
	err := command.Run([]string{"bootstrap", "github", root, "--accept", plan.ID})
	if err == nil || !strings.Contains(err.Error(), "readback mismatch") {
		t.Fatalf("bootstrap mismatch error = %v", err)
	}
}

func TestScanAndPlanDoNotWriteTrackedFiles(t *testing.T) {
	root := copyCLIFixture(t, "go")
	answersPath := filepath.Join(t.TempDir(), "answers.json")
	writeCLIJSON(t, answersPath, baselineCLIAnswers("go"))
	before := treeListing(t, root)
	command := New(io.Discard, io.Discard)
	if err := command.Run([]string{"scan", root}); err != nil {
		t.Fatal(err)
	}
	if err := command.Run([]string{"plan", root, "--answers", answersPath, "--output", filepath.Join(t.TempDir(), "plan.json")}); err != nil {
		t.Fatal(err)
	}
	after := treeListing(t, root)
	if before != after {
		t.Fatalf("scan/plan mutated the tree\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestBootstrapGitHubAndGitLabUseInjectedTransport(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/cli-bootstrap\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, provider := range []string{"github", "gitlab"} {
		provider := provider
		t.Run(provider, func(t *testing.T) {
			var stdout bytes.Buffer
			command := New(&stdout, &bytes.Buffer{})
			if err := command.Run([]string{"bootstrap", provider, root, "--format", "json"}); err != nil {
				t.Fatalf("bootstrap plan failed: %v\n%s", err, stdout.String())
			}
			if command.BootstrapTransport != nil {
				t.Fatal("planning called the transport")
			}
			var plan bootstrap.Plan
			if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
				t.Fatalf("plan JSON: %v\n%s", err, stdout.String())
			}
			if plan.ID == "" || len(plan.Mutations) == 0 {
				t.Fatalf("empty bootstrap plan: %#v", plan)
			}
			transport := &scriptedTransport{reads: []bootstrap.RemoteState{{DefaultBranch: "main"}, plan.Desired}}
			command.BootstrapTransport = transport
			stdout.Reset()
			if err := command.Run([]string{"bootstrap", provider, root, "--accept", plan.ID}); err != nil {
				t.Fatalf("bootstrap apply failed: %v\n%s", err, stdout.String())
			}
			if len(transport.applied) != 1 {
				t.Fatalf("Apply calls = %d, want 1", len(transport.applied))
			}
			stdout.Reset()
			if err := command.Run([]string{"bootstrap", provider, root, "--accept", plan.ID}); err != nil {
				t.Fatalf("idempotent bootstrap failed: %v\n%s", err, stdout.String())
			}
			if !strings.Contains(stdout.String(), "No provider mutations applied") {
				t.Fatalf("second bootstrap was not idempotent:\n%s", stdout.String())
			}
		})
	}
}

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
	standardize := false
	answers.StandardizeCommits = &standardize
	answers.CIAgentRuntime = &model.CIAgentRuntime{
		Host:        model.AgentHostOther,
		HostOther:   "fixture",
		LoginMethod: model.AgentLoginManual,
		LoginReason: "fixture uses no external reviewer credentials",
	}
	answersPath := filepath.Join(root, "answers.json")
	writeCLIJSON(t, answersPath, answers)
	outputPath := filepath.Join(t.TempDir(), "upgrade-plan.json")
	var stdout bytes.Buffer
	command := New(&stdout, &bytes.Buffer{})
	if err := command.Run([]string{"upgrade", root, "--to", model.HarnessVersion, "--answers", answersPath, "--output", outputPath, "--format", "json"}); err != nil {
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
	if err := command.Run([]string{"upgrade", root, "--to", model.HarnessVersion, "--output", filepath.Join(t.TempDir(), "upgrade-plan.json")}); err != nil {
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

func baselineCLIAnswers(stack string) map[string]any {
	answers := map[string]any{
		"criticality":           "low",
		"data_sensitivity":      "public",
		"deploys_to_production": false,
		"persistent_data":       false,
		"irreversible_actions":  false,
		"approvers":             []string{"owner"},
		"allow_ci_changes":      false,
		"allowed_actions":       []string{"write_repository"},
		"standardize_commits":   false,
	}
	if stack == "typescript" {
		answers["design_source_of_truth"] = "repository"
	}
	return answers
}

func copyCLIFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyCLIFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("..", "..", "testdata", "fixtures", name)
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func planIDFromOutput(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "Plan ID: ") {
			id := strings.TrimSpace(strings.TrimPrefix(line, "Plan ID: "))
			if id != "" {
				return id
			}
		}
	}
	t.Fatalf("no Plan ID in output:\n%s", output)
	return ""
}

func treeListing(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		lines = append(lines, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

type scriptedTransport struct {
	reads   []bootstrap.RemoteState
	index   int
	applied [][]bootstrap.Mutation
}

func (s *scriptedTransport) Read() (bootstrap.RemoteState, error) {
	if len(s.reads) == 0 {
		return bootstrap.RemoteState{}, nil
	}
	if s.index >= len(s.reads) {
		return s.reads[len(s.reads)-1], nil
	}
	state := s.reads[s.index]
	s.index++
	return state, nil
}

func (s *scriptedTransport) Apply(mutations []bootstrap.Mutation) error {
	cloned := append([]bootstrap.Mutation(nil), mutations...)
	s.applied = append(s.applied, cloned)
	return nil
}
