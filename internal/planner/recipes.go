package planner

import (
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

const defaultReviewTimeoutSeconds = 900

func ReviewerRecipe(host string) []string {
	switch host {
	case model.AgentHostCodex:
		return []string{"npx", "--yes", "@openai/codex@0.150.1", "exec", "--sandbox", "read-only", "--ephemeral", "--output-schema", "reviewer-output.schema.json", "-"}
	case model.AgentHostClaudeCode:
		return []string{"claude", "--print", "--output-format", "json"}
	case model.AgentHostGrok:
		return []string{"grok", "review", "--json"}
	default:
		return nil
	}
}

func ReviewTimeoutSeconds(answers model.Answers) int {
	if answers.ReviewTimeoutSeconds > 0 {
		return answers.ReviewTimeoutSeconds
	}
	return defaultReviewTimeoutSeconds
}

func applyRuntimeReviewers(workflow *model.WorkflowConfig, answers model.Answers) []string {
	if workflow == nil || answers.ConfirmRuntimeReviewers == nil || !*answers.ConfirmRuntimeReviewers {
		return nil
	}
	host := ""
	if answers.CIAgentRuntime != nil {
		host = answers.CIAgentRuntime.Host
	}
	command := ReviewerRecipe(host)
	if len(command) == 0 {
		return nil
	}
	timeout := ReviewTimeoutSeconds(answers)
	existing := map[model.ReviewerRole]model.ReviewerConfig{}
	for _, reviewer := range workflow.Reviewers {
		existing[reviewer.Role] = reviewer
	}
	reviewers := make([]model.ReviewerConfig, 0, len(model.ReviewerRoles))
	for _, role := range model.ReviewerRoles {
		if current, ok := existing[role]; ok && len(current.Command) > 0 {
			reviewers = append(reviewers, current)
			continue
		}
		reviewers = append(reviewers, model.ReviewerConfig{
			Role:               role,
			Command:            append([]string(nil), command...),
			TimeoutSeconds:     timeout,
			FilesystemReadOnly: true,
		})
	}
	workflow.Reviewers = reviewers
	return command
}

func proposedReviewerHost(answers model.Answers) (string, []string) {
	if answers.CIAgentRuntime == nil {
		return "", nil
	}
	command := ReviewerRecipe(answers.CIAgentRuntime.Host)
	if len(command) == 0 {
		return "", nil
	}
	return answers.CIAgentRuntime.Host, command
}

func uxCommandsFromScan(scan model.ScanResult) (browser, accessibility []string) {
	for _, stack := range scan.Stacks {
		if len(browser) == 0 {
			if command := stack.Commands["browser"]; len(command) > 0 {
				browser = append([]string(nil), command...)
			}
		}
		if len(accessibility) == 0 {
			if command := stack.Commands["accessibility"]; len(command) > 0 {
				accessibility = append([]string(nil), command...)
			}
		}
	}
	return browser, accessibility
}

func applyConfirmedUX(answers *model.Answers, browser, accessibility []string) {
	if answers == nil {
		return
	}
	confirmed := map[string]bool{}
	for _, category := range answers.ConfirmGuardDefaults {
		confirmed[category] = true
	}
	if len(answers.BrowserCommand) == 0 && strings.TrimSpace(answers.BrowserWaiver) == "" && confirmed["browser"] && len(browser) > 0 {
		answers.BrowserCommand = append([]string(nil), browser...)
	}
	if len(answers.AccessibilityCommand) == 0 && strings.TrimSpace(answers.AccessibilityWaiver) == "" && confirmed["accessibility"] && len(accessibility) > 0 {
		answers.AccessibilityCommand = append([]string(nil), accessibility...)
	}
}
