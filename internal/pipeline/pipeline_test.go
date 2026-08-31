package pipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestPhaseExecutionWritesCanonicalReceiptAndRejectsEscapingWorkdir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("SAM_HARNESS_PIPELINE_PHASE", "wrong-parent-value")
	writeExecutable(t, root, "scripts/static.sh", `#!/bin/sh
test "$SAM_HARNESS_PIPELINE_PHASE" = static
printf 'static ran\n'
`)
	if err := os.MkdirAll(filepath.Join(root, "work"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := testPipelineConfig()
	cfg.Gates = []model.Gate{{
		Name:     "static-fixture",
		Stage:    "local",
		Phase:    model.PhaseStatic,
		Workdir:  "work",
		Command:  []string{"../scripts/static.sh"},
		Required: true,
	}}
	writePipelineConfig(t, root, cfg)

	receipt, receiptPath, err := Run(root, model.PhaseStatic, true)
	if err != nil {
		t.Fatalf("Run() failed: %v\n%#v", err, receipt)
	}
	if !receipt.Passed || receipt.Status != StatusPassed || len(receipt.Commands) != 1+len(model.StaticGuardCategories) || receipt.Commands[0].Name != "static-fixture" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if receipt.Repository != "fixture" || receipt.Fingerprint == "" || receipt.FinalFingerprint == "" || receipt.StartedAt.IsZero() || receipt.FinishedAt.Before(receipt.StartedAt) {
		t.Fatalf("receipt lacks canonical evidence: %#v", receipt)
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Receipt
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("receipt is not JSON: %v", err)
	}
	if decoded.Repository != "fixture" || decoded.Phase != model.PhaseStatic || !decoded.Passed {
		t.Fatalf("written receipt = %#v", decoded)
	}
	htmlPath := strings.TrimSuffix(receiptPath, ".json") + ".html"
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("receipt HTML sidecar is not durable: %v", err)
	}
	if !strings.Contains(string(html), "Sam Harness static receipt") || !strings.Contains(string(html), "Harness version") {
		t.Fatalf("receipt HTML lacks human-readable identity: %s", html)
	}

	execution := execute(root, model.PhaseStatic, model.CommandSpec{
		Name:     "escape",
		Workdir:  "../outside",
		Command:  []string{"go", "version"},
		Required: true,
	}, nil, nil)
	if execution.result.Passed || !strings.Contains(execution.result.Output, "escapes root") {
		t.Fatalf("escaping command was not blocked: %#v", execution.result)
	}
}

func TestSnapshotGitControlIgnoresTransientGitLockFiles(t *testing.T) {
	root := t.TempDir()
	initializeTestGit(t, root)
	before, err := snapshotGitControl(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".git/index.lock", ".git/objects/maintenance.lock", ".git/objects/tmp_pack_123"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, path), []byte("transient\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	after, err := snapshotGitControl(root)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshotsEqual(before, after) {
		t.Fatal("snapshotGitControl treated transient Git lock files as repository mutations")
	}
}

func TestPipelineTrustedConfigOverrideGovernsPRWorktree(t *testing.T) {
	root := t.TempDir()
	trustedMarker := filepath.Join(t.TempDir(), "trusted-ran")
	exfilMarker := filepath.Join(t.TempDir(), "pr-config-ran")
	writeExecutable(t, root, "trusted.sh", "#!/bin/sh\nprintf trusted > \"$1\"\n")
	writeExecutable(t, root, "exfil.sh", "#!/bin/sh\nprintf exfiltrated > \"$1\"\n")

	prConfig := testPipelineConfig()
	prConfig.Gates = []model.Gate{{Name: "pr-controlled", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./exfil.sh", exfilMarker}, Required: true}}
	writePipelineConfig(t, root, prConfig)

	trustedConfig := testPipelineConfig()
	trustedConfig.Gates = []model.Gate{{Name: "trusted", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./trusted.sh", trustedMarker}, Required: true}}
	trustedPath := filepath.Join(t.TempDir(), "trusted-config.yaml")
	writePipelineConfigAt(t, trustedPath, trustedConfig)

	receipt, receiptPath, err := RunWithConfig(root, trustedPath, model.PhaseStatic, true)
	if err != nil {
		t.Fatalf("trusted config pipeline failed: %v\n%#v", err, receipt)
	}
	if got := readFile(t, trustedMarker); got != "trusted" {
		t.Fatalf("trusted command did not govern the PR worktree: %q", got)
	}
	if _, err := os.Stat(exfilMarker); !os.IsNotExist(err) {
		t.Fatalf("PR-controlled config command executed: %v", err)
	}
	wantSource, err := filepath.EvalSymlinks(trustedPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(trustedPath)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256(raw)
	if receipt.ConfigSource != wantSource || receipt.ConfigSHA256 != hex.EncodeToString(wantDigest[:]) {
		t.Fatalf("receipt config provenance = %#v", receipt)
	}
	persisted, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Receipt
	if err := json.Unmarshal(persisted, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ConfigSource != receipt.ConfigSource || decoded.ConfigSHA256 != receipt.ConfigSHA256 {
		t.Fatalf("persisted receipt lost config provenance: %#v", decoded)
	}
}

func TestPipelineConfigOverrideRejectsUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "trusted.yaml")
	writePipelineConfigAt(t, outside, testPipelineConfig())
	link := filepath.Join(root, "trusted-link.yaml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "trusted-dir")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name       string
		path       string
		errorMatch string
	}{
		{name: "missing", path: filepath.Join(root, "missing.yaml"), errorMatch: "does not exist"},
		{name: "symlink", path: link, errorMatch: "symbolic link"},
		{name: "non-regular", path: directory, errorMatch: "regular file"},
		{name: "relative escape", path: "../trusted.yaml", errorMatch: "escapes repository"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if _, _, err := RunWithConfig(root, scenario.path, model.PhaseStatic, false); err == nil || !strings.Contains(err.Error(), scenario.errorMatch) {
				t.Fatalf("unsafe config path was accepted: %v", err)
			}
		})
	}
}

func TestPipelineConfigOverrideAcceptsContainedRelativeFile(t *testing.T) {
	root := t.TempDir()
	relative := filepath.Join("trusted", "config.yaml")
	writePipelineConfigAt(t, filepath.Join(root, relative), testPipelineConfig())
	receipt, _, err := RunWithConfig(root, filepath.ToSlash(relative), model.PhaseStatic, false)
	if err != nil || !receipt.Passed {
		t.Fatalf("contained relative config failed: err=%v receipt=%#v", err, receipt)
	}
	if !filepath.IsAbs(receipt.ConfigSource) || receipt.ConfigSHA256 == "" {
		t.Fatalf("relative config was not canonicalized in receipt: %#v", receipt)
	}
}

func TestPipelineBlocksTrustedConfigReplacementDuringPhase(t *testing.T) {
	root := t.TempDir()
	trustedPath := filepath.Join(t.TempDir(), "trusted-config.yaml")
	writeExecutable(t, root, "replace-config.sh", "#!/bin/sh\ncp \"$1\" \"$1.replacement\"\nmv \"$1.replacement\" \"$1\"\n")
	cfg := testPipelineConfig()
	cfg.Gates = []model.Gate{{Name: "replace-config", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./replace-config.sh", trustedPath}, Required: true}}
	writePipelineConfigAt(t, trustedPath, cfg)

	receipt, _, err := RunWithConfig(root, trustedPath, model.PhaseStatic, false)
	if err == nil || receipt.Status != StatusBlocked || !strings.Contains(err.Error(), "configuration source changed") {
		t.Fatalf("trusted config mutation was accepted: err=%v receipt=%#v", err, receipt)
	}
}

func TestGuardCommandsAndWaiversHaveDistinctDeterministicEvidence(t *testing.T) {
	root := t.TempDir()
	guardLog := filepath.Join(t.TempDir(), "guards.log")
	writeExecutable(t, root, "guard.sh", `#!/bin/sh
test "$SAM_HARNESS_PIPELINE_PHASE" = "$1"
printf '%s:%s\n' "$1" "$2" >> "$3"
`)
	cfg := testPipelineConfig()
	cfg.Gates = nil
	cfg.Workflow.StaticGuards.Commands[model.GuardFormat] = model.CommandSpec{
		Name:           "format-guard",
		Workdir:        ".",
		Command:        []string{"./guard.sh", "static", model.GuardFormat, guardLog},
		Required:       true,
		TimeoutSeconds: 5,
	}
	delete(cfg.Workflow.StaticGuards.Waivers, model.GuardFormat)
	cfg.Workflow.TestGuards.Commands[model.GuardUnit] = model.CommandSpec{
		Name:           "unit-guard",
		Workdir:        ".",
		Command:        []string{"./guard.sh", "test", model.GuardUnit, guardLog},
		Required:       true,
		TimeoutSeconds: 5,
	}
	delete(cfg.Workflow.TestGuards.Waivers, model.GuardUnit)
	writePipelineConfig(t, root, cfg)

	staticReceipt, _, err := Run(root, model.PhaseStatic, false)
	if err != nil {
		t.Fatalf("static guards failed: %v\n%#v", err, staticReceipt)
	}
	testReceipt, _, err := Run(root, model.PhaseTest, false)
	if err != nil {
		t.Fatalf("test guards failed: %v\n%#v", err, testReceipt)
	}
	for _, phaseReceipt := range []struct {
		receipt    Receipt
		categories []string
		executed   string
	}{
		{staticReceipt, model.StaticGuardCategories, model.GuardFormat},
		{testReceipt, model.TestGuardCategories, model.GuardUnit},
	} {
		if len(phaseReceipt.receipt.Commands) != len(phaseReceipt.categories) {
			t.Fatalf("guard receipt has %d results: %#v", len(phaseReceipt.receipt.Commands), phaseReceipt.receipt)
		}
		for index, category := range phaseReceipt.categories {
			result := phaseReceipt.receipt.Commands[index]
			if result.Category != category {
				t.Fatalf("guard category order = %#v", phaseReceipt.receipt.Commands)
			}
			if category == phaseReceipt.executed {
				if !result.Passed || result.Skipped || result.Waiver != "" {
					t.Fatalf("executed guard disguised in receipt: %#v", result)
				}
			} else if result.Passed || !result.Skipped || result.Waiver == "" || result.Output != result.Waiver {
				t.Fatalf("waived guard disguised as execution: %#v", result)
			}
		}
	}
	if log := readFile(t, guardLog); log != "static:format\ntest:unit\n" {
		t.Fatalf("unexpected executed guards: %q", log)
	}
}

func TestPhaseBlocksStaticOrTestCommandRepositoryMutation(t *testing.T) {
	for _, phase := range []model.Phase{model.PhaseStatic, model.PhaseTest} {
		t.Run(string(phase), func(t *testing.T) {
			root := t.TempDir()
			writeExecutable(t, root, "mutate.sh", "#!/bin/sh\nprintf 'mutation\\n' > changed.txt\n")
			cfg := testPipelineConfig()
			cfg.Gates = []model.Gate{{Name: "mutator", Stage: "local", Phase: phase, Workdir: ".", Command: []string{"./mutate.sh"}, Required: true}}
			writePipelineConfig(t, root, cfg)

			receipt, _, err := Run(root, phase, false)
			if err == nil || !strings.Contains(err.Error(), "mutated the repository") || receipt.Status != StatusBlocked || receipt.Passed {
				t.Fatalf("mutating check was accepted: err=%v receipt=%#v", err, receipt)
			}
		})
	}
}

func TestReadOnlyGateIgnoresDependencyCacheMutationButStillTracksSource(t *testing.T) {
	root := t.TempDir()
	writeExecutable(t, root, "cache.sh", "#!/bin/sh\nmkdir -p node_modules/pkg target\nprintf cache > node_modules/pkg/cache.txt\nprintf cache > target/cache.txt\n")
	cfg := testPipelineConfig()
	cfg.Gates = []model.Gate{{Name: "cache-writer", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./cache.sh"}, Required: true}}
	writePipelineConfig(t, root, cfg)

	if receipt, _, err := Run(root, model.PhaseStatic, false); err != nil {
		t.Fatalf("dependency/cache output was treated as source mutation: %v\n%#v", err, receipt)
	}
	writeExecutable(t, root, "cache.sh", "#!/bin/sh\nprintf mutation > source.txt\n")
	if receipt, _, err := Run(root, model.PhaseStatic, false); err == nil || receipt.Passed || !strings.Contains(err.Error(), "mutated the repository") {
		t.Fatalf("real source mutation was not blocked: err=%v receipt=%#v", err, receipt)
	}
}

func TestPhaseAllWritesOneReceiptForEveryExecutedLifecyclePhase(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	writeArtifactFixture(t, root)
	writeFile(t, root, "source.txt", "base\n")
	writeExecutable(t, root, "review-clean.sh", `#!/bin/sh
cat >/dev/null
printf '{"review_complete":true,"findings":[{"role":"%s","severity":"P3","summary":"recorded","evidence":"fixture","path":"source.txt","line":1,"required_change":"preserve behavior","acceptance":"behavior remains stable"}]}\n' "$SAM_HARNESS_REVIEW_ROLE"
`)
	cfg := testPipelineConfig()
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"./review-clean.sh"}
	}
	writePipelineConfig(t, root, cfg)
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "source.txt", "head\n")
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)

	receipt, receiptPath, err := RunWithOptions(root, model.PhaseAll, true, RunOptions{ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA})
	if err != nil {
		t.Fatalf("all phases failed: %v\n%#v", err, receipt)
	}
	expected := []model.Phase{
		model.PhaseStatic,
		model.PhaseTest,
		model.PhaseReview,
		model.PhaseArtifact,
		model.PhaseStaging,
		model.PhaseMigration,
		model.PhaseProduction,
		model.PhaseObserve,
	}
	if !receipt.Passed || receipt.Status != StatusPassed || len(receipt.Phases) != len(expected) || receiptPath == "" {
		t.Fatalf("all receipt = %#v", receipt)
	}
	if receipt.ReviewHeadFingerprint == "" || receipt.RepairManifest == nil || receipt.RepairManifestSHA256 == "" || len(receipt.RepairManifest.Actions) != len(model.ReviewerRoles) {
		t.Fatal("aggregate all receipt lost review lineage evidence")
	}
	for index, phase := range expected {
		result := receipt.Phases[index]
		if result.Phase != phase || result.Status != StatusPassed || result.ReceiptPath == "" {
			t.Fatalf("phase receipt %d = %#v, want %s", index, result, phase)
		}
		if _, err := os.Stat(result.ReceiptPath); err != nil {
			t.Fatalf("phase receipt path is not durable: %v", err)
		}
	}
}

func TestReviewRunsSixRolesInParallelAndKeepsDeterministicOrder(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	trusted := t.TempDir()
	barrier := t.TempDir()
	t.Setenv("GH_TOKEN", "must-not-reach-reviewers")
	t.Setenv("REVIEW_AGENT_TOKEN", "review-allowlisted-secret")
	t.Setenv("REVIEW_BARRIER_DIR", barrier)
	writeFile(t, root, "node_modules/pkg/marker.txt", "must-not-be-copied\n")
	writeFile(t, root, "vendor/pkg/marker.txt", "must-not-be-copied\n")
	writeFile(t, root, "source.txt", "base\n")
	reviewCommand := filepath.Join(trusted, "review.sh")
	writeExecutable(t, trusted, "review.sh", `#!/bin/sh
payload=$(cat)
case "$payload" in
  *repository_fingerprint*) ;;
  *) exit 31 ;;
esac
test "$SAM_HARNESS_PIPELINE_PHASE" = review || exit 32
test -z "$GH_TOKEN" || exit 34
test "$REVIEW_AGENT_TOKEN" = review-allowlisted-secret || exit 35
test -z "$(git remote)" || exit 36
test ! -e node_modules/pkg/marker.txt || exit 37
test ! -e vendor/pkg/marker.txt || exit 38
: > "$REVIEW_BARRIER_DIR/$SAM_HARNESS_REVIEW_ROLE"
attempt=0
while [ "$(find "$REVIEW_BARRIER_DIR" -type f | wc -l | tr -d ' ')" -lt 6 ]; do
  attempt=$((attempt + 1))
  [ "$attempt" -lt 200 ] || exit 39
  sleep 0.05
done
printf '{"review_complete":true,"findings":[{"role":"%s","severity":"P2","summary":"recorded","evidence":"%s","path":"source.txt","line":1,"required_change":"remove %s from output","acceptance":"%s is absent"}]}\n' "$SAM_HARNESS_REVIEW_ROLE" "$REVIEW_AGENT_TOKEN" "$REVIEW_AGENT_TOKEN" "$REVIEW_AGENT_TOKEN"
`)
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "source.txt", "head\n")
	cfg := testPipelineConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {
		{Scope: model.CISecretScopeReview, Environment: "REVIEW_AGENT_TOKEN", Secret: "REVIEW_AGENT_TOKEN"},
		{Scope: model.CISecretScopeReview, Environment: "REVIEW_BARRIER_DIR", Secret: "REVIEW_BARRIER_DIR"},
	}}
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{reviewCommand}
		cfg.Workflow.Reviewers[index].TimeoutSeconds = 15
		cfg.Workflow.Reviewers[index].TrustedExternalCommand = true
	}
	writePipelineConfig(t, root, cfg)
	trustedConfig := filepath.Join(trusted, "config.yaml")
	writePipelineConfigAt(t, trustedConfig, cfg)
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)

	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ConfigPath: trustedConfig, ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA})
	if err != nil {
		t.Fatalf("review failed: %v\n%#v", err, receipt)
	}
	if len(receipt.Commands) != len(model.ReviewerRoles) || len(receipt.Findings) != len(model.ReviewerRoles) {
		t.Fatalf("review evidence is incomplete: %#v", receipt)
	}
	if receipt.RepairManifest == nil || len(receipt.RepairManifest.Actions) != len(model.ReviewerRoles) || receipt.RepairManifestSHA256 == "" {
		t.Fatalf("review repair manifest is incomplete: %#v", receipt)
	}
	if err := validateRepairManifest(receipt); err != nil {
		t.Fatalf("review repair manifest is invalid: %v", err)
	}
	for index, role := range model.ReviewerRoles {
		if receipt.Findings[index].Role != role || receipt.Commands[index].Name != "review:"+string(role) {
			t.Fatalf("review evidence order is not deterministic: %#v", receipt)
		}
		if receipt.RepairManifest.Actions[index] != receipt.Findings[index] {
			t.Fatalf("repair action %d differs from its finding: %#v", index, receipt.RepairManifest.Actions[index])
		}
		if receipt.Findings[index].Evidence != "[REDACTED]" || receipt.Findings[index].RequiredChange != "remove [REDACTED] from output" || receipt.Findings[index].Acceptance != "[REDACTED] is absent" || strings.Contains(receipt.Commands[index].Output, "review-allowlisted-secret") || strings.Contains(fmt.Sprintf("%#v", receipt.RepairManifest), "review-allowlisted-secret") {
			t.Fatalf("review secret was persisted: finding=%#v command=%#v", receipt.Findings[index], receipt.Commands[index])
		}
	}
}

func TestParseReviewerOutputRequiresCompletePrescriptiveReview(t *testing.T) {
	t.Parallel()
	valid := `{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"unsafe","evidence":"file.go:4","path":"file.go","line":4,"required_change":"validate input before use","acceptance":"the unsafe input is rejected"}]}`
	findings, err := parseReviewerOutput(valid, model.ReviewerSecurity)
	if err != nil || len(findings) != 1 || findings[0].RequiredChange == "" || findings[0].Acceptance == "" {
		t.Fatalf("complete prescriptive review was rejected: findings=%#v err=%v", findings, err)
	}
	for name, output := range map[string]string{
		"incomplete review":  `{"review_complete":false,"findings":[]}`,
		"missing correction": `{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"unsafe","evidence":"file.go:4","path":"file.go","line":4,"acceptance":"fixed"}]}`,
		"missing acceptance": `{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"unsafe","evidence":"file.go:4","path":"file.go","line":4,"required_change":"fix it"}]}`,
		"missing path":       `{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"unsafe","evidence":"file.go:4","line":4,"required_change":"fix it","acceptance":"fixed"}]}`,
		"missing line":       `{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"unsafe","evidence":"file.go:4","path":"file.go","required_change":"fix it","acceptance":"fixed"}]}`,
		"escaping path":      `{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"unsafe","evidence":"file.go:4","path":"../file.go","line":4,"required_change":"fix it","acceptance":"fixed"}]}`,
		"padded path":        `{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"unsafe","evidence":"file.go:4","path":" file.go ","line":4,"required_change":"fix it","acceptance":"fixed"}]}`,
		"backslash path":     `{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"unsafe","evidence":"file.go:4","path":"dir\\file.go","line":4,"required_change":"fix it","acceptance":"fixed"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseReviewerOutput(output, model.ReviewerSecurity); err == nil {
				t.Fatalf("invalid reviewer output was accepted: %s", output)
			}
		})
	}
}

func TestLowRiskReviewSkipsRolesAndStillBlocksOnP0(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	writeFile(t, root, "main.go", "package main\n")
	writeExecutable(t, root, "review.sh", `#!/bin/sh
role="$SAM_HARNESS_REVIEW_ROLE"
if [ "$role" = correctness ]; then
  printf '{"review_complete":true,"findings":[{"role":"correctness","severity":"P0","summary":"broken","evidence":"fail","path":"main.go","line":2,"required_change":"fix main","acceptance":"main passes"}]}\n'
  exit 0
fi
printf '{"review_complete":true,"findings":[]}\n'
`)
	cfg := testPipelineConfig()
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
	}
	writePipelineConfig(t, root, cfg)
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "main.go", "package main\nfunc main() {}\n")
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)
	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA, Risk: model.ChangeRiskLow})
	if err == nil || receipt.Passed || receipt.Status != StatusBlocked {
		t.Fatalf("low-risk P0 was accepted: err=%v receipt=%#v", err, receipt)
	}
	if len(receipt.Commands) != 2 {
		t.Fatalf("low-risk invoked %d roles, want 2: %#v", len(receipt.Commands), receipt.Commands)
	}
	if len(receipt.Commands) == len(model.ReviewerRoles) {
		t.Fatal("low-risk invoked every configured role")
	}
	foundP0 := false
	for _, finding := range receipt.Findings {
		if finding.Severity == "P0" && finding.Role == model.ReviewerCorrectness {
			foundP0 = true
		}
	}
	if !foundP0 {
		t.Fatalf("P0 from a running role was dropped: %#v", receipt.Findings)
	}
	if receipt.RepairManifest == nil || len(receipt.RepairManifest.Actions) != 1 {
		t.Fatalf("complete blocked review did not emit its repair manifest: %#v", receipt)
	}
}

func TestReviewArbiterBlocksConflictingFindings(t *testing.T) {
	root := t.TempDir()
	writeExecutable(t, root, "review.sh", `#!/bin/sh
role="$SAM_HARNESS_REVIEW_ROLE"
case "$role" in
  architecture)
    printf '{"review_complete":true,"findings":[{"role":"architecture","severity":"P0","summary":"bug","evidence":"a","path":"file.go","line":4,"required_change":"change architecture","acceptance":"architecture passes"}]}\n'
    ;;
  correctness)
    printf '{"review_complete":true,"findings":[{"role":"correctness","severity":"P3","summary":"not a bug","evidence":"b","path":"file.go","line":4,"required_change":"keep behavior","acceptance":"behavior remains"}]}\n'
    ;;
  *)
    printf '{"review_complete":true,"findings":[]}\n'
    ;;
esac
`)
	cfg := testPipelineConfig()
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
	}
	writePipelineConfig(t, root, cfg)
	receipt, _, err := Run(root, model.PhaseReview, false)
	if err == nil || !receipt.ArbiterBlocked || receipt.Status != StatusBlocked {
		t.Fatalf("conflicting findings were not blocked: err=%v receipt=%#v", err, receipt)
	}
}

func TestReviewArbiterKeepsIdenticalAttribution(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	writeFile(t, root, "file.go", "one\ntwo\nthree\n")
	writeExecutable(t, root, "review.sh", `#!/bin/sh
printf '{"review_complete":true,"findings":[{"role":"%s","severity":"P2","summary":"same","evidence":"e","path":"file.go","line":4,"required_change":"apply same fix","acceptance":"same issue passes"}]}\n' "$SAM_HARNESS_REVIEW_ROLE"
`)
	cfg := testPipelineConfig()
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
	}
	writePipelineConfig(t, root, cfg)
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "file.go", "one\ntwo\nthree\nfour\n")
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)
	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA, Risk: model.ChangeRiskLow})
	if err != nil {
		t.Fatalf("identical findings failed review: %v\n%#v", err, receipt)
	}
	roles := map[model.ReviewerRole]bool{}
	for _, finding := range receipt.Findings {
		if finding.Path != "file.go" || finding.Line != 4 || finding.Severity != "P2" {
			t.Fatalf("finding mutated: %#v", finding)
		}
		roles[finding.Role] = true
	}
	if !roles[model.ReviewerCorrectness] || !roles[model.ReviewerSimplicity] {
		t.Fatalf("identical findings lost role attribution: %#v", receipt.Findings)
	}
}

func TestReviewReceivesCanonicalBaseToHeadChangeEvidence(t *testing.T) {
	root := t.TempDir()
	promptDirectory := t.TempDir()
	writeFile(t, root, "source.txt", "base\n")
	writeExecutable(t, root, "review.sh", fmt.Sprintf(`#!/bin/sh
payload=$(cat)
patch_path=$(printf '%%s' "$payload" | sed -n 's/.*"review_patch_path":"\([^"]*\)".*/\1/p')
test -r "$patch_path"
cat "$patch_path" > %q/"$SAM_HARNESS_REVIEW_ROLE.patch"
printf '%%s' "$payload" > %q/"$SAM_HARNESS_REVIEW_ROLE.json"
git diff --quiet HEAD -- && exit 51
printf '{"review_complete":true,"findings":[{"role":"%%s","severity":"P3","summary":"change reviewed","evidence":"base-to-head","path":"source.txt","line":1,"required_change":"apply reviewed change","acceptance":"change is accepted"}]}\n' "$SAM_HARNESS_REVIEW_ROLE"
`, promptDirectory, promptDirectory))
	cfg := testPipelineConfig()
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
	}
	writePipelineConfig(t, root, cfg)
	base := t.TempDir()
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "source.txt", "head\n")
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)

	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: base, ReviewBaseSHA: strings.ToUpper(baseSHA), ReviewHeadSHA: strings.ToUpper(headSHA)})
	if err != nil {
		t.Fatalf("review with base failed: %v\n%#v", err, receipt)
	}
	if receipt.ReviewBaseRoot == "" || receipt.ReviewBaseSHA != baseSHA || receipt.ReviewBaseFingerprint == "" || receipt.ReviewHeadSHA != headSHA || receipt.ReviewHeadFingerprint == "" || receipt.ReviewPatch == "" || receipt.ReviewPatchSHA256 == "" {
		t.Fatalf("review receipt lacks base-to-head evidence: %#v", receipt)
	}
	patch, err := os.ReadFile(receipt.ReviewPatch)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(patch)
	if receipt.ReviewPatchSHA256 != hex.EncodeToString(digest[:]) || !strings.Contains(string(patch), "-base") || !strings.Contains(string(patch), "+head") {
		t.Fatalf("review patch is not canonical base-to-head evidence: receipt=%#v patch=%s", receipt, patch)
	}
	var prompt struct {
		BaseRoot        string `json:"review_base_root"`
		BaseSHA         string `json:"review_base_sha"`
		BaseFingerprint string `json:"review_base_fingerprint"`
		HeadSHA         string `json:"review_head_sha"`
		HeadFingerprint string `json:"review_head_fingerprint"`
		PatchPath       string `json:"review_patch_path"`
		PatchSHA256     string `json:"review_patch_sha256"`
	}
	promptData, err := os.ReadFile(filepath.Join(promptDirectory, string(model.ReviewerArchitecture)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(promptData, &prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.BaseRoot != receipt.ReviewBaseRoot || prompt.BaseSHA != receipt.ReviewBaseSHA || prompt.BaseFingerprint != receipt.ReviewBaseFingerprint || prompt.HeadSHA != receipt.ReviewHeadSHA || prompt.HeadFingerprint != receipt.ReviewHeadFingerprint || prompt.PatchPath == "" || !filepath.IsAbs(prompt.PatchPath) || prompt.PatchSHA256 != receipt.ReviewPatchSHA256 {
		t.Fatalf("review prompt lineage differs from receipt: prompt=%#v receipt=%#v", prompt, receipt)
	}
	reviewerPatch, err := os.ReadFile(filepath.Join(promptDirectory, string(model.ReviewerArchitecture)+".patch"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reviewerPatch, patch) {
		t.Fatal("reviewer did not read the exact canonical patch through review_patch_path")
	}
	var promptFields map[string]json.RawMessage
	if err := json.Unmarshal(promptData, &promptFields); err != nil {
		t.Fatal(err)
	}
	if _, embedded := promptFields["review_patch"]; embedded {
		t.Fatal("review prompt embedded the canonical patch body")
	}
}

func TestReviewLargePatchUsesBoundedPointerTransport(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	promptDirectory := t.TempDir()
	largeContent := "large-review-patch-marker\n" + strings.Repeat("x", 40*1024) + "\n"
	writeFile(t, root, "source.txt", "base\n")
	writeExecutable(t, root, "review.sh", fmt.Sprintf(`#!/bin/sh
payload=$(cat)
patch_path=$(printf '%%s' "$payload" | sed -n 's/.*"review_patch_path":"\([^"]*\)".*/\1/p')
test -r "$patch_path"
cat "$patch_path" > %q/"$SAM_HARNESS_REVIEW_ROLE.patch"
printf '%%s' "$payload" > %q/"$SAM_HARNESS_REVIEW_ROLE.json"
printf '{"review_complete":true,"findings":[]}\n'
`, promptDirectory, promptDirectory))
	cfg := testPipelineConfig()
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
	}
	writePipelineConfig(t, root, cfg)
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "source.txt", largeContent)
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)

	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA})
	if err != nil {
		t.Fatalf("large patch review failed: %v\n%#v", err, receipt)
	}
	patch, err := os.ReadFile(receipt.ReviewPatch)
	if err != nil {
		t.Fatal(err)
	}
	if len(patch) <= outputLimit {
		t.Fatalf("test patch is not greater than %d bytes: %d", outputLimit, len(patch))
	}
	patchDigest := sha256.Sum256(patch)
	if receipt.ReviewPatchSHA256 != hex.EncodeToString(patchDigest[:]) {
		t.Fatalf("receipt patch digest = %q, want %x", receipt.ReviewPatchSHA256, patchDigest)
	}

	promptData, err := os.ReadFile(filepath.Join(promptDirectory, string(model.ReviewerArchitecture)+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(promptData) >= outputLimit {
		t.Fatalf("review prompt is not bounded below %d bytes: %d", outputLimit, len(promptData))
	}
	if bytes.Contains(promptData, []byte(largeContent)) || bytes.Contains(promptData, []byte("large-review-patch-marker")) {
		t.Fatal("large canonical patch body was embedded in the reviewer prompt")
	}
	var prompt struct {
		PatchPath   string `json:"review_patch_path"`
		PatchSHA256 string `json:"review_patch_sha256"`
	}
	if err := json.Unmarshal(promptData, &prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.PatchPath == "" || prompt.PatchSHA256 != receipt.ReviewPatchSHA256 {
		t.Fatalf("large patch prompt pointer = %#v, receipt = %#v", prompt, receipt)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(promptData, &fields); err != nil {
		t.Fatal(err)
	}
	if _, embedded := fields["review_patch"]; embedded {
		t.Fatal("large patch prompt contains review_patch body field")
	}
	reviewerPatch, err := os.ReadFile(filepath.Join(promptDirectory, string(model.ReviewerArchitecture)+".patch"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reviewerPatch, patch) {
		t.Fatal("reviewer-readable patch differs from the receipt artifact")
	}
	reviewerDigest := sha256.Sum256(reviewerPatch)
	if prompt.PatchSHA256 != hex.EncodeToString(reviewerDigest[:]) {
		t.Fatalf("reviewer-readable patch digest = %x, prompt = %q", reviewerDigest, prompt.PatchSHA256)
	}
}

func TestMaterializeReviewPatchUsesCanonicalBytesAndExclusiveDestination(t *testing.T) {
	sandbox := t.TempDir()
	canonical := []byte("canonical patch bytes\n")
	digest := sha256.Sum256(canonical)
	expectedDigest := hex.EncodeToString(digest[:])
	external := filepath.Join(t.TempDir(), "outside.patch")
	if err := os.WriteFile(external, []byte("outside bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sandboxRoot, err := openReviewSandboxRoot(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	defer sandboxRoot.Close()

	path, err := materializeReviewPatch(sandboxRoot, canonical, expectedDigest)
	if err != nil {
		t.Fatalf("materializeReviewPatch() failed: %v", err)
	}
	if path != reviewerPatchPath(sandbox) || filepath.Dir(path) != sandbox {
		t.Fatalf("review patch was not placed at the fixed sandbox location: %q", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, canonical) {
		t.Fatalf("materialized patch = %q, want %q", got, canonical)
	}
	if err := os.WriteFile(external, []byte("substituted source bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, canonical) {
		t.Fatalf("materialized patch changed after source substitution: %q", got)
	}
	if err := removeReviewSandboxPatch(sandboxRoot); err != nil {
		t.Fatal(err)
	}

	outsideOriginal := "outside-original\n"
	if err := os.WriteFile(external, []byte(outsideOriginal), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeReviewPatch(sandboxRoot, canonical, expectedDigest); err == nil {
		t.Fatal("existing destination symlink was overwritten")
	}
	if got := readFile(t, external); got != outsideOriginal {
		t.Fatalf("destination substitution changed the outside target: %q", got)
	}
	if err := removeReviewSandboxPatch(sandboxRoot); err != nil {
		t.Fatal(err)
	}

	path, err = materializeReviewPatch(sandboxRoot, canonical, expectedDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err := removeReviewSandboxPatch(sandboxRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, path); err != nil {
		t.Fatal(err)
	}
	if err := verifyReviewSandboxPatch(sandboxRoot, expectedDigest); err == nil {
		t.Fatal("substituted destination symlink was accepted")
	}
	if got := readFile(t, external); got != outsideOriginal {
		t.Fatalf("verify after destination substitution changed the outside target: %q", got)
	}
}

func TestReviewSandboxRootRejectsParentSwapAndCleansHeldDescriptor(t *testing.T) {
	parent := t.TempDir()
	sandbox := filepath.Join(parent, "sandbox")
	if err := os.Mkdir(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	sandboxRoot, err := openReviewSandboxRoot(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	defer sandboxRoot.Close()

	moved := filepath.Join(parent, "sandbox-moved")
	if err := os.Rename(sandbox, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sandbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := sandboxRoot.confirmIdentity(); err == nil {
		t.Fatal("parent swap was accepted by sandbox identity confirmation")
	}

	canonical := []byte("parent swap patch\n")
	digest := sha256.Sum256(canonical)
	if _, err := materializeReviewPatch(sandboxRoot, canonical, hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("parent swap was accepted for review patch materialization")
	}
	if _, err := os.Lstat(filepath.Join(sandbox, ".sam-harness-"+reviewerPatchFilename)); !os.IsNotExist(err) {
		t.Fatalf("attacker directory received a review patch: %v", err)
	}

	file, err := sandboxRoot.root.OpenFile(".sam-harness-"+reviewerPatchFilename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(canonical); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := removeReviewSandboxPatch(sandboxRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(moved, ".sam-harness-"+reviewerPatchFilename)); !os.IsNotExist(err) {
		t.Fatalf("held descriptor patch was not cleaned from renamed sandbox: %v", err)
	}
}

func TestSecretBearingReviewRejectsTargetControlledExecutableBeforeItRuns(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	trusted := t.TempDir()
	marker := filepath.Join(t.TempDir(), "target-reviewer-ran")
	writeExecutable(t, root, "reviewer", fmt.Sprintf("#!/bin/sh\ncat >/dev/null\nprintf ran > %q\n", marker))
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REVIEW_TOKEN", "review-secret")

	cfg := testPipelineConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: model.CISecretScopeReview, Environment: "REVIEW_TOKEN", Secret: "REVIEW_TOKEN"}}}
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"reviewer"}
		cfg.Workflow.Reviewers[index].TrustedExternalCommand = true
	}
	writePipelineConfig(t, root, cfg)
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	trustedConfig := filepath.Join(trusted, "config.yaml")
	writePipelineConfigAt(t, trustedConfig, cfg)
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)

	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ConfigPath: trustedConfig, ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA})
	if err == nil || receipt.Status != StatusBlocked || len(receipt.Commands) == 0 || !strings.Contains(receipt.Commands[0].Output, "resolves inside the target repository") {
		t.Fatalf("target-controlled reviewer was accepted: err=%v receipt=%#v", err, receipt)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("target-controlled reviewer executed with a secret: %v", err)
	}
}

func TestSecretBearingReviewUsesTrustedConfigArgumentAndRequiresBase(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	trusted := t.TempDir()
	t.Setenv("REVIEW_TOKEN", "review-secret")
	writeFile(t, root, "source.txt", "base\n")
	writeFile(t, root, "reviewer-output.schema.json", "target-controlled\n")
	reviewer := filepath.Join(trusted, "reviewer")
	writeExecutable(t, trusted, "reviewer", `#!/bin/sh
payload=$(cat)
test "$REVIEW_TOKEN" = review-secret || exit 61
test "$(cat "$1")" = trusted || exit 62
printf '{"review_complete":true,"findings":[{"role":"%s","severity":"P3","summary":"trusted","evidence":"schema","path":"source.txt","line":1,"required_change":"apply trusted correction","acceptance":"trusted schema passes"}]}\n' "$SAM_HARNESS_REVIEW_ROLE"
`)
	writeFile(t, trusted, "reviewer-output.schema.json", "trusted\n")

	cfg := testPipelineConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: model.CISecretScopeReview, Environment: "REVIEW_TOKEN", Secret: "REVIEW_TOKEN"}}}
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{reviewer, "reviewer-output.schema.json"}
		cfg.Workflow.Reviewers[index].TrustedExternalCommand = true
		cfg.Workflow.Reviewers[index].TrustedConfigArguments = []int{1}
	}
	writePipelineConfig(t, root, cfg)
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "source.txt", "head\n")
	trustedConfig := filepath.Join(trusted, "config.yaml")
	writePipelineConfigAt(t, trustedConfig, cfg)
	baseSHA := initializeTestGit(t, base)
	headSHA := initializeTestGit(t, root)

	if receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ConfigPath: trustedConfig, ReviewBase: base}); err == nil || receipt.Status != StatusBlocked || !strings.Contains(err.Error(), "requires --review-base") {
		t.Fatalf("secret-bearing review without commit identities was accepted: err=%v receipt=%#v", err, receipt)
	}
	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ConfigPath: trustedConfig, ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA})
	if err != nil || !receipt.Passed || len(receipt.Findings) != len(model.ReviewerRoles) {
		t.Fatalf("trusted reviewer config argument failed: err=%v receipt=%#v", err, receipt)
	}
}

func TestReviewBaseMustBeAbsoluteRegularDirectory(t *testing.T) {
	root := t.TempDir()
	writePipelineConfig(t, root, testPipelineConfig())
	if _, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: "relative/base"}); err == nil || !strings.Contains(err.Error(), "absolute directory") {
		t.Fatalf("relative review base was accepted: %v", err)
	}
	base := t.TempDir()
	link := filepath.Join(t.TempDir(), "base-link")
	if err := os.Symlink(base, link); err != nil {
		t.Fatal(err)
	}
	if _, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: link}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink review base was accepted: %v", err)
	}
}

func TestReviewIdentityArgumentsArePairedScopedAndValidated(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	writePipelineConfig(t, root, testPipelineConfig())
	valid := strings.Repeat("a", 40)
	base64, head64, err := normalizeReviewIdentities(base, strings.Repeat("A", 64), strings.Repeat("B", 64))
	if err != nil || base64 != strings.Repeat("a", 64) || head64 != strings.Repeat("b", 64) {
		t.Fatalf("64-character identities were not normalized: base=%q head=%q err=%v", base64, head64, err)
	}
	for _, scenario := range []struct {
		name    string
		phase   model.Phase
		options RunOptions
		want    string
	}{
		{name: "wrong phase", phase: model.PhaseStatic, options: RunOptions{ReviewBase: base, ReviewBaseSHA: valid, ReviewHeadSHA: valid}, want: "only valid for review or all"},
		{name: "missing head", phase: model.PhaseReview, options: RunOptions{ReviewBase: base, ReviewBaseSHA: valid}, want: "must be provided together"},
		{name: "missing base", phase: model.PhaseReview, options: RunOptions{ReviewBaseSHA: valid, ReviewHeadSHA: valid}, want: "require --review-base"},
		{name: "bad length", phase: model.PhaseReview, options: RunOptions{ReviewBase: base, ReviewBaseSHA: "abcd", ReviewHeadSHA: valid}, want: "40 or 64"},
		{name: "non hex", phase: model.PhaseReview, options: RunOptions{ReviewBase: base, ReviewBaseSHA: strings.Repeat("z", 40), ReviewHeadSHA: valid}, want: "hexadecimal"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			if _, _, err := RunWithOptions(root, scenario.phase, false, scenario.options); err == nil || !strings.Contains(err.Error(), scenario.want) {
				t.Fatalf("identity arguments accepted: err=%v", err)
			}
		})
	}
}

func TestReviewRejectsGitIdentityMismatchBeforeReviewerRuns(t *testing.T) {
	root := t.TempDir()
	base := t.TempDir()
	marker := filepath.Join(t.TempDir(), "reviewer-ran")
	writeExecutable(t, root, "review.sh", fmt.Sprintf("#!/bin/sh\ncat >/dev/null\nprintf ran > %q\n", marker))
	cfg := testPipelineConfig()
	for index := range cfg.Workflow.Reviewers {
		cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
	}
	writePipelineConfig(t, root, cfg)
	if err := copyRepository(root, base, copyForReview); err != nil {
		t.Fatal(err)
	}
	baseSHA := initializeTestGit(t, base)
	initializeTestGit(t, root)

	receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: strings.Repeat("0", 40)})
	if err == nil || receipt.Status != StatusBlocked || !strings.Contains(err.Error(), "review head SHA mismatch") {
		t.Fatalf("mismatched head identity was accepted: err=%v receipt=%#v", err, receipt)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("reviewer ran before identity validation: %v", err)
	}
}

func TestReviewDetectsBaseAndHeadGitIdentityDrift(t *testing.T) {
	for _, drift := range []string{"base", "head"} {
		t.Run(drift, func(t *testing.T) {
			root := t.TempDir()
			base := t.TempDir()
			driftRoot := base
			if drift == "head" {
				driftRoot = root
			}
			writeExecutable(t, root, "review.sh", fmt.Sprintf(`#!/bin/sh
cat >/dev/null
git -C %q -c user.name=review-drift -c user.email=review@localhost commit --allow-empty --no-verify -m drift >/dev/null 2>&1 || true
printf '{"review_complete":true,"findings":[{"role":"%%s","severity":"P3","summary":"reviewed","evidence":"identity","path":"source.txt","line":1,"required_change":"preserve identity","acceptance":"identity is stable"}]}\n' "$SAM_HARNESS_REVIEW_ROLE"
`, driftRoot))
			cfg := testPipelineConfig()
			for index := range cfg.Workflow.Reviewers {
				cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
				cfg.Workflow.Reviewers[index].TimeoutSeconds = 30
			}
			writePipelineConfig(t, root, cfg)
			if err := copyRepository(root, base, copyForReview); err != nil {
				t.Fatal(err)
			}
			baseSHA := initializeTestGit(t, base)
			headSHA := initializeTestGit(t, root)

			receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA})
			if err == nil || receipt.Status != StatusBlocked || !strings.Contains(err.Error(), "after reviewers SHA mismatch") {
				t.Fatalf("%s identity drift was accepted: err=%v receipt=%#v", drift, err, receipt)
			}
		})
	}
}

func TestReviewBlocksP1MalformedOutputAndRepositoryMutation(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		security   string
		errorMatch string
	}{
		{
			name:       "P1",
			security:   `printf '{"review_complete":true,"findings":[{"role":"security","severity":"P1","summary":"blocker","evidence":"fixture","path":"source.txt","line":1,"required_change":"remove blocker","acceptance":"blocker is absent"}]}\n'`,
			errorMatch: "review blocked",
		},
		{name: "malformed", security: `printf 'not-json\n'`, errorMatch: "review blocked"},
		{name: "mutation", security: `printf 'mutation\n' > source.txt; printf '{"review_complete":true,"findings":[]}\n'`, errorMatch: "mutated the repository"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			base := t.TempDir()
			writeFile(t, root, "source.txt", "base\n")
			writeExecutable(t, root, "review.sh", `#!/bin/sh
cat >/dev/null
if [ "$SAM_HARNESS_REVIEW_ROLE" = security ]; then
  `+scenario.security+`
else
  printf '{"review_complete":true,"findings":[]}\n'
fi
`)
			cfg := testPipelineConfig()
			for index := range cfg.Workflow.Reviewers {
				cfg.Workflow.Reviewers[index].Command = []string{"./review.sh"}
			}
			writePipelineConfig(t, root, cfg)
			if err := copyRepository(root, base, copyForReview); err != nil {
				t.Fatal(err)
			}
			writeFile(t, root, "source.txt", "head\n")
			baseSHA := initializeTestGit(t, base)
			headSHA := initializeTestGit(t, root)

			receipt, _, err := RunWithOptions(root, model.PhaseReview, false, RunOptions{ReviewBase: base, ReviewBaseSHA: baseSHA, ReviewHeadSHA: headSHA})
			if err == nil || !strings.Contains(err.Error(), scenario.errorMatch) {
				t.Fatalf("review error = %v, receipt = %#v", err, receipt)
			}
			if receipt.Status != StatusBlocked || receipt.Passed {
				t.Fatalf("unsafe review did not block: %#v", receipt)
			}
			if scenario.name == "mutation" {
				content, readErr := os.ReadFile(filepath.Join(root, "source.txt"))
				if readErr != nil || string(content) != "head\n" {
					t.Fatalf("reviewer mutation escaped its isolated copy: content=%q err=%v", content, readErr)
				}
			}
		})
	}
}

func TestArtifactDigestPromotionAndOrderedCanaries(t *testing.T) {
	root := t.TempDir()
	writeExecutable(t, root, "build.sh", `#!/bin/sh
test "$SAM_HARNESS_PIPELINE_PHASE" = artifact
mkdir -p out .sam-harness/evidence
printf 'artifact-v1\n' > out/app.bin
printf 'build\n' >> .sam-harness/evidence/build.log
`)
	writeExecutable(t, root, "sbom.sh", `#!/bin/sh
test "$SAM_HARNESS_PIPELINE_PHASE" = artifact
printf '{"sbom":true}\n' > out/sbom.json
`)
	writeExecutable(t, root, "provenance.sh", `#!/bin/sh
test "$SAM_HARNESS_PIPELINE_PHASE" = artifact
printf '{"provenance":true}\n' > out/provenance.json
`)
	writeExecutable(t, root, "deploy.sh", `#!/bin/sh
test -n "$SAM_HARNESS_PIPELINE_PHASE"
test "$SAM_HARNESS_ARTIFACT_PATH" = out/app.bin
test -n "$SAM_HARNESS_ARTIFACT_SHA256"
printf '%s:%s\n' "$SAM_HARNESS_PIPELINE_PHASE" "${SAM_HARNESS_CANARY_PERCENTAGE:-none}" >> .sam-harness/evidence/deploy.log
`)
	writeExecutable(t, root, "health.sh", `#!/bin/sh
test -n "$SAM_HARNESS_PIPELINE_PHASE"
printf 'health:%s:%s\n' "$SAM_HARNESS_PIPELINE_PHASE" "${SAM_HARNESS_CANARY_PERCENTAGE:-none}" >> .sam-harness/evidence/deploy.log
`)
	cfg := testPipelineConfig()
	cfg.Workflow.Artifact.Build.Command = []string{"./build.sh"}
	cfg.Workflow.Artifact.SBOM.Command = []string{"./sbom.sh"}
	cfg.Workflow.Artifact.Provenance.Command = []string{"./provenance.sh"}
	cfg.Workflow.Deployment.Staging.Command = []string{"./deploy.sh"}
	cfg.Workflow.Deployment.Production.Command = []string{"./deploy.sh"}
	cfg.Workflow.Deployment.HealthChecks[0].Command = []string{"./health.sh"}
	cfg.Workflow.Deployment.CanaryPercentages = []int{10, 50, 100}
	writePipelineConfig(t, root, cfg)

	artifactReceipt, _, err := Run(root, model.PhaseArtifact, true)
	if err != nil {
		t.Fatalf("artifact failed: %v\n%#v", err, artifactReceipt)
	}
	expected := sha256.Sum256([]byte("artifact-v1\n"))
	if artifactReceipt.Artifact == nil || artifactReceipt.Artifact.SHA256 != hex.EncodeToString(expected[:]) {
		t.Fatalf("artifact digest = %#v", artifactReceipt.Artifact)
	}
	if artifactReceipt.Artifact.SourceFingerprint == "" || artifactReceipt.Artifact.SBOMSHA256 == "" || artifactReceipt.Artifact.ProvenanceSHA256 == "" {
		t.Fatalf("artifact receipt is not bound to source and all evidence digests: %#v", artifactReceipt.Artifact)
	}
	if _, _, err := Run(root, model.PhaseStaging, false); err != nil {
		t.Fatalf("staging failed: %v", err)
	}
	productionReceipt, _, err := Run(root, model.PhaseProduction, false)
	if err != nil {
		t.Fatalf("production failed: %v\n%#v", err, productionReceipt)
	}
	log := readFile(t, filepath.Join(root, ".sam-harness/evidence/deploy.log"))
	expectedLog := "staging:none\nhealth:staging:none\nproduction:10\nhealth:production:10\nproduction:50\nhealth:production:50\nproduction:100\nhealth:production:100\n"
	if log != expectedLog {
		t.Fatalf("promotion order = %q, want %q", log, expectedLog)
	}
	if strings.Count(readFile(t, filepath.Join(root, ".sam-harness/evidence/build.log")), "build") != 1 {
		t.Fatal("artifact was rebuilt during promotion")
	}

	if err := os.WriteFile(filepath.Join(root, "out/app.bin"), []byte("tampered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, filepath.Join(root, ".sam-harness/evidence/deploy.log"))
	receipt, _, err := Run(root, model.PhaseStaging, false)
	if err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered artifact was promoted: err=%v receipt=%#v", err, receipt)
	}
	if after := readFile(t, filepath.Join(root, ".sam-harness/evidence/deploy.log")); after != before {
		t.Fatal("deployment command ran after artifact mismatch")
	}
}

func TestArtifactBlocksSourceCheckoutMutation(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, root, "build.sh", `#!/bin/sh
mkdir -p out
printf 'artifact-v1\n' > out/app.bin
printf 'mutated\n' > source.txt
`)
	writeExecutable(t, root, "sbom.sh", "#!/bin/sh\nprintf '{}\\n' > out/sbom.json\n")
	writeExecutable(t, root, "provenance.sh", "#!/bin/sh\nprintf '{}\\n' > out/provenance.json\n")
	cfg := testPipelineConfig()
	cfg.Workflow.Artifact.Build.Command = []string{"./build.sh"}
	cfg.Workflow.Artifact.SBOM.Command = []string{"./sbom.sh"}
	cfg.Workflow.Artifact.Provenance.Command = []string{"./provenance.sh"}
	writePipelineConfig(t, root, cfg)

	receipt, _, err := Run(root, model.PhaseArtifact, false)
	if err == nil || !strings.Contains(err.Error(), "mutated the source checkout") || receipt.Passed {
		t.Fatalf("artifact source mutation was accepted: err=%v receipt=%#v", err, receipt)
	}
}

func TestStandalonePromotionRejectsSourceAndEvidenceDigestDrift(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		mutate string
	}{
		{name: "source", mutate: "source.txt"},
		{name: "SBOM", mutate: "out/sbom.json"},
		{name: "provenance", mutate: "out/provenance.json"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			writeArtifactFixture(t, root)
			if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("original\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(t.TempDir(), "deployed")
			writeExecutable(t, root, "deploy.sh", "#!/bin/sh\nprintf 'ran\\n' > \"$1\"\n")
			cfg := testPipelineConfig()
			cfg.Workflow.Deployment.Staging.Command = []string{"./deploy.sh", marker}
			writePipelineConfig(t, root, cfg)
			writePassingArtifactReceipt(t, root, cfg)
			if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(scenario.mutate)), []byte("drifted\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			receipt, _, err := Run(root, model.PhaseStaging, false)
			if err == nil || receipt.Passed || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(scenario.name)) {
				t.Fatalf("%s drift was accepted: err=%v receipt=%#v", scenario.name, err, receipt)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("promotion ran despite %s drift: %v", scenario.name, err)
			}
		})
	}
}

func TestPromotionRechecksSBOMAndProvenanceAfterCommands(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		deployment string
		health     string
	}{
		{name: "SBOM after promotion", deployment: "printf 'changed\\n' > out/sbom.json", health: ":"},
		{name: "provenance after health", deployment: ":", health: "printf 'changed\\n' > out/provenance.json"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			writeArtifactFixture(t, root)
			writeExecutable(t, root, "deploy.sh", "#!/bin/sh\n"+scenario.deployment+"\n")
			writeExecutable(t, root, "health.sh", "#!/bin/sh\n"+scenario.health+"\n")
			cfg := testPipelineConfig()
			cfg.Workflow.Deployment.Production.Command = []string{"./deploy.sh"}
			cfg.Workflow.Deployment.HealthChecks[0].Command = []string{"./health.sh"}
			cfg.Workflow.Deployment.CanaryPercentages = []int{100}
			writePipelineConfig(t, root, cfg)
			writePassingArtifactReceipt(t, root, cfg)

			receipt, _, err := Run(root, model.PhaseProduction, false)
			if err == nil || receipt.Passed || !strings.Contains(err.Error(), "digest mismatch") {
				t.Fatalf("evidence mutation was accepted: err=%v receipt=%#v", err, receipt)
			}
		})
	}
}

func TestStandalonePromotionUsesOnlyValidArtifactPhaseReceipt(t *testing.T) {
	root := t.TempDir()
	writeArtifactFixture(t, root)
	cfg := testPipelineConfig()
	writePipelineConfig(t, root, cfg)
	validPath := writePassingArtifactReceipt(t, root, cfg)

	fake := newReceipt(root, model.PhaseStaging, "fabricated")
	fake.Passed = true
	fake.Status = StatusPassed
	fake.Artifact = &ArtifactEvidence{Path: cfg.Workflow.Artifact.ArtifactPath, SHA256: "fabricated"}
	fake.FinishedAt = time.Now().UTC()
	if _, err := writeReceiptFile(root, cfg.Evidence.ReceiptDirectory, fake); err != nil {
		t.Fatal(err)
	}

	receipt, _, err := Run(root, model.PhaseStaging, false)
	if err != nil {
		t.Fatalf("valid artifact receipt was hidden by unrelated newer receipt: %v\n%#v", err, receipt)
	}
	if receipt.SourceReceipt != validPath {
		t.Fatalf("promotion source receipt = %q, want %q", receipt.SourceReceipt, validPath)
	}
}

func TestProductionStopsAfterFirstFailingCanaryHealthGate(t *testing.T) {
	root := t.TempDir()
	writeArtifactFixture(t, root)
	writeExecutable(t, root, "deploy.sh", `#!/bin/sh
printf '%s\n' "$SAM_HARNESS_CANARY_PERCENTAGE" >> .sam-harness/evidence/canary.log
`)
	writeExecutable(t, root, "health.sh", `#!/bin/sh
printf 'health:%s\n' "$SAM_HARNESS_CANARY_PERCENTAGE" >> .sam-harness/evidence/canary.log
[ "$SAM_HARNESS_CANARY_PERCENTAGE" != 50 ]
`)
	cfg := testPipelineConfig()
	cfg.Workflow.Deployment.Production.Command = []string{"./deploy.sh"}
	cfg.Workflow.Deployment.HealthChecks[0].Command = []string{"./health.sh"}
	cfg.Workflow.Deployment.CanaryPercentages = []int{10, 50, 100}
	writePipelineConfig(t, root, cfg)
	writePassingArtifactReceipt(t, root, cfg)

	receipt, _, err := Run(root, model.PhaseProduction, false)
	if err == nil || receipt.Passed {
		t.Fatalf("production passed a failing canary: err=%v receipt=%#v", err, receipt)
	}
	if log := readFile(t, filepath.Join(root, ".sam-harness/evidence/canary.log")); log != "10\nhealth:10\n50\nhealth:50\n" {
		t.Fatalf("canary did not stop at first failure: %q", log)
	}
}

func TestArtifactMutationDuringOnlyProductionCanaryBlocksBeforeHealth(t *testing.T) {
	root := t.TempDir()
	healthMarker := filepath.Join(t.TempDir(), "health-ran")
	writeArtifactFixture(t, root)
	writeExecutable(t, root, "mutating-deploy.sh", `#!/bin/sh
test "$SAM_HARNESS_PIPELINE_PHASE" = production
test "$SAM_HARNESS_CANARY_PERCENTAGE" = 100
printf 'mutated-during-promotion\n' > out/app.bin
`)
	writeExecutable(t, root, "health-marker.sh", `#!/bin/sh
printf 'ran\n' > "$1"
`)
	cfg := testPipelineConfig()
	cfg.Workflow.Deployment.Production.Command = []string{"./mutating-deploy.sh"}
	cfg.Workflow.Deployment.HealthChecks[0].Command = []string{"./health-marker.sh", healthMarker}
	cfg.Workflow.Deployment.CanaryPercentages = []int{100}
	writePipelineConfig(t, root, cfg)
	writePassingArtifactReceipt(t, root, cfg)

	receipt, _, err := Run(root, model.PhaseProduction, false)
	if err == nil || !strings.Contains(err.Error(), "after promotion command") || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("last-canary artifact mutation was accepted: err=%v receipt=%#v", err, receipt)
	}
	if receipt.Passed || receipt.Status != StatusFailed || len(receipt.Commands) != 1 {
		t.Fatalf("mutation receipt is not fail-closed: %#v", receipt)
	}
	if _, err := os.Stat(healthMarker); !os.IsNotExist(err) {
		t.Fatalf("health command ran after artifact mutation: %v", err)
	}
}

func TestPhaseAuthorityBlocksDeploymentCommands(t *testing.T) {
	root := t.TempDir()
	cfg := testPipelineConfig()
	cfg.Authority.Deploy = false
	fingerprint, err := repositoryFingerprint(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []model.Phase{model.PhaseStaging, model.PhaseProduction, model.PhaseObserve, model.PhaseRollback, model.PhaseMigration} {
		receipt, err := runPhase(root, cfg, phase, fingerprint, nil, "")
		if err == nil || receipt.Status != StatusBlocked || len(receipt.Commands) != 0 {
			t.Fatalf("%s crossed deploy authority: err=%v receipt=%#v", phase, err, receipt)
		}
	}
}

func TestReviewRequiresNetworkAuthority(t *testing.T) {
	root := t.TempDir()
	cfg := testPipelineConfig()
	cfg.Authority.Network = false
	fingerprint, err := repositoryFingerprint(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := runPhase(root, cfg, model.PhaseReview, fingerprint, nil, "")
	if err == nil || receipt.Status != StatusBlocked || len(receipt.Commands) != 0 || !strings.Contains(err.Error(), "network authority") {
		t.Fatalf("review crossed network authority: err=%v receipt=%#v", err, receipt)
	}
}

func TestReviewRequiresFilesystemReadOnlyAttestation(t *testing.T) {
	cfg := testPipelineConfig()
	cfg.Workflow.Reviewers[0].FilesystemReadOnly = false
	if err := validateReviewerSet(cfg.Workflow.Reviewers); err == nil || !strings.Contains(err.Error(), "filesystem_read_only attestation") {
		t.Fatalf("reviewer without filesystem attestation was accepted: %v", err)
	}
}

func TestRemoteWorkflowPhasesRequireNetworkAuthority(t *testing.T) {
	root := t.TempDir()
	cfg := testPipelineConfig()
	cfg.Authority.Network = false
	fingerprint, err := repositoryFingerprint(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []model.Phase{model.PhaseStaging, model.PhaseProduction, model.PhaseObserve, model.PhaseRollback, model.PhaseMigration} {
		receipt, err := runPhase(root, cfg, phase, fingerprint, nil, "")
		if err == nil || receipt.Status != StatusBlocked || len(receipt.Commands) != 0 || !strings.Contains(err.Error(), "network authority") {
			t.Fatalf("%s crossed network authority: err=%v receipt=%#v", phase, err, receipt)
		}
	}
}

func TestProductionAndRollbackRequireReleaseAuthority(t *testing.T) {
	root := t.TempDir()
	cfg := testPipelineConfig()
	cfg.Authority.Deploy = true
	cfg.Authority.Release = false
	fingerprint, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []model.Phase{model.PhaseProduction, model.PhaseRollback} {
		receipt, err := runPhase(root, cfg, phase, fingerprint, nil, "")
		if err == nil || receipt.Status != StatusBlocked || len(receipt.Commands) != 0 || !strings.Contains(err.Error(), "release authority") {
			t.Fatalf("%s crossed release authority: err=%v receipt=%#v", phase, err, receipt)
		}
	}
}

func TestRequiredGateRunsAsWorkflowPhasePrecondition(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "deployed")
	writeExecutable(t, root, "gate.sh", "#!/bin/sh\nexit 17\n")
	writeExecutable(t, root, "deploy.sh", "#!/bin/sh\nprintf 'ran\\n' > \"$1\"\n")
	cfg := testPipelineConfig()
	cfg.Gates = []model.Gate{{Name: "production-precondition", Stage: "production", Phase: model.PhaseProduction, Workdir: ".", Command: []string{"./gate.sh"}, Required: true}}
	cfg.Workflow.Deployment.Production.Command = []string{"./deploy.sh", marker}
	writePipelineConfig(t, root, cfg)

	receipt, _, err := Run(root, model.PhaseProduction, false)
	if err == nil || receipt.Passed || len(receipt.Commands) != 1 || receipt.Commands[0].Name != "production-precondition" {
		t.Fatalf("required workflow gate was not enforced first: err=%v receipt=%#v", err, receipt)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("production ran after failed precondition: %v", err)
	}
}

func TestReceiptOutputRedactsSensitiveEnvironmentValues(t *testing.T) {
	root := t.TempDir()
	secret := "receipt-must-not-contain-this-secret"
	t.Setenv("DEPLOY_API_TOKEN", secret)
	writeExecutable(t, root, "print-secret.sh", "#!/bin/sh\nprintf '%s\\n' \"$DEPLOY_API_TOKEN\"\n")
	cfg := testPipelineConfig()
	cfg.Gates = []model.Gate{{Name: "secret-printer", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./print-secret.sh"}, Required: true}}
	writePipelineConfig(t, root, cfg)

	receipt, receiptPath, err := Run(root, model.PhaseStatic, true)
	if err != nil {
		t.Fatalf("secret fixture failed: %v", err)
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(receipt.Commands[0].Output, secret) || strings.Contains(string(data), secret) || !strings.Contains(receipt.Commands[0].Output, "[REDACTED]") {
		t.Fatalf("secret was persisted: result=%q receipt=%s", receipt.Commands[0].Output, data)
	}
}

func writeArtifactFixture(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "out"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]string{
		"out/app.bin":         "artifact-v1\n",
		"out/sbom.json":       "{}\n",
		"out/provenance.json": "{}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writePassingArtifactReceipt(t *testing.T, root string, cfg model.Config) string {
	t.Helper()
	_, digest, err := hashRepositoryFile(root, cfg.Workflow.Artifact.ArtifactPath)
	if err != nil {
		t.Fatal(err)
	}
	_, sbomDigest, err := hashRepositoryFile(root, cfg.Workflow.Artifact.SBOMPath)
	if err != nil {
		t.Fatal(err)
	}
	_, provenanceDigest, err := hashRepositoryFile(root, cfg.Workflow.Artifact.ProvenancePath)
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := repositoryFingerprint(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	receipt := newReceipt(root, model.PhaseArtifact, fingerprint)
	receipt.Passed = true
	receipt.Status = StatusPassed
	receipt.Artifact = &ArtifactEvidence{
		Path:              cfg.Workflow.Artifact.ArtifactPath,
		SHA256:            digest,
		SBOMPath:          cfg.Workflow.Artifact.SBOMPath,
		SBOMSHA256:        sbomDigest,
		ProvenancePath:    cfg.Workflow.Artifact.ProvenancePath,
		ProvenanceSHA256:  provenanceDigest,
		SourceFingerprint: fingerprint,
	}
	receipt.FinalFingerprint = fingerprint
	receipt.FinishedAt = receipt.StartedAt
	path, err := writeReceiptFile(root, cfg.Evidence.ReceiptDirectory, receipt)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func testPipelineConfig() model.Config {
	reviewers := make([]model.ReviewerConfig, 0, len(model.ReviewerRoles))
	for _, role := range model.ReviewerRoles {
		reviewers = append(reviewers, model.ReviewerConfig{Role: role, Command: []string{"go", "version"}, TimeoutSeconds: 5, FilesystemReadOnly: true})
	}
	command := func(name string) model.CommandSpec {
		return model.CommandSpec{Name: name, Workdir: ".", Command: []string{"go", "version"}, Required: true, TimeoutSeconds: 5}
	}
	return model.Config{
		SchemaVersion:  model.SchemaVersion,
		HarnessVersion: model.HarnessVersion,
		Profile:        model.ProfileBaseline,
		Repository:     "fixture",
		Authority:      model.Authority{WriteRepository: true, Network: true, Deploy: true, Release: true},
		Evidence:       model.Evidence{ReceiptDirectory: ".sam-harness/evidence", RequiredStates: []string{"source"}},
		Governance: model.GovernanceConfig{
			Approvers:       []string{"owner"},
			Criticality:     "low",
			DataSensitivity: "public",
		},
		Workflow: &model.WorkflowConfig{
			Enabled:      true,
			StaticGuards: waivedGuardSet(model.StaticGuardCategories),
			TestGuards:   waivedGuardSet(model.TestGuardCategories),
			Reviewers:    reviewers,
			Correction: model.CorrectionConfig{
				Enabled: false,
			},
			Artifact: model.ArtifactWorkflow{
				Build:          command("build"),
				ArtifactPath:   "out/app.bin",
				SBOM:           command("sbom"),
				SBOMPath:       "out/sbom.json",
				Provenance:     command("provenance"),
				ProvenancePath: "out/provenance.json",
			},
			Deployment: model.DeploymentWorkflow{
				Staging:           command("staging"),
				Production:        command("production"),
				Rollback:          command("rollback"),
				HealthChecks:      []model.CommandSpec{command("health")},
				ObservationChecks: []model.CommandSpec{command("observe")},
				CanaryPercentages: []int{100},
			},
			Migration:       []model.CommandSpec{command("migration")},
			ReleaseSchedule: model.ReleaseSchedule{Cron: "0 9 * * 1", Timezone: "UTC"},
		},
	}
}

func waivedGuardSet(categories []string) model.GuardSet {
	waivers := make(map[string]string, len(categories))
	for _, category := range categories {
		waivers[category] = "not applicable in this focused runtime fixture"
	}
	return model.GuardSet{Commands: map[string]model.CommandSpec{}, Waivers: waivers}
}

func writePipelineConfig(t *testing.T, root string, cfg model.Config) {
	t.Helper()
	path := filepath.Join(root, ".sam-harness", "config.yaml")
	writePipelineConfigAt(t, path, cfg)
	if err := os.MkdirAll(filepath.Join(root, ".sam-harness", "evidence"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writePipelineConfigAt(t *testing.T, path string, cfg model.Config) {
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

func writeExecutable(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func initializeTestGit(t *testing.T, root string) string {
	t.Helper()
	if err := initializeSandboxGit(root); err != nil {
		t.Fatalf("initialize test Git repository: %v", err)
	}
	command := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("read test Git HEAD: %v: %s", err, output)
	}
	return strings.TrimSpace(string(output))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
