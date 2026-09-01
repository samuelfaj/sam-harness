package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"strconv"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

const receiptHTMLCommandOutputLimit = 4096

type receiptHTMLPage struct {
	Title                     string
	HarnessVersion            string
	Kind                      string
	Phase                     string
	Status                    string
	Passed                    string
	Error                     string
	Repository                string
	Root                      string
	Fingerprint               string
	FinalFingerprint          string
	ConfigSource              string
	ConfigSHA256              string
	ReviewBaseSHA             string
	ReviewBaseFingerprint     string
	ReviewHeadSHA             string
	ReviewHeadFingerprint     string
	ReviewPatch               string
	ReviewPatchSHA256         string
	ReviewLineageSHA256       string
	PriorReviewReceipt        string
	PriorReviewReceiptSHA256  string
	PriorReviewManifestSHA256 string
	ReviewConvergence         string
	ResolvedFindingIDs        string
	UnresolvedFindingIDs      string
	RegressionFindingIDs      string
	RepairPatch               string
	RepairPatchSHA256         string
	RepairManifestSHA256      string
	SourceReceipt             string
	Findings                  []receiptHTMLFinding
	ExcludedFindings          []receiptHTMLFinding
	ReviewExecutionFailures   []receiptHTMLExecutionFailure
	ReviewIncomplete          bool
	ReviewClean               bool
	ReviewNoFindingsMessage   string
	ReviewFailureAction       string
}

type receiptHTMLFinding struct {
	ID             string
	Role           string
	Severity       string
	Status         string
	Lineage        string
	Summary        string
	Evidence       string
	Path           string
	Line           string
	RequiredChange string
	Acceptance     string
}

type receiptHTMLExecutionFailure struct {
	Role     string
	Name     string
	ExitCode string
	TimedOut string
	Skipped  string
	Status   string
	Output   string
}

var receiptHTMLTemplate = template.Must(template.New("receipt").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#202124;--muted:#5f6368;--line:#dadce0;--code:#f1f3f4;--bad:#b3261e;--good:#137333}
@media(prefers-color-scheme:dark){:root{--bg:#202124;--fg:#e8eaed;--muted:#bdc1c6;--line:#5f6368;--code:#303134}}
body{background:var(--bg);color:var(--fg);font:15px/1.5 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:0;padding:2rem;overflow-wrap:anywhere}
main{margin:0 auto;max-width:1200px}h1{margin-top:0}h2{border-bottom:1px solid var(--line);padding-bottom:.35rem;margin-top:2rem}
dl{display:grid;grid-template-columns:minmax(12rem,22rem) 1fr;gap:.35rem 1rem}dt{color:var(--muted);font-weight:600}dd{margin:0;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap}.alert{border:2px solid var(--bad);padding:1rem;margin:1rem 0}.alert pre{margin:.35rem 0 0}
table{border-collapse:collapse;width:100%;table-layout:fixed}th,td{border:1px solid var(--line);padding:.6rem;vertical-align:top;text-align:left}th{background:var(--code)}pre{font:inherit;margin:0;white-space:pre-wrap}.finding-meta{color:var(--muted);font-family:ui-monospace,SFMono-Regular,Menlo,monospace}.good{color:var(--good)}.bad{color:var(--bad)}
</style>
</head>
<body><main>
<h1>{{.Title}}</h1>
{{if .Error}}<div class="alert bad"><strong>Receipt error / block reason</strong><pre>{{.Error}}</pre></div>{{end}}
<dl>
<dt>Harness version</dt><dd>{{.HarnessVersion}}</dd><dt>Kind</dt><dd>{{.Kind}}</dd><dt>Phase</dt><dd>{{.Phase}}</dd>
<dt>Status</dt><dd class="{{if eq .Status "passed"}}good{{else}}bad{{end}}">{{.Status}}</dd><dt>Passed</dt><dd>{{.Passed}}</dd>
<dt>Repository</dt><dd>{{.Repository}}</dd><dt>Root</dt><dd>{{.Root}}</dd><dt>Source fingerprint</dt><dd>{{.Fingerprint}}</dd><dt>Final fingerprint</dt><dd>{{.FinalFingerprint}}</dd>
<dt>Config source</dt><dd>{{.ConfigSource}}</dd><dt>Config SHA-256</dt><dd>{{.ConfigSHA256}}</dd>
</dl>
<h2>Review lineage</h2><dl>
<dt>Review base SHA</dt><dd>{{.ReviewBaseSHA}}</dd><dt>Review base fingerprint</dt><dd>{{.ReviewBaseFingerprint}}</dd>
<dt>Review head SHA</dt><dd>{{.ReviewHeadSHA}}</dd><dt>Review head fingerprint</dt><dd>{{.ReviewHeadFingerprint}}</dd>
<dt>Review patch</dt><dd>{{.ReviewPatch}}</dd><dt>Review patch SHA-256</dt><dd>{{.ReviewPatchSHA256}}</dd><dt>Review lineage SHA-256</dt><dd>{{.ReviewLineageSHA256}}</dd>
<dt>Prior review receipt</dt><dd>{{.PriorReviewReceipt}}</dd><dt>Prior receipt SHA-256</dt><dd>{{.PriorReviewReceiptSHA256}}</dd><dt>Prior manifest SHA-256</dt><dd>{{.PriorReviewManifestSHA256}}</dd>
</dl>
<p>Finding scope: initial correction findings must identify a current added or modified line in the base-to-head diff, or line 0 only for deletion-only, deleted, or pure-rename file-level evidence. Out-of-scope P0/P1 findings block; out-of-scope P2/P3 suggestions are recorded below but excluded from correction. During convergence, frozen actions remain eligible; new findings use the same scope rule in the prior-head-to-current-head diff.</p>
{{if .ReviewExecutionFailures}}<h2>Review execution failures</h2><p class="bad">{{.ReviewFailureAction}}</p><table><thead><tr><th>Role</th><th>Name</th><th>Exit code</th><th>Timeout</th><th>Skipped</th><th>Status</th><th>Output excerpt</th></tr></thead><tbody>{{range .ReviewExecutionFailures}}<tr><td>{{.Role}}</td><td>{{.Name}}</td><td>{{.ExitCode}}</td><td>{{.TimedOut}}</td><td>{{.Skipped}}</td><td>{{.Status}}</td><td><pre>{{.Output}}</pre></td></tr>{{end}}</tbody></table>{{end}}
<h2>Findings and required changes</h2>
{{if .Findings}}<table><thead><tr><th>Finding</th><th>Evidence</th><th>Exact required change</th><th>Acceptance</th></tr></thead><tbody>{{range .Findings}}<tr><td><div class="finding-meta">id={{.ID}}<br>role={{.Role}} severity={{.Severity}} status={{.Status}} line={{.Line}}<br>lineage={{.Lineage}}</div><strong>{{.Summary}}</strong><br><code>{{.Path}}</code></td><td><pre>{{.Evidence}}</pre></td><td><pre>{{.RequiredChange}}</pre></td><td><pre>{{.Acceptance}}</pre></td></tr>{{end}}</tbody></table>{{else if .ReviewNoFindingsMessage}}<p class="{{if .ReviewClean}}good{{else}}bad{{end}}">{{.ReviewNoFindingsMessage}}</p>{{else}}<p>No findings were recorded.</p>{{end}}
{{if .ExcludedFindings}}<h2>Excluded reviewer suggestions</h2><p>These P2/P3 suggestions were recorded for human inspection but excluded from the correction manifest because their evidence is outside the changed diff.</p><table><thead><tr><th>Suggestion</th><th>Evidence</th><th>Suggested change</th><th>Acceptance</th></tr></thead><tbody>{{range .ExcludedFindings}}<tr><td><div class="finding-meta">id={{.ID}}<br>role={{.Role}} severity={{.Severity}} status={{.Status}} line={{.Line}}<br>lineage={{.Lineage}}</div><strong>{{.Summary}}</strong><br><code>{{.Path}}</code></td><td><pre>{{.Evidence}}</pre></td><td><pre>{{.RequiredChange}}</pre></td><td><pre>{{.Acceptance}}</pre></td></tr>{{end}}</tbody></table>{{end}}
{{if .ReviewIncomplete}}<p class="bad"><strong>INCOMPLETE REVIEW — no approval or complete correction manifest exists.</strong> Resolve the block and rerun the review.</p>{{end}}
<h2>Resolution and convergence</h2><dl>
<dt>Convergence state</dt><dd>{{.ReviewConvergence}}</dd><dt>Resolved finding IDs</dt><dd>{{.ResolvedFindingIDs}}</dd><dt>Unresolved finding IDs</dt><dd>{{.UnresolvedFindingIDs}}</dd><dt>Regression finding IDs</dt><dd>{{.RegressionFindingIDs}}</dd>
</dl>
<h2>Repair identity</h2><dl>
<dt>Source receipt</dt><dd>{{.SourceReceipt}}</dd><dt>Repair patch</dt><dd>{{.RepairPatch}}</dd><dt>Repair patch SHA-256</dt><dd>{{.RepairPatchSHA256}}</dd><dt>Repair manifest SHA-256</dt><dd>{{.RepairManifestSHA256}}</dd>
</dl>
</main></body></html>
`))

func receiptHTML(receipt Receipt) (string, error) {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	page := receiptHTMLPage{
		Title:                     fmt.Sprintf("Sam Harness %s receipt", receipt.Phase),
		HarnessVersion:            stringValue(value, "harness_version"),
		Kind:                      stringValue(value, "kind"),
		Phase:                     stringValue(value, "phase"),
		Status:                    stringValue(value, "status"),
		Passed:                    stringValue(value, "passed"),
		Error:                     stringValue(value, "error"),
		Repository:                stringValue(value, "repository"),
		Root:                      stringValue(value, "root"),
		Fingerprint:               stringValue(value, "repository_fingerprint"),
		FinalFingerprint:          stringValue(value, "final_repository_fingerprint"),
		ConfigSource:              stringValue(value, "config_source"),
		ConfigSHA256:              stringValue(value, "config_sha256"),
		ReviewBaseSHA:             stringValue(value, "review_base_sha"),
		ReviewBaseFingerprint:     stringValue(value, "review_base_fingerprint"),
		ReviewHeadSHA:             stringValue(value, "review_head_sha"),
		ReviewHeadFingerprint:     stringValue(value, "review_head_fingerprint"),
		ReviewPatch:               stringValue(value, "review_patch"),
		ReviewPatchSHA256:         stringValue(value, "review_patch_sha256"),
		ReviewLineageSHA256:       stringValue(value, "review_lineage_sha256"),
		PriorReviewReceipt:        stringValue(value, "prior_review_receipt"),
		PriorReviewReceiptSHA256:  stringValue(value, "prior_review_receipt_sha256"),
		PriorReviewManifestSHA256: stringValue(value, "prior_review_manifest_sha256"),
		ReviewConvergence:         stringValue(value, "review_convergence"),
		ResolvedFindingIDs:        stringList(value, "resolved_finding_ids"),
		UnresolvedFindingIDs:      stringList(value, "unresolved_finding_ids"),
		RegressionFindingIDs:      stringList(value, "regression_finding_ids"),
		RepairPatch:               stringValue(value, "repair_patch"),
		RepairPatchSHA256:         stringValue(value, "repair_patch_sha256"),
		RepairManifestSHA256:      stringValue(value, "repair_manifest_sha256"),
		SourceReceipt:             stringValue(value, "source_receipt"),
	}
	if rawFindings, ok := value["findings"].([]any); ok {
		page.Findings = receiptHTMLFindings(rawFindings)
	}
	if rawFindings, ok := value["excluded_findings"].([]any); ok {
		page.ExcludedFindings = receiptHTMLFindings(rawFindings)
	}
	page.ReviewExecutionFailures = reviewExecutionFailures(value)
	reviewAttempted := reviewEvidencePresent(receipt, value)
	reviewPhaseFailed := reviewPhaseFailed(value)
	page.ReviewIncomplete = reviewAttempted && (len(page.ReviewExecutionFailures) > 0 || reviewPhaseFailed || (receipt.Phase == model.PhaseReview && !receipt.Passed))
	page.ReviewClean = reviewAttempted && !page.ReviewIncomplete && receipt.Passed && len(page.Findings) == 0
	switch {
	case page.ReviewClean:
		page.ReviewNoFindingsMessage = "CLEAN — no findings were recorded."
	case page.ReviewIncomplete:
		page.ReviewNoFindingsMessage = "INCOMPLETE REVIEW — no approval or complete correction manifest exists. Resolve the block and rerun the review."
		if len(page.ReviewExecutionFailures) > 0 {
			page.ReviewFailureAction = "Fix every failed required reviewer command listed above and rerun the review; no approval or complete correction manifest exists yet."
		} else {
			page.ReviewFailureAction = "Resolve the review block shown above and rerun the review; no approval or complete correction manifest exists yet."
		}
	case reviewAttempted && len(page.Findings) == 0:
		page.ReviewNoFindingsMessage = "REVIEW COMPLETE — no findings were recorded."
	case receipt.Phase == model.PhaseAll && !receipt.Passed:
		page.ReviewNoFindingsMessage = "REVIEW NOT REACHED — this aggregate pipeline failed before a review result was produced."
	}
	var output bytes.Buffer
	if err := receiptHTMLTemplate.Execute(&output, page); err != nil {
		return "", err
	}
	return output.String(), nil
}

func receiptHTMLFindings(rawFindings []any) []receiptHTMLFinding {
	findings := make([]receiptHTMLFinding, 0, len(rawFindings))
	for _, rawFinding := range rawFindings {
		finding, ok := rawFinding.(map[string]any)
		if !ok {
			continue
		}
		findings = append(findings, receiptHTMLFinding{
			ID: stringValue(finding, "id"), Role: stringValue(finding, "role"), Severity: stringValue(finding, "severity"),
			Status: stringValue(finding, "status"), Lineage: stringValue(finding, "lineage"), Summary: stringValue(finding, "summary"),
			Evidence: stringValue(finding, "evidence"), Path: stringValue(finding, "path"), Line: stringValue(finding, "line"),
			RequiredChange: stringValue(finding, "required_change"), Acceptance: stringValue(finding, "acceptance"),
		})
	}
	return findings
}

func reviewEvidencePresent(receipt Receipt, values map[string]any) bool {
	if receipt.Phase == model.PhaseReview {
		return true
	}
	if reviewPhasePresent(values) || reviewCommandPresent(values) {
		return true
	}
	return stringValue(values, "review_base_sha") != "" || stringValue(values, "review_head_sha") != "" || stringValue(values, "review_patch") != ""
}

func reviewPhasePresent(values map[string]any) bool {
	phases, ok := values["phases"].([]any)
	if !ok {
		return false
	}
	for _, raw := range phases {
		phase, ok := raw.(map[string]any)
		if ok && stringValue(phase, "phase") == string(model.PhaseReview) {
			return true
		}
	}
	return false
}

func reviewCommandPresent(values map[string]any) bool {
	commands, ok := values["commands"].([]any)
	if !ok {
		return false
	}
	for _, raw := range commands {
		command, ok := raw.(map[string]any)
		if ok && stringValue(command, "phase") == string(model.PhaseReview) && strings.HasPrefix(stringValue(command, "name"), "review:") {
			return true
		}
	}
	return false
}

func reviewPhaseFailed(values map[string]any) bool {
	phases, ok := values["phases"].([]any)
	if !ok {
		return false
	}
	for _, raw := range phases {
		phase, ok := raw.(map[string]any)
		if ok && stringValue(phase, "phase") == string(model.PhaseReview) && stringValue(phase, "status") != string(StatusPassed) {
			return true
		}
	}
	return false
}

func reviewExecutionFailures(values map[string]any) []receiptHTMLExecutionFailure {
	commands, ok := values["commands"].([]any)
	if !ok {
		return nil
	}
	failures := make([]receiptHTMLExecutionFailure, 0)
	for _, raw := range commands {
		command, ok := raw.(map[string]any)
		if !ok || stringValue(command, "phase") != string(model.PhaseReview) || !boolValue(command, "required") || boolValue(command, "passed") {
			continue
		}
		name := stringValue(command, "name")
		if !strings.HasPrefix(name, "review:") {
			continue
		}
		timedOut := boolValue(command, "timed_out")
		skipped := boolValue(command, "skipped")
		status := "failed"
		if timedOut {
			status = "timed_out"
		} else if skipped {
			status = "skipped"
		}
		failures = append(failures, receiptHTMLExecutionFailure{
			Role:     strings.TrimPrefix(name, "review:"),
			Name:     name,
			ExitCode: stringValue(command, "exit_code"),
			TimedOut: fmt.Sprint(timedOut),
			Skipped:  fmt.Sprint(skipped),
			Status:   status,
			Output:   receiptHTMLCommandOutput(stringValue(command, "output")),
		})
	}
	return failures
}

func boolValue(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && value
}

func receiptHTMLCommandOutput(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(no output)"
	}
	if len(value) <= receiptHTMLCommandOutputLimit {
		return value
	}
	const markerPrefix = "\n[output excerpt truncated by sam-harness: "
	const markerSuffix = " bytes omitted]\n"
	omitted := len(value) - (receiptHTMLCommandOutputLimit - len(markerPrefix) - len(markerSuffix) - 20)
	for range 4 {
		marker := markerPrefix + strconv.Itoa(omitted) + markerSuffix
		available := receiptHTMLCommandOutputLimit - len(marker)
		omitted = len(value) - available
	}
	marker := markerPrefix + strconv.Itoa(omitted) + markerSuffix
	available := receiptHTMLCommandOutputLimit - len(marker)
	head := available / 2
	tail := available - head
	return value[:head] + marker + value[len(value)-tail:]
}

func stringValue(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func stringList(values map[string]any, key string) string {
	value, ok := values[key].([]any)
	if !ok {
		return ""
	}
	result := make([]string, 0, len(value))
	for _, item := range value {
		result = append(result, fmt.Sprint(item))
	}
	return fmt.Sprint(result)
}
