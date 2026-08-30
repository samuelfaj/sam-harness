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

	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestRepairEnforcesBudgetAndRerunsStaticAndTest(t *testing.T) {
	root := t.TempDir()
	trusted := t.TempDir()
	gateLog := filepath.Join(t.TempDir(), "gates.log")
	t.Setenv("GH_TOKEN", "must-not-reach-repair")
	t.Setenv("REPAIR_AGENT_TOKEN", "repair-allowlisted-secret")
	repairCommand := filepath.Join(trusted, "repair.sh")
	writeExecutable(t, trusted, "repair.sh", `#!/bin/sh
payload=$(cat)
case "$payload" in
  *untrusted*'"current_repository_fingerprint"'*'"budget"'*'"failed_receipt"'*) ;;
  *) exit 41 ;;
esac
test "$SAM_HARNESS_PIPELINE_PHASE" = repair
test "$SAM_HARNESS_REPAIR_ATTEMPT" = 1
test -z "$GH_TOKEN"
test "$REPAIR_AGENT_TOKEN" = repair-allowlisted-secret
test -z "$(git remote)"
printf '%s\n' "$REPAIR_AGENT_TOKEN"
printf 'fixed\n' > target.txt
`)
	writeExecutable(t, root, "static.sh", `#!/bin/sh
test "$SAM_HARNESS_PIPELINE_PHASE" = static
grep -q '^fixed$' target.txt
printf 'static\n' >> "$1"
`)
	writeExecutable(t, root, "test.sh", `#!/bin/sh
test "$SAM_HARNESS_PIPELINE_PHASE" = test
grep -q '^fixed$' target.txt
printf 'test\n' >> "$1"
`)
	if err := os.WriteFile(filepath.Join(root, "target.txt"), []byte("broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testPipelineConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: model.CISecretScopeRepair, Environment: "REPAIR_AGENT_TOKEN", Secret: "REPAIR_AGENT_TOKEN"}}}
	cfg.Workflow.Correction = model.CorrectionConfig{
		Enabled:                true,
		FilesystemSandboxed:    true,
		Command:                []string{repairCommand},
		TrustedExternalCommand: true,
		MaxAttempts:            2,
		MaxChangedFiles:        1,
		MaxChangedLines:        2,
		BranchPrefix:           "sam-repair/",
	}
	cfg.Gates = []model.Gate{
		{Name: "static", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./static.sh", gateLog}, Required: true},
		{Name: "test", Stage: "local", Phase: model.PhaseTest, Workdir: ".", Command: []string{"./test.sh", gateLog}, Required: true},
	}
	writePipelineConfig(t, root, cfg)
	trustedConfig := filepath.Join(trusted, "config.yaml")
	writePipelineConfigAt(t, trustedConfig, cfg)
	failedPath := writeFailedReceiptWithConfig(t, root, trustedConfig)

	receipt, outputPath, err := RepairWithConfig(root, trustedConfig, failedPath, true)
	if err != nil {
		t.Fatalf("Repair() failed: %v\n%#v", err, receipt)
	}
	if !receipt.Passed || receipt.Status != StatusPassed || len(receipt.Attempts) != 1 {
		t.Fatalf("repair receipt = %#v", receipt)
	}
	attempt := receipt.Attempts[0]
	if attempt.ChangedFiles != 1 || attempt.ChangedLines != 2 || attempt.Static == nil || !attempt.Static.Passed || attempt.Test == nil || !attempt.Test.Passed {
		t.Fatalf("repair attempt evidence = %#v", attempt)
	}
	if outputPath == "" || readFile(t, gateLog) != "static\ntest\n" {
		t.Fatalf("repair did not rerun both phases: output=%q", outputPath)
	}
	if receipt.RepairPatch == "" {
		t.Fatal("successful repair did not emit its validated correction patch")
	}
	if strings.Contains(receipt.Attempts[0].Command.Output, "repair-allowlisted-secret") || !strings.Contains(receipt.Attempts[0].Command.Output, "[REDACTED]") {
		t.Fatalf("repair secret was persisted in command evidence: %#v", receipt.Attempts[0].Command)
	}
	patch := readFile(t, receipt.RepairPatch)
	if !strings.Contains(patch, "target.txt") || !strings.Contains(patch, "+fixed") {
		t.Fatalf("repair patch does not represent the applied correction: %s", patch)
	}
	if receipt.RepairPatchSHA256 == "" || verifyRepairPatch(receipt.RepairPatch, receipt.RepairPatchSHA256) != nil {
		t.Fatalf("repair patch digest is missing or invalid: %#v", receipt)
	}
	if err := os.WriteFile(receipt.RepairPatch, append([]byte(patch), []byte("tampered\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyRepairPatch(receipt.RepairPatch, receipt.RepairPatchSHA256); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("tampered repair patch was not detected: %v", err)
	}
}

func TestReviewRepairRequiresIntactManifestAndIncludesEveryActionInPrompt(t *testing.T) {
	root := t.TempDir()
	cfg := testPipelineConfig()
	writePipelineConfig(t, root, cfg)
	testReceiptPath := writeFailedReceipt(t, root)
	var failed Receipt
	if err := json.Unmarshal([]byte(readFile(t, testReceiptPath)), &failed); err != nil {
		t.Fatal(err)
	}
	failed.Phase = model.PhaseReview
	failed.Error = "review blocked by P1 findings"
	failed.ReviewBaseSHA = strings.Repeat("a", 40)
	failed.ReviewBaseFingerprint = strings.Repeat("d", sha256.Size*2)
	failed.ReviewHeadSHA = strings.Repeat("b", 40)
	failed.ReviewHeadFingerprint = failed.Fingerprint
	failed.ReviewPatchSHA256 = strings.Repeat("c", sha256.Size*2)
	failed.Findings = []Finding{
		{Role: model.ReviewerSecurity, Severity: "P1", Summary: "unsafe input", Evidence: "input.go:4", Path: "input.go", Line: 4, RequiredChange: "validate input", Acceptance: "invalid input is rejected"},
		{Role: model.ReviewerSimplicity, Severity: "P2", Summary: "duplicate branch", Evidence: "input.go:8", Path: "input.go", Line: 8, RequiredChange: "remove the duplicate branch", Acceptance: "one branch remains"},
	}
	if err := attachRepairManifest(&failed); err != nil {
		t.Fatal(err)
	}
	reviewReceiptPath := filepath.Join(root, cfg.Evidence.ReceiptDirectory, receiptFilename(failed))
	writeReceiptAt(t, reviewReceiptPath, failed)
	rawConfig, err := os.ReadFile(filepath.Join(root, ".sam-harness", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	configDigest := sha256.Sum256(rawConfig)
	_, loaded, err := loadFailedReceipt(root, cfg, reviewReceiptPath, failed.Fingerprint, hex.EncodeToString(configDigest[:]))
	if err != nil {
		t.Fatalf("intact review manifest was rejected: %v", err)
	}
	prompt, err := correctionPrompt(root, failed.Fingerprint, model.CorrectionConfig{MaxAttempts: 1, MaxChangedFiles: 2, MaxChangedLines: 4}, 1, loaded)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Instruction      string          `json:"instruction"`
		TopLevelManifest *RepairManifest `json:"repair_manifest"`
		FailedReceipt    Receipt         `json:"failed_receipt"`
	}
	if err := json.Unmarshal(prompt, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TopLevelManifest != nil || decoded.FailedReceipt.RepairManifest == nil || len(decoded.FailedReceipt.RepairManifest.Actions) != 2 || !strings.Contains(decoded.Instruction, "every verified action") {
		t.Fatalf("correction prompt omitted consolidated actions: %s", prompt)
	}

	tampered := failed
	tampered.RepairManifest = nil
	writeReceiptAt(t, reviewReceiptPath, tampered)
	if _, _, err := loadFailedReceipt(root, cfg, reviewReceiptPath, failed.Fingerprint, hex.EncodeToString(configDigest[:])); err == nil || !strings.Contains(err.Error(), "no complete repair manifest") {
		t.Fatalf("review receipt without manifest was accepted: %v", err)
	}
	tampered = failed
	manifestCopy := *failed.RepairManifest
	manifestCopy.Actions = append([]Finding(nil), failed.RepairManifest.Actions...)
	manifestCopy.Actions[0].Acceptance = "different acceptance"
	tampered.RepairManifest = &manifestCopy
	writeReceiptAt(t, reviewReceiptPath, tampered)
	if _, _, err := loadFailedReceipt(root, cfg, reviewReceiptPath, failed.Fingerprint, hex.EncodeToString(configDigest[:])); err == nil || !strings.Contains(err.Error(), "actions do not match findings") {
		t.Fatalf("tampered review manifest was accepted: %v", err)
	}

	for _, test := range []struct {
		name       string
		mutate     func(*Receipt)
		errorMatch string
	}{
		{name: "digest", mutate: func(receipt *Receipt) { receipt.RepairManifestSHA256 = strings.Repeat("0", sha256.Size*2) }, errorMatch: "digest does not match"},
		{name: "base fingerprint", mutate: func(receipt *Receipt) { receipt.RepairManifest.ReviewBaseFingerprint = "different" }, errorMatch: "lineage does not match"},
		{name: "empty actions", mutate: func(receipt *Receipt) { receipt.RepairManifest.Actions = nil }, errorMatch: "actions do not match findings"},
		{name: "invalid action", mutate: func(receipt *Receipt) { receipt.RepairManifest.Actions[0].Path = "../escape" }, errorMatch: "invalid"},
		{name: "no blocker", mutate: func(receipt *Receipt) {
			for index := range receipt.Findings {
				receipt.Findings[index].Severity = "P2"
				receipt.RepairManifest.Actions[index].Severity = "P2"
			}
			digest, digestErr := repairManifestDigest(*receipt.RepairManifest)
			if digestErr != nil {
				t.Fatal(digestErr)
			}
			receipt.RepairManifestSHA256 = digest
		}, errorMatch: "no blocking finding"},
		{name: "arbiter conflict", mutate: func(receipt *Receipt) { receipt.ArbiterBlocked = true }, errorMatch: "conflicting review findings"},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(failed)
			if err != nil {
				t.Fatal(err)
			}
			var candidate Receipt
			if err := json.Unmarshal(raw, &candidate); err != nil {
				t.Fatal(err)
			}
			test.mutate(&candidate)
			writeReceiptAt(t, reviewReceiptPath, candidate)
			if _, _, err := loadFailedReceipt(root, cfg, reviewReceiptPath, failed.Fingerprint, hex.EncodeToString(configDigest[:])); err == nil || !strings.Contains(err.Error(), test.errorMatch) {
				t.Fatalf("tampered review receipt was accepted: %v", err)
			}
		})
	}
}

func TestReviewRepairAppliesEveryManifestActionInOneAttempt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "first.txt", "broken first\n")
	writeFile(t, root, "second.txt", "broken second\n")
	writeExecutable(t, root, "repair.sh", `#!/bin/sh
payload=$(cat)
case "$payload" in
  *'fix first file'*'fix second file'*) ;;
  *) exit 41 ;;
esac
printf 'fixed first\n' > first.txt
printf 'fixed second\n' > second.txt
`)
	writeExecutable(t, root, "static.sh", `#!/bin/sh
grep -q '^fixed first$' first.txt
grep -q '^fixed second$' second.txt
`)
	writeExecutable(t, root, "test.sh", `#!/bin/sh
grep -q '^fixed first$' first.txt
grep -q '^fixed second$' second.txt
`)
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{
		Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"},
		MaxAttempts: 1, MaxChangedFiles: 2, MaxChangedLines: 4, BranchPrefix: "sam-repair/",
	}
	cfg.Gates = []model.Gate{
		{Name: "static", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./static.sh"}, Required: true},
		{Name: "test", Stage: "local", Phase: model.PhaseTest, Workdir: ".", Command: []string{"./test.sh"}, Required: true},
	}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)
	var failed Receipt
	if err := json.Unmarshal([]byte(readFile(t, failedPath)), &failed); err != nil {
		t.Fatal(err)
	}
	failed.Phase = model.PhaseReview
	failed.Status = StatusBlocked
	failed.Error = "review blocked by complete findings"
	failed.ReviewBaseSHA = strings.Repeat("a", 40)
	failed.ReviewBaseFingerprint = strings.Repeat("b", sha256.Size*2)
	failed.ReviewHeadSHA = strings.Repeat("c", 40)
	failed.ReviewHeadFingerprint = failed.Fingerprint
	failed.ReviewPatchSHA256 = strings.Repeat("d", sha256.Size*2)
	failed.Findings = []Finding{
		{Role: model.ReviewerCorrectness, Severity: "P1", Summary: "first is broken", Evidence: "first.txt:1", Path: "first.txt", Line: 1, RequiredChange: "fix first file", Acceptance: "first file is fixed"},
		{Role: model.ReviewerBusinessRules, Severity: "P2", Summary: "second is broken", Evidence: "second.txt:1", Path: "second.txt", Line: 1, RequiredChange: "fix second file", Acceptance: "second file is fixed"},
	}
	if err := attachRepairManifest(&failed); err != nil {
		t.Fatal(err)
	}
	reviewReceiptPath := filepath.Join(root, cfg.Evidence.ReceiptDirectory, receiptFilename(failed))
	writeReceiptAt(t, reviewReceiptPath, failed)

	receipt, _, err := RepairWithConfig(root, "", reviewReceiptPath, false)
	if err != nil {
		t.Fatalf("multi-action review repair failed: %v\n%#v", err, receipt)
	}
	if len(receipt.Attempts) != 1 || readFile(t, filepath.Join(root, "first.txt")) != "fixed first\n" || readFile(t, filepath.Join(root, "second.txt")) != "fixed second\n" {
		t.Fatalf("review repair did not apply every action in one attempt: %#v", receipt)
	}
	patch := readFile(t, receipt.RepairPatch)
	if !strings.Contains(patch, "first.txt") || !strings.Contains(patch, "second.txt") {
		t.Fatalf("review repair patch omitted an action: %s", patch)
	}
}

func TestRepairTrustedConfigOverrideGovernsPRWorktree(t *testing.T) {
	root := t.TempDir()
	exfilMarker := filepath.Join(t.TempDir(), "pr-repair-ran")
	writeFile(t, root, "target.txt", "broken\n")
	writeExecutable(t, root, "trusted-repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf 'fixed\\n' > target.txt\n")
	writeExecutable(t, root, "exfil-repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf exfiltrated > \"$1\"\n")

	prConfig := testPipelineConfig()
	prConfig.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./exfil-repair.sh", exfilMarker}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, prConfig)

	trustedConfig := testPipelineConfig()
	trustedConfig.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./trusted-repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	trustedPath := filepath.Join(t.TempDir(), "trusted-config.yaml")
	writePipelineConfigAt(t, trustedPath, trustedConfig)
	failedPath := writeFailedReceiptWithConfig(t, root, trustedPath)

	receipt, receiptPath, err := RepairWithConfig(root, trustedPath, failedPath, true)
	if err != nil {
		t.Fatalf("trusted config repair failed: %v\n%#v", err, receipt)
	}
	if got := readFile(t, filepath.Join(root, "target.txt")); got != "fixed\n" {
		t.Fatalf("trusted correction was not applied: %q", got)
	}
	if _, err := os.Stat(exfilMarker); !os.IsNotExist(err) {
		t.Fatalf("PR-controlled correction command executed: %v", err)
	}
	if receiptPath == "" || receipt.ConfigSource == "" || receipt.ConfigSHA256 == "" {
		t.Fatalf("repair receipt lacks trusted config provenance: path=%q receipt=%#v", receiptPath, receipt)
	}
}

func TestRepairAcceptsRelocatedReceiptOnlyWithMatchingRepositoryLineage(t *testing.T) {
	source := t.TempDir()
	target := t.TempDir()
	for _, root := range []string{source, target} {
		writeFile(t, root, "target.txt", "broken\n")
		writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf 'fixed\\n' > target.txt\n")
		cfg := testPipelineConfig()
		cfg.Repository = "example/relocatable"
		cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
		writePipelineConfig(t, root, cfg)
	}
	sourceReceipt := writeFailedReceiptWithConfig(t, source, filepath.Join(source, ".sam-harness", "config.yaml"))
	var failed Receipt
	if err := json.Unmarshal([]byte(readFile(t, sourceReceipt)), &failed); err != nil {
		t.Fatal(err)
	}
	failed.Repository = "example/relocatable"
	targetReceipt := filepath.Join(target, ".sam-harness", "evidence", filepath.Base(sourceReceipt))
	if err := os.MkdirAll(filepath.Dir(targetReceipt), 0o755); err != nil {
		t.Fatal(err)
	}
	writeReceiptAt(t, targetReceipt, failed)

	receipt, _, err := Repair(target, targetReceipt, false)
	if err != nil || !receipt.Passed {
		t.Fatalf("matching relocated receipt was rejected: err=%v receipt=%#v", err, receipt)
	}
	if got := readFile(t, filepath.Join(target, "target.txt")); got != "fixed\n" {
		t.Fatalf("relocated repair did not apply: %q", got)
	}

	other := t.TempDir()
	writeFile(t, other, "target.txt", "broken\n")
	writeExecutable(t, other, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf 'fixed\\n' > target.txt\n")
	cfg := testPipelineConfig()
	cfg.Repository = "different/repository"
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, other, cfg)
	otherReceipt := filepath.Join(other, ".sam-harness", "evidence", filepath.Base(sourceReceipt))
	if err := os.MkdirAll(filepath.Dir(otherReceipt), 0o755); err != nil {
		t.Fatal(err)
	}
	foreign := failed
	otherConfig := filepath.Join(other, ".sam-harness", "config.yaml")
	otherConfigData, err := os.ReadFile(otherConfig)
	if err != nil {
		t.Fatal(err)
	}
	otherConfigDigest := sha256.Sum256(otherConfigData)
	foreign.ConfigSHA256 = hex.EncodeToString(otherConfigDigest[:])
	foreign.ConfigSource = otherConfig
	foreign.Fingerprint, err = sourceFingerprint(other, nil)
	if err != nil {
		t.Fatal(err)
	}
	foreign.FinalFingerprint = foreign.Fingerprint
	writeReceiptAt(t, otherReceipt, foreign)
	blocked, _, err := Repair(other, otherReceipt, false)
	if err == nil || blocked.Status != StatusBlocked || !strings.Contains(err.Error(), "different repository") {
		t.Fatalf("foreign relocated receipt was accepted: err=%v receipt=%#v", err, blocked)
	}
}

func TestSecretBearingRepairRejectsTargetControlledExecutableBeforeItRuns(t *testing.T) {
	root := t.TempDir()
	trusted := t.TempDir()
	marker := filepath.Join(t.TempDir(), "target-repairer-ran")
	writeFile(t, root, "target.txt", "broken\n")
	writeExecutable(t, root, "repairer", fmt.Sprintf("#!/bin/sh\ncat >/dev/null\nprintf ran > %q\n", marker))
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("REPAIR_TOKEN", "repair-secret")

	cfg := testPipelineConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: model.CISecretScopeRepair, Environment: "REPAIR_TOKEN", Secret: "REPAIR_TOKEN"}}}
	cfg.Workflow.Correction = model.CorrectionConfig{
		Enabled:                true,
		FilesystemSandboxed:    true,
		Command:                []string{"repairer"},
		TrustedExternalCommand: true,
		MaxAttempts:            1,
		MaxChangedFiles:        1,
		MaxChangedLines:        2,
		BranchPrefix:           "sam-repair/",
	}
	writePipelineConfig(t, root, cfg)
	trustedConfig := filepath.Join(trusted, "config.yaml")
	writePipelineConfigAt(t, trustedConfig, cfg)
	failedPath := writeFailedReceiptWithConfig(t, root, trustedConfig)

	receipt, _, err := RepairWithConfig(root, trustedConfig, failedPath, false)
	if err == nil || receipt.Status != StatusBlocked || !strings.Contains(err.Error(), "resolves inside the target repository") {
		t.Fatalf("target-controlled correction was accepted: err=%v receipt=%#v", err, receipt)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("target-controlled correction executed with a secret: %v", err)
	}
}

func TestSecretBearingRepairBlocksSecretInDeltaBeforeApplyPatchOrRetry(t *testing.T) {
	root := t.TempDir()
	trusted := t.TempDir()
	t.Setenv("BOUND_VALUE", "audit-repair-secret-must-not-enter-patch")
	writeFile(t, root, "target.txt", "broken\n")
	repairCommand := filepath.Join(trusted, "repair.sh")
	writeExecutable(t, trusted, "repair.sh", `#!/bin/sh
cat >/dev/null
if test "$SAM_HARNESS_REPAIR_ATTEMPT" = 1; then
  printf '%s\n' "$BOUND_VALUE" > target.txt
  exit 1
fi
printf 'retry-ran\n' > retry.txt
printf 'fixed\n' > target.txt
`)

	cfg := testPipelineConfig()
	cfg.CI.Providers = []string{"github"}
	cfg.CI.SecretBindings = map[string][]model.CISecretBinding{"github": {{Scope: model.CISecretScopeRepair, Environment: "BOUND_VALUE", Secret: "REPAIR_TOKEN"}}}
	cfg.Workflow.Correction = model.CorrectionConfig{
		Enabled:                true,
		FilesystemSandboxed:    true,
		Command:                []string{repairCommand},
		TrustedExternalCommand: true,
		MaxAttempts:            2,
		MaxChangedFiles:        2,
		MaxChangedLines:        4,
		BranchPrefix:           "sam-repair/",
	}
	writePipelineConfig(t, root, cfg)
	trustedConfig := filepath.Join(trusted, "config.yaml")
	writePipelineConfigAt(t, trustedConfig, cfg)
	failedPath := writeFailedReceiptWithConfig(t, root, trustedConfig)

	receipt, _, err := RepairWithConfig(root, trustedConfig, failedPath, true)
	if err == nil || receipt.Status != StatusBlocked || !strings.Contains(err.Error(), "protected secret") {
		t.Fatalf("secret-bearing repair delta was accepted: err=%v receipt=%#v", err, receipt)
	}
	if strings.Contains(err.Error(), os.Getenv("BOUND_VALUE")) || strings.Contains(receipt.Error, os.Getenv("BOUND_VALUE")) {
		t.Fatalf("secret value leaked into repair error: err=%v receipt=%#v", err, receipt)
	}
	if got := readFile(t, filepath.Join(root, "target.txt")); got != "broken\n" {
		t.Fatalf("secret-bearing delta reached target: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "retry.txt")); !os.IsNotExist(err) {
		t.Fatalf("repair retried after detecting a secret delta: %v", err)
	}
	if receipt.RepairPatch != "" || receipt.RepairPatchSHA256 != "" {
		t.Fatalf("blocked secret delta emitted a publishable patch: %#v", receipt)
	}
	patches, globErr := filepath.Glob(filepath.Join(root, cfg.Evidence.ReceiptDirectory, "*-repair.patch"))
	if globErr != nil || len(patches) != 0 {
		t.Fatalf("blocked secret delta wrote repair patch: paths=%v err=%v", patches, globErr)
	}
}

func TestRepairSecretDeltaDetectionCoversNewTextBinarySymlinkAndPreservesUnchangedBaseline(t *testing.T) {
	secret := "repair-secret-value"
	baseline := map[string]fileState{
		"unchanged.txt": {data: []byte("existing " + secret + " remains untouched\n"), mode: 0o644},
		"moved.txt":     {data: []byte(secret + "\nold\n"), mode: 0o644},
	}
	safe := cloneSnapshot(baseline)
	safe["safe.txt"] = fileState{data: []byte("fixed\n"), mode: 0o644}
	safePatch, err := canonicalSnapshotPatch(baseline, safe)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateRepairSecretDelta(baseline, safe, safePatch, []string{secret}); err != nil {
		t.Fatalf("unchanged baseline secret was rejected: %v", err)
	}

	cases := map[string]map[string]fileState{
		"text": func() map[string]fileState {
			value := cloneSnapshot(baseline)
			value["new.txt"] = fileState{data: []byte("prefix " + secret + " suffix\n"), mode: 0o644}
			return value
		}(),
		"binary": func() map[string]fileState {
			value := cloneSnapshot(baseline)
			value["new.bin"] = fileState{data: append(append([]byte{0x00, 0xff}, []byte(secret)...), 0x00), mode: 0o644}
			return value
		}(),
		"symlink": func() map[string]fileState {
			value := cloneSnapshot(baseline)
			value["new-link"] = fileState{link: "../" + secret, mode: os.ModeSymlink | 0o777}
			return value
		}(),
	}
	for name, after := range cases {
		t.Run(name, func(t *testing.T) {
			patch, err := canonicalSnapshotPatch(baseline, after)
			if err != nil {
				t.Fatal(err)
			}
			if err := validateRepairSecretDelta(baseline, after, patch, []string{secret}); err == nil {
				t.Fatal("secret-bearing delta was accepted")
			}
		})
	}
}

func cloneSnapshot(snapshot map[string]fileState) map[string]fileState {
	cloned := make(map[string]fileState, len(snapshot))
	for path, state := range snapshot {
		state.data = append([]byte(nil), state.data...)
		cloned[path] = state
	}
	return cloned
}

func TestRepairRejectsFailedReceiptFromDifferentConfig(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "repair-ran")
	writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf ran > \"$1\"\n")
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh", marker}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 1, BranchPrefix: "sam-repair/"}
	trustedPath := filepath.Join(t.TempDir(), "trusted-config.yaml")
	writePipelineConfigAt(t, trustedPath, cfg)
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceiptWithConfig(t, root, trustedPath)
	var failed Receipt
	raw, err := os.ReadFile(failedPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &failed); err != nil {
		t.Fatal(err)
	}
	failed.ConfigSHA256 = strings.Repeat("0", sha256.Size*2)
	writeReceiptAt(t, failedPath, failed)

	receipt, _, err := RepairWithConfig(root, trustedPath, failedPath, false)
	if err == nil || receipt.Status != StatusBlocked || !strings.Contains(err.Error(), "config digest") {
		t.Fatalf("mismatched receipt config was accepted: err=%v receipt=%#v", err, receipt)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("repair command ran before config lineage validation: %v", err)
	}
}

func TestRepairBlocksCorrectionThatChangesDefaultTrustedConfig(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "target.txt", "broken\n")
	writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf '\\n# replaced\\n' >> .sam-harness/config.yaml\nprintf 'fixed\\n' > target.txt\n")
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 2, MaxChangedLines: 4, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	configBefore := readFile(t, filepath.Join(root, ".sam-harness", "config.yaml"))
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || receipt.Status != StatusBlocked || !strings.Contains(err.Error(), "changed trusted configuration") {
		t.Fatalf("trusted config correction was accepted: err=%v receipt=%#v", err, receipt)
	}
	if got := readFile(t, filepath.Join(root, ".sam-harness", "config.yaml")); got != configBefore {
		t.Fatalf("blocked correction changed trusted config: %q", got)
	}
	if got := readFile(t, filepath.Join(root, "target.txt")); got != "broken\n" {
		t.Fatalf("blocked config correction partially applied source delta: %q", got)
	}
}

func TestRepairIgnoresDependencyCacheDeltaAndNeverAppliesIt(t *testing.T) {
	root := t.TempDir()
	writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nmkdir -p node_modules/pkg target\nprintf cache > node_modules/pkg/cache.txt\nprintf cache > target/cache.txt\nprintf fixed > target.txt\n")
	writeFile(t, root, "target.txt", "broken")
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err != nil {
		t.Fatalf("cache output false-blocked repair: %v\n%#v", err, receipt)
	}
	if receipt.Attempts[0].ChangedFiles != 1 || !strings.Contains(readFile(t, receipt.RepairPatch), "target.txt") {
		t.Fatalf("cache output entered repair budget/patch: %#v", receipt)
	}
	for _, path := range []string{"node_modules/pkg/cache.txt", "target/cache.txt"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Fatalf("sandbox cache output was applied to target %s: %v", path, err)
		}
	}
}

func TestRepairCopyKeepsRunnableDependenciesAndSkipsRebuildableCaches(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"node_modules/pkg/keep.txt", "vendor/pkg/keep.txt", ".venv/keep.txt"} {
		writeFile(t, root, path, "required\n")
	}
	for _, path := range []string{"target/skip.txt", "dist/skip.txt", "build/skip.txt", ".tox/skip.txt", ".mypy_cache/skip.txt", ".pytest_cache/skip.txt", ".ruff_cache/skip.txt", "pkg/__pycache__/skip.txt"} {
		writeFile(t, root, path, "rebuildable\n")
	}
	writeFile(t, root, "target.txt", "broken\n")
	writeExecutable(t, root, "repair.sh", `#!/bin/sh
cat >/dev/null
test -f node_modules/pkg/keep.txt
test -f vendor/pkg/keep.txt
test -f .venv/keep.txt
test ! -e target/skip.txt
test ! -e dist/skip.txt
test ! -e build/skip.txt
test ! -e .tox/skip.txt
test ! -e .mypy_cache/skip.txt
test ! -e .pytest_cache/skip.txt
test ! -e .ruff_cache/skip.txt
test ! -e pkg/__pycache__/skip.txt
printf 'fixed\n' > target.txt
`)
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err != nil {
		t.Fatalf("repair sandbox copy policy failed: %v\n%#v", err, receipt)
	}
	if got := readFile(t, filepath.Join(root, "target.txt")); got != "fixed\n" {
		t.Fatalf("validated repair was not applied: %q", got)
	}
}

func TestRepairBlocksChangeBudgetOverflow(t *testing.T) {
	root := t.TempDir()
	writeExecutable(t, root, "repair.sh", `#!/bin/sh
cat >/dev/null
printf 'one\ntwo\n' > generated.txt
`)
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{
		Enabled:             true,
		FilesystemSandboxed: true,
		Command:             []string{"./repair.sh"},
		MaxAttempts:         2,
		MaxChangedFiles:     1,
		MaxChangedLines:     1,
		BranchPrefix:        "sam-repair/",
	}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || !strings.Contains(err.Error(), "change budget exceeded") {
		t.Fatalf("budget overflow was accepted: err=%v receipt=%#v", err, receipt)
	}
	if receipt.Status != StatusBlocked || len(receipt.Attempts) != 1 || receipt.Attempts[0].Static != nil || receipt.Attempts[0].Test != nil {
		t.Fatalf("budget overflow did not stop before gates: %#v", receipt)
	}
	if _, statErr := os.Stat(filepath.Join(root, "generated.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("over-budget repair left a target mutation: %v", statErr)
	}
	if receipt.RepairPatch != "" {
		t.Fatalf("blocked repair emitted an applicable patch: %#v", receipt)
	}
}

func TestRepairStopsAtAttemptLimitAfterFreshStaticAndTestRuns(t *testing.T) {
	root := t.TempDir()
	gateLog := filepath.Join(t.TempDir(), "gates.log")
	writeExecutable(t, root, "repair.sh", `#!/bin/sh
cat >/dev/null
printf 'repair:%s\n' "$SAM_HARNESS_REPAIR_ATTEMPT" >> .sam-harness/evidence/attempts.log
`)
	writeExecutable(t, root, "static.sh", `#!/bin/sh
printf 'static\n' >> "$1"
exit 1
`)
	writeExecutable(t, root, "test.sh", `#!/bin/sh
printf 'test\n' >> "$1"
`)
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{
		Enabled:             true,
		FilesystemSandboxed: true,
		Command:             []string{"./repair.sh"},
		MaxAttempts:         2,
		MaxChangedFiles:     1,
		MaxChangedLines:     1,
		BranchPrefix:        "sam-repair/",
	}
	cfg.Gates = []model.Gate{
		{Name: "static", Stage: "local", Phase: model.PhaseStatic, Workdir: ".", Command: []string{"./static.sh", gateLog}, Required: true},
		{Name: "test", Stage: "local", Phase: model.PhaseTest, Workdir: ".", Command: []string{"./test.sh", gateLog}, Required: true},
	}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || !strings.Contains(err.Error(), "attempt limit exhausted") {
		t.Fatalf("attempt exhaustion was accepted: err=%v receipt=%#v", err, receipt)
	}
	if receipt.Status != StatusBlocked || len(receipt.Attempts) != 2 {
		t.Fatalf("attempt ledger = %#v", receipt)
	}
	if gates := readFile(t, gateLog); gates != "static\ntest\nstatic\ntest\n" {
		t.Fatalf("static/test were not rerun after every attempt: %q", gates)
	}
}

func TestRepairRejectsUnrelatedOrStaleReceiptBeforeCorrection(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		mutate     func(*Receipt)
		errorMatch string
	}{
		{name: "wrong kind", mutate: func(receipt *Receipt) { receipt.Kind = "check" }, errorMatch: "pipeline receipt"},
		{name: "unrepairable phase", mutate: func(receipt *Receipt) { receipt.Phase = model.PhaseAll }, errorMatch: "repairable pipeline phase"},
		{name: "deployment phase", mutate: func(receipt *Receipt) { receipt.Phase = model.PhaseProduction }, errorMatch: "repairable pipeline phase"},
		{name: "wrong harness version", mutate: func(receipt *Receipt) { receipt.HarnessVersion = "0.1.0" }, errorMatch: "harness version"},
		{name: "mutating gate lineage", mutate: func(receipt *Receipt) { receipt.Fingerprint = "before-gate-mutation" }, errorMatch: "fingerprint"},
		{name: "stale fingerprint", mutate: func(receipt *Receipt) { receipt.FinalFingerprint = "stale" }, errorMatch: "fingerprint"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(t.TempDir(), "correction-ran")
			writeExecutable(t, root, "repair.sh", "#!/bin/sh\nprintf ran > \"$1\"\n")
			cfg := testPipelineConfig()
			cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh", marker}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 1, BranchPrefix: "sam-repair/"}
			writePipelineConfig(t, root, cfg)
			failedPath := writeFailedReceipt(t, root)
			var failed Receipt
			data, err := os.ReadFile(failedPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &failed); err != nil {
				t.Fatal(err)
			}
			scenario.mutate(&failed)
			writeReceiptAt(t, failedPath, failed)

			receipt, _, err := Repair(root, failedPath, false)
			if err == nil || !strings.Contains(err.Error(), scenario.errorMatch) || receipt.Status != StatusBlocked {
				t.Fatalf("invalid receipt was accepted: err=%v receipt=%#v", err, receipt)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("correction executed for invalid receipt: %v", err)
			}
		})
	}
}

func TestRepairFailureCannotMutateIgnoredOrGitControlPaths(t *testing.T) {
	root := t.TempDir()
	for path, value := range map[string]string{
		"vendor/dependency.txt": "vendor-original\n",
		"target/cache.txt":      "target-original\n",
		"dist/bundle.txt":       "dist-original\n",
		"build/output.txt":      "build-original\n",
		".git/config":           "git-original\n",
	} {
		writeFile(t, root, path, value)
	}
	writeExecutable(t, root, "repair.sh", `#!/bin/sh
cat >/dev/null
printf 'vendor-mutated\n' > vendor/dependency.txt
printf 'target-mutated\n' > target/cache.txt
printf 'dist-mutated\n' > dist/bundle.txt
printf 'build-mutated\n' > build/output.txt
printf 'git-mutated\n' > .git/config
exit 9
`)
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 10, MaxChangedLines: 20, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || receipt.Passed {
		t.Fatalf("failed correction was accepted: err=%v receipt=%#v", err, receipt)
	}
	for path, value := range map[string]string{
		"vendor/dependency.txt": "vendor-original\n",
		"target/cache.txt":      "target-original\n",
		"dist/bundle.txt":       "dist-original\n",
		"build/output.txt":      "build-original\n",
		".git/config":           "git-original\n",
	} {
		if got := readFile(t, filepath.Join(root, filepath.FromSlash(path))); got != value {
			t.Fatalf("failed repair mutated %s: %q", path, got)
		}
	}
	if receipt.RepairPatch != "" {
		t.Fatalf("failed repair emitted a patch: %#v", receipt)
	}
}

func TestRepairRejectsSymlinkThatEscapesSandbox(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(t.TempDir(), "external.txt")
	if err := os.WriteFile(external, []byte("outside-original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf escaped > escape.txt\n")
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || !strings.Contains(err.Error(), "symlink escapes repair sandbox") || receipt.Passed {
		t.Fatalf("escaping symlink was accepted: err=%v receipt=%#v", err, receipt)
	}
	if got := readFile(t, external); got != "outside-original\n" {
		t.Fatalf("external symlink target was mutated: %q", got)
	}
}

func TestRepairRejectsExternalAbsoluteArgvBeforeCorrectionExecutes(t *testing.T) {
	root := t.TempDir()
	externalMarker := filepath.Join(t.TempDir(), "outside-marker")
	writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf escaped > \"$1\"\n")
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh", externalMarker}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 1, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || !strings.Contains(err.Error(), "absolute path outside the repository") || receipt.Status != StatusBlocked {
		t.Fatalf("external absolute argv was accepted: err=%v receipt=%#v", err, receipt)
	}
	if _, err := os.Stat(externalMarker); !os.IsNotExist(err) {
		t.Fatalf("correction wrote through external argv before block: %v", err)
	}
}

func TestSandboxCommandAllowsAttestedAbsoluteExecutableOnly(t *testing.T) {
	root := t.TempDir()
	sandbox := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command, err := sandboxCommand(root, sandbox, []string{executable, "relative-input"})
	if err != nil || command[0] != executable {
		t.Fatalf("attested absolute executable was rejected: command=%#v err=%v", command, err)
	}
	if _, err := sandboxCommand(root, sandbox, []string{executable, filepath.Join(t.TempDir(), "external-input")}); err == nil {
		t.Fatal("external absolute data argument was accepted")
	}
}

func TestRepairRejectsNewSymlinkThatWouldEscapeTarget(t *testing.T) {
	root := t.TempDir()
	external := filepath.Join(t.TempDir(), "external.txt")
	if err := os.WriteFile(external, []byte("outside-original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeExecutable(t, root, "repair.sh", fmt.Sprintf("#!/bin/sh\ncat >/dev/null\nln -s %q escape-new\n", external))
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 1, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || !strings.Contains(err.Error(), "symlink escapes the repository") || receipt.Status != StatusBlocked {
		t.Fatalf("new escaping symlink was accepted: err=%v receipt=%#v", err, receipt)
	}
	if _, err := os.Lstat(filepath.Join(root, "escape-new")); !os.IsNotExist(err) {
		t.Fatalf("escaping symlink was applied to target: %v", err)
	}
	if got := readFile(t, external); got != "outside-original\n" {
		t.Fatalf("external symlink target changed: %q", got)
	}
}

func TestRepairPatchExcludesPreexistingUserWorkAndAppliesUnstaged(t *testing.T) {
	root := t.TempDir()
	runRepairGit(t, root, "init")
	runRepairGit(t, root, "config", "user.email", "fixture@example.com")
	runRepairGit(t, root, "config", "user.name", "Fixture")
	writeFile(t, root, "target.txt", "broken\n")
	writeFile(t, root, "staged.txt", "base\n")
	writeFile(t, root, "unstaged.txt", "base\n")
	runRepairGit(t, root, "add", ".")
	runRepairGit(t, root, "commit", "-m", "baseline")
	writeFile(t, root, "staged.txt", "user-staged\n")
	runRepairGit(t, root, "add", "staged.txt")
	writeFile(t, root, "unstaged.txt", "user-unstaged\n")
	writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf 'fixed\\n' > target.txt\n")
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, true)
	if err != nil {
		t.Fatalf("Repair() failed: %v\n%#v", err, receipt)
	}
	patch := readFile(t, receipt.RepairPatch)
	if strings.Contains(patch, "staged.txt") || strings.Contains(patch, "unstaged.txt") || !strings.Contains(patch, "target.txt") {
		t.Fatalf("repair patch mixed preexisting user work into correction delta: %s", patch)
	}
	if got := readFile(t, filepath.Join(root, "staged.txt")); got != "user-staged\n" {
		t.Fatalf("repair changed staged user work: %q", got)
	}
	if got := readFile(t, filepath.Join(root, "unstaged.txt")); got != "user-unstaged\n" {
		t.Fatalf("repair changed unstaged user work: %q", got)
	}
	if staged := runRepairGit(t, root, "diff", "--cached", "--name-only"); strings.TrimSpace(staged) != "staged.txt" {
		t.Fatalf("repair changed the git index: %q", staged)
	}
	if unstaged := runRepairGit(t, root, "diff", "--name-only"); !strings.Contains(unstaged, "target.txt") {
		t.Fatalf("validated correction was not applied unstaged: %q", unstaged)
	}
}

func TestCanonicalRepairPatchReproducesTextBinaryModesAddsAndDeletes(t *testing.T) {
	before := map[string]fileState{
		"literal.txt": {data: []byte("keep\na/before/ must remain literal\n"), mode: 0o644},
		"binary.bin":  {data: []byte{0, 1, 2, 3, 4}, mode: 0o644},
		"deleted.txt": {data: []byte("delete me\n"), mode: 0o644},
	}
	after := map[string]fileState{
		"literal.txt": {data: []byte("keep\na/before/ must remain literal\nadded\n"), mode: 0o755},
		"binary.bin":  {data: []byte{0, 1, 9, 3, 4, 0xff}, mode: 0o644},
		"added.txt":   {data: []byte("new\n"), mode: 0o644},
	}
	patch, err := canonicalRepairPatch(before, after)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(patch, []byte(" a/before/ must remain literal")) {
		t.Fatalf("canonical path normalization corrupted hunk content:\n%s", patch)
	}
	root := t.TempDir()
	if err := materializeSnapshot(root, before); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "apply", "--binary", "--whitespace=nowarn", "-")
	command.Dir = root
	command.Stdin = bytes.NewReader(patch)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("canonical patch is not applicable: %v\n%s\n%s", err, output, patch)
	}
	actual, err := snapshotRepository(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshotsEqual(actual, after) {
		t.Fatalf("canonical patch did not reproduce validated delta: actual=%#v after=%#v", actual, after)
	}
}

func TestRepairBlocksCorrectionThatStagesOrCommits(t *testing.T) {
	root := t.TempDir()
	runRepairGit(t, root, "init")
	runRepairGit(t, root, "config", "user.email", "fixture@example.com")
	runRepairGit(t, root, "config", "user.name", "Fixture")
	writeFile(t, root, "target.txt", "broken\n")
	runRepairGit(t, root, "add", ".")
	runRepairGit(t, root, "commit", "-m", "baseline")
	writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf 'fixed\\n' > target.txt\ngit add target.txt\n")
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || !strings.Contains(err.Error(), "Git control data") || receipt.Status != StatusBlocked {
		t.Fatalf("staging correction was accepted: err=%v receipt=%#v", err, receipt)
	}
	if got := readFile(t, filepath.Join(root, "target.txt")); got != "broken\n" {
		t.Fatalf("blocked staging correction reached target: %q", got)
	}
}

func TestRepairUsesStandaloneGitForLinkedWorktree(t *testing.T) {
	repository := t.TempDir()
	runRepairGit(t, repository, "init")
	runRepairGit(t, repository, "config", "user.email", "fixture@example.com")
	runRepairGit(t, repository, "config", "user.name", "Fixture")
	writeFile(t, repository, "tracked.txt", "baseline\n")
	runRepairGit(t, repository, "add", ".")
	runRepairGit(t, repository, "commit", "-m", "baseline")
	root := filepath.Join(t.TempDir(), "linked")
	runRepairGit(t, repository, "worktree", "add", "-b", "repair-linked", root)
	writeExecutable(t, root, "repair.sh", "#!/bin/sh\ncat >/dev/null\nprintf changed > tracked.txt\ngit add tracked.txt\ngit config user.name Escaped\ngit commit -m escaped\n")
	cfg := testPipelineConfig()
	cfg.Workflow.Correction = model.CorrectionConfig{Enabled: true, FilesystemSandboxed: true, Command: []string{"./repair.sh"}, MaxAttempts: 1, MaxChangedFiles: 1, MaxChangedLines: 2, BranchPrefix: "sam-repair/"}
	writePipelineConfig(t, root, cfg)
	failedPath := writeFailedReceipt(t, root)
	headBefore := runRepairGit(t, root, "rev-parse", "HEAD")
	statusBefore := runRepairGit(t, root, "status", "--porcelain=v1", "--untracked-files=all")
	nameBefore := runRepairGit(t, root, "config", "user.name")

	receipt, _, err := Repair(root, failedPath, false)
	if err == nil || !strings.Contains(err.Error(), "Git control data") || receipt.Passed {
		t.Fatalf("linked worktree repair was accepted: err=%v receipt=%#v", err, receipt)
	}
	if got := runRepairGit(t, root, "rev-parse", "HEAD"); got != headBefore {
		t.Fatalf("linked worktree HEAD changed: before=%q after=%q", headBefore, got)
	}
	if got := runRepairGit(t, root, "status", "--porcelain=v1", "--untracked-files=all"); got != statusBefore {
		t.Fatalf("linked worktree index/worktree changed: before=%q after=%q", statusBefore, got)
	}
	if got := runRepairGit(t, root, "config", "user.name"); got != nameBefore {
		t.Fatalf("linked worktree config changed: before=%q after=%q", nameBefore, got)
	}
}

func TestRepairRequiresEnabledCorrectionAndWriteAuthority(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		enabled    bool
		canWrite   bool
		canNetwork bool
		errorText  string
	}{
		{name: "disabled", canWrite: true, canNetwork: true, errorText: "correction is not enabled"},
		{name: "no write authority", enabled: true, canNetwork: true, errorText: "write_repository authority"},
		{name: "no network authority", enabled: true, canWrite: true, errorText: "network authority"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			cfg := testPipelineConfig()
			cfg.Authority.WriteRepository = scenario.canWrite
			cfg.Authority.Network = scenario.canNetwork
			cfg.Workflow.Correction = model.CorrectionConfig{
				Enabled:             scenario.enabled,
				FilesystemSandboxed: scenario.enabled,
				Command:             []string{"go", "version"},
				MaxAttempts:         1,
				MaxChangedFiles:     1,
				MaxChangedLines:     1,
				BranchPrefix:        "sam-repair/",
			}
			writePipelineConfig(t, root, cfg)
			failedPath := writeFailedReceipt(t, root)
			receipt, _, err := Repair(root, failedPath, false)
			if err == nil || !strings.Contains(err.Error(), scenario.errorText) || receipt.Status != StatusBlocked {
				t.Fatalf("authority boundary failed: err=%v receipt=%#v", err, receipt)
			}
		})
	}
}

func writeFailedReceipt(t *testing.T, root string) string {
	t.Helper()
	return writeFailedReceiptWithConfig(t, root, filepath.Join(root, ".sam-harness", "config.yaml"))
}

func writeFailedReceiptWithConfig(t *testing.T, root, configPath string) string {
	t.Helper()
	fingerprint, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	configDigest := sha256.Sum256(rawConfig)
	configSource, err := filepath.EvalSymlinks(configPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt := Receipt{
		HarnessVersion:   model.HarnessVersion,
		Kind:             "pipeline",
		Repository:       "fixture",
		Root:             root,
		Phase:            model.PhaseTest,
		ConfigSource:     configSource,
		ConfigSHA256:     hex.EncodeToString(configDigest[:]),
		Fingerprint:      fingerprint,
		FinalFingerprint: fingerprint,
		StartedAt:        time.Now().UTC().Add(-time.Second),
		FinishedAt:       time.Now().UTC(),
		Passed:           false,
		Status:           StatusFailed,
		Error:            "test phase failed",
	}
	path := filepath.Join(root, ".sam-harness", "evidence", receiptFilename(receipt))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeReceiptAt(t, path, receipt)
	return path
}

func writeReceiptAt(t *testing.T, path string, receipt Receipt) {
	t.Helper()
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, root, relative, value string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runRepairGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}
