package pipeline

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

// RequiredReviewerRoles returns the independent roles that must run for risk.
// Unknown or empty risk fail closed by requiring every configured role.
func RequiredReviewerRoles(risk string) []model.ReviewerRole {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case model.ChangeRiskLow:
		return []model.ReviewerRole{model.ReviewerCorrectness, model.ReviewerSimplicity}
	case model.ChangeRiskMedium:
		return []model.ReviewerRole{
			model.ReviewerSecurity,
			model.ReviewerCorrectness,
			model.ReviewerTestQuality,
			model.ReviewerSimplicity,
		}
	default:
		return append([]model.ReviewerRole(nil), model.ReviewerRoles...)
	}
}

func selectReviewers(configured []model.ReviewerConfig, risk string) ([]model.ReviewerConfig, error) {
	byRole := make(map[model.ReviewerRole]model.ReviewerConfig, len(configured))
	for _, reviewer := range configured {
		byRole[reviewer.Role] = reviewer
	}
	required := RequiredReviewerRoles(risk)
	selected := make([]model.ReviewerConfig, 0, len(required))
	for _, role := range required {
		reviewer, ok := byRole[role]
		if !ok {
			return nil, fmt.Errorf("review is missing required reviewer role %q", role)
		}
		selected = append(selected, reviewer)
	}
	return selected, nil
}

// Arbitrate preserves every independent finding and its role attribution.
// Conflicting severity at the same path and line blocks until resolved.
func Arbitrate(findings []Finding) []string {
	type location struct {
		path string
		line int
	}
	groups := map[location][]Finding{}
	for _, finding := range findings {
		key := location{path: filepath.ToSlash(strings.ToLower(strings.TrimSpace(finding.Path))), line: finding.Line}
		if key.path == "" {
			key.path = "summary:" + strings.ToLower(strings.TrimSpace(finding.Summary))
		}
		groups[key] = append(groups[key], finding)
	}
	var conflicts []string
	for key, group := range groups {
		severities := map[string]bool{}
		for _, finding := range group {
			severities[finding.Severity] = true
		}
		if len(severities) > 1 {
			conflicts = append(conflicts, fmt.Sprintf("%s:%d", key.path, key.line))
		}
	}
	sort.Strings(conflicts)
	return conflicts
}
