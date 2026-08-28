package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestRunReturnsSpecificEvidenceForPassingAndFailingGates(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig()
	writeConfig(t, root, cfg)
	report, _, err := Run(root, false)
	if err == nil {
		t.Fatal("Run() succeeded with a required failing gate")
	}
	if len(report.Results) != 2 || !report.Results[0].Passed || report.Results[1].Passed {
		t.Fatalf("results = %#v", report.Results)
	}
	if report.Results[1].ExitCode == 0 {
		t.Fatal("failing gate has exit code 0")
	}
	for _, result := range report.Results {
		if result.Duration <= 0 || result.FinishedAt.IsZero() || result.FinishedAt.Before(result.StartedAt) {
			t.Fatalf("gate timing was not recorded: %#v", result)
		}
		if result.Duration > time.Minute {
			t.Fatalf("unexpected gate duration: %s", result.Duration)
		}
	}
}

func TestContainedPathRejectsTraversalAndSymbolicLinks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if _, err := containedPath(root, "../outside"); err == nil {
		t.Fatal("containedPath() accepted traversal")
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := containedPath(root, "linked/evidence"); err == nil {
		t.Fatal("containedPath() accepted a symbolic-link parent")
	}
}

func TestRunResolvesRelativeExecutableAgainstGateWorkdir(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "local-check.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Gates = []model.Gate{{
		Name:     "relative",
		Stage:    "local",
		Workdir:  ".",
		Command:  []string{"./local-check.sh"},
		Required: true,
	}}
	writeConfig(t, root, cfg)
	report, _, err := Run(root, false)
	if err != nil {
		t.Fatalf("Run() failed for a repository-relative executable: %v\n%#v", err, report.Results)
	}
	if len(report.Results) != 1 || !report.Results[0].Passed {
		t.Fatalf("relative gate did not pass: %#v", report.Results)
	}
}

func TestRunIncludesExecutedAndWaivedWorkflowGuards(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "guard.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Gates = nil
	cfg.Workflow = validWorkflow()
	cfg.Workflow.StaticGuards.Commands[model.GuardFormat] = model.CommandSpec{
		Name: "format", Workdir: ".", Command: []string{"./guard.sh"}, Required: true, TimeoutSeconds: 5,
	}
	delete(cfg.Workflow.StaticGuards.Waivers, model.GuardFormat)
	writeConfig(t, root, cfg)

	report, _, err := Run(root, false)
	if err != nil {
		t.Fatalf("Run() failed: %v\n%#v", err, report)
	}
	if !report.Passed || len(report.Results) != len(model.StaticGuardCategories)+len(model.TestGuardCategories) {
		t.Fatalf("guard report = %#v", report)
	}
	if !report.Results[0].Passed || report.Results[0].Skipped {
		t.Fatalf("executed guard was not recorded as a pass: %#v", report.Results[0])
	}
	if !report.Results[1].Skipped || report.Results[1].Passed || report.Results[1].Output == "" {
		t.Fatalf("waiver was disguised as execution: %#v", report.Results[1])
	}
}

func TestRunReportsPhaseBoundaryWhenPassingGateMutatesRepository(t *testing.T) {
	root := t.TempDir()
	script := filepath.Join(root, "mutate.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'generated\\n' > generated.txt\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Gates = []model.Gate{{
		Name: "passing command with forbidden side effect", Stage: "local", Workdir: ".",
		Command: []string{"./mutate.sh"}, Required: true,
	}}
	writeConfig(t, root, cfg)

	report, _, err := Run(root, false)
	if err == nil {
		t.Fatal("Run() succeeded after a passing gate mutated the repository")
	}
	if report.Passed {
		t.Fatal("report.Passed = true after a phase boundary failure")
	}
	if len(report.Results) != 2 || !report.Results[0].Passed || report.Results[1].Passed {
		t.Fatalf("results = %#v, want a passing command followed by a failing phase boundary", report.Results)
	}
	boundary := report.Results[1]
	if boundary.Name != "static phase boundary" || !boundary.Required || boundary.ExitCode != -1 {
		t.Fatalf("boundary result = %#v", boundary)
	}
	if !strings.Contains(boundary.Output, "mutated the repository") {
		t.Fatalf("boundary output = %q, want the specific mutation error", boundary.Output)
	}
	if boundary.StartedAt.IsZero() || boundary.FinishedAt.Before(boundary.StartedAt) {
		t.Fatalf("boundary timing was not recorded: %#v", boundary)
	}
}

func testConfig() model.Config {
	return model.Config{
		SchemaVersion:  model.SchemaVersion,
		HarnessVersion: model.HarnessVersion,
		Profile:        model.ProfileBaseline,
		Repository:     "fixture",
		Stacks:         []model.Stack{},
		Gates: []model.Gate{
			{Name: "pass", Stage: "local", Workdir: ".", Command: []string{"go", "version"}, Required: true},
			{Name: "fail", Stage: "local", Workdir: ".", Command: []string{"go", "tool", "definitely-not-a-tool"}, Required: true},
		},
		Authority: model.Authority{},
		Evidence:  model.Evidence{ReceiptDirectory: ".sam-harness/evidence", RequiredStates: []string{"source"}},
		CI:        model.CIConfig{},
		Release:   model.ReleaseConfig{},
		Migration: model.MigrationConfig{},
		Design:    model.DesignConfig{},
		Governance: model.GovernanceConfig{
			Approvers:       []string{"owner"},
			Criticality:     "low",
			DataSensitivity: "public",
		},
	}
}

func writeConfig(t *testing.T, root string, cfg model.Config) {
	t.Helper()
	data, err := config.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".sam-harness", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func validWorkflow() *model.WorkflowConfig {
	command := func(name string) model.CommandSpec {
		return model.CommandSpec{Name: name, Workdir: ".", Command: []string{"go", "version"}, Required: true, TimeoutSeconds: 5}
	}
	waivers := func(categories []string) model.GuardSet {
		values := make(map[string]string, len(categories))
		for _, category := range categories {
			values[category] = "not applicable to the check fixture"
		}
		return model.GuardSet{Commands: map[string]model.CommandSpec{}, Waivers: values}
	}
	reviewers := make([]model.ReviewerConfig, 0, len(model.ReviewerRoles))
	for _, role := range model.ReviewerRoles {
		reviewers = append(reviewers, model.ReviewerConfig{Role: role, Command: []string{"go", "version"}, TimeoutSeconds: 5, FilesystemReadOnly: true})
	}
	return &model.WorkflowConfig{
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
			HealthChecks: []model.CommandSpec{command("health")}, ObservationChecks: []model.CommandSpec{command("observe")}, CanaryPercentages: []int{100},
		},
		Migration: []model.CommandSpec{command("migration")}, ReleaseSchedule: model.ReleaseSchedule{Cron: "0 9 * * 1", Timezone: "UTC"},
	}
}
