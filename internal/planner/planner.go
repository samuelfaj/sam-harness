package planner

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/render"
)

func LoadAnswers(path string) (model.Answers, error) {
	if path == "" {
		return model.Answers{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Answers{}, err
	}
	var answers model.Answers
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&answers); err != nil {
		return model.Answers{}, fmt.Errorf("parse answers: %w", err)
	}
	return answers, nil
}

func Create(scan model.ScanResult, requested model.Profile, answers model.Answers) (model.Plan, error) {
	if !requested.Valid(true) {
		return model.Plan{}, fmt.Errorf("invalid profile %q", requested)
	}
	if err := validateAnswers(answers); err != nil {
		return model.Plan{}, err
	}
	answers.Workflow = cloneWorkflowConfig(answers.Workflow)
	if answers.Workflow != nil && strings.TrimSpace(answers.Workflow.AdoptionPhase) == "" && strings.TrimSpace(answers.AdoptionPhase) != "" {
		answers.Workflow.AdoptionPhase = answers.AdoptionPhase
	}
	resolvedScan, commandQuestions, err := resolveCommands(scan, answers)
	if err != nil {
		return model.Plan{}, err
	}
	if len(answers.CIProviders) > 0 {
		resolvedScan.CIProviders = append([]string(nil), answers.CIProviders...)
		sort.Strings(resolvedScan.CIProviders)
	}
	selectedProviders := make(map[string]bool, len(resolvedScan.CIProviders))
	for _, provider := range resolvedScan.CIProviders {
		selectedProviders[provider] = true
	}
	for provider := range answers.CISecretBindings {
		if !selectedProviders[provider] {
			return model.Plan{}, fmt.Errorf("CI secret bindings do not match a selected provider: %s", provider)
		}
	}
	for provider := range answers.CISecretWaivers {
		if !selectedProviders[provider] {
			return model.Plan{}, fmt.Errorf("CI secret waiver does not match a selected provider: %s", provider)
		}
	}
	for provider := range answers.AgentSecretEnvironments {
		if !selectedProviders[provider] {
			return model.Plan{}, fmt.Errorf("CI agent secret environment does not match a selected provider: %s", provider)
		}
	}
	for provider := range answers.AgentControlPlanes {
		if !selectedProviders[provider] {
			return model.Plan{}, fmt.Errorf("CI agent control plane does not match a selected provider: %s", provider)
		}
	}
	recommended := Recommend(resolvedScan, answers)
	applied := requested
	if requested == model.ProfileAuto {
		applied = recommended
	}
	proposedDefaults := ProposedGuardDefaults(resolvedScan)
	if err := applyConfirmedGuardDefaults(answers.Workflow, proposedDefaults, answers.ConfirmGuardDefaults); err != nil {
		return model.Plan{}, err
	}
	browserCommand, accessibilityCommand := uxCommandsFromScan(resolvedScan)
	applyConfirmedUX(&answers, browserCommand, accessibilityCommand)
	reviewerCommand := applyRuntimeReviewers(answers.Workflow, answers)
	unresolved := answers.Missing(scan)
	if unresolved == nil {
		unresolved = []string{}
	}
	unresolved = append(unresolved, commandQuestions...)
	if resolvedScan.HasUI && profileRank(applied) >= profileRank(model.ProfileProduction) {
		if len(answers.BrowserCommand) == 0 && strings.TrimSpace(answers.BrowserWaiver) == "" {
			unresolved = append(unresolved, "design.browser")
		}
		if len(answers.AccessibilityCommand) == 0 && strings.TrimSpace(answers.AccessibilityWaiver) == "" {
			unresolved = append(unresolved, "design.accessibility")
		}
	}
	if profileRank(applied) >= profileRank(model.ProfileProduction) {
		if strings.TrimSpace(answers.ObservationWindow) == "" {
			unresolved = append(unresolved, "observation_window")
		}
		if strings.TrimSpace(answers.RollbackOwner) == "" {
			unresolved = append(unresolved, "rollback_owner")
		}
		if strings.TrimSpace(answers.ProductionEnvironment) == "" {
			unresolved = append(unresolved, "production_environment")
		}
	}
	workflowRequired := profileRank(applied) >= profileRank(model.ProfileProduction)
	workflowQuestions := missingWorkflowAnswers(answers.Workflow, workflowRequired || (answers.Workflow != nil && answers.Workflow.Enabled), workflowRequired)
	unresolved = append(unresolved, workflowQuestions...)
	phase := adoptionPhaseFrom(answers)
	blocking, deferred := splitWorkflowAnswers(unresolved, phase)
	unresolved = blocking
	if !hasWorkflowPrefix(unresolved) && answers.Workflow != nil && answers.Workflow.Enabled {
		if err := config.ValidateWorkflow(answers.Workflow, workflowRequired); err != nil {
			return model.Plan{}, fmt.Errorf("workflow: %w", err)
		}
	}
	trustedCommandQuestions := []string{}
	if answers.AllowCIChanges != nil && *answers.AllowCIChanges {
		trustedCommandQuestions = missingTrustedCommandAnswers(answers.Workflow, answers.CISecretBindings)
		unresolved = append(unresolved, trustedCommandQuestions...)
		if !hasWorkflowPrefix(unresolved) && len(trustedCommandQuestions) == 0 && answers.Workflow != nil && answers.Workflow.Enabled {
			if err := config.ValidateCITrustedCommandBoundaries(answers.Workflow, answers.CISecretBindings); err != nil {
				return model.Plan{}, fmt.Errorf("trusted command boundary: %w", err)
			}
		}
	}
	if applied == model.ProfileRegulated && len(answers.Approvers) < 2 {
		unresolved = append(unresolved, "separated_approvers")
	}
	if profileRank(applied) < profileRank(recommended) && strings.TrimSpace(answers.RiskAcceptance) == "" {
		unresolved = append(unresolved, "risk_acceptance")
	}
	if !allowsWrite(answers.AllowedActions) {
		unresolved = append(unresolved, "authority:write_repository")
	}
	if answers.Workflow != nil && answers.Workflow.Correction.OpenChangeRequest {
		for _, action := range []string{"network", "commit", "push"} {
			if !allowsAction(answers.AllowedActions, action) {
				unresolved = append(unresolved, "authority:"+action)
			}
		}
	}
	if answers.AllowCIChanges != nil && *answers.AllowCIChanges && hasNonGoStack(resolvedScan.Stacks) {
		for _, provider := range resolvedScan.CIProviders {
			if len(answers.CISetupCommands[provider]) == 0 && strings.TrimSpace(answers.CISetupWaivers[provider]) == "" {
				unresolved = append(unresolved, "ci_setup:"+provider)
			}
			if provider == "gitlab" && strings.TrimSpace(answers.GitLabImage) == "" {
				unresolved = append(unresolved, "gitlab_image")
			}
		}
	}
	if workflowRequired && answers.AllowCIChanges != nil && *answers.AllowCIChanges && answers.Workflow != nil && answers.Workflow.Enabled {
		for _, provider := range resolvedScan.CIProviders {
			if !config.CISecretDecisionComplete(answers.CISecretBindings[provider], answers.CISecretWaivers[provider], answers.Workflow.Correction.Enabled) {
				unresolved = append(unresolved, "ci_secrets:"+provider)
			}
		}
	}
	if agentRuntimeRequired(applied, answers) {
		unresolved = append(unresolved, missingAgentRuntimeAnswers(answers.CIAgentRuntime)...)
	}
	if workflowRequired && answers.AllowCIChanges != nil && *answers.AllowCIChanges {
		for _, provider := range resolvedScan.CIProviders {
			if len(answers.CISecretBindings[provider]) > 0 && strings.TrimSpace(answers.AgentSecretEnvironments[provider]) == "" {
				unresolved = append(unresolved, "ci_agent_secret_environment:"+provider)
			}
			if config.CIProviderAgentSecretsBound(answers.CISecretBindings[provider]) {
				if _, ok := answers.AgentControlPlanes[provider]; !ok {
					unresolved = append(unresolved, "ci_agent_control_plane:"+provider)
				}
			}
		}
	}
	sort.Strings(unresolved)
	sort.Strings(deferred)
	createdAt := time.Now().UTC()
	host, proposedReviewer := proposedReviewerHost(answers)
	if len(reviewerCommand) > 0 {
		proposedReviewer = reviewerCommand
		if answers.CIAgentRuntime != nil {
			host = answers.CIAgentRuntime.Host
		}
	} else if answers.ConfirmRuntimeReviewers != nil && *answers.ConfirmRuntimeReviewers {
		host = ""
		proposedReviewer = nil
	}
	if len(answers.BrowserCommand) > 0 || strings.TrimSpace(answers.BrowserWaiver) != "" {
		browserCommand = nil
	}
	if len(answers.AccessibilityCommand) > 0 || strings.TrimSpace(answers.AccessibilityWaiver) != "" {
		accessibilityCommand = nil
	}
	plan := model.Plan{
		PlanVersion:                  "1",
		CreatedAt:                    createdAt,
		ExpiresAt:                    createdAt.Add(30 * time.Minute),
		Root:                         scan.Root,
		Fingerprint:                  scan.Fingerprint,
		RequestedProfile:             requested,
		RecommendedProfile:           recommended,
		AppliedProfile:               applied,
		Answers:                      answers,
		Unresolved:                   unresolved,
		Deferred:                     deferred,
		ProposedGuardDefaults:        undecidedProposedGuards(answers.Workflow, proposedDefaults),
		ProposedReviewerHost:         host,
		ProposedReviewerCommand:      proposedReviewer,
		ProposedBrowserCommand:       browserCommand,
		ProposedAccessibilityCommand: accessibilityCommand,
	}
	if len(unresolved) == 0 {
		operations, err := render.Build(resolvedScan, applied, answers)
		if err != nil {
			return model.Plan{}, err
		}
		plan.Operations = operations
	}
	plan.ID = CalculateID(plan)
	return plan, nil
}

func Recommend(scan model.ScanResult, answers model.Answers) model.Profile {
	if answers.DataSensitivity == "regulated" || (boolValue(answers.IrreversibleActions) && answers.Criticality == "high") {
		return model.ProfileRegulated
	}
	if boolValue(answers.DeploysToProduction) || boolValue(answers.PersistentData) {
		return model.ProfileProduction
	}
	if answers.DeploysToProduction == nil && scan.HasDeployment {
		return model.ProfileProduction
	}
	if answers.PersistentData == nil && scan.HasPersistence {
		return model.ProfileProduction
	}
	return model.ProfileBaseline
}

func CalculateID(plan model.Plan) string {
	plan.ID = ""
	data, _ := json.Marshal(plan)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func Save(plan model.Plan, path string) (string, error) {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if path == "" {
		file, err := os.CreateTemp("", "sam-harness-plan-*.json")
		if err != nil {
			return "", err
		}
		path = file.Name()
		if err := file.Chmod(0o600); err != nil {
			file.Close()
			return "", err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			return "", err
		}
		if err := file.Close(); err != nil {
			return "", err
		}
		return path, nil
	}
	if err := validateOutputPath(plan.Root, path); err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func validateOutputPath(root, path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("plan output already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	parentReal, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("resolve plan output directory: %w", err)
	}
	target := filepath.Join(parentReal, filepath.Base(path))
	relative, err := filepath.Rel(rootReal, target)
	if err != nil {
		return err
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return fmt.Errorf("plan output must stay outside the repository: %s", path)
	}
	return nil
}

func Load(path string) (model.Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Plan{}, err
	}
	var plan model.Plan
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&plan); err != nil {
		return model.Plan{}, fmt.Errorf("parse plan: %w", err)
	}
	if plan.ID == "" || CalculateID(plan) != plan.ID {
		return model.Plan{}, fmt.Errorf("plan ID does not match its contents")
	}
	return plan, nil
}

func validateAnswers(answers model.Answers) error {
	if answers.Criticality != "" && answers.Criticality != "low" && answers.Criticality != "medium" && answers.Criticality != "high" {
		return fmt.Errorf("criticality must be low, medium, or high")
	}
	if answers.DataSensitivity != "" && answers.DataSensitivity != "public" && answers.DataSensitivity != "internal" && answers.DataSensitivity != "confidential" && answers.DataSensitivity != "regulated" {
		return fmt.Errorf("data_sensitivity must be public, internal, confidential, or regulated")
	}
	if err := answers.CIAgentRuntime.Validate(); err != nil {
		return fmt.Errorf("ci agent runtime: %w", err)
	}
	if answers.AllowedActions != nil {
		allowed := map[string]bool{"write_repository": true, "network": true, "commit": true, "push": true, "release": true, "deploy": true}
		seen := map[string]bool{}
		for _, action := range *answers.AllowedActions {
			if !allowed[action] {
				return fmt.Errorf("unknown allowed action %q", action)
			}
			if seen[action] {
				return fmt.Errorf("duplicate allowed action %q", action)
			}
			seen[action] = true
		}
	}
	seenProviders := map[string]bool{}
	for _, provider := range answers.CIProviders {
		if provider != "github" && provider != "gitlab" {
			return fmt.Errorf("unknown CI provider %q", provider)
		}
		if seenProviders[provider] {
			return fmt.Errorf("duplicate CI provider %q", provider)
		}
		seenProviders[provider] = true
	}
	for key, gates := range answers.CommandOverrides {
		if strings.TrimSpace(key) == "" || len(gates) == 0 {
			return fmt.Errorf("command override %q must define at least one gate", key)
		}
		if answers.CommandWaiver(key) {
			return fmt.Errorf("command override %q cannot also have a waiver", key)
		}
		for gate, command := range gates {
			if strings.TrimSpace(gate) == "" || len(command) == 0 {
				return fmt.Errorf("command override %q has an empty gate or command", key)
			}
			for _, argument := range command {
				if argument == "" {
					return fmt.Errorf("command override %q contains an empty argument", key)
				}
			}
		}
	}
	for key, reason := range answers.CommandWaivers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("command waiver %q requires a reason", key)
		}
	}
	for provider, commands := range answers.CISetupCommands {
		if provider != "github" && provider != "gitlab" {
			return fmt.Errorf("unknown CI setup provider %q", provider)
		}
		if strings.TrimSpace(answers.CISetupWaivers[provider]) != "" {
			return fmt.Errorf("CI setup provider %q cannot also have a waiver", provider)
		}
		if len(commands) == 0 {
			return fmt.Errorf("CI setup provider %q has no commands", provider)
		}
		for _, setup := range commands {
			if !safeRelative(setup.Workdir) || len(setup.Command) == 0 {
				return fmt.Errorf("CI setup provider %q has an invalid workdir or command", provider)
			}
			for _, argument := range setup.Command {
				if argument == "" {
					return fmt.Errorf("CI setup provider %q contains an empty argument", provider)
				}
			}
		}
	}
	for provider, reason := range answers.CISetupWaivers {
		if (provider != "github" && provider != "gitlab") || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("invalid CI setup waiver for %q", provider)
		}
	}
	if err := config.ValidateCISecretBindings(answers.CISecretBindings, answers.CISecretWaivers); err != nil {
		return fmt.Errorf("CI secret bindings: %w", err)
	}
	if err := config.ValidateCIAgentSecretEnvironments(answers.AgentSecretEnvironments); err != nil {
		return fmt.Errorf("CI agent secret environments: %w", err)
	}
	if err := config.ValidateCIAgentControlPlanes(answers.AgentControlPlanes); err != nil {
		return fmt.Errorf("CI agent control planes: %w", err)
	}
	if strings.TrimSpace(answers.AdoptionPhase) != "" {
		if _, err := model.NormalizeAdoptionPhase(answers.AdoptionPhase); err != nil {
			return err
		}
	}
	if answers.Workflow != nil && strings.TrimSpace(answers.Workflow.AdoptionPhase) != "" {
		if _, err := model.NormalizeAdoptionPhase(answers.Workflow.AdoptionPhase); err != nil {
			return err
		}
	}
	return nil
}

func missingWorkflowAnswers(workflow *model.WorkflowConfig, required, requireCorrection bool) []string {
	if !required {
		return nil
	}
	if workflow == nil {
		missing := []string{
			"workflow.enabled",
		}
		for _, category := range model.StaticGuardCategories {
			missing = append(missing, "workflow.static_guards."+category)
		}
		for _, category := range model.TestGuardCategories {
			missing = append(missing, "workflow.test_guards."+category)
		}
		return append(missing,
			"workflow.reviewers.architecture",
			"workflow.reviewers.architecture.filesystem_read_only",
			"workflow.reviewers.security",
			"workflow.reviewers.security.filesystem_read_only",
			"workflow.reviewers.correctness",
			"workflow.reviewers.correctness.filesystem_read_only",
			"workflow.reviewers.test_quality",
			"workflow.reviewers.test_quality.filesystem_read_only",
			"workflow.reviewers.business_rules",
			"workflow.reviewers.business_rules.filesystem_read_only",
			"workflow.reviewers.simplicity",
			"workflow.reviewers.simplicity.filesystem_read_only",
			"workflow.correction",
			"workflow.correction.filesystem_sandboxed",
			"workflow.artifact.build",
			"workflow.artifact.path",
			"workflow.artifact.sbom",
			"workflow.artifact.sbom_path",
			"workflow.artifact.provenance",
			"workflow.artifact.provenance_path",
			"workflow.deployment.staging",
			"workflow.deployment.production",
			"workflow.deployment.rollback",
			"workflow.deployment.health_checks",
			"workflow.deployment.observation_checks",
			"workflow.deployment.canary_percentages",
			"workflow.migration",
			"workflow.release_schedule.cron",
			"workflow.release_schedule.timezone",
		)
	}
	var missing []string
	if !workflow.Enabled {
		missing = append(missing, "workflow.enabled")
	}
	for _, category := range missingGuardCategories(workflow.StaticGuards, model.StaticGuardCategories) {
		missing = append(missing, "workflow.static_guards."+category)
	}
	for _, category := range missingGuardCategories(workflow.TestGuards, model.TestGuardCategories) {
		missing = append(missing, "workflow.test_guards."+category)
	}
	roles := make(map[model.ReviewerRole]bool, len(workflow.Reviewers))
	readOnlyRoles := make(map[model.ReviewerRole]bool, len(workflow.Reviewers))
	for _, reviewer := range workflow.Reviewers {
		if reviewer.Role.Valid() && len(reviewer.Command) > 0 && reviewer.TimeoutSeconds > 0 {
			roles[reviewer.Role] = true
		}
		if reviewer.Role.Valid() && reviewer.FilesystemReadOnly {
			readOnlyRoles[reviewer.Role] = true
		}
	}
	for _, role := range model.ReviewerRoles {
		if !roles[role] {
			missing = append(missing, "workflow.reviewers."+string(role))
		}
		if !readOnlyRoles[role] {
			missing = append(missing, "workflow.reviewers."+string(role)+".filesystem_read_only")
		}
	}
	if (requireCorrection && !workflow.Correction.Enabled) || (workflow.Correction.Enabled && (len(workflow.Correction.Command) == 0 || workflow.Correction.MaxAttempts <= 0 || workflow.Correction.MaxChangedFiles <= 0 || workflow.Correction.MaxChangedLines <= 0 || strings.TrimSpace(workflow.Correction.BranchPrefix) == "")) {
		missing = append(missing, "workflow.correction")
	}
	if workflow.Correction.Enabled && !workflow.Correction.FilesystemSandboxed {
		missing = append(missing, "workflow.correction.filesystem_sandboxed")
	}
	if commandSpecMissing(workflow.Artifact.Build) {
		missing = append(missing, "workflow.artifact.build")
	}
	if strings.TrimSpace(workflow.Artifact.ArtifactPath) == "" {
		missing = append(missing, "workflow.artifact.path")
	}
	if commandSpecMissing(workflow.Artifact.SBOM) {
		missing = append(missing, "workflow.artifact.sbom")
	}
	if strings.TrimSpace(workflow.Artifact.SBOMPath) == "" {
		missing = append(missing, "workflow.artifact.sbom_path")
	}
	if commandSpecMissing(workflow.Artifact.Provenance) {
		missing = append(missing, "workflow.artifact.provenance")
	}
	if strings.TrimSpace(workflow.Artifact.ProvenancePath) == "" {
		missing = append(missing, "workflow.artifact.provenance_path")
	}
	if commandSpecMissing(workflow.Deployment.Staging) {
		missing = append(missing, "workflow.deployment.staging")
	}
	if commandSpecMissing(workflow.Deployment.Production) {
		missing = append(missing, "workflow.deployment.production")
	}
	if commandSpecMissing(workflow.Deployment.Rollback) {
		missing = append(missing, "workflow.deployment.rollback")
	}
	if commandSpecsMissing(workflow.Deployment.HealthChecks) {
		missing = append(missing, "workflow.deployment.health_checks")
	}
	if commandSpecsMissing(workflow.Deployment.ObservationChecks) {
		missing = append(missing, "workflow.deployment.observation_checks")
	}
	if len(workflow.Deployment.CanaryPercentages) == 0 {
		missing = append(missing, "workflow.deployment.canary_percentages")
	}
	if commandSpecsMissing(workflow.Migration) {
		missing = append(missing, "workflow.migration")
	}
	if strings.TrimSpace(workflow.ReleaseSchedule.Cron) == "" {
		missing = append(missing, "workflow.release_schedule.cron")
	}
	if strings.TrimSpace(workflow.ReleaseSchedule.Timezone) == "" {
		missing = append(missing, "workflow.release_schedule.timezone")
	}
	return missing
}

func missingTrustedCommandAnswers(workflow *model.WorkflowConfig, bindings map[string][]model.CISecretBinding) []string {
	if workflow == nil || !workflow.Enabled {
		return nil
	}
	missing := []string{}
	if config.CISecretScopeBound(bindings, model.CISecretScopeReview) {
		for _, reviewer := range workflow.Reviewers {
			if !reviewer.TrustedExternalCommand {
				missing = append(missing, "workflow.reviewers."+string(reviewer.Role)+".trusted_external_command")
			}
		}
	}
	if workflow.Correction.Enabled && config.CISecretScopeBound(bindings, model.CISecretScopeRepair) && !workflow.Correction.TrustedExternalCommand {
		missing = append(missing, "workflow.correction.trusted_external_command")
	}
	return missing
}

func missingGuardCategories(guards model.GuardSet, categories []string) []string {
	var missing []string
	for _, category := range categories {
		command, hasCommand := guards.Commands[category]
		waiver, hasWaiver := guards.Waivers[category]
		if hasCommand == hasWaiver || (hasCommand && commandSpecMissing(command)) || (hasWaiver && strings.TrimSpace(waiver) == "") {
			missing = append(missing, category)
		}
	}
	return missing
}

func commandSpecsMissing(commands []model.CommandSpec) bool {
	if len(commands) == 0 {
		return true
	}
	for _, command := range commands {
		if commandSpecMissing(command) {
			return true
		}
	}
	return false
}

func commandSpecMissing(command model.CommandSpec) bool {
	return strings.TrimSpace(command.Name) == "" || strings.TrimSpace(command.Workdir) == "" || len(command.Command) == 0 || !command.Required || command.TimeoutSeconds <= 0
}

func safeRelative(path string) bool {
	if strings.TrimSpace(path) == "" || filepath.IsAbs(path) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func hasNonGoStack(stacks []model.Stack) bool {
	for _, stack := range stacks {
		if stack.Kind != "go" {
			return true
		}
	}
	return false
}

func resolveCommands(scan model.ScanResult, answers model.Answers) (model.ScanResult, []string, error) {
	resolved := scan
	resolved.Stacks = make([]model.Stack, len(scan.Stacks))
	known := map[string]bool{}
	var unresolved []string
	for index, stack := range scan.Stacks {
		key := stack.Kind + ":" + stack.Path
		known[key] = true
		copyStack := stack
		copyStack.Commands = cloneCommands(stack.Commands)
		if override, ok := answers.CommandOverrides[key]; ok {
			copyStack.Commands = cloneCommands(override)
		}
		if len(copyStack.Commands) == 0 && !answers.CommandWaiver(key) {
			unresolved = append(unresolved, "commands:"+key)
		}
		resolved.Stacks[index] = copyStack
	}
	for key := range answers.CommandOverrides {
		if !known[key] {
			return model.ScanResult{}, nil, fmt.Errorf("command override does not match a detected stack: %s", key)
		}
	}
	for key := range answers.CommandWaivers {
		if !known[key] {
			return model.ScanResult{}, nil, fmt.Errorf("command waiver does not match a detected stack: %s", key)
		}
	}
	sort.Strings(unresolved)
	return resolved, unresolved, nil
}

func hasWorkflowPrefix(values []string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, "workflow.") {
			return true
		}
	}
	return false
}

func cloneCommands(commands map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(commands))
	for gate, command := range commands {
		cloned[gate] = append([]string(nil), command...)
	}
	return cloned
}

func agentRuntimeRequired(profile model.Profile, answers model.Answers) bool {
	if answers.AllowCIChanges == nil || !*answers.AllowCIChanges {
		return false
	}
	if profileRank(profile) >= profileRank(model.ProfileProduction) {
		return true
	}
	return answers.Workflow != nil && answers.Workflow.Enabled
}

func missingAgentRuntimeAnswers(runtime *model.CIAgentRuntime) []string {
	var missing []string
	if runtime == nil || !runtime.HostComplete() {
		missing = append(missing, "ci_agent_host")
	}
	if runtime == nil || !runtime.LoginComplete() {
		missing = append(missing, "ci_agent_login")
	}
	return missing
}

func profileRank(profile model.Profile) int {
	switch profile {
	case model.ProfileRegulated:
		return 3
	case model.ProfileProduction:
		return 2
	default:
		return 1
	}
}

func boolValue(value *bool) bool {
	return value != nil && *value
}

func allowsWrite(actions *[]string) bool {
	return allowsAction(actions, "write_repository")
}

func allowsAction(actions *[]string, target string) bool {
	if actions == nil {
		return false
	}
	for _, action := range *actions {
		if action == target {
			return true
		}
	}
	return false
}
