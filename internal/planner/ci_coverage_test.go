package planner

import (
	"reflect"
	"strings"
	"testing"

	harnessconfig "github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestResolveCommandsOmitsExactClientCICoverage(t *testing.T) {
	scan := model.ScanResult{
		Stacks: []model.Stack{{Kind: "typescript", Path: "frontend", Commands: map[string][]string{
			"build": {"bun", "run", "build"},
			"test":  {"bun", "run", "test"},
		}}},
		CICommands: []model.CICommand{{
			Provider: "gitlab", File: ".gitlab-ci.yml", Job: "build-frontend",
			Workdir: "frontend", Command: []string{"bun", "run", "build"},
		}},
	}
	resolved, questions, err := resolveCommands(scan, model.Answers{})
	if err != nil {
		t.Fatal(err)
	}
	if len(questions) != 0 {
		t.Fatalf("questions = %v", questions)
	}
	commands := resolved.Stacks[0].Commands
	if _, exists := commands["build"]; exists {
		t.Fatalf("duplicate build remained: %#v", commands)
	}
	if !reflect.DeepEqual(commands["test"], []string{"bun", "run", "test"}) {
		t.Fatalf("non-equivalent test command removed: %#v", commands)
	}
	if len(resolved.ExternalCICoverage) != 1 || resolved.ExternalCICoverage[0].StackKind != "typescript" || resolved.ExternalCICoverage[0].Job != "build-frontend" {
		t.Fatalf("coverage = %#v", resolved.ExternalCICoverage)
	}
}

func TestCreateRepresentsExternalCoverageWithoutGeneratingDuplicateGate(t *testing.T) {
	scan := model.ScanResult{
		Root:        t.TempDir(),
		Fingerprint: "fingerprint",
		CIProviders: []string{"gitlab"},
		Stacks: []model.Stack{{Kind: "typescript", Path: "frontend", Commands: map[string][]string{
			"build": {"bun", "run", "build"},
			"test":  {"bun", "run", "test"},
		}}},
		CICommands: []model.CICommand{{
			Provider: "gitlab", File: ".gitlab-ci.yml", Job: "build-frontend",
			Workdir: "frontend", Command: []string{"bun", "run", "build"},
		}},
	}
	plan, err := Create(scan, model.ProfileBaseline, completeAnswers())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Unresolved) != 0 {
		t.Fatalf("unresolved = %v", plan.Unresolved)
	}
	var configContent, gatesContent string
	for _, operation := range plan.Operations {
		switch operation.Path {
		case ".sam-harness/config.yaml":
			configContent = operation.Content
		case ".sam-harness/GATES.md":
			gatesContent = operation.Content
		}
	}
	cfg, err := harnessconfig.Parse([]byte(configContent))
	if err != nil {
		t.Fatalf("generated config: %v\n%s", err, configContent)
	}
	if len(cfg.CI.ExternalCoverage) != 1 || cfg.CI.ExternalCoverage[0].Gate != "build" {
		t.Fatalf("external coverage = %#v", cfg.CI.ExternalCoverage)
	}
	if len(cfg.Gates) != 1 || cfg.Gates[0].Name != "typescript:frontend:test" {
		t.Fatalf("local gates = %#v, want only non-equivalent test", cfg.Gates)
	}
	if !strings.Contains(gatesContent, "Externally covered commands") || !strings.Contains(gatesContent, "build-frontend") {
		t.Fatalf("generated gates omit external evidence:\n%s", gatesContent)
	}
}

func TestResolveCommandsKeepsSameCommandInDifferentWorkdir(t *testing.T) {
	scan := model.ScanResult{
		Stacks: []model.Stack{{Kind: "typescript", Path: "frontend", Commands: map[string][]string{
			"build": {"bun", "run", "build"},
		}}},
		CICommands: []model.CICommand{{
			Provider: "gitlab", File: ".gitlab-ci.yml", Job: "build-root",
			Workdir: ".", Command: []string{"bun", "run", "build"},
		}},
	}
	resolved, _, err := resolveCommands(scan, model.Answers{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resolved.Stacks[0].Commands["build"], []string{"bun", "run", "build"}) {
		t.Fatalf("different-workdir command removed: %#v", resolved.Stacks[0].Commands)
	}
	if len(resolved.ExternalCICoverage) != 0 {
		t.Fatalf("coverage = %#v, want none", resolved.ExternalCICoverage)
	}
}
