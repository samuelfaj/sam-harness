package status

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/pipeline"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

func TestEvaluateDoesNotPromoteLaterStatesFromLocalChecks(t *testing.T) {
	root := t.TempDir()
	writeMinimalConfig(t, root)
	initGit(t, root)
	fingerprint, err := repo.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	report := model.CheckReport{
		HarnessVersion: model.HarnessVersion,
		Root:           root,
		Profile:        model.ProfileBaseline,
		Fingerprint:    fingerprint,
		Passed:         true,
		CreatedAt:      time.Now().UTC(),
	}
	writeJSON(t, filepath.Join(root, ".sam-harness", "evidence", "check.json"), report)

	got, err := Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if !StateByName(got, StateSource).Proven {
		t.Fatalf("source should be proven: %#v", got)
	}
	if !StateByName(got, StateLocalChecks).Proven {
		t.Fatalf("local_checks should be proven from the check receipt: %#v", got)
	}
	for _, name := range []string{StateReview, StateCI, StateArtifact, StateDeployment, StateLiveProof} {
		state := StateByName(got, name)
		if state.Proven {
			t.Fatalf("%s was promoted from a local-check receipt: %#v", name, state)
		}
	}
}

func TestEvaluateRequiresMatchingPipelineReceiptsPerState(t *testing.T) {
	root := t.TempDir()
	writeMinimalConfig(t, root)
	initGit(t, root)
	fingerprint, err := repo.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(root, ".sam-harness", "evidence", "artifact.json"), pipeline.Receipt{
		HarnessVersion: model.HarnessVersion,
		Kind:           "pipeline",
		Phase:          model.PhaseArtifact,
		Fingerprint:    fingerprint,
		Passed:         true,
		Status:         pipeline.StatusPassed,
	})
	got, err := Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if !StateByName(got, StateArtifact).Proven {
		t.Fatal("artifact receipt did not prove artifact")
	}
	if StateByName(got, StateDeployment).Proven || StateByName(got, StateLiveProof).Proven || StateByName(got, StateReview).Proven {
		t.Fatalf("later or sibling states were inferred from artifact: %#v", got)
	}
}

func writeMinimalConfig(t *testing.T, root string) {
	t.Helper()
	path := filepath.Join(root, ".sam-harness", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema_version: \"1\"\n" +
		"harness_version: \"" + model.HarnessVersion + "\"\n" +
		"profile: baseline\n" +
		"repository: fixture\n" +
		"stacks: []\n" +
		"gates: []\n" +
		"authority:\n  write_repository: true\n  network: false\n  commit: false\n  push: false\n  release: false\n  deploy: false\n" +
		"evidence:\n  receipt_directory: .sam-harness/evidence\n  required_states: [source]\n" +
		"ci:\n  providers: []\n  managed: false\n  branch_protection_required: false\n" +
		"release:\n  immutable_artifact: false\n  sbom: false\n  provenance: false\n  promotion_required: false\n" +
		"migration:\n  required: false\n  reconciliation_gate: false\n  restore_test: false\n" +
		"design:\n  applicable: false\n  browser_proof: false\n  human_labels: false\n  accessibility: false\n  localization: false\n" +
		"governance:\n  approvers: [owner]\n  criticality: low\n  data_sensitivity: public\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func initGit(t *testing.T, root string) {
	t.Helper()
	commands := [][]string{
		{"git", "init", "-q"},
		{"git", "add", "."},
		{"git", "-c", "user.name=sam-harness", "-c", "user.email=sam-harness@example.invalid", "commit", "-qm", "fixture"},
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, output)
		}
	}
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
