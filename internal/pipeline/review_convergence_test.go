package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samuelfaj/sam-harness/internal/model"
)

func convergenceTestFinding(role model.ReviewerRole, severity, path string, line int) Finding {
	return Finding{
		Role: role, Severity: severity, Summary: "unsafe changed behavior", Evidence: path,
		Path: path, Line: line, RequiredChange: "correct the changed behavior", Acceptance: "the changed behavior is safe",
	}
}

func convergenceTestReceipt(t *testing.T, root, headSHA string, finding Finding) Receipt {
	t.Helper()
	receipt := Receipt{
		HarnessVersion: model.HarnessVersion, Kind: "pipeline", Phase: model.PhaseReview, Repository: "fixture",
		Root: root, ReviewBaseSHA: strings.Repeat("a", 40), ReviewBaseFingerprint: strings.Repeat("b", sha256.Size*2),
		ReviewHeadSHA: headSHA, ReviewHeadFingerprint: strings.Repeat(headSHA[:1], sha256.Size*2), Status: StatusBlocked,
		Error: "review blocked by finding", StartedAt: testReceiptTime(), FinishedAt: testReceiptTime(), Findings: []Finding{finding},
	}
	receipt.ReviewLineageSHA256 = reviewLineageDigest(&receipt)
	if err := attachRepairManifest(&receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func testReceiptTime() (value time.Time) {
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
}

func TestReviewManifestTamperRejectsIdentityStatusAndLineageChanges(t *testing.T) {
	t.Parallel()
	receipt := convergenceTestReceipt(t, t.TempDir(), strings.Repeat("c", 40), convergenceTestFinding(model.ReviewerSecurity, "P1", "changed.go", 2))
	for name, mutate := range map[string]func(*Receipt){
		"identity": func(value *Receipt) {
			value.RepairManifest.Actions[0].ID = strings.Repeat("d", 64)
		},
		"status": func(value *Receipt) {
			value.RepairManifest.Actions[0].Status = findingStatusUnresolved
		},
		"lineage": func(value *Receipt) {
			value.RepairManifest.Actions[0].Lineage = strings.Repeat("e", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			mutated := receipt
			manifest := *receipt.RepairManifest
			manifest.Actions = append([]Finding(nil), receipt.RepairManifest.Actions...)
			mutated.RepairManifest = &manifest
			mutate(&mutated)
			if err := validateRepairManifest(mutated); err == nil {
				t.Fatalf("tampered %s manifest was accepted", name)
			}
		})
	}
}

func TestPriorReviewRequiresAncestorHead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	priorHead := initializeTestGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("one\nupdated\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentHead := initializeTestGit(t, root)
	prior := convergenceTestReceipt(t, root, priorHead, convergenceTestFinding(model.ReviewerSecurity, "P1", "changed.go", 2))
	current := Receipt{Repository: "fixture", ReviewBaseSHA: prior.ReviewBaseSHA, ReviewBaseFingerprint: prior.ReviewBaseFingerprint, ReviewHeadSHA: currentHead, ReviewHeadFingerprint: strings.Repeat("f", sha256.Size*2)}
	if err := validatePriorReviewLineage(root, prior, current); err != nil {
		t.Fatalf("descendant head rejected: %v", err)
	}
	if ancestor, err := gitAncestor(root, currentHead, priorHead); err != nil || ancestor {
		t.Fatalf("reverse ancestor relation accepted: ancestor=%t err=%v", ancestor, err)
	}
	for name, mutate := range map[string]func(*Receipt, *Receipt){
		"missing prior SHA":   func(prior, _ *Receipt) { prior.ReviewHeadSHA = "" },
		"missing current SHA": func(_, current *Receipt) { current.ReviewHeadSHA = "" },
	} {
		t.Run(name, func(t *testing.T) {
			candidatePrior := prior
			candidateCurrent := current
			mutate(&candidatePrior, &candidateCurrent)
			if err := validatePriorReviewLineage(root, candidatePrior, candidateCurrent); err == nil {
				t.Fatal("convergence accepted a missing review head SHA")
			}
		})
	}
	unrelatedRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(unrelatedRoot, "unrelated.go"), []byte("package unrelated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	unrelatedSHA := initializeTestGit(t, unrelatedRoot)
	fetch := exec.Command("git", "-C", root, "fetch", "--no-tags", unrelatedRoot, unrelatedSHA)
	if output, err := fetch.CombinedOutput(); err != nil {
		t.Fatalf("fetch unrelated test object: %v: %s", err, output)
	}
	unrelatedPrior := prior
	unrelatedPrior.ReviewHeadSHA = unrelatedSHA
	if err := validatePriorReviewLineage(root, unrelatedPrior, current); err == nil || !strings.Contains(err.Error(), "not a descendant") {
		t.Fatalf("unrelated review heads were accepted: %v", err)
	}
}

func TestReviewConvergenceTracksUnresolvedAndIgnoresUnrelatedFinding(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	priorHead := initializeTestGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("one\nupdated\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentHead := initializeTestGit(t, root)
	priorFinding := convergenceTestFinding(model.ReviewerSecurity, "P1", "changed.go", 2)
	prior := convergenceTestReceipt(t, root, priorHead, priorFinding)
	unresolved := Receipt{ReviewHeadSHA: currentHead, ReviewHeadFingerprint: strings.Repeat("f", sha256.Size*2), ReviewLineageSHA256: strings.Repeat("1", 64), Findings: []Finding{
		{ID: prior.RepairManifest.Actions[0].ID, Role: model.ReviewerSecurity, Severity: "P1", Summary: "wording changed after repair", Evidence: "changed.go:3", Path: "changed.go", Line: 3, RequiredChange: "use the corrected implementation", Acceptance: "the corrected implementation is safe"},
	}}
	if err := classifyReviewConvergence(root, prior, &unresolved); err == nil {
		t.Fatal("unresolved frozen action did not block convergence")
	}
	if unresolved.ReviewConvergence != reviewConvergenceBlocked || len(unresolved.UnresolvedFindingIDs) != 1 || unresolved.Findings[0].Status != findingStatusUnresolved || unresolved.Findings[0].ID != prior.RepairManifest.Actions[0].ID {
		t.Fatalf("unresolved frozen action was not preserved: %#v", unresolved)
	}
	resolved := Receipt{ReviewHeadSHA: currentHead, ReviewHeadFingerprint: strings.Repeat("f", sha256.Size*2), ReviewLineageSHA256: strings.Repeat("1", 64), Findings: []Finding{
		convergenceTestFinding(model.ReviewerCorrectness, "P1", "preexisting.go", 4),
	}}
	if err := classifyReviewConvergence(root, prior, &resolved); err != nil {
		t.Fatalf("resolved action or unrelated finding blocked convergence: %v", err)
	}
	if resolved.ReviewConvergence != reviewConvergencePassed || len(resolved.ResolvedFindingIDs) != 1 || len(resolved.UnresolvedFindingIDs) != 0 || len(resolved.RegressionFindingIDs) != 0 || resolved.Findings[0].Status != findingStatusRecorded {
		t.Fatalf("unexpected resolved convergence ledger: %#v", resolved)
	}
}

func TestReviewConvergenceRejectsForgedOrWrongRoleFrozenID(t *testing.T) {
	prior := convergenceTestReceipt(t, t.TempDir(), strings.Repeat("c", 40), convergenceTestFinding(model.ReviewerSecurity, "P1", "changed.go", 2))
	actionID := prior.RepairManifest.Actions[0].ID
	for name, scenario := range map[string]struct {
		id         string
		role       model.ReviewerRole
		errorMatch string
	}{
		"forged":          {id: strings.Repeat("f", 64), role: model.ReviewerSecurity, errorMatch: "not a frozen action"},
		"wrong role":      {id: actionID, role: model.ReviewerCorrectness, errorMatch: "not a frozen action"},
		"padded":          {id: " " + actionID + " ", role: model.ReviewerSecurity, errorMatch: "not canonical"},
		"whitespace-only": {id: "   ", role: model.ReviewerSecurity, errorMatch: "not canonical"},
	} {
		t.Run(name, func(t *testing.T) {
			current := Receipt{Findings: []Finding{{ID: scenario.id, Role: scenario.role, Severity: "P1", Summary: "still unsafe", Evidence: "changed.go", Path: "changed.go", Line: 3, RequiredChange: "correct it", Acceptance: "it is safe"}}}
			if err := classifyReviewConvergence(t.TempDir(), prior, &current); err == nil || !strings.Contains(err.Error(), scenario.errorMatch) {
				t.Fatalf("invalid frozen ID error = %v, want %q", err, scenario.errorMatch)
			}
		})
	}
}

func TestReviewConvergenceRequiresExplicitIDForFrozenActionCollision(t *testing.T) {
	prior := convergenceTestReceipt(t, t.TempDir(), strings.Repeat("c", 40), convergenceTestFinding(model.ReviewerSecurity, "P1", "changed.go", 2))
	finding := prior.RepairManifest.Actions[0]
	finding.ID = ""
	finding.Status = ""
	finding.Lineage = ""
	current := Receipt{Findings: []Finding{finding}}

	err := classifyReviewConvergence(t.TempDir(), prior, &current)
	if err == nil || !strings.Contains(err.Error(), "did not return its explicit exact manifest ID") {
		t.Fatalf("implicit frozen ID collision error = %v", err)
	}
}

func TestReviewConvergenceOnlyNewP0P1InCorrectionDeltaBlock(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("one\ntwo\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	priorHead := initializeTestGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "changed.go"), []byte("one\nupdated\nthree\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	currentHead := initializeTestGit(t, root)
	prior := convergenceTestReceipt(t, root, priorHead, convergenceTestFinding(model.ReviewerSecurity, "P1", "old.go", 1))
	for name, scenario := range map[string]struct {
		severity       string
		line           int
		wantRegression bool
	}{
		"P0 on changed line":   {severity: "P0", line: 2, wantRegression: true},
		"P1 on changed line":   {severity: "P1", line: 2, wantRegression: true},
		"P2 on changed line":   {severity: "P2", line: 2, wantRegression: false},
		"P3 on changed line":   {severity: "P3", line: 2, wantRegression: false},
		"P1 on unchanged line": {severity: "P1", line: 1, wantRegression: false},
	} {
		t.Run(name, func(t *testing.T) {
			line := scenario.line
			wantRegression := scenario.wantRegression
			current := Receipt{ReviewHeadSHA: currentHead, ReviewHeadFingerprint: strings.Repeat("f", sha256.Size*2), ReviewLineageSHA256: strings.Repeat("1", 64), Findings: []Finding{convergenceTestFinding(model.ReviewerCorrectness, scenario.severity, "changed.go", line)}}
			err := classifyReviewConvergence(root, prior, &current)
			if wantRegression && err == nil {
				t.Fatalf("changed-hunk %s regression was accepted", scenario.severity)
			}
			if !wantRegression && (err != nil || len(current.RegressionFindingIDs) != 0 || current.Findings[0].Status != findingStatusRecorded) {
				t.Fatalf("non-blocking %s finding was treated as regression: err=%v receipt=%#v", scenario.severity, err, current)
			}
		})
	}
}

func TestReviewConvergenceLineZeroRequiresDeletionOnlyOrPureRename(t *testing.T) {
	for name, scenario := range map[string]struct {
		before         map[string]string
		after          map[string]string
		path           string
		wantRegression bool
	}{
		"deletion only":  {before: map[string]string{"changed.go": "one\ntwo\n"}, after: map[string]string{"changed.go": "one\n"}, path: "changed.go", wantRegression: true},
		"deleted file":   {before: map[string]string{"deleted.go": "one\n"}, after: map[string]string{}, path: "deleted.go", wantRegression: true},
		"pure rename":    {before: map[string]string{"old.go": "one\ntwo\n"}, after: map[string]string{"new.go": "one\ntwo\n"}, path: "new.go", wantRegression: true},
		"added line":     {before: map[string]string{"changed.go": "one\n"}, after: map[string]string{"changed.go": "one\ntwo\n"}, path: "changed.go", wantRegression: false},
		"unrelated path": {before: map[string]string{"changed.go": "one\n"}, after: map[string]string{"changed.go": "one\ntwo\n"}, path: "other.go", wantRegression: false},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for path, content := range scenario.before {
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			priorHead := initializeTestGit(t, root)
			for path := range scenario.before {
				if _, exists := scenario.after[path]; !exists {
					if err := os.Remove(filepath.Join(root, path)); err != nil {
						t.Fatal(err)
					}
				}
			}
			for path, content := range scenario.after {
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			currentHead := initializeTestGit(t, root)
			prior := convergenceTestReceipt(t, root, priorHead, convergenceTestFinding(model.ReviewerSecurity, "P1", "frozen.go", 1))
			current := Receipt{ReviewHeadSHA: currentHead, ReviewHeadFingerprint: strings.Repeat("f", sha256.Size*2), ReviewLineageSHA256: strings.Repeat("1", 64), Findings: []Finding{convergenceTestFinding(model.ReviewerCorrectness, "P1", scenario.path, 0)}}
			err := classifyReviewConvergence(root, prior, &current)
			if scenario.wantRegression && (err == nil || current.Findings[0].Status != findingStatusRegression) {
				t.Fatalf("line-zero altered-file regression was not blocked: err=%v receipt=%#v", err, current)
			}
			if !scenario.wantRegression && (err != nil || current.Findings[0].Status != findingStatusRecorded) {
				t.Fatalf("invalid line-zero anchor was accepted as a regression: err=%v receipt=%#v", err, current)
			}
		})
	}
}

func TestInitialFindingMustBeInsideChangedHunk(t *testing.T) {
	validPatch := "diff --git a/changed.go b/changed.go\n@@ -1,3 +1,3 @@\n one\n-two\n+updated\n three\n"
	change := reviewChangeEvidence{baseRoot: "/trusted/base", patch: []byte(validPatch)}
	lineage := strings.Repeat("a", 64)
	inScope, excluded, err := scopeInitialFindings([]Finding{convergenceTestFinding(model.ReviewerSecurity, "P1", "changed.go", 2)}, change, lineage)
	if err != nil || len(inScope) != 1 || len(excluded) != 0 {
		t.Fatalf("changed-line finding rejected: %v", err)
	}
	if _, _, err := scopeInitialFindings([]Finding{convergenceTestFinding(model.ReviewerSecurity, "P1", "changed.go", 1)}, change, lineage); err == nil {
		t.Fatal("out-of-hunk line accepted")
	}
	if _, _, err := scopeInitialFindings([]Finding{convergenceTestFinding(model.ReviewerSecurity, "P1", "other.go", 2)}, change, lineage); err == nil {
		t.Fatal("out-of-diff path accepted")
	}
	lowSeverity := convergenceTestFinding(model.ReviewerTestQuality, "P2", "changed.go", 1)
	inScope, excluded, err = scopeInitialFindings([]Finding{lowSeverity}, change, lineage)
	if err != nil || len(inScope) != 0 || len(excluded) != 1 {
		t.Fatalf("out-of-scope P2 was not excluded: err=%v in_scope=%#v excluded=%#v", err, inScope, excluded)
	}
	if excluded[0].Status != findingStatusExcluded || excluded[0].Lineage != lineage || excluded[0].ID != findingIdentity(lowSeverity) {
		t.Fatalf("excluded P2 lacks stable human evidence: %#v", excluded[0])
	}
}

func TestInitialFindingScopeParsesQuotedRenameAndDeletionDiffs(t *testing.T) {
	validPatch := "diff --git a/changed.go b/changed.go\n@@ -1,3 +1,3 @@\n one\n-two\n+updated\n three\n"
	for name, scenario := range map[string]struct {
		patch string
		path  string
		line  int
		valid bool
	}{
		"quoted path": {
			patch: "diff --git \"a/dir/file with space.go\" \"b/dir/file with space.go\"\n--- \"a/dir/file with space.go\"\n+++ \"b/dir/file with space.go\"\n@@ -1 +1 @@\n-old\n+new\n",
			path:  "dir/file with space.go", line: 1, valid: true,
		},
		"rename with edit": {
			patch: "diff --git a/old.go b/new.go\nsimilarity index 80%\nrename from old.go\nrename to new.go\n--- a/old.go\n+++ b/new.go\n@@ -2 +2 @@\n-old\n+new\n",
			path:  "new.go", line: 2, valid: true,
		},
		"deletion-only anchor": {
			patch: "diff --git a/trim.go b/trim.go\n--- a/trim.go\n+++ b/trim.go\n@@ -2 +1,0 @@\n-removed\n",
			path:  "trim.go", line: 0, valid: true,
		},
		"deleted-file anchor": {
			patch: "diff --git a/deleted.go b/deleted.go\ndeleted file mode 100644\n--- a/deleted.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-removed\n",
			path:  "deleted.go", line: 0, valid: true,
		},
		"pure-rename anchor": {
			patch: "diff --git a/old.go b/new.go\nsimilarity index 100%\nrename from old.go\nrename to new.go\n",
			path:  "new.go", line: 0, valid: true,
		},
		"line zero on added line": {
			patch: validPatch,
			path:  "changed.go", line: 0, valid: false,
		},
		"line zero outside diff": {
			patch: validPatch,
			path:  "other.go", line: 0, valid: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			change := reviewChangeEvidence{baseRoot: "/trusted/base", patch: []byte(scenario.patch)}
			_, _, err := scopeInitialFindings([]Finding{convergenceTestFinding(model.ReviewerSecurity, "P1", scenario.path, scenario.line)}, change, strings.Repeat("a", 64))
			if scenario.valid && err != nil {
				t.Fatalf("valid diff scope was rejected: %v", err)
			}
			if !scenario.valid && err == nil {
				t.Fatal("invalid diff scope was accepted")
			}
		})
	}
}

func TestReviewReceiptHTMLRecordsExcludedOutOfScopeSuggestions(t *testing.T) {
	finding := convergenceTestFinding(model.ReviewerTestQuality, "P2", "unchanged.go", 9)
	finding.ID = findingIdentity(finding)
	finding.Status = findingStatusExcluded
	finding.Lineage = strings.Repeat("a", 64)
	receipt := Receipt{
		HarnessVersion:   model.HarnessVersion,
		Kind:             "pipeline",
		Phase:            model.PhaseReview,
		Passed:           true,
		Status:           StatusPassed,
		ExcludedFindings: []Finding{finding},
	}
	html, err := receiptHTML(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"CLEAN — no findings were recorded.",
		"Excluded reviewer suggestions",
		"excluded_out_of_scope",
		"outside the changed diff",
		"unchanged.go",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("HTML receipt omitted %q", required)
		}
	}
}

func TestReceiptHTMLEscapesUntrustedFindingValues(t *testing.T) {
	receipt := Receipt{HarnessVersion: model.HarnessVersion, Kind: "pipeline", Phase: model.PhaseReview, Status: StatusBlocked, Repository: "<repo>", Findings: []Finding{{Role: model.ReviewerSecurity, Severity: "P1", Summary: "<script>alert(1)</script>", Evidence: "</pre><script>alert(2)</script>", Path: "changed.go", Line: 2, RequiredChange: "<b>fix</b>", Acceptance: "<i>accepted</i>", ID: strings.Repeat("a", 64), Status: findingStatusOpen, Lineage: strings.Repeat("b", 64)}}}
	html, err := receiptHTML(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"<script>alert(1)</script>", "<b>fix</b>", "</pre><script>alert(2)</script>"} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("unescaped value %q found in HTML", forbidden)
		}
	}
	for _, escaped := range []string{"&lt;script&gt;alert(1)&lt;/script&gt;", "&lt;b&gt;fix&lt;/b&gt;", "&lt;/pre&gt;&lt;script&gt;alert(2)&lt;/script&gt;"} {
		if !strings.Contains(html, escaped) {
			t.Fatalf("escaped value %q missing from HTML", escaped)
		}
	}
	if !strings.Contains(html, "line 0 only for deletion-only, deleted, or pure-rename file-level evidence") {
		t.Fatal("HTML receipt omitted the file-level line-zero scope")
	}
}

func TestReviewReceiptHTMLDistinguishesIncompleteExecutionFromClean(t *testing.T) {
	errorText := "review blocked: <script>alert(1)</script>"
	output := "reviewer output: <bad>\n" + strings.Repeat("x", receiptHTMLCommandOutputLimit+100) + "\noutput-after-limit"
	receipt := Receipt{
		HarnessVersion: model.HarnessVersion,
		Kind:           "pipeline",
		Phase:          model.PhaseReview,
		Status:         StatusBlocked,
		Error:          errorText,
		Commands: []CommandResult{
			{Name: "review:security", Phase: model.PhaseReview, Required: true, ExitCode: 1, Output: output},
			{Name: "review:correctness", Phase: model.PhaseReview, Required: true, ExitCode: -1, TimedOut: true},
			{Name: "review:simplicity", Phase: model.PhaseReview, Required: true, Passed: true, ExitCode: 0},
			{Name: "static-check", Phase: model.PhaseStatic, Required: true, ExitCode: 1},
		},
	}
	html, err := receiptHTML(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		`class="alert bad"`,
		"Receipt error / block reason",
		"review blocked: &lt;script&gt;alert(1)&lt;/script&gt;",
		"Review execution failures",
		"security",
		"review:security",
		">1<",
		">true<",
		"timed_out",
		"review:correctness",
		"Fix every failed required reviewer command listed above and rerun the review; no approval or complete correction manifest exists yet.",
		"INCOMPLETE REVIEW — no approval or complete correction manifest exists. Resolve the block and rerun the review.",
		"reviewer output: &lt;bad&gt;",
		"[output excerpt truncated by sam-harness: ",
		"output-after-limit",
		"bytes omitted]",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("incomplete review HTML omitted %q:\n%s", required, html)
		}
	}
	for _, forbidden := range []string{"<script>alert(1)</script>", "<bad>", "No findings were recorded.", "CLEAN — no findings were recorded."} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("incomplete review HTML contains misleading or unescaped value %q", forbidden)
		}
	}
	excerpt := receiptHTMLCommandOutput(output)
	if len(excerpt) > receiptHTMLCommandOutputLimit || !strings.Contains(excerpt, "reviewer output: <bad>") || !strings.Contains(excerpt, "output-after-limit") || !strings.Contains(excerpt, "bytes omitted]") {
		t.Fatalf("reviewer output excerpt did not preserve bounded head and tail: %q", excerpt)
	}

	clean, err := receiptHTML(Receipt{HarnessVersion: model.HarnessVersion, Kind: "pipeline", Phase: model.PhaseReview, Status: StatusPassed, Passed: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(clean, "CLEAN — no findings were recorded.") || strings.Contains(clean, "INCOMPLETE REVIEW") || strings.Contains(clean, "Review execution failures") {
		t.Fatalf("clean review HTML is not distinct: %s", clean)
	}
}

func TestReviewReceiptHTMLPreservesSchemaBlockDiagnostics(t *testing.T) {
	const blockReason = "review blocked: required reviewer output is invalid"
	receipt := Receipt{
		HarnessVersion: model.HarnessVersion,
		Kind:           "pipeline",
		Phase:          model.PhaseReview,
		Status:         StatusBlocked,
		Error:          blockReason,
		Commands: []CommandResult{{
			Name:     "review:security",
			Phase:    model.PhaseReview,
			Required: true,
			ExitCode: 1,
			Output:   "invalid_json_schema\nMissing 'id'",
		}},
	}
	html, err := receiptHTML(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{blockReason, "invalid_json_schema", "Missing &#39;id&#39;", "INCOMPLETE REVIEW"} {
		if !strings.Contains(html, required) {
			t.Fatalf("schema-blocked review HTML omitted %q:\n%s", required, html)
		}
	}
	for _, forbidden := range []string{"No findings were recorded.", "CLEAN — no findings were recorded."} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("schema-blocked review HTML contains misleading value %q", forbidden)
		}
	}
}

func TestAggregateReceiptHTMLDoesNotCallFailedReviewClean(t *testing.T) {
	receipt := Receipt{
		HarnessVersion: model.HarnessVersion,
		Kind:           "pipeline",
		Phase:          model.PhaseAll,
		Status:         StatusFailed,
		Error:          "review phase failed: required reviewer exited unsuccessfully",
		Commands: []CommandResult{
			{Name: "review:security", Phase: model.PhaseReview, Required: true, ExitCode: 1, Output: "reviewer failed"},
		},
		Phases: []PhaseResult{{Phase: model.PhaseReview, Status: StatusFailed, Error: "required reviewer exited unsuccessfully"}},
	}
	html, err := receiptHTML(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"Receipt error / block reason",
		"review phase failed: required reviewer exited unsuccessfully",
		"Review execution failures",
		"review:security",
		"Fix every failed required reviewer command listed above and rerun the review",
		"INCOMPLETE REVIEW — no approval or complete correction manifest exists.",
	} {
		if !strings.Contains(html, required) {
			t.Fatalf("aggregate incomplete review HTML omitted %q:\n%s", required, html)
		}
	}
	for _, forbidden := range []string{"No findings were recorded.", "CLEAN — no findings were recorded."} {
		if strings.Contains(html, forbidden) {
			t.Fatalf("aggregate incomplete review HTML contains misleading value %q", forbidden)
		}
	}
}

func TestRepairReceiptWritesHTMLSidecarWithPatchIdentity(t *testing.T) {
	root := t.TempDir()
	cfg := testPipelineConfig()
	receipt := Receipt{
		HarnessVersion:    model.HarnessVersion,
		Kind:              "repair",
		Root:              root,
		StartedAt:         testReceiptTime(),
		FinishedAt:        testReceiptTime(),
		Status:            StatusPassed,
		Passed:            true,
		RepairPatch:       filepath.Join(root, ".sam-harness", "evidence", "repair.patch"),
		RepairPatchSHA256: strings.Repeat("a", 64),
	}
	path, err := writeReceiptFile(root, cfg.Evidence.ReceiptDirectory, receipt)
	if err != nil {
		t.Fatal(err)
	}
	htmlPath := strings.TrimSuffix(path, ".json") + ".html"
	html, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), "Repair identity") || !strings.Contains(string(html), receipt.RepairPatch) || !strings.Contains(string(html), receipt.RepairPatchSHA256) {
		t.Fatalf("repair HTML sidecar omitted patch identity: %s", html)
	}
}

func TestFindingIdentityIsDeterministic(t *testing.T) {
	finding := convergenceTestFinding(model.ReviewerSecurity, "P1", "changed.go", 2)
	first := findingIdentity(finding)
	second := findingIdentity(finding)
	if first != second || len(first) != sha256.Size*2 {
		t.Fatalf("finding identity is not deterministic: %q %q", first, second)
	}
	if _, err := hex.DecodeString(first); err != nil {
		t.Fatalf("finding identity is not hexadecimal: %v", err)
	}
}
