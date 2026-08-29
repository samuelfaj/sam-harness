package stage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/repo"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const receiptSchemaJSON = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["stage", "plan_id", "fingerprint", "started_at", "finished_at", "risk", "affected_paths", "authority", "output", "proof"],
  "properties": {
    "stage": {"enum": ["classifier", "context", "planning", "implementation", "review", "repair"]},
    "plan_id": {"type": "string", "minLength": 1},
    "fingerprint": {"type": "string", "minLength": 1},
    "started_at": {"type": "string", "minLength": 1},
    "finished_at": {"type": "string", "minLength": 1},
    "risk": {"type": "string"},
    "affected_paths": {"type": "array", "items": {"type": "string"}},
    "authority": {
      "type": "object",
      "required": ["write_repository", "network", "commit", "push", "release", "deploy"],
      "properties": {
        "write_repository": {"type": "boolean"},
        "network": {"type": "boolean"},
        "commit": {"type": "boolean"},
        "push": {"type": "boolean"},
        "release": {"type": "boolean"},
        "deploy": {"type": "boolean"}
      }
    },
    "output": {"type": "object"},
    "summary": {"type": "string"},
    "proof": {"type": "boolean", "const": false}
  }
}`

func TestRunSixStagesEmitSchemaValidReceiptsWithSharedPlanAndFingerprint(t *testing.T) {
	t.Parallel()
	root := contextRepo(t)
	fingerprint := mustFingerprint(t, root)
	planID := "stage-chain-plan"
	paths := []string{"main.go"}
	risk := ""
	inputs := map[string]any{
		Classifier:     map[string]any{"paths": paths, "hints": []string{"docs"}},
		Context:        map[string]any{},
		Planning:       map[string]any{"task": "keep edits inside affected paths"},
		Implementation: map[string]any{"action": "edit"},
		Review:         map[string]any{},
		Repair:         map[string]any{},
	}
	var receipts []Receipt
	for _, name := range []string{Classifier, Context, Planning, Implementation, Review, Repair} {
		rec, err := Run(Request{
			Stage:         name,
			PlanID:        planID,
			Fingerprint:   fingerprint,
			Root:          root,
			Risk:          risk,
			AffectedPaths: paths,
			Input:         mustJSON(t, inputs[name]),
		})
		if err != nil {
			t.Fatalf("Run(%s) = %v", name, err)
		}
		assertSchemaValidReceipt(t, rec)
		if rec.PlanID != planID || rec.Fingerprint != fingerprint {
			t.Fatalf("%s receipt bound to plan %q fingerprint %q, want %q %q", name, rec.PlanID, rec.Fingerprint, planID, fingerprint)
		}
		if rec.Proof {
			t.Fatalf("%s set Proof; a summary is not proof", name)
		}
		assertStageOutput(t, rec)
		risk = rec.Risk
		if len(rec.AffectedPaths) > 0 {
			paths = rec.AffectedPaths
		}
		receipts = append(receipts, rec)
	}
	for i := 1; i < len(receipts); i++ {
		if err := ValidateChain(receipts[i-1], receipts[i]); err != nil {
			t.Fatalf("ValidateChain(%s -> %s) = %v", receipts[i-1].Stage, receipts[i].Stage, err)
		}
	}
}

func TestRunContextListsExistingFiles(t *testing.T) {
	t.Parallel()
	root := contextRepo(t)
	rec, err := Run(baseRequest(t, root, Context, nil))
	if err != nil {
		t.Fatal(err)
	}
	assertSchemaValidReceipt(t, rec)
	out := decodeObject(t, rec.Output)
	if !hasPath(out["instructions"], "AGENTS.md") {
		t.Fatalf("instructions = %v, want AGENTS.md", out["instructions"])
	}
	if !hasPath(out["skills"], "skills/example/SKILL.md") {
		t.Fatalf("skills = %v, want skills/example/SKILL.md", out["skills"])
	}
	if !hasPath(out["source"], "main.go") {
		t.Fatalf("source = %v, want main.go", out["source"])
	}
	if !hasPath(out["tests"], "example_test.go") {
		t.Fatalf("tests = %v, want example_test.go", out["tests"])
	}
	if !hasPath(out["schemas"], "schema/example.json") {
		t.Fatalf("schemas = %v, want schema/example.json", out["schemas"])
	}
	if !hasPath(out["evidence"], ".sam-harness/evidence/run.json") {
		t.Fatalf("evidence = %v, want .sam-harness/evidence/run.json", out["evidence"])
	}
	if _, ok := out["contracts"]; !ok {
		t.Fatal("contracts key missing")
	}
}

func TestRunPlanningIncludesRequiredKeys(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "fixture\n")
	rec, err := Run(baseRequest(t, root, Planning, map[string]any{"task": "add a bounded change"}))
	if err != nil {
		t.Fatal(err)
	}
	assertSchemaValidReceipt(t, rec)
	out := decodeObject(t, rec.Output)
	for _, key := range []string{
		"acceptance_criteria",
		"affected_files",
		"commands",
		"risks",
		"mitigations",
		"budgets",
		"stop_conditions",
		"proof_requirements",
	} {
		requireStringArray(t, out, key)
	}
}

func TestValidateChainRejectsMutatedFingerprintAndSwappedPlanID(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "fixture\n")
	fingerprint := mustFingerprint(t, root)
	first, err := Run(Request{
		Stage:       Classifier,
		PlanID:      "plan-a",
		Fingerprint: fingerprint,
		Root:        root,
		Input:       mustJSON(t, map[string]any{"paths": []string{"README.md"}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(Request{
		Stage:       Review,
		PlanID:      "plan-a",
		Fingerprint: fingerprint,
		Root:        root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateChain(first, second); err != nil {
		t.Fatalf("ValidateChain() = %v, want matching plan and fingerprint to pass", err)
	}

	swapped, err := Run(Request{
		Stage:       Review,
		PlanID:      "plan-b",
		Fingerprint: fingerprint,
		Root:        root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateChain(first, swapped); err == nil || !strings.Contains(err.Error(), "plan id") {
		t.Fatalf("ValidateChain() swapped plan id error = %v, want rejection", err)
	}

	mutated := second
	mutated.Fingerprint = fingerprint + "ff"
	if err := ValidateChain(first, mutated); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("ValidateChain() mutated fingerprint error = %v, want rejection", err)
	}

	early := Receipt{
		PlanID:      "plan-a",
		Fingerprint: fingerprint,
		StartedAt:   time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}
	late := Receipt{
		PlanID:      "plan-a",
		Fingerprint: fingerprint,
		FinishedAt:  time.Date(2026, 1, 1, 12, 0, 1, 0, time.UTC),
	}
	if err := ValidateChain(late, early); err == nil {
		t.Fatal("ValidateChain() accepted a next receipt that started before the previous finished")
	}
}

func TestRunImplementationBlockedWithoutAuthority(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "fixture\n")
	fingerprint := mustFingerprint(t, root)
	paths := []string{"README.md"}
	for _, action := range []string{"commit", "push", "pr", "mr", "release", "deploy"} {
		_, err := Run(Request{
			Stage:         Implementation,
			PlanID:        "plan-auth",
			Fingerprint:   fingerprint,
			Root:          root,
			AffectedPaths: paths,
			Input:         mustJSON(t, map[string]any{"action": action}),
		})
		if err == nil || !strings.Contains(err.Error(), "authority") {
			t.Fatalf("Run(action=%s) error = %v, want authority denial", action, err)
		}
	}

	granted := map[string]model.Authority{
		"commit":  {Commit: true},
		"push":    {Push: true},
		"pr":      {Push: true},
		"mr":      {Push: true},
		"release": {Release: true},
		"deploy":  {Deploy: true},
	}
	for action, auth := range granted {
		rec, err := Run(Request{
			Stage:         Implementation,
			PlanID:        "plan-auth",
			Fingerprint:   fingerprint,
			Root:          root,
			AffectedPaths: paths,
			Authority:     auth,
			Input:         mustJSON(t, map[string]any{"action": action}),
		})
		if err != nil {
			t.Fatalf("Run(action=%s) with matching authority: %v", action, err)
		}
		if rec.Proof {
			t.Fatalf("implementation receipt for %s set Proof", action)
		}
		out := decodeObject(t, rec.Output)
		allowed, _ := out["allowed"].(bool)
		if !allowed {
			t.Fatalf("implementation output for %s = %#v, want allowed true", action, out)
		}
	}

	_, err := Run(Request{
		Stage:         Implementation,
		PlanID:        "plan-auth",
		Fingerprint:   fingerprint,
		Root:          root,
		AffectedPaths: paths,
		Authority:     model.Authority{Commit: true},
		Input:         mustJSON(t, map[string]any{"action": "push"}),
	})
	if err == nil || !strings.Contains(err.Error(), "authority") {
		t.Fatalf("commit authority allowed push: %v", err)
	}
}

func TestRunImplementationDoesNotWriteRepository(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	unrelated := filepath.Join(root, "UNRELATED.txt")
	writeFile(t, unrelated, "leave me\n")
	writeFile(t, filepath.Join(root, "target.go"), "package target\n")
	fingerprint := mustFingerprint(t, root)
	before, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(Request{
		Stage:         Implementation,
		PlanID:        "plan-write",
		Fingerprint:   fingerprint,
		Root:          root,
		AffectedPaths: []string{"target.go"},
		Authority:     model.Authority{WriteRepository: true, Commit: true},
		Input:         mustJSON(t, map[string]any{"action": "commit"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(unrelated)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("implementation mutated UNRELATED.txt")
	}
	got, err := repo.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if got != fingerprint {
		t.Fatal("implementation wrote the repository")
	}
}

func TestRunSummaryNeverSetsProof(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "fixture\n")
	for _, name := range []string{Classifier, Context, Planning, Implementation, Review, Repair} {
		rec, err := Run(baseRequest(t, root, name, map[string]any{"paths": []string{"README.md"}, "task": "summarize", "action": "edit"}))
		if err != nil {
			t.Fatalf("Run(%s) = %v", name, err)
		}
		if rec.Summary == "" {
			t.Fatalf("%s omitted summary; the proof test needs a narrative that must still not count as proof", name)
		}
		if rec.Proof {
			t.Fatalf("%s set Proof from a summary", name)
		}
		assertSchemaValidReceipt(t, rec)
	}
}

func TestRunRejectsEmptyIdentityUnknownStageAndFingerprintMismatch(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"), "fixture\n")
	fingerprint := mustFingerprint(t, root)

	if _, err := Run(Request{Stage: Classifier, Fingerprint: fingerprint, Root: root}); err == nil || !strings.Contains(err.Error(), "plan id") {
		t.Fatalf("empty plan id error = %v", err)
	}
	if _, err := Run(Request{Stage: Classifier, PlanID: "plan", Root: root}); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("empty fingerprint error = %v", err)
	}
	if _, err := Run(Request{PlanID: "plan", Fingerprint: fingerprint, Root: root}); err == nil || !strings.Contains(err.Error(), "stage") {
		t.Fatalf("empty stage error = %v", err)
	}
	if _, err := Run(Request{Stage: "deploy", PlanID: "plan", Fingerprint: fingerprint, Root: root}); err == nil || !strings.Contains(err.Error(), "unknown stage") {
		t.Fatalf("unknown stage error = %v", err)
	}
	if _, err := Run(Request{Stage: Classifier, PlanID: "plan", Fingerprint: fingerprint, Root: "   "}); err == nil || !strings.Contains(err.Error(), "root") {
		t.Fatalf("whitespace root error = %v, want root required", err)
	}
	if _, err := Run(Request{Stage: Classifier, PlanID: "plan", Fingerprint: fingerprint + "ab", Root: root}); err == nil || !strings.Contains(err.Error(), "fingerprint") {
		t.Fatalf("mismatched fingerprint error = %v", err)
	}
}

func TestRunClassifierDerivesRiskFromHints(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "prod.go"), "package prod\n")
	rec, err := Run(baseRequest(t, root, Classifier, map[string]any{
		"paths": []string{"prod.go"},
		"hints": []string{"production deploy"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	out := decodeObject(t, rec.Output)
	if out["risk"] != "critical" {
		t.Fatalf("risk = %v, want critical for production deploy hints", out["risk"])
	}
	if rec.Risk != "critical" {
		t.Fatalf("receipt risk = %q, want classified risk", rec.Risk)
	}
}

func contextRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "AGENTS.md"), "# Agents\n")
	writeFile(t, filepath.Join(root, "skills", "example", "SKILL.md"), "# Skill\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	writeFile(t, filepath.Join(root, "example_test.go"), "package main\n")
	writeFile(t, filepath.Join(root, "schema", "example.json"), "{}\n")
	writeFile(t, filepath.Join(root, ".sam-harness", "evidence", "run.json"), "{\"ok\":true}\n")
	return root
}

func baseRequest(t *testing.T, root, stage string, input any) Request {
	t.Helper()
	return Request{
		Stage:         stage,
		PlanID:        "test-plan",
		Fingerprint:   mustFingerprint(t, root),
		Root:          root,
		AffectedPaths: []string{"main.go"},
		Input:         mustJSON(t, input),
	}
}

func mustFingerprint(t *testing.T, root string) string {
	t.Helper()
	fingerprint, err := repo.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	if value == nil {
		return nil
	}
	if raw, ok := value.(json.RawMessage); ok {
		return raw
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeObject(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("output is not an object: %v", err)
	}
	return out
}

func requireStringArray(t *testing.T, obj map[string]any, key string) {
	t.Helper()
	value, ok := obj[key]
	if !ok {
		t.Fatalf("missing key %q", key)
	}
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("%s = %T, want array", key, value)
	}
	for i, item := range items {
		if _, ok := item.(string); !ok {
			t.Fatalf("%s[%d] = %T, want string", key, i, item)
		}
	}
}

func hasPath(value any, want string) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if s, ok := item.(string); ok && s == want {
			return true
		}
	}
	return false
}

func assertStageOutput(t *testing.T, rec Receipt) {
	t.Helper()
	out := decodeObject(t, rec.Output)
	switch rec.Stage {
	case Classifier:
		risk, _ := out["risk"].(string)
		if !validRisk(risk) {
			t.Fatalf("classifier risk = %v", out["risk"])
		}
		requireStringArray(t, out, "affected_paths")
	case Context:
		for _, key := range []string{"instructions", "skills", "source", "contracts", "tests", "schemas", "evidence"} {
			requireStringArray(t, out, key)
		}
	case Planning:
		for _, key := range []string{"acceptance_criteria", "affected_files", "commands", "risks", "mitigations", "budgets", "stop_conditions", "proof_requirements"} {
			requireStringArray(t, out, key)
		}
	case Implementation:
		if allowed, _ := out["allowed"].(bool); !allowed {
			t.Fatalf("implementation output = %#v, want allowed true", out)
		}
		requireStringArray(t, out, "scope")
	case Review:
		requireStringArray(t, out, "findings")
		status, _ := out["status"].(string)
		if status != "pass" && status != "fail" {
			t.Fatalf("review status = %v", out["status"])
		}
	case Repair:
		if _, ok := out["attempted"].(bool); !ok {
			t.Fatalf("repair attempted = %T", out["attempted"])
		}
		if _, ok := out["reason"].(string); !ok {
			t.Fatalf("repair reason = %T", out["reason"])
		}
	default:
		t.Fatalf("unknown stage %q", rec.Stage)
	}
}

func assertSchemaValidReceipt(t *testing.T, rec Receipt) {
	t.Helper()
	if rec.Proof {
		t.Fatal("Proof is true; summaries are not proof")
	}
	if rec.FinishedAt.Before(rec.StartedAt) {
		t.Fatalf("FinishedAt %s is before StartedAt %s", rec.FinishedAt, rec.StartedAt)
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	var schemaDocument any
	if err := json.Unmarshal([]byte(receiptSchemaJSON), &schemaDocument); err != nil {
		t.Fatal(err)
	}
	if err := compiler.AddResource("receipt.schema.json", schemaDocument); err != nil {
		t.Fatal(err)
	}
	compiled, err := compiler.Compile("receipt.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatal(err)
	}
	if err := compiled.Validate(value); err != nil {
		t.Fatalf("receipt failed schema: %v\n%s", err, data)
	}
}
