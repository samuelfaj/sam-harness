package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/model"
	"gopkg.in/yaml.v3"
)

func TestBuildPreservesExistingInstructions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Existing rules\n\nKeep this line.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	operations, err := Build(model.ScanResult{Root: root}, model.ProfileBaseline, answers())
	if err != nil {
		t.Fatal(err)
	}
	agents := operationContent(t, operations, "AGENTS.md")
	if !strings.Contains(agents, "Keep this line.") || !strings.Contains(agents, markdownStart) {
		t.Fatalf("managed AGENTS.md did not preserve existing content:\n%s", agents)
	}
}

func TestMergeGitLabIncludePreservesExistingSequence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".gitlab-ci.yml")
	existing := "include:\n  - local: '.gitlab/common.yml'\n\ntest:\n  script: echo test\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err := mergeGitLabInclude(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(merged, ".gitlab/common.yml") || !strings.Contains(merged, ".sam-harness/ci/gitlab.yml") || !strings.Contains(merged, "script: echo test") {
		t.Fatalf("merge lost existing GitLab content:\n%s", merged)
	}
	if err := os.WriteFile(path, []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := mergeGitLabInclude(path)
	if err != nil {
		t.Fatal(err)
	}
	if second != merged {
		t.Fatalf("second GitLab merge changed content:\n%s", second)
	}
}

func TestBuildGeneratesParseableCIWithApprovedSetup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitlab-ci.yml"), []byte("include:\n  - local: '.gitlab/common.yml'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	approved := answers()
	allowCI := true
	approved.AllowCIChanges = &allowCI
	approved.CISetupCommands = map[string][]model.SetupCommand{
		"github": {{Workdir: ".", Command: []string{"npm", "ci"}}},
		"gitlab": {{Workdir: ".", Command: []string{"npm", "ci"}}},
	}
	approved.GitLabImage = "registry.example.test/go-node:1"
	operations, err := Build(model.ScanResult{
		Root:        root,
		CIProviders: []string{"github", "gitlab"},
		Stacks:      []model.Stack{{Kind: "typescript", Path: ".", Commands: map[string][]string{"test": {"npm", "test"}}}},
	}, model.ProfileBaseline, approved)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".github/workflows/sam-harness.yml", ".sam-harness/ci/gitlab.yml", ".gitlab-ci.yml"} {
		content := operationContent(t, operations, path)
		var document any
		if err := yaml.Unmarshal([]byte(content), &document); err != nil {
			t.Fatalf("generated %s is not YAML: %v\n%s", path, err, content)
		}
		if path != ".gitlab-ci.yml" && !strings.Contains(content, "npm") {
			t.Fatalf("generated %s lost approved setup commands", path)
		}
	}
}

func answers() model.Answers {
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

func operationContent(t *testing.T, operations []model.Operation, path string) string {
	t.Helper()
	for _, operation := range operations {
		if operation.Path == path {
			return operation.Content
		}
	}
	t.Fatalf("operation %s not found", path)
	return ""
}
