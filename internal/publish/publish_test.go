package publish

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
)

type fakeRunner struct {
	calls [][]string
}

func (f *fakeRunner) Run(dir, name string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	joined := strings.Join(append([]string{name}, args...), " ")
	switch {
	case strings.HasPrefix(joined, "git symbolic-ref"):
		return "origin/main", nil
	case strings.HasPrefix(joined, "git rev-parse"):
		return "abc123def456", nil
	case strings.HasPrefix(joined, "gh pr create"):
		return "https://github.com/example/repo/pull/9", nil
	default:
		return "", nil
	}
}

func TestRunRequiresCommitPushNetworkAndRefusesDefaultBranch(t *testing.T) {
	root := t.TempDir()
	cfg := testPublishConfig(false)
	writePublishConfig(t, root, cfg)
	runner := &fakeRunner{}
	_, err := Run(Request{Root: root, Branch: "sam-harness/change", Title: "feat: x", Paths: []string{"README.md"}, Runner: runner})
	if err == nil || !strings.Contains(err.Error(), "commit, push, and network") {
		t.Fatalf("missing authority: %v", err)
	}
	cfg.Authority = model.Authority{Commit: true, Push: true, Network: true}
	writePublishConfig(t, root, cfg)
	_, err = Run(Request{Root: root, Branch: "main", Title: "feat: x", Paths: []string{"README.md"}, Runner: runner})
	if err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("default branch: %v", err)
	}
}

func TestRunPushesFeatureBranchAndOpensPullRequest(t *testing.T) {
	root := t.TempDir()
	cfg := testPublishConfig(true)
	writePublishConfig(t, root, cfg)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	result, err := Run(Request{
		Root:   root,
		Branch: "sam-harness/change",
		Title:  "feat: example",
		Paths:  []string{"README.md"},
		Body:   "Evidence ladder remains unproven for CI.",
		Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.HeadSHA != "abc123def456" || result.URL == "" || result.Branch != "sam-harness/change" {
		t.Fatalf("result = %#v", result)
	}
	joined := ""
	for _, call := range runner.calls {
		joined += strings.Join(call, " ") + "\n"
	}
	if !strings.Contains(joined, "git push -u origin sam-harness/change") {
		t.Fatalf("did not push feature branch:\n%s", joined)
	}
	if strings.Contains(joined, "git push") && strings.Contains(joined, " origin main") && strings.Contains(joined, "git push -u origin main") {
		t.Fatalf("pushed default branch:\n%s", joined)
	}
}

func testPublishConfig(grant bool) model.Config {
	return model.Config{
		SchemaVersion:  model.SchemaVersion,
		HarnessVersion: model.HarnessVersion,
		Profile:        model.ProfileBaseline,
		Repository:     "fixture",
		Authority:      model.Authority{WriteRepository: true, Network: grant, Commit: grant, Push: grant},
		Evidence:       model.Evidence{ReceiptDirectory: ".sam-harness/evidence", RequiredStates: []string{"source"}},
		Governance:     model.GovernanceConfig{Approvers: []string{"owner"}, Criticality: "low", DataSensitivity: "public"},
	}
}

func writePublishConfig(t *testing.T, root string, cfg model.Config) {
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
