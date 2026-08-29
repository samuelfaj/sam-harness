package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/samuelfaj/sam-harness/internal/model"
	configschema "github.com/samuelfaj/sam-harness/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

var (
	ciSecretNamePattern        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	ciAgentEnvironmentPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	ciRequiredCheckPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`)
	ciExternalProjectPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(?:/[A-Za-z0-9][A-Za-z0-9._-]*)+$`)
	pinnedNPXPackagePattern    = regexp.MustCompile(`^(?:@[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*|[A-Za-z0-9][A-Za-z0-9._-]*)@[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)
	trustedCommandInterpreters = map[string]bool{
		"bash": true, "bun": true, "dash": true, "deno": true,
		"env": true, "go": true, "ksh": true, "node": true, "nodejs": true,
		"npm": true, "osascript": true, "perl": true,
		"php": true, "pnpm": true, "powershell": true, "pwsh": true,
		"python": true, "python3": true, "ruby": true, "sh": true,
		"uv": true, "xargs": true, "yarn": true, "zsh": true,
	}
	trustedCommandFileExtensions = map[string]bool{
		".bash": true, ".cjs": true, ".js": true, ".json": true,
		".mjs": true, ".ps1": true, ".py": true, ".rb": true,
		".schema": true, ".sh": true, ".toml": true, ".ts": true,
		".tsx": true, ".xml": true, ".yaml": true, ".yml": true,
		".zsh": true,
	}
	forbiddenNPXOptions = map[string]bool{
		"--call": true, "--package": true, "--shell": true,
		"-c": true, "-p": true,
	}
)

func Load(path string) (model.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Config{}, err
	}
	return Parse(data)
}

func Parse(data []byte) (model.Config, error) {
	var raw any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return model.Config{}, fmt.Errorf("parse yaml: %w", err)
	}
	jsonData, err := json.Marshal(raw)
	if err != nil {
		return model.Config{}, fmt.Errorf("convert yaml to json: %w", err)
	}
	if err := validateJSON(jsonData); err != nil {
		return model.Config{}, err
	}
	var cfg model.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return model.Config{}, fmt.Errorf("decode config: %w", err)
	}
	defaultGatePhases(cfg.Gates)
	if err := validateSemantics(cfg); err != nil {
		return model.Config{}, err
	}
	return cfg, nil
}

func Marshal(cfg model.Config) ([]byte, error) {
	cfg.Gates = append([]model.Gate(nil), cfg.Gates...)
	defaultGatePhases(cfg.Gates)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := Parse(data); err != nil {
		return nil, err
	}
	return data, nil
}

func validateJSON(data []byte) error {
	compiler := jsonschema.NewCompiler()
	var schemaDocument any
	if err := json.Unmarshal(configschema.ConfigJSON, &schemaDocument); err != nil {
		return fmt.Errorf("decode config schema: %w", err)
	}
	if err := compiler.AddResource("config.schema.json", schemaDocument); err != nil {
		return fmt.Errorf("load config schema: %w", err)
	}
	compiled, err := compiler.Compile("config.schema.json")
	if err != nil {
		return fmt.Errorf("compile config schema: %w", err)
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return fmt.Errorf("decode config json: %w", err)
	}
	if err := compiled.Validate(value); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	return nil
}

func validateSemantics(cfg model.Config) error {
	stackCommands := map[string]int{}
	for _, stack := range cfg.Stacks {
		if !safeRelativePath(stack.Path) {
			return fmt.Errorf("validate config: unsafe stack path %q", stack.Path)
		}
		stackCommands[stack.Kind+":"+stack.Path] = len(stack.Commands)
	}
	for _, gate := range cfg.Gates {
		if !gate.Phase.Valid() {
			return fmt.Errorf("validate config: invalid gate phase %q", gate.Phase)
		}
		if !safeRelativePath(gate.Workdir) {
			return fmt.Errorf("validate config: unsafe gate workdir %q", gate.Workdir)
		}
	}
	if !safeRelativePath(cfg.Evidence.ReceiptDirectory) {
		return fmt.Errorf("validate config: unsafe receipt directory %q", cfg.Evidence.ReceiptDirectory)
	}
	if cfg.CI.Managed && len(cfg.CI.Providers) == 0 {
		return fmt.Errorf("validate config: managed CI requires a provider")
	}
	providerSet := map[string]bool{}
	for _, provider := range cfg.CI.Providers {
		providerSet[provider] = true
	}
	for provider, setups := range cfg.CI.SetupCommands {
		if !providerSet[provider] || len(setups) == 0 || strings.TrimSpace(cfg.CI.SetupWaivers[provider]) != "" {
			return fmt.Errorf("validate config: invalid CI setup for %s", provider)
		}
		for _, setup := range setups {
			if !safeRelativePath(setup.Workdir) || len(setup.Command) == 0 {
				return fmt.Errorf("validate config: invalid CI setup command for %s", provider)
			}
		}
	}
	for provider, reason := range cfg.CI.SetupWaivers {
		if !providerSet[provider] || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("validate config: invalid CI setup waiver for %s", provider)
		}
	}
	if err := validateCISecretDecisions(cfg.CI, providerSet); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}
	if cfg.CI.Managed && hasNonGoStack(cfg.Stacks) {
		for _, provider := range cfg.CI.Providers {
			if len(cfg.CI.SetupCommands[provider]) == 0 && strings.TrimSpace(cfg.CI.SetupWaivers[provider]) == "" {
				return fmt.Errorf("validate config: non-Go CI requires setup commands or a waiver for %s", provider)
			}
			if provider == "gitlab" && strings.TrimSpace(cfg.CI.GitLabImage) == "" {
				return fmt.Errorf("validate config: non-Go GitLab CI requires an image")
			}
		}
	}
	if cfg.Design.Applicable && strings.TrimSpace(cfg.Design.SourceOfTruth) == "" {
		return fmt.Errorf("validate config: a user interface requires a design source of truth")
	}
	for key, reason := range cfg.Governance.CommandWaivers {
		commandCount, ok := stackCommands[key]
		if !ok || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("validate config: command waiver does not match a stack: %s", key)
		}
		if commandCount != 0 {
			return fmt.Errorf("validate config: command waiver %s conflicts with configured gates", key)
		}
	}
	for key, commandCount := range stackCommands {
		if commandCount == 0 && strings.TrimSpace(cfg.Governance.CommandWaivers[key]) == "" {
			return fmt.Errorf("validate config: stack %s has no commands or recorded waiver", key)
		}
	}
	if cfg.Migration.Required && (!cfg.Migration.ReconciliationGate || !cfg.Migration.RestoreTest) {
		return fmt.Errorf("validate config: a required migration needs reconciliation and restore gates")
	}
	if cfg.Profile == model.ProfileProduction || cfg.Profile == model.ProfileRegulated {
		if !cfg.Release.ImmutableArtifact || !cfg.Release.SBOM || !cfg.Release.Provenance || !cfg.Release.PromotionRequired || !cfg.CI.BranchProtectionRequired {
			return fmt.Errorf("validate config: %s requires immutable artifacts, SBOM, provenance, promotion, and branch protection", cfg.Profile)
		}
		phase := workflowAdoptionPhase(cfg.Workflow)
		if model.AdoptionPhaseRank(phase) >= model.AdoptionPhaseRank(model.AdoptionPhaseDelivery) {
			if strings.TrimSpace(cfg.Release.RollbackOwner) == "" || strings.TrimSpace(cfg.Release.ObservationWindow) == "" || strings.TrimSpace(cfg.Release.ProductionEnvironment) == "" {
				return fmt.Errorf("validate config: %s requires rollback owner, observation window, and production environment", cfg.Profile)
			}
		}
	}
	requireWorkflow := (cfg.Profile == model.ProfileProduction || cfg.Profile == model.ProfileRegulated) && cfg.HarnessVersion == model.HarnessVersion
	if cfg.Workflow != nil || requireWorkflow {
		if err := ValidateWorkflow(cfg.Workflow, requireWorkflow); err != nil {
			return fmt.Errorf("validate config: %s workflow: %w", cfg.Profile, err)
		}
		if cfg.Workflow != nil && cfg.Workflow.Correction.OpenChangeRequest && (!cfg.Authority.Network || !cfg.Authority.Commit || !cfg.Authority.Push) {
			return fmt.Errorf("validate config: opening a correction change request requires network, commit, and push authority")
		}
	}
	if cfg.CI.Managed && cfg.Workflow != nil && cfg.Workflow.Enabled {
		if err := ValidateCITrustedCommandBoundaries(cfg.Workflow, cfg.CI.SecretBindings); err != nil {
			return fmt.Errorf("validate config: trusted command boundary: %w", err)
		}
	}
	if (cfg.Profile == model.ProfileProduction || cfg.Profile == model.ProfileRegulated) && cfg.CI.Managed {
		for _, provider := range cfg.CI.Providers {
			if len(cfg.CI.SecretBindings[provider]) > 0 && strings.TrimSpace(cfg.CI.AgentSecretEnvironments[provider]) == "" {
				return fmt.Errorf("validate config: managed %s secret bindings require a protected agent environment for %s", cfg.Profile, provider)
			}
			if CIProviderAgentSecretsBound(cfg.CI.SecretBindings[provider]) {
				if _, ok := cfg.CI.AgentControlPlanes[provider]; !ok {
					return fmt.Errorf("validate config: managed %s agent secrets require a trusted control plane for %s", cfg.Profile, provider)
				}
			}
		}
	}
	if (cfg.Profile == model.ProfileProduction || cfg.Profile == model.ProfileRegulated) && cfg.CI.Managed && cfg.Workflow != nil && cfg.Workflow.Enabled {
		for _, provider := range cfg.CI.Providers {
			if !CISecretDecisionComplete(cfg.CI.SecretBindings[provider], cfg.CI.SecretWaivers[provider], cfg.Workflow.Correction.Enabled) {
				return fmt.Errorf("validate config: managed %s workflow requires CI secret bindings or an explicit waiver for %s", cfg.Profile, provider)
			}
		}
	}
	if cfg.Profile == model.ProfileRegulated && len(cfg.Governance.Approvers) < 2 {
		return fmt.Errorf("validate config: regulated profile requires at least two approvers")
	}
	return nil
}

func validateCISecretDecisions(ci model.CIConfig, providers map[string]bool) error {
	if err := ValidateCISecretBindings(ci.SecretBindings, ci.SecretWaivers); err != nil {
		return err
	}
	if err := ValidateCIAgentSecretEnvironments(ci.AgentSecretEnvironments); err != nil {
		return err
	}
	if err := ValidateCIAgentControlPlanes(ci.AgentControlPlanes); err != nil {
		return err
	}
	if err := ValidateCIAgentRuntime(ci.AgentRuntime); err != nil {
		return err
	}
	for provider := range ci.SecretBindings {
		if !providers[provider] {
			return fmt.Errorf("CI secret bindings do not match provider %q", provider)
		}
	}
	for provider := range ci.SecretWaivers {
		if !providers[provider] {
			return fmt.Errorf("CI secret waiver does not match provider %q", provider)
		}
	}
	for provider := range ci.AgentSecretEnvironments {
		if !providers[provider] {
			return fmt.Errorf("CI agent secret environment does not match provider %q", provider)
		}
	}
	for provider := range ci.AgentControlPlanes {
		if !providers[provider] {
			return fmt.Errorf("CI agent control plane does not match provider %q", provider)
		}
	}
	return nil
}

// ValidateCIAgentSecretEnvironments validates protected provider environment
// names without inspecting or accepting secret values.
func ValidateCIAgentSecretEnvironments(environments map[string]string) error {
	for provider, environment := range environments {
		if provider != "github" && provider != "gitlab" {
			return fmt.Errorf("unknown CI agent secret environment provider %q", provider)
		}
		if !ciAgentEnvironmentPattern.MatchString(environment) {
			return fmt.Errorf("CI agent secret provider %q has unsafe environment name %q", provider, environment)
		}
	}
	return nil
}

// ValidateCIAgentRuntime validates the chosen CI agent host and login
// identifiers. Nil is allowed; present values must be complete and must not
// contain credential material.
func ValidateCIAgentRuntime(runtime *model.CIAgentRuntime) error {
	if runtime == nil {
		return nil
	}
	if err := runtime.Validate(); err != nil {
		return fmt.Errorf("CI agent runtime: %w", err)
	}
	if runtime.Host != "" && !runtime.HostComplete() {
		return fmt.Errorf("CI agent runtime host is incomplete")
	}
	if runtime.LoginMethod != "" && !runtime.LoginComplete() {
		return fmt.Errorf("CI agent runtime login is incomplete")
	}
	return nil
}

// ValidateCIAgentControlPlanes validates provider-specific trusted control
// plane metadata. The contract contains credential names, never values.
func ValidateCIAgentControlPlanes(controlPlanes map[string]model.AgentControlPlane) error {
	for provider, control := range controlPlanes {
		if !ciRequiredCheckPattern.MatchString(control.RequiredCheck) {
			return fmt.Errorf("CI agent control plane provider %q has unsafe required check %q", provider, control.RequiredCheck)
		}
		switch provider {
		case "github":
			if control.Mode != model.AgentControlPlaneModeGitHubApp {
				return fmt.Errorf("CI agent control plane provider %q requires mode %q", provider, model.AgentControlPlaneModeGitHubApp)
			}
			if !ciSecretNamePattern.MatchString(control.AppIDSecret) || !ciSecretNamePattern.MatchString(control.AppPrivateKeySecret) {
				return fmt.Errorf("CI agent control plane provider %q requires safe GitHub App secret names", provider)
			}
			if control.ExternalProject != "" {
				return fmt.Errorf("CI agent control plane provider %q cannot set external_project", provider)
			}
		case "gitlab":
			if control.Mode != model.AgentControlPlaneModeExternal {
				return fmt.Errorf("CI agent control plane provider %q requires mode %q", provider, model.AgentControlPlaneModeExternal)
			}
			if !ciExternalProjectPattern.MatchString(control.ExternalProject) {
				return fmt.Errorf("CI agent control plane provider %q has unsafe external project %q", provider, control.ExternalProject)
			}
			if control.AppIDSecret != "" || control.AppPrivateKeySecret != "" {
				return fmt.Errorf("CI agent control plane provider %q cannot set GitHub App secrets", provider)
			}
		default:
			return fmt.Errorf("unknown CI agent control plane provider %q", provider)
		}
	}
	return nil
}

// CIProviderAgentSecretsBound reports whether one provider binds a secret to
// the review or repair agent scopes that require an out-of-band control plane.
func CIProviderAgentSecretsBound(bindings []model.CISecretBinding) bool {
	for _, binding := range bindings {
		if binding.Scope == model.CISecretScopeReview || binding.Scope == model.CISecretScopeRepair {
			return true
		}
	}
	return false
}

// CISecretScopeBound reports whether any provider binds a secret to scope.
func CISecretScopeBound(bindings map[string][]model.CISecretBinding, scope string) bool {
	for _, providerBindings := range bindings {
		for _, binding := range providerBindings {
			if binding.Scope == scope {
				return true
			}
		}
	}
	return false
}

// ValidateCITrustedCommandBoundaries prevents provider-bound agent secrets
// from reaching executables or helper files controlled by the target checkout.
// Trusted helper paths remain relative here; runtime resolves them against the
// directory containing the trusted canonical configuration.
func ValidateCITrustedCommandBoundaries(workflow *model.WorkflowConfig, bindings map[string][]model.CISecretBinding) error {
	if workflow == nil || !workflow.Enabled {
		return nil
	}
	if CISecretScopeBound(bindings, model.CISecretScopeReview) {
		for _, reviewer := range workflow.Reviewers {
			if !reviewer.TrustedExternalCommand {
				return fmt.Errorf("reviewer %s requires trusted_external_command attestation", reviewer.Role)
			}
			if err := validateTrustedExternalCommand(reviewer.Command, reviewer.TrustedConfigArguments); err != nil {
				return fmt.Errorf("reviewer %s: %w", reviewer.Role, err)
			}
		}
	}
	if workflow.Correction.Enabled && CISecretScopeBound(bindings, model.CISecretScopeRepair) {
		if !workflow.Correction.TrustedExternalCommand {
			return fmt.Errorf("correction requires trusted_external_command attestation")
		}
		if err := validateTrustedExternalCommand(workflow.Correction.Command, workflow.Correction.TrustedConfigArguments); err != nil {
			return fmt.Errorf("correction: %w", err)
		}
	}
	return nil
}

// ValidateCISecretBindings validates secret identifiers without accepting or
// inspecting secret values. Provider membership is validated by the caller.
func ValidateCISecretBindings(bindings map[string][]model.CISecretBinding, waivers map[string]string) error {
	for provider, providerBindings := range bindings {
		if provider != "github" && provider != "gitlab" {
			return fmt.Errorf("unknown CI secret provider %q", provider)
		}
		seen := make(map[string]bool, len(providerBindings))
		for _, binding := range providerBindings {
			if !validCISecretScope(binding.Scope) {
				return fmt.Errorf("CI secret provider %q has invalid scope %q", provider, binding.Scope)
			}
			if !ciSecretNamePattern.MatchString(binding.Environment) {
				return fmt.Errorf("CI secret provider %q has unsafe environment name %q", provider, binding.Environment)
			}
			if !ciSecretNamePattern.MatchString(binding.Secret) {
				return fmt.Errorf("CI secret provider %q has unsafe secret name %q", provider, binding.Secret)
			}
			key := binding.Scope + "\x00" + binding.Environment
			if seen[key] {
				return fmt.Errorf("CI secret provider %q repeats environment %s in scope %s", provider, binding.Environment, binding.Scope)
			}
			seen[key] = true
		}
	}
	for provider, waiver := range waivers {
		if (provider != "github" && provider != "gitlab") || strings.TrimSpace(waiver) == "" {
			return fmt.Errorf("invalid CI secret waiver for %q", provider)
		}
	}
	return nil
}

// CISecretDecisionComplete reports whether agentic review and repair
// authentication is explicit for one CI provider.
func CISecretDecisionComplete(bindings []model.CISecretBinding, waiver string, correctionEnabled bool) bool {
	if strings.TrimSpace(waiver) != "" {
		return true
	}
	hasReview := false
	hasRepair := false
	for _, binding := range bindings {
		switch binding.Scope {
		case model.CISecretScopeReview:
			hasReview = true
		case model.CISecretScopeRepair:
			hasRepair = true
		}
	}
	return hasReview && (!correctionEnabled || hasRepair)
}

func validCISecretScope(scope string) bool {
	switch scope {
	case model.CISecretScopeStatic,
		model.CISecretScopeTest,
		model.CISecretScopeReview,
		model.CISecretScopeRepair,
		model.CISecretScopeArtifact,
		model.CISecretScopeStaging,
		model.CISecretScopeProduction,
		model.CISecretScopeObserve,
		model.CISecretScopeRollback,
		model.CISecretScopeMigration:
		return true
	default:
		return false
	}
}

// ValidateWorkflow enforces the executable workflow contract shared by
// configuration loading and planning. A required workflow must be present and
// enabled; whenever one is present, its configured controls are validated.
func ValidateWorkflow(workflow *model.WorkflowConfig, required bool) error {
	if workflow == nil {
		if required {
			return fmt.Errorf("workflow is required")
		}
		return nil
	}
	if required && !workflow.Enabled {
		return fmt.Errorf("workflow must be enabled")
	}
	if required && !workflow.Correction.Enabled {
		return fmt.Errorf("correction must be enabled")
	}
	if strings.TrimSpace(workflow.AdoptionPhase) != "" {
		if _, err := model.NormalizeAdoptionPhase(workflow.AdoptionPhase); err != nil {
			return err
		}
	}
	if err := validateGuardSet("static", workflow.StaticGuards, model.StaticGuardCategories); err != nil {
		return err
	}
	if err := validateGuardSet("test", workflow.TestGuards, model.TestGuardCategories); err != nil {
		return err
	}

	seenRoles := make(map[model.ReviewerRole]bool, len(workflow.Reviewers))
	for _, reviewer := range workflow.Reviewers {
		if !reviewer.Role.Valid() {
			return fmt.Errorf("invalid reviewer role %q", reviewer.Role)
		}
		if seenRoles[reviewer.Role] {
			return fmt.Errorf("duplicate reviewer role %q", reviewer.Role)
		}
		seenRoles[reviewer.Role] = true
		if err := validateCommand(reviewer.Command); err != nil {
			return fmt.Errorf("reviewer %s command: %w", reviewer.Role, err)
		}
		if err := validateTrustedConfigArguments(reviewer.Command, reviewer.TrustedConfigArguments); err != nil {
			return fmt.Errorf("reviewer %s trusted_config_arguments: %w", reviewer.Role, err)
		}
		if reviewer.TimeoutSeconds <= 0 {
			return fmt.Errorf("reviewer %s timeout_seconds must be positive", reviewer.Role)
		}
		if !reviewer.FilesystemReadOnly {
			return fmt.Errorf("reviewer %s requires filesystem_read_only attestation", reviewer.Role)
		}
	}
	for _, role := range model.ReviewerRoles {
		if !seenRoles[role] {
			return fmt.Errorf("reviewer role %s is required", role)
		}
	}

	if workflow.Correction.Enabled {
		if !workflow.Correction.FilesystemSandboxed {
			return fmt.Errorf("enabled correction requires filesystem_sandboxed attestation")
		}
		if err := validateCommand(workflow.Correction.Command); err != nil {
			return fmt.Errorf("correction command: %w", err)
		}
		if err := validateTrustedConfigArguments(workflow.Correction.Command, workflow.Correction.TrustedConfigArguments); err != nil {
			return fmt.Errorf("correction trusted_config_arguments: %w", err)
		}
		if workflow.Correction.MaxAttempts <= 0 || workflow.Correction.MaxChangedFiles <= 0 || workflow.Correction.MaxChangedLines <= 0 {
			return fmt.Errorf("correction budgets must be positive")
		}
		if strings.TrimSpace(workflow.Correction.BranchPrefix) == "" {
			return fmt.Errorf("correction branch_prefix is required")
		}
	} else if workflow.Correction.OpenChangeRequest {
		return fmt.Errorf("correction must be enabled before opening a change request")
	}

	phase := workflowAdoptionPhase(workflow)
	rank := model.AdoptionPhaseRank(phase)
	requireArtifact := rank >= model.AdoptionPhaseRank(model.AdoptionPhaseArtifact) || artifactPresent(workflow.Artifact)
	requireDelivery := rank >= model.AdoptionPhaseRank(model.AdoptionPhaseDelivery) || deliveryPresent(workflow)

	if requireArtifact {
		commands := []struct {
			control string
			spec    model.CommandSpec
		}{
			{"artifact build", workflow.Artifact.Build},
			{"artifact SBOM", workflow.Artifact.SBOM},
			{"artifact provenance", workflow.Artifact.Provenance},
		}
		for _, command := range commands {
			if err := validateCommandSpec(command.spec, workflow.Enabled || required); err != nil {
				return fmt.Errorf("%s: %w", command.control, err)
			}
		}
		for _, path := range []struct {
			control string
			path    string
		}{
			{"artifact", workflow.Artifact.ArtifactPath},
			{"SBOM", workflow.Artifact.SBOMPath},
			{"provenance", workflow.Artifact.ProvenancePath},
		} {
			if !safeRelativePath(path.path) {
				return fmt.Errorf("unsafe %s path %q", path.control, path.path)
			}
		}
	}

	if requireDelivery {
		commands := []struct {
			control string
			spec    model.CommandSpec
		}{
			{"deployment staging", workflow.Deployment.Staging},
			{"deployment production", workflow.Deployment.Production},
			{"deployment rollback", workflow.Deployment.Rollback},
		}
		for _, healthCheck := range workflow.Deployment.HealthChecks {
			commands = append(commands, struct {
				control string
				spec    model.CommandSpec
			}{"deployment health check", healthCheck})
		}
		for _, observationCheck := range workflow.Deployment.ObservationChecks {
			commands = append(commands, struct {
				control string
				spec    model.CommandSpec
			}{"deployment observation check", observationCheck})
		}
		for _, migration := range workflow.Migration {
			commands = append(commands, struct {
				control string
				spec    model.CommandSpec
			}{"migration", migration})
		}
		for _, command := range commands {
			if err := validateCommandSpec(command.spec, workflow.Enabled || required); err != nil {
				return fmt.Errorf("%s: %w", command.control, err)
			}
		}
		if len(workflow.Deployment.HealthChecks) == 0 {
			return fmt.Errorf("at least one deployment health check is required")
		}
		if len(workflow.Deployment.ObservationChecks) == 0 {
			return fmt.Errorf("at least one deployment observation check is required")
		}
		if len(workflow.Migration) == 0 {
			return fmt.Errorf("at least one migration command is required")
		}
		if err := validateCanaryPercentages(workflow.Deployment.CanaryPercentages); err != nil {
			return err
		}
		if len(strings.Fields(workflow.ReleaseSchedule.Cron)) != 5 {
			return fmt.Errorf("release schedule cron must contain five fields")
		}
		if _, err := time.LoadLocation(workflow.ReleaseSchedule.Timezone); err != nil {
			return fmt.Errorf("release schedule timezone %q is not an IANA timezone: %w", workflow.ReleaseSchedule.Timezone, err)
		}
	}
	return nil
}

func workflowAdoptionPhase(workflow *model.WorkflowConfig) string {
	if workflow == nil {
		return model.AdoptionPhaseDelivery
	}
	normalized, err := model.NormalizeAdoptionPhase(workflow.AdoptionPhase)
	if err != nil {
		return model.AdoptionPhaseDelivery
	}
	return normalized
}

func artifactPresent(artifact model.ArtifactWorkflow) bool {
	return strings.TrimSpace(artifact.ArtifactPath) != "" || strings.TrimSpace(artifact.SBOMPath) != "" || strings.TrimSpace(artifact.ProvenancePath) != "" || len(artifact.Build.Command) > 0 || len(artifact.SBOM.Command) > 0 || len(artifact.Provenance.Command) > 0
}

func deliveryPresent(workflow *model.WorkflowConfig) bool {
	if workflow == nil {
		return false
	}
	return len(workflow.Deployment.Staging.Command) > 0 || len(workflow.Deployment.Production.Command) > 0 || len(workflow.Deployment.Rollback.Command) > 0 || len(workflow.Deployment.HealthChecks) > 0 || len(workflow.Deployment.ObservationChecks) > 0 || len(workflow.Migration) > 0 || strings.TrimSpace(workflow.ReleaseSchedule.Cron) != "" || strings.TrimSpace(workflow.ReleaseSchedule.Timezone) != ""
}

func validateGuardSet(phase string, guards model.GuardSet, categories []string) error {
	allowed := make(map[string]bool, len(categories))
	for _, category := range categories {
		allowed[category] = true
	}
	for category := range guards.Commands {
		if !allowed[category] {
			return fmt.Errorf("%s guard has unknown category %q", phase, category)
		}
	}
	for category := range guards.Waivers {
		if !allowed[category] {
			return fmt.Errorf("%s guard waiver has unknown category %q", phase, category)
		}
	}
	for _, category := range categories {
		command, hasCommand := guards.Commands[category]
		waiver, hasWaiver := guards.Waivers[category]
		if hasCommand == hasWaiver {
			return fmt.Errorf("%s guard %s requires exactly one command or waiver", phase, category)
		}
		if hasWaiver {
			if strings.TrimSpace(waiver) == "" {
				return fmt.Errorf("%s guard %s waiver must be non-empty", phase, category)
			}
			continue
		}
		if err := validateCommandSpec(command, true); err != nil {
			return fmt.Errorf("%s guard %s: %w", phase, category, err)
		}
	}
	return nil
}

func defaultGatePhases(gates []model.Gate) {
	for index := range gates {
		if gates[index].Phase == "" {
			gates[index].Phase = model.PhaseStatic
		}
	}
}

func validateCommandSpec(spec model.CommandSpec, require bool) error {
	if strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !safeRelativePath(spec.Workdir) {
		return fmt.Errorf("unsafe workdir %q", spec.Workdir)
	}
	if err := validateCommand(spec.Command); err != nil {
		return err
	}
	if spec.TimeoutSeconds <= 0 {
		return fmt.Errorf("timeout_seconds must be positive")
	}
	if require && !spec.Required {
		return fmt.Errorf("must be required")
	}
	return nil
}

func validateCommand(command []string) error {
	if len(command) == 0 {
		return fmt.Errorf("must contain argv")
	}
	for _, argument := range command {
		if argument == "" {
			return fmt.Errorf("contains an empty argument")
		}
	}
	return nil
}

func validateTrustedConfigArguments(command []string, indexes []int) error {
	seen := make(map[int]bool, len(indexes))
	for _, index := range indexes {
		if index <= 0 || index >= len(command) {
			return fmt.Errorf("index %d must be between 1 and %d", index, len(command)-1)
		}
		if seen[index] {
			return fmt.Errorf("index %d is duplicated", index)
		}
		seen[index] = true
		argument := command[index]
		if strings.HasPrefix(argument, "-") || strings.ContainsRune(argument, '\x00') || !safeRelativePath(argument) {
			return fmt.Errorf("index %d must name a separate safe relative path", index)
		}
	}
	return nil
}

func validateTrustedExternalCommand(command []string, trustedConfigArguments []int) error {
	if len(command) == 0 {
		return fmt.Errorf("command must contain argv")
	}
	executable := command[0]
	if executable == "." || executable == ".." || (!filepath.IsAbs(filepath.FromSlash(executable)) && strings.ContainsAny(executable, `/\\`)) {
		return fmt.Errorf("executable %q is relative to the target checkout", executable)
	}
	trusted := make(map[int]bool, len(trustedConfigArguments))
	for _, index := range trustedConfigArguments {
		trusted[index] = true
	}
	executableBase := strings.ToLower(filepath.Base(filepath.FromSlash(executable)))
	npxPackageIndex := -1
	if executableBase == "npx" {
		var err error
		npxPackageIndex, err = validatePinnedNPXCommand(command)
		if err != nil {
			return err
		}
		if trusted[npxPackageIndex] {
			return fmt.Errorf("npx package argument %d is a pinned package reference, not a trusted config path", npxPackageIndex)
		}
	}
	interpreter := trustedCommandInterpreters[executableBase]
	for index := 1; index < len(command); index++ {
		if index == npxPackageIndex {
			continue
		}
		argument := command[index]
		pathLike := trustedCommandArgumentIsPathLike(argument)
		interpreterInput := interpreter && argument != "-" && !strings.HasPrefix(argument, "-")
		if (pathLike || interpreterInput) && !trusted[index] {
			return fmt.Errorf("argument %d %q may reference target-controlled content; add its index to trusted_config_arguments", index, argument)
		}
	}
	return nil
}

func validatePinnedNPXCommand(command []string) (int, error) {
	for index := 1; index < len(command); index++ {
		argument := command[index]
		if argument == "--yes" || argument == "-y" {
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return -1, fmt.Errorf("npx option %q is not allowed before the pinned package", argument)
		}
		if !pinnedNPXPackagePattern.MatchString(argument) {
			return -1, fmt.Errorf("npx package %q must use an exact semantic version", argument)
		}
		for _, remaining := range command[index+1:] {
			option := remaining
			if name, _, found := strings.Cut(option, "="); found {
				option = name
			}
			if forbiddenNPXOptions[option] {
				return -1, fmt.Errorf("npx option %q can add or replace executable package content", remaining)
			}
		}
		return index, nil
	}
	return -1, fmt.Errorf("npx requires an exact-version package reference")
}

func trustedCommandArgumentIsPathLike(argument string) bool {
	value := argument
	if strings.HasPrefix(value, "-") {
		_, optionValue, found := strings.Cut(value, "=")
		if !found {
			return false
		}
		value = optionValue
	}
	if value == "" || value == "-" {
		return false
	}
	if filepath.IsAbs(filepath.FromSlash(value)) || strings.ContainsAny(value, `/\\`) || strings.HasPrefix(value, ".") {
		return true
	}
	return trustedCommandFileExtensions[strings.ToLower(filepath.Ext(value))]
}

func validateCanaryPercentages(percentages []int) error {
	if len(percentages) == 0 {
		return fmt.Errorf("canary_percentages must not be empty")
	}
	previous := 0
	for _, percentage := range percentages {
		if percentage <= previous || percentage > 100 {
			return fmt.Errorf("canary_percentages must be strictly increasing values from 1 through 100")
		}
		previous = percentage
	}
	if previous != 100 {
		return fmt.Errorf("canary_percentages must end at 100")
	}
	return nil
}

func safeRelativePath(path string) bool {
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
