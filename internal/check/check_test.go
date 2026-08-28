package check

import (
	"os"
	"path/filepath"
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
