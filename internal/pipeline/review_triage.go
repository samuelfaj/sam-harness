package pipeline

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/samuelfaj/sam-harness/internal/jev"
	"github.com/samuelfaj/sam-harness/internal/model"
)

const (
	jevSkipThreshold  = 0.10
	jevTriagePatchMax = 60000
	jevTriageTimeout  = 45 * time.Second
)

// ReviewTriageDecision records the Jev relevance estimate for one reviewer role.
type ReviewTriageDecision struct {
	Role        string  `json:"role"`
	Probability float64 `json:"probability"`
	Skipped     bool    `json:"skipped"`
	Model       string  `json:"model"`
	Error       string  `json:"error,omitempty"`
}

var reviewerDomains = map[model.ReviewerRole]string{
	model.ReviewerArchitecture:  "architecture: module boundaries, layering, coupling, public interfaces, dependency direction and structural design",
	model.ReviewerSecurity:      "security: authentication, authorization, input validation, injection, secrets, unsafe file or process handling and data exposure",
	model.ReviewerCorrectness:   "correctness: logic errors, edge cases, error handling, concurrency, resource handling and behavior regressions in code",
	model.ReviewerTestQuality:   "test quality: whether tests exist, assert meaningful behavior, cover edge cases and would fail on a regression",
	model.ReviewerBusinessRules: "business rules: domain rules, product requirements, validation of user-visible behavior and data semantics",
	model.ReviewerSimplicity:    "simplicity: unnecessary abstraction, duplication, dead code, over-engineering and avoidable complexity",
}

// reviewTriage asks Jev once which roles the patch plausibly concerns. It
// returns a decision per role; any failure yields decisions that skip nothing.
func reviewTriage(client *jev.Client, patch []byte, reviewers []model.ReviewerConfig) []ReviewTriageDecision {
	decisions := make([]ReviewTriageDecision, len(reviewers))
	questions := make(map[string]string, len(reviewers))
	for index, reviewer := range reviewers {
		role := string(reviewer.Role)
		decisions[index].Role = role
		domain := reviewerDomains[reviewer.Role]
		if domain == "" {
			domain = role
		}
		questions[role] = fmt.Sprintf("Does this diff plausibly touch the domain of a %s reviewer, so that it could contain an issue a %s reviewer must check? Answer yes if there is any reasonable chance.", domain, role)
	}
	text := string(patch)
	if len(text) > jevTriagePatchMax {
		text = text[:jevTriagePatchMax]
	}
	state := map[string]any{"changed_files": patchChangedFiles(text), "patch": text}
	ctx, cancel := context.WithTimeout(context.Background(), jevTriageTimeout)
	defer cancel()
	answers, served, err := client.Nouls(ctx, state, questions)
	if err != nil {
		for index := range decisions {
			decisions[index].Error = err.Error()
		}
		return decisions
	}
	for index := range decisions {
		decisions[index].Probability = answers[decisions[index].Role]
		decisions[index].Model = served
		decisions[index].Skipped = decisions[index].Probability < jevSkipThreshold
	}
	return decisions
}

func patchChangedFiles(patch string) []string {
	seen := map[string]bool{}
	files := []string{}
	for _, line := range strings.Split(patch, "\n") {
		var path string
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			path = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "--- a/"):
			path = strings.TrimPrefix(line, "--- a/")
		default:
			continue
		}
		path = strings.TrimSpace(path)
		if path != "" && !seen[path] {
			seen[path] = true
			files = append(files, path)
		}
	}
	sort.Strings(files)
	return files
}

func skippedReviewResult(reviewer model.ReviewerConfig, decision ReviewTriageDecision) CommandResult {
	now := time.Now().UTC()
	return CommandResult{
		Name: "review:" + string(reviewer.Role), Phase: model.PhaseReview, Workdir: ".",
		Command: append([]string(nil), reviewer.Command...), Required: true, Passed: true, ExitCode: 0,
		Output:    fmt.Sprintf("skipped by Jev triage: p=%.3f model=%s", decision.Probability, decision.Model),
		StartedAt: now, FinishedAt: now,
	}
}
