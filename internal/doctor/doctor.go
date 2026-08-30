package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

type Report struct {
	Passed   bool     `json:"passed"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

func Run(path string) (Report, error) {
	root, err := repo.ResolveRoot(path)
	if err != nil {
		return Report{}, err
	}
	cfg, err := config.Load(filepath.Join(root, ".sam-harness", "config.yaml"))
	if err != nil {
		return Report{Errors: []string{err.Error()}}, nil
	}
	report := Report{Passed: true}
	if cfg.HarnessVersion != model.HarnessVersion {
		report.Warnings = append(report.Warnings, fmt.Sprintf("config uses harness %s; CLI is %s", cfg.HarnessVersion, model.HarnessVersion))
	}
	requiredDocuments := map[string][]string{
		"AGENTS.md":                              {"<!-- sam-harness:start -->", "<!-- sam-harness:end -->"},
		"CLAUDE.md":                              {"<!-- sam-harness:start -->", "<!-- sam-harness:end -->"},
		"GEMINI.md":                              {"<!-- sam-harness:start -->", "<!-- sam-harness:end -->"},
		".github/copilot-instructions.md":        {"<!-- sam-harness:start -->", "<!-- sam-harness:end -->"},
		".gitignore":                             {"# sam-harness:start", ".sam-harness/evidence/", "# sam-harness:end"},
		".sam-harness/GATES.md":                  {"# Gates"},
		".sam-harness/DELEGATION.md":             {"# Authority and delegation"},
		".sam-harness/UX_GATES.md":               {"# User experience gates"},
		".sam-harness/INVARIANTS.md":             {"# Project invariants"},
		".sam-harness/WORKFLOW.md":               {"# Executable workflow"},
		".sam-harness/REVIEWERS.md":              {"# Independent reviewers", "filesystem_read_only: true", "trusted_external_command: true", "trusted_config_arguments", "does not OS-sandbox arbitrary argv"},
		".sam-harness/CHANGE_BUDGET.md":          {"# Bounded correction", "local workspace", "remote authority", "cumulative", "frozen baseline", "filesystem_sandboxed: true", "trusted_external_command: true", "trusted_config_arguments", "does not OS-sandbox arbitrary argv"},
		".sam-harness/runbooks/observability.md": {"# Observability and release observation"},
		".sam-harness/runbooks/retirement.md":    {"# Migration and retirement"},
	}
	for path, fragments := range requiredDocuments {
		requireFile(&report, root, path, fragments...)
	}
	for _, lifecycle := range []string{"classify", "context", "plan", "implement", "review", "repair", "release"} {
		path := ".agents/skills/sam-harness-" + lifecycle + "/SKILL.md"
		requireFile(&report, root, path, "name: sam-harness-"+lifecycle, "canonical configuration")
		if lifecycle == "repair" {
			requireFile(&report, root, path, "local workspace", "remote authority", "cumulative", "frozen baseline", "filesystem_sandboxed: true", "trusted_external_command: true", "trusted_config_arguments", "does not OS-sandbox arbitrary argv")
		}
		if lifecycle == "review" {
			requireFile(&report, root, path, "filesystem_read_only: true", "trusted_external_command: true", "trusted_config_arguments", "does not OS-sandbox arbitrary argv")
		}
	}
	requireFile(&report, root, ".github/pull_request_template.md", "<!-- sam-harness:start -->", "### Evidence ladder", "### Human-facing and UX checks", "<!-- sam-harness:end -->")
	requireFile(&report, root, ".gitlab/merge_request_templates/sam-harness.md", "<!-- sam-harness:start -->", "### Evidence ladder", "### Human-facing and UX checks", "<!-- sam-harness:end -->")
	for _, workspace := range nestedWorkspaces(cfg.Stacks) {
		requireFile(&report, root, filepath.ToSlash(filepath.Join(workspace, "AGENTS.md")), "<!-- sam-harness:start -->", "Sam Harness workspace contract", "<!-- sam-harness:end -->")
	}
	for _, gate := range cfg.Gates {
		if len(gate.Command) == 0 {
			report.Errors = append(report.Errors, fmt.Sprintf("empty command for %s", gate.Name))
			continue
		}
		if _, err := exec.LookPath(gate.Command[0]); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("command unavailable for %s: %s", gate.Name, gate.Command[0]))
		}
	}
	if cfg.Design.Applicable && strings.TrimSpace(cfg.Design.SourceOfTruth) == "" {
		report.Errors = append(report.Errors, "design source of truth is required for a user interface")
	}
	if cfg.Profile == model.ProfileProduction || cfg.Profile == model.ProfileRegulated {
		if cfg.Workflow == nil || !cfg.Workflow.Enabled {
			report.Errors = append(report.Errors, "production workflow is not enabled")
		}
		if !cfg.CI.Managed {
			report.Errors = append(report.Errors, "production workflow requires managed CI")
		}
		for _, path := range []string{".sam-harness/runbooks/release.md", ".sam-harness/runbooks/migration.md", ".sam-harness/runbooks/incident.md"} {
			requireFile(&report, root, path, "#")
		}
		requireFile(&report, root, ".sam-harness/WORKFLOW.md", "## Provider-side controls", "remote rule", "pre-merge required-status gate", "trusted base", "--review-base-sha", "exact provider SHAs", "fails closed")
		if cfg.Workflow != nil {
			requireFile(&report, root, ".sam-harness/WORKFLOW.md", cfg.Workflow.ReleaseSchedule.Cron, cfg.Workflow.ReleaseSchedule.Timezone)
		}
		for _, category := range append(append([]string(nil), model.StaticGuardCategories...), model.TestGuardCategories...) {
			requireFile(&report, root, ".sam-harness/WORKFLOW.md", "- "+category)
			requireFile(&report, root, ".sam-harness/GATES.md", "- "+category)
		}
		requireFile(&report, root, ".sam-harness/GATES.md", "## Provider-side controls", "Local YAML", "external trusted-control", "fails closed")
	}
	if cfg.Profile == model.ProfileRegulated {
		for _, path := range []string{".sam-harness/runbooks/threat-model.md", ".sam-harness/runbooks/data-governance.md"} {
			requireFile(&report, root, path, "#")
		}
	}
	if cfg.CI.Managed {
		for _, provider := range cfg.CI.Providers {
			if len(cfg.CI.SecretBindings[provider]) > 0 {
				requireFile(&report, root, ".sam-harness/WORKFLOW.md", cfg.CI.AgentSecretEnvironments[provider], cfg.CI.AgentControlPlanes[provider].RequiredCheck, "fails closed")
			}
			if providerUsesExternalAgentControl(cfg, provider) {
				requireFile(&report, root, ".sam-harness/WORKFLOW.md", cfg.CI.AgentControlPlanes[provider].ExternalProject, cfg.CI.AgentControlPlanes[provider].RequiredCheck, "external trusted control plane", "absence blocks merge")
			}
			switch provider {
			case "github":
				fragments := []string{"# Generated by sam-harness", "merge_group:", "permissions:\n  contents: read", "  static:", "  test:"}
				if cfg.Workflow != nil && cfg.Workflow.Enabled {
					fragments = append(fragments, "workflow_dispatch:", "artifact_run_id:", "  artifact:", "  staging:", "  production:", "  observe:", "environment:", "sam-harness-receipts-artifact", "${RUNNER_TEMP}/sam-harness-immutable-artifact.tar", "tar -cf", "tar -xf")
					if cfg.Workflow.ReleaseSchedule.Cron != "" {
						fragments = append(fragments, cfg.Workflow.ReleaseSchedule.Cron, cfg.Workflow.ReleaseSchedule.Timezone)
					}
				}
				path := ".github/workflows/sam-harness.yml"
				requireFile(&report, root, path, fragments...)
				validateGitHubWorkflow(&report, root, path, cfg)
				requireFile(&report, root, ".github/workflows/sam-harness-merge-queue-dispatch.yml", "merge_group:", "sam_harness_merge_group_review")
				if providerHasAgentBindings(cfg, "github") {
					agentPath := ".github/workflows/sam-harness-agents.yml"
					fragments := []string{"name: sam-harness agents", "actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349"}
					if len(scopedSecretBindings(cfg, "github", model.CISecretScopeReview)) > 0 {
						fragments = append(fragments, "pull_request_target:", "repository_dispatch:", "types: [sam_harness_merge_group_review]", cfg.CI.AgentControlPlanes["github"].RequiredCheck)
					}
					if repairEnabled(cfg) && len(scopedSecretBindings(cfg, "github", model.CISecretScopeRepair)) > 0 {
						fragments = append(fragments, "workflow_run:")
					}
					requireFile(&report, root, agentPath, fragments...)
					validateGitHubAgentsWorkflow(&report, root, agentPath, cfg)
				}
			case "gitlab":
				fragments := []string{"# Generated by sam-harness", "sam-harness-static:", "sam-harness-test:"}
				if cfg.Workflow != nil && cfg.Workflow.Enabled {
					fragments = append(fragments, "sam-harness-artifact:", "sam-harness-staging:", "sam-harness-production:", "sam-harness-observe:", "name: production", "when: manual", "allow_failure: false", ".sam-harness/evidence/transport/sam-harness-immutable-artifact.tar", "tar -cf", "tar -xf")
					if !providerUsesExternalAgentControl(cfg, "gitlab") && len(scopedSecretBindings(cfg, "gitlab", model.CISecretScopeReview)) == 0 {
						fragments = append(fragments, "sam-harness-review:")
					}
					if cfg.Workflow.Correction.OpenChangeRequest && !providerUsesExternalAgentControl(cfg, "gitlab") && len(scopedSecretBindings(cfg, "gitlab", model.CISecretScopeRepair)) == 0 {
						fragments = append(fragments, "sam-harness-publish-repair:", "core.hooksPath /dev/null", "*-repair.patch", "*-repair.json", "repair_patch_sha256", "patch_count", "receipt_count", "SAM_HARNESS_PUBLISH_REPAIR", "when: always")
					}
				}
				path := ".sam-harness/ci/gitlab.yml"
				requireFile(&report, root, path, fragments...)
				validateGitLabWorkflow(&report, root, path, cfg)
				requireFile(&report, root, ".gitlab-ci.yml", "# sam-harness:start", ".sam-harness/ci/gitlab.yml", "# sam-harness:end")
			}
		}
	}
	report.Passed = len(report.Errors) == 0
	return report, nil
}

func nestedWorkspaces(stacks []model.Stack) []string {
	seen := map[string]bool{}
	var paths []string
	for _, stack := range stacks {
		path := filepath.ToSlash(filepath.Clean(filepath.FromSlash(stack.Path)))
		if path == "." || seen[path] {
			continue
		}
		seen[path] = true
		paths = append(paths, path)
	}
	return paths
}

func requireFile(report *Report, root, path string, fragments ...string) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		report.Errors = append(report.Errors, fmt.Sprintf("missing %s", path))
		return
	}
	content := string(data)
	for _, fragment := range fragments {
		if !strings.Contains(content, fragment) {
			report.Errors = append(report.Errors, fmt.Sprintf("incomplete %s: missing %q", path, fragment))
		}
	}
}

func validateGitHubWorkflow(report *Report, root, path string, cfg model.Config) {
	if cfg.Workflow == nil || !cfg.Workflow.Enabled {
		return
	}
	production := requireSection(report, root, path, "  production:\n", "  observe:\n")
	rollback := requireSection(report, root, path, "  rollback:\n", "")
	sections := map[model.Phase]string{
		model.PhaseStaging:    requireSection(report, root, path, "  staging:\n", githubNextJob(cfg, model.PhaseStaging)),
		model.PhaseProduction: production,
		model.PhaseObserve:    requireSection(report, root, path, "  observe:\n", "  rollback:\n"),
		model.PhaseRollback:   rollback,
	}
	if len(cfg.Workflow.Migration) > 0 {
		sections[model.PhaseMigration] = requireSection(report, root, path, "  migration:\n", "  production:\n")
	}
	for phase, section := range sections {
		if strings.Count(section, "\n    if:") != 1 {
			report.Errors = append(report.Errors, fmt.Sprintf("incomplete %s: %s job must declare exactly one job-level if condition", path, phase))
		}
		if phaseAuthorizedForCI(cfg, phase) {
			if phase == model.PhaseRollback {
				requireFragments(report, path, section, "github.event_name == 'workflow_dispatch'", "inputs.phase == 'rollback'", "run-id: ${{ inputs.artifact_run_id }}")
			} else {
				requireFragments(report, path, section, "github.event_name != 'workflow_dispatch'")
			}
		} else {
			requireFragments(report, path, section, "if: ${{ false }}")
		}
	}
	reviewBound := len(scopedSecretBindings(cfg, "github", model.CISecretScopeReview)) > 0
	repairBound := len(scopedSecretBindings(cfg, "github", model.CISecretScopeRepair)) > 0
	localRepair := repairEnabled(cfg) && !repairBound
	artifactEnd := "  staging:\n"
	if localRepair {
		artifactEnd = "  repair_static:\n"
	}
	testEnd := "  artifact:\n"
	if !reviewBound {
		review := requireSection(report, root, path, "  review:\n", "  artifact:\n")
		testEnd = "  review:\n"
		requireFragments(report, path, review, "github.event_name == 'pull_request'", "github.event_name == 'merge_group'", "trusted-control/.sam-harness/config.yaml", "--review-base-sha", "--review-head-sha", "persist-credentials: false")
		forbidFragments(report, path, review, "${{ secrets.")
	} else {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err == nil {
			content := string(data)
			forbidFragments(report, path, content, "  review:\n")
			if repairBound {
				forbidFragments(report, path, content, "  repair_static:\n", "  repair_test:\n", "  repair_review:\n", "  repair_artifact:\n", "  publish_repair:\n")
			}
			for _, scope := range []string{model.CISecretScopeReview, model.CISecretScopeRepair} {
				for _, binding := range scopedSecretBindings(cfg, "github", scope) {
					forbidFragments(report, path, content, "secrets."+binding.Secret)
				}
			}
		}
	}
	if localRepair {
		phases := []model.Phase{model.PhaseStatic, model.PhaseTest}
		if !reviewBound {
			phases = append(phases, model.PhaseReview)
		}
		phases = append(phases, model.PhaseArtifact)
		for index, phase := range phases {
			end := "  staging:\n"
			if index+1 < len(phases) {
				end = "  repair_" + string(phases[index+1]) + ":\n"
			} else if cfg.Workflow.Correction.OpenChangeRequest {
				end = "  publish_repair:\n"
			}
			repair := requireSection(report, root, path, "  repair_"+string(phase)+":\n", end)
			requireFragments(report, path, repair, "Run bounded repair from trusted control plane", "trusted-control/.sam-harness/config.yaml", "repair_patch_sha256", "actual_repair_patch_sha256", "persist-credentials: false")
			forbidFragments(report, path, repair, "${{ secrets.", "contents: write")
		}
		if cfg.Workflow.Correction.OpenChangeRequest {
			publisher := requireSection(report, root, path, "  publish_repair:\n", "  staging:\n")
			requireFragments(report, path, publisher, `test "$patch_count" -eq 1`, `test "$receipt_count" -eq 1`, "expected_patch_sha256", "actual_patch_sha256", "core.hooksPath /dev/null")
			forbidFragments(report, path, publisher, "OPENAI_API_KEY", "REPAIR_API_KEY")
		}
	}
	for _, bounds := range [][2]string{
		{"  static:\n", "  test:\n"},
		{"  test:\n", testEnd},
		{"  artifact:\n", artifactEnd},
	} {
		phaseJob := requireSection(report, root, path, bounds[0], bounds[1])
		forbidFragments(report, path, phaseJob, "REPAIR_ENV", "Run bounded repair", " repair ")
	}
	for phase, bounds := range map[model.Phase][2]string{
		model.PhaseStatic:   {"  static:\n", "  test:\n"},
		model.PhaseTest:     {"  test:\n", testEnd},
		model.PhaseArtifact: {"  artifact:\n", artifactEnd},
	} {
		bindings := scopedSecretBindings(cfg, "github", string(phase))
		if len(bindings) == 0 {
			continue
		}
		phaseJob := requireSection(report, root, path, bounds[0], bounds[1])
		requireFragments(report, path, phaseJob, "agent secrets cannot be injected into pull-request-controlled phase jobs")
		for _, binding := range bindings {
			forbidFragments(report, path, phaseJob, fmt.Sprintf("${{ secrets.%s }}", binding.Secret))
		}
	}
	if phaseAuthorizedForCI(cfg, model.PhaseProduction) {
		requireFragments(report, path, production, "contents: write", "GH_TOKEN: ${{ github.token }}")
		requireFragments(report, path, rollback, "contents: write", "GH_TOKEN: ${{ github.token }}")
	} else {
		for _, section := range []string{production, rollback} {
			forbidFragments(report, path, section, "contents: write", "GH_TOKEN:")
		}
	}
	forbidFragments(report, path, rollback, "needs: production", "failure()")
	for _, bounds := range [][2]string{
		{"  static:\n", "  test:\n"},
		{"  test:\n", testEnd},
		{"  artifact:\n", artifactEnd},
		{"  staging:\n", githubNextJob(cfg, model.PhaseStaging)},
		{"  production:\n", "  observe:\n"},
		{"  observe:\n", "  rollback:\n"},
		{"  rollback:\n", ""},
	} {
		requireFragments(report, path, requireSection(report, root, path, bounds[0], bounds[1]), "persist-credentials: false")
	}
	if len(cfg.Workflow.Migration) > 0 {
		requireFragments(report, path, requireSection(report, root, path, "  migration:\n", "  production:\n"), "persist-credentials: false")
	}
}

func validateGitHubAgentsWorkflow(report *Report, root, path string, cfg model.Config) {
	contentBytes, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return
	}
	content := string(contentBytes)
	control := cfg.CI.AgentControlPlanes["github"]
	environment := "environment:\n      name: '" + cfg.CI.AgentSecretEnvironments["github"] + "'"
	reviewBound := len(scopedSecretBindings(cfg, "github", model.CISecretScopeReview)) > 0
	repairBound := repairEnabled(cfg) && len(scopedSecretBindings(cfg, "github", model.CISecretScopeRepair)) > 0
	repairReview := repairEnabled(cfg) && reviewBound
	requireFragments(report, path, content, "actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349", "secrets."+control.AppIDSecret, "secrets."+control.AppPrivateKeySecret)
	if reviewBound {
		requireFragments(report, path, content, "pull_request_target:\n    types: [opened, synchronize, reopened, ready_for_review]", "repository_dispatch:\n    types: [sam_harness_merge_group_review]")
		forbidFragments(report, path, content, "\n  merge_group:\n", "github.event.merge_group")
	} else {
		forbidFragments(report, path, content, "  pull_request_target:\n", "  repository_dispatch:\n", "  merge_group:\n")
	}
	if repairBound {
		requireFragments(report, path, content, "workflow_run:\n", "workflows: [sam-harness]")
	} else {
		forbidFragments(report, path, content, "  workflow_run:\n")
	}
	resolveEnd := ""
	if reviewBound {
		resolveEnd = "  start_review_check:\n"
	} else if repairBound {
		resolveEnd = "  repair_failed_phase:\n"
	}
	resolve := requireSection(report, root, path, "  resolve:\n", resolveEnd)
	requireFragments(report, path, resolve, environment, "actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349")

	if reviewBound {
		requireFragments(report, path, content, "github.event.client_payload.head_sha", "github.event.client_payload.base_sha", "github.event.client_payload.merge_group_ref", "refs/heads/gh-readonly-queue/*", "merge-group head drifted before trusted agent execution", "merge-group base drifted before trusted agent execution", "Pull request or merge-group identity changed")
		start := requireSection(report, root, path, "  start_review_check:\n", "  review:\n")
		review := requireSection(report, root, path, "  review:\n", "  conclude_review_check:\n")
		concludeEnd := ""
		if repairReview {
			concludeEnd = "  repair_review:\n"
		}
		conclude := requireSection(report, root, path, "  conclude_review_check:\n", concludeEnd)
		for _, appSection := range []string{start, conclude} {
			requireFragments(report, path, appSection, environment, "actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349")
		}
		requireFragments(report, path, start, "permission-checks: write", control.RequiredCheck, "status=in_progress", "head_sha=\"$REVIEW_HEAD_SHA\"")
		requireFragments(report, path, review, environment, "ref: ${{ needs.resolve.outputs.head_sha }}", "ref: ${{ needs.resolve.outputs.base_sha }}", "trusted-control/.sam-harness/config.yaml", "--review-base-sha", "--review-head-sha", "review_base_sha", "review_head_sha", "review_patch_sha256", "actual_review_patch_sha256", "go run github.com/samuelfaj/sam-harness/cmd/sam-harness@v"+cfg.HarnessVersion)
		forbidFragments(report, path, review, "Prepare repository", "before_script", "go run ./cmd/sam-harness", "uses: ./", "actions/cache")
		validateGitHubAgentBoundary(report, path, review, cfg, model.CISecretScopeReview)
		requireFragments(report, path, conclude, "permission-checks: write", "permission-pull-requests: read", "permission-contents: read", "current_head", "current_base", "CHECK_RUN_ID", "status=completed", "conclusion=\"$conclusion\"")
		forbidFragments(report, path, start, "github.token", "GITHUB_TOKEN")
		forbidFragments(report, path, conclude, "github.token", "GITHUB_TOKEN")
	}

	if repairReview || repairBound {
		var repairs []string
		var repairReviewSection string
		if repairReview {
			repairReviewEnd := ""
			if repairBound {
				repairReviewEnd = "  repair_failed_phase:\n"
			} else if cfg.Workflow.Correction.OpenChangeRequest {
				repairReviewEnd = "  publish_repair:\n"
			}
			repairReviewSection = requireSection(report, root, path, "  repair_review:\n", repairReviewEnd)
			repairs = append(repairs, repairReviewSection)
		}
		var repairPhase string
		if repairBound {
			repairPhaseEnd := ""
			if cfg.Workflow.Correction.OpenChangeRequest {
				repairPhaseEnd = "  publish_repair:\n"
			}
			repairPhase = requireSection(report, root, path, "  repair_failed_phase:\n", repairPhaseEnd)
			repairs = append(repairs, repairPhase)
		}
		for _, repair := range repairs {
			requireFragments(report, path, repair, environment, "trusted-control/.sam-harness/config.yaml", "repair_patch_sha256", "actual_repair_patch_sha256", "steps.repair.outputs.repair_patch", "steps.repair.outputs.repair_receipt", "persist-credentials: false")
			forbidFragments(report, path, repair, "Prepare repository", "before_script", "go run ./cmd/sam-harness", "uses: ./", "actions/cache", "permission-contents: write")
			validateGitHubAgentBoundary(report, path, repair, cfg, model.CISecretScopeRepair)
		}
		if cfg.Authority.Network {
			if repairReviewSection != "" {
				branchPrefix := "'" + strings.ReplaceAll(cfg.Workflow.Correction.BranchPrefix, "'", "''") + "'"
				requireFragments(report, path, repairReviewSection, "github.event_name == 'pull_request_target'", "!startsWith(needs.resolve.outputs.head_ref, "+branchPrefix+")", `test "$(jq -r '.status' "$receipt")" = blocked`)
				forbidFragments(report, path, repairReviewSection, "github.event_name == 'merge_group'", "github.event_name == 'repository_dispatch'")
			}
			if repairBound {
				requireFragments(report, path, repairPhase, "github.event_name == 'workflow_run'")
			}
		} else {
			if repairReviewSection != "" {
				requireFragments(report, path, repairReviewSection, "if: ${{ false }}")
			}
			if repairBound {
				requireFragments(report, path, repairPhase, "if: ${{ false }}")
			}
		}
		if repairBound {
			expected := "triggering run must contain exactly one failed static, test, or artifact receipt"
			if !reviewBound {
				expected = "triggering run must contain exactly one failed static, test, review, or artifact receipt"
			}
			requireFragments(report, path, repairPhase, expected, "run-id: ${{ needs.resolve.outputs.source_run_id }}")
		}
		if cfg.Workflow.Correction.OpenChangeRequest {
			publisher := requireSection(report, root, path, "  publish_repair:\n", "")
			requireFragments(report, path, publisher, environment, "permission-contents: write", "permission-pull-requests: write", `test "$patch_count" -eq 1`, `test "$receipt_count" -eq 1`, "expected_patch_sha256", "actual_patch_sha256", "current_head", "core.hooksPath /dev/null", "gh pr create")
			forbidFragments(report, path, publisher, "OPENAI_API_KEY", "REPAIR_API_KEY", "pipeline \"", "repair \"${GITHUB_WORKSPACE}", "github.token", "GITHUB_TOKEN")
		}
	}
}

func validateGitHubAgentBoundary(report *Report, path, section string, cfg model.Config, scope string) {
	for _, binding := range scopedSecretBindings(cfg, "github", scope) {
		requireFragments(report, path, section, fmt.Sprintf("%s: ${{ secrets.%s }}", binding.Environment, binding.Secret), `test -n "${`+binding.Environment+`:-}"`)
	}
}

func validateGitLabWorkflow(report *Report, root, path string, cfg model.Config) {
	if cfg.Workflow == nil || !cfg.Workflow.Enabled {
		return
	}
	rollback := requireSection(report, root, path, "sam-harness-rollback:\n", "")
	forbidFragments(report, path, rollback, "job: sam-harness-production")
	externalControl := providerUsesExternalAgentControl(cfg, "gitlab")
	reviewBound := externalControl || len(scopedSecretBindings(cfg, "gitlab", model.CISecretScopeReview)) > 0
	repairBound := externalControl || len(scopedSecretBindings(cfg, "gitlab", model.CISecretScopeRepair)) > 0
	localRepair := repairEnabled(cfg) && !repairBound
	artifactEnd := "sam-harness-staging:\n"
	if localRepair {
		artifactEnd = "sam-harness-repair-static:\n"
	}
	sections := map[model.Phase]string{
		model.PhaseStaging:    requireSection(report, root, path, "sam-harness-staging:\n", gitLabNextJob(cfg, model.PhaseStaging)),
		model.PhaseProduction: requireSection(report, root, path, "sam-harness-production:\n", "sam-harness-observe:\n"),
		model.PhaseObserve:    requireSection(report, root, path, "sam-harness-observe:\n", "sam-harness-rollback:\n"),
		model.PhaseRollback:   rollback,
	}
	if len(cfg.Workflow.Migration) > 0 {
		sections[model.PhaseMigration] = requireSection(report, root, path, "sam-harness-migration:\n", "sam-harness-production:\n")
	}
	for phase, section := range sections {
		if phaseAuthorizedForCI(cfg, phase) {
			forbidFragments(report, path, section, "rules:\n    - when: never")
		} else {
			requireFragments(report, path, section, "rules:\n    - when: never")
		}
	}
	testEnd := "sam-harness-artifact:\n"
	if reviewBound {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err == nil {
			content := string(data)
			forbidden := []string{"sam-harness-review:\n"}
			if environment := strings.TrimSpace(cfg.CI.AgentSecretEnvironments["gitlab"]); environment != "" {
				forbidden = append(forbidden, environment)
			}
			forbidFragments(report, path, content, forbidden...)
			if repairBound {
				forbidFragments(report, path, content, "sam-harness-repair-static:\n", "sam-harness-repair-test:\n", "sam-harness-repair-review:\n", "sam-harness-repair-artifact:\n", "sam-harness-publish-repair:\n")
			}
			for _, scope := range []string{model.CISecretScopeReview, model.CISecretScopeRepair} {
				for _, binding := range scopedSecretBindings(cfg, "gitlab", scope) {
					forbidFragments(report, path, content, binding.Secret, binding.Environment)
				}
			}
		}
	} else {
		review := requireSection(report, root, path, "sam-harness-review:\n", "sam-harness-artifact:\n")
		testEnd = "sam-harness-review:\n"
		requireFragments(report, path, review, `$CI_PIPELINE_SOURCE == "merge_request_event"`, "sam-harness-trusted-${CI_PROJECT_ID}-${CI_JOB_ID}/.sam-harness/config.yaml", "--config", `--review-base "$CI_BUILDS_DIR/sam-harness-trusted-${CI_PROJECT_ID}-${CI_JOB_ID}"`, "go run github.com/samuelfaj/sam-harness/cmd/sam-harness@v"+cfg.HarnessVersion, "initial adoption fails closed")
		forbidFragments(report, path, review, "extends: .sam-harness-base", "before_script", "go run ./cmd/sam-harness", "env REVIEW_ENV")
	}
	for _, bounds := range [][2]string{
		{"sam-harness-static:\n", "sam-harness-test:\n"},
		{"sam-harness-test:\n", testEnd},
		{"sam-harness-artifact:\n", artifactEnd},
	} {
		phaseJob := requireSection(report, root, path, bounds[0], bounds[1])
		forbidFragments(report, path, phaseJob, "REPAIR_ENV", "after_script:", " repair ")
	}
	for phase, bounds := range map[model.Phase][2]string{
		model.PhaseStatic:   {"sam-harness-static:\n", "sam-harness-test:\n"},
		model.PhaseTest:     {"sam-harness-test:\n", testEnd},
		model.PhaseArtifact: {"sam-harness-artifact:\n", artifactEnd},
	} {
		bindings := scopedSecretBindings(cfg, "gitlab", string(phase))
		if len(bindings) == 0 {
			continue
		}
		phaseJob := requireSection(report, root, path, bounds[0], bounds[1])
		requireFragments(report, path, phaseJob, "agent secrets cannot be injected into merge-request-controlled phase jobs")
		for _, binding := range bindings {
			forbidFragments(report, path, phaseJob, fmt.Sprintf(`%s="${%s}"`, binding.Environment, binding.Secret))
		}
	}
	if repairEnabled(cfg) && !repairBound {
		phases := []model.Phase{model.PhaseStatic, model.PhaseTest}
		if !reviewBound {
			phases = append(phases, model.PhaseReview)
		}
		phases = append(phases, model.PhaseArtifact)
		for index, phase := range phases {
			end := gitLabRepairEnd(cfg)
			if index+1 < len(phases) {
				end = "sam-harness-repair-" + string(phases[index+1]) + ":\n"
			}
			repair := requireSection(report, root, path, "sam-harness-repair-"+string(phase)+":\n", end)
			requireFragments(report, path, repair, "stage: repair", "sam-harness-trusted-${CI_PROJECT_ID}-${CI_JOB_ID}/.sam-harness/config.yaml", "--config", "go run github.com/samuelfaj/sam-harness/cmd/sam-harness@v"+cfg.HarnessVersion, "repair_patch_sha256", "actual_repair_patch_sha256", "artifacts:\n    when: always", "repair-artifacts/")
			if cfg.Authority.Network {
				requireFragments(report, path, repair, "when: on_failure")
			} else {
				requireFragments(report, path, repair, "rules:\n    - when: never")
			}
			forbidFragments(report, path, repair, "extends: .sam-harness-base", "before_script", "go run ./cmd/sam-harness", "git push")
			validateGitLabAgentBoundary(report, path, repair, cfg, model.CISecretScopeRepair)
		}
	}
	if cfg.Workflow.Correction.OpenChangeRequest && !repairBound {
		publisher := requireSection(report, root, path, "sam-harness-publish-repair:\n", "sam-harness-staging:\n")
		requireFragments(report, path, publisher, "stage: repair", "job: sam-harness-repair-static", "job: sam-harness-repair-test", "job: sam-harness-repair-artifact", `SAM_HARNESS_PUBLISH_REPAIR == "true"`, "when: always", `test "$patch_count" -eq 1`, `test "$receipt_count" -eq 1`, "expected_patch_sha256", "actual_patch_sha256")
		if reviewBound {
			forbidFragments(report, path, publisher, "job: sam-harness-repair-review")
		} else {
			requireFragments(report, path, publisher, "job: sam-harness-repair-review")
		}
		forbidFragments(report, path, publisher, "pipeline .", "repair .", "OPENAI_API_KEY", "REPAIR_API_KEY")
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err == nil && strings.Count(string(data), "  - repair\n") != 1 {
			report.Errors = append(report.Errors, fmt.Sprintf("incomplete %s: repair stage must be declared exactly once", path))
		}
	}
}

func validateGitLabAgentBoundary(report *Report, path, section string, cfg model.Config, scope string) {
	bindings := scopedSecretBindings(cfg, "gitlab", scope)
	if len(bindings) == 0 {
		return
	}
	requireFragments(report, path, section, "environment:\n    name: '"+cfg.CI.AgentSecretEnvironments["gitlab"]+"'")
	for _, binding := range bindings {
		requireFragments(report, path, section, fmt.Sprintf(`%s="${%s}"`, binding.Environment, binding.Secret), `test -n "${`+binding.Secret+`:-}"`)
	}
}

func phaseAuthorizedForCI(cfg model.Config, phase model.Phase) bool {
	switch phase {
	case model.PhaseReview:
		return cfg.Authority.Network
	case model.PhaseStaging, model.PhaseMigration:
		return cfg.Authority.Network && cfg.Authority.Deploy
	case model.PhaseProduction, model.PhaseRollback:
		return cfg.Authority.Network && cfg.Authority.Deploy && cfg.Authority.Release
	case model.PhaseObserve:
		return cfg.Authority.Network && cfg.Authority.Deploy
	default:
		return true
	}
}

func githubNextJob(cfg model.Config, phase model.Phase) string {
	if phase == model.PhaseStaging && cfg.Workflow != nil && len(cfg.Workflow.Migration) > 0 {
		return "  migration:\n"
	}
	return "  production:\n"
}

func githubNextJobAfterArtifact(cfg model.Config) string {
	if repairEnabled(cfg) {
		return "  repair_static:\n"
	}
	return "  staging:\n"
}

func githubRepairEnd(cfg model.Config) string {
	if cfg.Workflow != nil && cfg.Workflow.Correction.OpenChangeRequest {
		return "  publish_repair:\n"
	}
	return "  staging:\n"
}

func gitLabNextJob(cfg model.Config, phase model.Phase) string {
	if phase == model.PhaseStaging && cfg.Workflow != nil && len(cfg.Workflow.Migration) > 0 {
		return "sam-harness-migration:\n"
	}
	return "sam-harness-production:\n"
}

func gitLabNextJobAfterArtifact(cfg model.Config) string {
	if repairEnabled(cfg) {
		return "sam-harness-repair-static:\n"
	}
	return "sam-harness-staging:\n"
}

func gitLabRepairEnd(cfg model.Config) string {
	if cfg.Workflow != nil && cfg.Workflow.Correction.OpenChangeRequest {
		return "sam-harness-publish-repair:\n"
	}
	return "sam-harness-staging:\n"
}

func repairEnabled(cfg model.Config) bool {
	return cfg.Workflow != nil && cfg.Workflow.Correction.Enabled
}

func scopedSecretBindings(cfg model.Config, provider, scope string) []model.CISecretBinding {
	var bindings []model.CISecretBinding
	for _, binding := range cfg.CI.SecretBindings[provider] {
		if binding.Scope == scope {
			bindings = append(bindings, binding)
		}
	}
	return bindings
}

func providerHasAgentBindings(cfg model.Config, provider string) bool {
	return len(scopedSecretBindings(cfg, provider, model.CISecretScopeReview)) > 0 || len(scopedSecretBindings(cfg, provider, model.CISecretScopeRepair)) > 0
}

func providerUsesExternalAgentControl(cfg model.Config, provider string) bool {
	control, ok := cfg.CI.AgentControlPlanes[provider]
	return ok && control.Mode == model.AgentControlPlaneModeExternal
}

func requireSection(report *Report, root, path, start, end string) string {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		return ""
	}
	content := string(data)
	startIndex := strings.Index(content, start)
	if startIndex < 0 {
		report.Errors = append(report.Errors, fmt.Sprintf("incomplete %s: missing section %q", path, strings.TrimSpace(start)))
		return ""
	}
	section := content[startIndex:]
	if end != "" {
		endIndex := strings.Index(section[len(start):], end)
		if endIndex < 0 {
			report.Errors = append(report.Errors, fmt.Sprintf("incomplete %s: section %q has no boundary %q", path, strings.TrimSpace(start), strings.TrimSpace(end)))
			return section
		}
		section = section[:len(start)+endIndex]
	}
	return section
}

func requireFragments(report *Report, path, content string, fragments ...string) {
	for _, fragment := range fragments {
		if !strings.Contains(content, fragment) {
			report.Errors = append(report.Errors, fmt.Sprintf("incomplete %s section: missing %q", path, fragment))
		}
	}
}

func forbidFragments(report *Report, path, content string, fragments ...string) {
	for _, fragment := range fragments {
		if strings.Contains(content, fragment) {
			report.Errors = append(report.Errors, fmt.Sprintf("unsafe %s section: contains %q", path, fragment))
		}
	}
}
