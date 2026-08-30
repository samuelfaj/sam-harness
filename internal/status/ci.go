package status

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ProviderCheck is a provider-side check or commit status for one SHA.
type ProviderCheck struct {
	Name       string `json:"name"`
	SHA        string `json:"sha"`
	Conclusion string `json:"conclusion"`
	URL        string `json:"url,omitempty"`
}

// ProveCI reports whether required checks succeeded for head. It never infers
// CI from local static/test receipts.
func ProveCI(head string, required []string, checks []ProviderCheck) (bool, string) {
	head = strings.TrimSpace(head)
	if head == "" {
		return false, "git HEAD is missing"
	}
	if len(required) == 0 {
		required = []string{"static", "test"}
	}
	byName := map[string]ProviderCheck{}
	for _, check := range checks {
		if strings.TrimSpace(check.SHA) != "" && !shaEqual(check.SHA, head) {
			continue
		}
		name := strings.TrimSpace(check.Name)
		if name == "" {
			continue
		}
		byName[name] = check
	}
	var missing []string
	for _, name := range required {
		check, ok := byName[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		if !checkPassed(check.Conclusion) {
			return false, fmt.Sprintf("provider check %s concluded %s on %s", name, check.Conclusion, head)
		}
	}
	if len(missing) > 0 {
		return false, "missing provider checks: " + strings.Join(missing, ", ")
	}
	return true, "provider checks passed on " + head
}

func checkPassed(conclusion string) bool {
	switch strings.ToLower(strings.TrimSpace(conclusion)) {
	case "success", "passed", "pass", "ok":
		return true
	default:
		return false
	}
}

func shaEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

type githubCheckRuns struct {
	CheckRuns []struct {
		Name       string `json:"name"`
		HeadSHA    string `json:"head_sha"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
	} `json:"check_runs"`
}

// ParseGitHubCheckRuns maps the GitHub check-runs API payload for sha.
func ParseGitHubCheckRuns(sha string, payload []byte) ([]ProviderCheck, error) {
	var parsed githubCheckRuns
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("parse GitHub check-runs: %w", err)
	}
	out := make([]ProviderCheck, 0, len(parsed.CheckRuns))
	for _, run := range parsed.CheckRuns {
		out = append(out, ProviderCheck{
			Name:       run.Name,
			SHA:        valueOr(run.HeadSHA, sha),
			Conclusion: run.Conclusion,
			URL:        run.HTMLURL,
		})
	}
	return out, nil
}

type gitlabCommitStatus struct {
	Name   string `json:"name"`
	SHA    string `json:"sha"`
	Status string `json:"status"`
	Target string `json:"target_url"`
}

// ParseGitLabCommitStatuses maps the GitLab commit statuses API payload.
func ParseGitLabCommitStatuses(sha string, payload []byte) ([]ProviderCheck, error) {
	var parsed []gitlabCommitStatus
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, fmt.Errorf("parse GitLab commit statuses: %w", err)
	}
	out := make([]ProviderCheck, 0, len(parsed))
	for _, item := range parsed {
		out = append(out, ProviderCheck{
			Name:       item.Name,
			SHA:        valueOr(item.SHA, sha),
			Conclusion: item.Status,
			URL:        item.Target,
		})
	}
	return out, nil
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
