package pipeline

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/jev"
	"github.com/samuelfaj/sam-harness/internal/model"
)

func triageReviewFixture(t *testing.T) (root, base, markerDir string) {
	t.Helper()
	root = t.TempDir()
	markerDir = t.TempDir()
	writeFile(t, root, "source.txt", "base\n")
	writeExecutable(t, root, "review.sh", `#!/bin/sh
cat >/dev/null
touch `+markerDir+`/"$SAM_HARNESS_REVIEW_ROLE"
printf '{"review_complete":true,"findings":[]}\n'
`)
	cfg := testPipelineConfig()
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
	}
	writePipelineConfig(t, root, cfg)
	base = t.TempDir()
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "source.txt", "head\n")
	return root, base, markerDir
}

func runTriageReview(t *testing.T, root, base string) (Receipt, error) {
	t.Helper()
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)
	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA})
	return receipt, err
}

func TestReviewTriageSkipsLowProbabilityRole(t *testing.T) {
	var gotState struct {
		State struct {
			ChangedFiles []string `json:"changed_files"`
			Patch        string   `json:"patch"`
		} `json:"state"`
		Questions map[string]json.RawMessage `json:"questions"`
	}
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotState)
		answers := map[string]any{}
		for id := range gotState.Questions {
			p := 0.8
			if id == string(model.ReviewerSecurity) {
				p = 0.02
			}
			answers[id] = map[string]any{"type": "noul", "noul": p}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "typesafe/jev-test", "answers": answers})
	}))
	defer server.Close()
	t.Setenv(jev.EnvKey, "triage-key")
	t.Setenv(jev.EnvBaseURL, server.URL)
	root, base, markers := triageReviewFixture(t)
	receipt, err := runTriageReview(t, root, base)
	if err != nil {
		t.Fatalf("review failed: %v\n%#v", err, receipt)
	}
	if auth != "Bearer triage-key" || len(gotState.Questions) != len(model.ReviewerRoles) {
		t.Fatalf("unexpected jev request: auth=%q questions=%d", auth, len(gotState.Questions))
	}
	if len(gotState.State.ChangedFiles) != 1 || gotState.State.ChangedFiles[0] != "source.txt" || !strings.Contains(gotState.State.Patch, "+head") {
		t.Fatalf("unexpected triage state: %#v", gotState.State)
	}
	if _, err := os.Stat(filepath.Join(markers, string(model.ReviewerSecurity))); !os.IsNotExist(err) {
		t.Fatal("skipped security reviewer was launched")
	}
	for _, role := range model.ReviewerRoles {
		if role == model.ReviewerSecurity {
			continue
		}
		if _, err := os.Stat(filepath.Join(markers, string(role))); err != nil {
			t.Fatalf("reviewer %s did not run: %v", role, err)
		}
	}
	if len(receipt.Commands) != len(model.ReviewerRoles) {
		t.Fatalf("commands = %d", len(receipt.Commands))
	}
	for index, role := range model.ReviewerRoles {
		command := receipt.Commands[index]
		if command.Name != "review:"+string(role) || !command.Passed || !command.Required {
			t.Fatalf("command %d = %#v", index, command)
		}
		if role == model.ReviewerSecurity && !strings.Contains(command.Output, "skipped by Jev triage: p=0.020 model=typesafe/jev-test") {
			t.Fatalf("skip output = %q", command.Output)
		}
	}
	skipped := 0
	for _, decision := range receipt.ReviewTriage {
		if decision.Skipped {
			skipped++
			if decision.Role != string(model.ReviewerSecurity) || decision.Model != "typesafe/jev-test" {
				t.Fatalf("unexpected decision %#v", decision)
			}
		}
	}
	if len(receipt.ReviewTriage) != len(model.ReviewerRoles) || skipped != 1 {
		t.Fatalf("triage = %#v", receipt.ReviewTriage)
	}
}

func TestReviewTriageErrorSkipsNothing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer server.Close()
	t.Setenv(jev.EnvKey, "triage-key")
	t.Setenv(jev.EnvBaseURL, server.URL)
	root, base, markers := triageReviewFixture(t)
	receipt, err := runTriageReview(t, root, base)
	if err != nil {
		t.Fatalf("review failed: %v\n%#v", err, receipt)
	}
	for _, role := range model.ReviewerRoles {
		if _, err := os.Stat(filepath.Join(markers, string(role))); err != nil {
			t.Fatalf("reviewer %s did not run after jev failure: %v", role, err)
		}
	}
	if len(receipt.ReviewTriage) != len(model.ReviewerRoles) {
		t.Fatalf("triage = %#v", receipt.ReviewTriage)
	}
	for _, decision := range receipt.ReviewTriage {
		if decision.Skipped || !strings.Contains(decision.Error, "500") || strings.Contains(decision.Error, "triage-key") {
			t.Fatalf("unexpected decision %#v", decision)
		}
	}
}

// An unbound key must never reach repository-controlled agent commands.
func TestScopedEnvironmentPassesModelsButNotUnboundKey(t *testing.T) {
	t.Setenv(jev.EnvKey, "key-value")
	t.Setenv(jev.EnvModel, "model-value")
	t.Setenv(jev.EnvJevModel, "jev-value")
	environment, secrets, err := scopedCommandEnvironment(model.Config{}, model.CISecretScopeReview, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{jev.EnvModel + "=model-value", jev.EnvJevModel + "=jev-value"} {
		if !containsString(environment, want) {
			t.Fatalf("missing %s in %v", want, environment)
		}
	}
	for _, entry := range environment {
		if strings.Contains(entry, "key-value") {
			t.Fatalf("unbound key leaked: %v", environment)
		}
	}
	if len(secrets) != 0 {
		t.Fatalf("secrets = %v", secrets)
	}
}

func TestScopedEnvironmentAllowsOnlyOpenRouterKeyBinding(t *testing.T) {
	t.Setenv(jev.EnvKey, "key-value")
	cfg := model.Config{}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{
		"github": {{Scope: model.CISecretScopeReview, Environment: jev.EnvKey, Secret: "OPENROUTER"}},
	}
	environment, secrets, err := scopedCommandEnvironment(cfg, model.CISecretScopeReview, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range environment {
		if strings.HasPrefix(entry, jev.EnvKey+"=") {
			count++
		}
	}
	if count != 1 || !containsString(secrets, "key-value") {
		t.Fatalf("duplicate key entry: %v secrets=%v", environment, secrets)
	}
	cfg.CI.SecretBindings["github"][0].Environment = "SAM_HARNESS_OTHER"
	if _, _, err := scopedCommandEnvironment(cfg, model.CISecretScopeReview, t.TempDir()); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("SAM_HARNESS_OTHER accepted: %v", err)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
