package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
)

type receiptHTMLPage struct {
	Title                     string
	HarnessVersion            string
	Kind                      string
	Phase                     string
	Status                    string
	Passed                    string
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
dl{display:grid;grid-template-columns:minmax(12rem,22rem) 1fr;gap:.35rem 1rem}dt{color:var(--muted);font-weight:600}dd{margin:0;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;white-space:pre-wrap}
table{border-collapse:collapse;width:100%;table-layout:fixed}th,td{border:1px solid var(--line);padding:.6rem;vertical-align:top;text-align:left}th{background:var(--code)}pre{font:inherit;margin:0;white-space:pre-wrap}.finding-meta{color:var(--muted);font-family:ui-monospace,SFMono-Regular,Menlo,monospace}.good{color:var(--good)}.bad{color:var(--bad)}
</style>
</head>
<body><main>
<h1>{{.Title}}</h1>
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
<p>Finding scope: initial findings must identify a current added or modified line in the base-to-head diff, or line 0 only for deletion-only, deleted, or pure-rename file-level evidence. During convergence, frozen actions remain eligible; new findings use the same scope rule in the prior-head-to-current-head diff.</p>
<h2>Findings and required changes</h2>
{{if .Findings}}<table><thead><tr><th>Finding</th><th>Evidence</th><th>Exact required change</th><th>Acceptance</th></tr></thead><tbody>{{range .Findings}}<tr><td><div class="finding-meta">id={{.ID}}<br>role={{.Role}} severity={{.Severity}} status={{.Status}} line={{.Line}}<br>lineage={{.Lineage}}</div><strong>{{.Summary}}</strong><br><code>{{.Path}}</code></td><td><pre>{{.Evidence}}</pre></td><td><pre>{{.RequiredChange}}</pre></td><td><pre>{{.Acceptance}}</pre></td></tr>{{end}}</tbody></table>{{else}}<p>No findings were recorded.</p>{{end}}
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
		page.Findings = make([]receiptHTMLFinding, 0, len(rawFindings))
		for _, rawFinding := range rawFindings {
			finding, ok := rawFinding.(map[string]any)
			if !ok {
				continue
			}
			page.Findings = append(page.Findings, receiptHTMLFinding{
				ID: stringValue(finding, "id"), Role: stringValue(finding, "role"), Severity: stringValue(finding, "severity"),
				Status: stringValue(finding, "status"), Lineage: stringValue(finding, "lineage"), Summary: stringValue(finding, "summary"),
				Evidence: stringValue(finding, "evidence"), Path: stringValue(finding, "path"), Line: stringValue(finding, "line"),
				RequiredChange: stringValue(finding, "required_change"), Acceptance: stringValue(finding, "acceptance"),
			})
		}
	}
	var output bytes.Buffer
	if err := receiptHTMLTemplate.Execute(&output, page); err != nil {
		return "", err
	}
	return output.String(), nil
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
