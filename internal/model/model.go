package model

import (
	"fmt"
	"strings"
	"time"
)

var HarnessVersion = "0.7.1"

const SchemaVersion = "1"

type Profile string

const (
	ProfileAuto       Profile = "auto"
	ProfileBaseline   Profile = "baseline"
	ProfileProduction Profile = "production"
	ProfileRegulated  Profile = "regulated"
)

const (
	AdoptionPhaseCore     = "core"
	AdoptionPhaseArtifact = "artifact"
	AdoptionPhaseDelivery = "delivery"
)

func NormalizeAdoptionPhase(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case "", AdoptionPhaseDelivery:
		return AdoptionPhaseDelivery, nil
	case AdoptionPhaseCore, AdoptionPhaseArtifact:
		return strings.TrimSpace(value), nil
	default:
		return "", fmt.Errorf("adoption_phase must be core, artifact, or delivery")
	}
}

func AdoptionPhaseRank(value string) int {
	normalized, err := NormalizeAdoptionPhase(value)
	if err != nil {
		return 0
	}
	switch normalized {
	case AdoptionPhaseCore:
		return 1
	case AdoptionPhaseArtifact:
		return 2
	default:
		return 3
	}
}

const (
	ChangeRiskLow      = "low"
	ChangeRiskMedium   = "medium"
	ChangeRiskHigh     = "high"
	ChangeRiskCritical = "critical"
)

func (p Profile) Valid(allowAuto bool) bool {
	switch p {
	case ProfileBaseline, ProfileProduction, ProfileRegulated:
		return true
	case ProfileAuto:
		return allowAuto
	default:
		return false
	}
}

type Phase string

const (
	PhaseStatic     Phase = "static"
	PhaseTest       Phase = "test"
	PhaseReview     Phase = "review"
	PhaseArtifact   Phase = "artifact"
	PhaseStaging    Phase = "staging"
	PhaseProduction Phase = "production"
	PhaseObserve    Phase = "observe"
	PhaseRollback   Phase = "rollback"
	PhaseMigration  Phase = "migration"
	PhaseAll        Phase = "all"
)

func (p Phase) Valid() bool {
	switch p {
	case PhaseStatic, PhaseTest, PhaseReview, PhaseArtifact, PhaseStaging, PhaseProduction, PhaseObserve, PhaseRollback, PhaseMigration, PhaseAll:
		return true
	default:
		return false
	}
}

const (
	CISecretScopeStatic     = "static"
	CISecretScopeTest       = "test"
	CISecretScopeReview     = "review"
	CISecretScopeRepair     = "repair"
	CISecretScopeArtifact   = "artifact"
	CISecretScopeStaging    = "staging"
	CISecretScopeProduction = "production"
	CISecretScopeObserve    = "observe"
	CISecretScopeRollback   = "rollback"
	CISecretScopeMigration  = "migration"
)

const (
	AgentControlPlaneModeGitHubApp = "github_app"
	AgentControlPlaneModeExternal  = "external"
)

var CISecretScopes = []string{
	CISecretScopeStatic,
	CISecretScopeTest,
	CISecretScopeReview,
	CISecretScopeRepair,
	CISecretScopeArtifact,
	CISecretScopeStaging,
	CISecretScopeProduction,
	CISecretScopeObserve,
	CISecretScopeRollback,
	CISecretScopeMigration,
}

const (
	GuardFormat             = "format"
	GuardLint               = "lint"
	GuardTypecheck          = "typecheck"
	GuardArchitecture       = "architecture"
	GuardSecurity           = "security"
	GuardDependencies       = "dependencies"
	GuardSchema             = "schema"
	GuardProjectPolicies    = "project_policies"
	GuardUnit               = "unit"
	GuardIntegration        = "integration"
	GuardContract           = "contract"
	GuardBusinessInvariants = "business_invariants"
	GuardProperty           = "property"
	GuardMutation           = "mutation"
	GuardE2E                = "e2e"
	GuardPerformance        = "performance"
)

var StaticGuardCategories = []string{
	GuardFormat,
	GuardLint,
	GuardTypecheck,
	GuardArchitecture,
	GuardSecurity,
	GuardDependencies,
	GuardSchema,
	GuardProjectPolicies,
}

var TestGuardCategories = []string{
	GuardUnit,
	GuardIntegration,
	GuardContract,
	GuardBusinessInvariants,
	GuardProperty,
	GuardMutation,
	GuardE2E,
	GuardPerformance,
}

type ReviewerRole string

const (
	ReviewerArchitecture  ReviewerRole = "architecture"
	ReviewerSecurity      ReviewerRole = "security"
	ReviewerCorrectness   ReviewerRole = "correctness"
	ReviewerTestQuality   ReviewerRole = "test_quality"
	ReviewerBusinessRules ReviewerRole = "business_rules"
	ReviewerSimplicity    ReviewerRole = "simplicity"
)

var ReviewerRoles = []ReviewerRole{
	ReviewerArchitecture,
	ReviewerSecurity,
	ReviewerCorrectness,
	ReviewerTestQuality,
	ReviewerBusinessRules,
	ReviewerSimplicity,
}

func (r ReviewerRole) Valid() bool {
	switch r {
	case ReviewerArchitecture, ReviewerSecurity, ReviewerCorrectness, ReviewerTestQuality, ReviewerBusinessRules, ReviewerSimplicity:
		return true
	default:
		return false
	}
}

type Stack struct {
	Kind           string              `json:"kind" yaml:"kind"`
	Path           string              `json:"path" yaml:"path"`
	PackageManager string              `json:"package_manager,omitempty" yaml:"package_manager,omitempty"`
	Commands       map[string][]string `json:"commands,omitempty" yaml:"commands,omitempty"`
	UI             bool                `json:"ui" yaml:"ui"`
	Persistence    bool                `json:"persistence" yaml:"persistence"`
}

type GitState struct {
	Repository bool   `json:"repository"`
	Head       string `json:"head,omitempty"`
	Dirty      bool   `json:"dirty"`
	RemoteHost string `json:"remote_host,omitempty"`
}

type ScanResult struct {
	Root            string   `json:"root"`
	Fingerprint     string   `json:"fingerprint"`
	Git             GitState `json:"git"`
	Stacks          []Stack  `json:"stacks"`
	CIProviders     []string `json:"ci_providers"`
	HasUI           bool     `json:"has_ui"`
	HasPersistence  bool     `json:"has_persistence"`
	HasDeployment   bool     `json:"has_deployment"`
	ExistingHarness bool     `json:"existing_harness"`
	Questions       []string `json:"questions"`
}

type Answers struct {
	Criticality             string                         `json:"criticality"`
	DataSensitivity         string                         `json:"data_sensitivity"`
	DeploysToProduction     *bool                          `json:"deploys_to_production"`
	PersistentData          *bool                          `json:"persistent_data"`
	IrreversibleActions     *bool                          `json:"irreversible_actions"`
	DesignSourceOfTruth     string                         `json:"design_source_of_truth,omitempty"`
	Approvers               []string                       `json:"approvers"`
	AllowCIChanges          *bool                          `json:"allow_ci_changes"`
	CIProviders             []string                       `json:"ci_providers,omitempty"`
	AllowedActions          *[]string                      `json:"allowed_actions"`
	CommandOverrides        map[string]map[string][]string `json:"command_overrides,omitempty"`
	CommandWaivers          map[string]string              `json:"command_waivers,omitempty"`
	CISetupCommands         map[string][]SetupCommand      `json:"ci_setup_commands,omitempty"`
	CISetupWaivers          map[string]string              `json:"ci_setup_waivers,omitempty"`
	CISecretBindings        map[string][]CISecretBinding   `json:"ci_secret_bindings,omitempty"`
	CISecretWaivers         map[string]string              `json:"ci_secret_waivers,omitempty"`
	AgentSecretEnvironments map[string]string              `json:"agent_secret_environments,omitempty"`
	AgentControlPlanes      map[string]AgentControlPlane   `json:"agent_control_planes,omitempty"`
	CIAgentRuntime          *CIAgentRuntime                `json:"ci_agent_runtime,omitempty"`
	StandardizeCommits      *bool                          `json:"standardize_commits,omitempty"`
	GitLabImage             string                         `json:"gitlab_image,omitempty"`
	RiskAcceptance          string                         `json:"risk_acceptance,omitempty"`
	ObservationWindow       string                         `json:"observation_window,omitempty"`
	RollbackOwner           string                         `json:"rollback_owner,omitempty"`
	ProductionEnvironment   string                         `json:"production_environment,omitempty"`
	Workflow                *WorkflowConfig                `json:"workflow,omitempty"`
	AdoptionPhase           string                         `json:"adoption_phase,omitempty"`
	ConfirmGuardDefaults    []string                       `json:"confirm_guard_defaults,omitempty"`
	ConfirmRuntimeReviewers *bool                          `json:"confirm_runtime_reviewers,omitempty"`
	ReviewTimeoutSeconds    int                            `json:"review_timeout_seconds,omitempty"`
	BrowserCommand          []string                       `json:"browser_command,omitempty"`
	BrowserWaiver           string                         `json:"browser_waiver,omitempty"`
	AccessibilityCommand    []string                       `json:"accessibility_command,omitempty"`
	AccessibilityWaiver     string                         `json:"accessibility_waiver,omitempty"`
}

func (a Answers) Missing(scan ScanResult) []string {
	var missing []string
	if a.Criticality == "" {
		missing = append(missing, "criticality")
	}
	if a.DataSensitivity == "" {
		missing = append(missing, "data_sensitivity")
	}
	if a.DeploysToProduction == nil {
		missing = append(missing, "deploys_to_production")
	}
	if a.PersistentData == nil {
		missing = append(missing, "persistent_data")
	}
	if a.IrreversibleActions == nil {
		missing = append(missing, "irreversible_actions")
	}
	if a.AllowCIChanges == nil {
		missing = append(missing, "allow_ci_changes")
	}
	if a.AllowCIChanges != nil && *a.AllowCIChanges && len(scan.CIProviders) == 0 && len(a.CIProviders) == 0 {
		missing = append(missing, "ci_providers")
	}
	if a.AllowedActions == nil {
		missing = append(missing, "allowed_actions")
	}
	if a.StandardizeCommits == nil {
		missing = append(missing, "standardize_commits")
	}
	if len(a.Approvers) == 0 {
		missing = append(missing, "approvers")
	}
	if scan.HasUI && a.DesignSourceOfTruth == "" {
		missing = append(missing, "design_source_of_truth")
	}
	return missing
}

func (a Answers) CommandWaiver(key string) bool {
	return strings.TrimSpace(a.CommandWaivers[key]) != ""
}

type Gate struct {
	Name     string   `json:"name" yaml:"name"`
	Stage    string   `json:"stage" yaml:"stage"`
	Phase    Phase    `json:"phase,omitempty" yaml:"phase,omitempty"`
	Workdir  string   `json:"workdir" yaml:"workdir"`
	Command  []string `json:"command" yaml:"command"`
	Required bool     `json:"required" yaml:"required"`
}

type CommandSpec struct {
	Name           string   `json:"name" yaml:"name"`
	Workdir        string   `json:"workdir" yaml:"workdir"`
	Command        []string `json:"command" yaml:"command"`
	Required       bool     `json:"required" yaml:"required"`
	TimeoutSeconds int      `json:"timeout_seconds" yaml:"timeout_seconds"`
}

type GuardSet struct {
	Commands map[string]CommandSpec `json:"commands" yaml:"commands"`
	Waivers  map[string]string      `json:"waivers" yaml:"waivers"`
}

type ReviewerConfig struct {
	Role                   ReviewerRole `json:"role" yaml:"role"`
	Command                []string     `json:"command" yaml:"command"`
	TimeoutSeconds         int          `json:"timeout_seconds" yaml:"timeout_seconds"`
	FilesystemReadOnly     bool         `json:"filesystem_read_only" yaml:"filesystem_read_only"`
	TrustedExternalCommand bool         `json:"trusted_external_command,omitempty" yaml:"trusted_external_command,omitempty"`
	TrustedConfigArguments []int        `json:"trusted_config_arguments,omitempty" yaml:"trusted_config_arguments,omitempty"`
}

type CorrectionConfig struct {
	Enabled                bool     `json:"enabled" yaml:"enabled"`
	FilesystemSandboxed    bool     `json:"filesystem_sandboxed" yaml:"filesystem_sandboxed"`
	Command                []string `json:"command,omitempty" yaml:"command,omitempty"`
	TrustedExternalCommand bool     `json:"trusted_external_command,omitempty" yaml:"trusted_external_command,omitempty"`
	TrustedConfigArguments []int    `json:"trusted_config_arguments,omitempty" yaml:"trusted_config_arguments,omitempty"`
	MaxAttempts            int      `json:"max_attempts" yaml:"max_attempts"`
	MaxChangedFiles        int      `json:"max_changed_files" yaml:"max_changed_files"`
	MaxChangedLines        int      `json:"max_changed_lines" yaml:"max_changed_lines"`
	BranchPrefix           string   `json:"branch_prefix,omitempty" yaml:"branch_prefix,omitempty"`
	OpenChangeRequest      bool     `json:"open_change_request" yaml:"open_change_request"`
}

type ArtifactWorkflow struct {
	Build          CommandSpec `json:"build" yaml:"build"`
	ArtifactPath   string      `json:"artifact_path" yaml:"artifact_path"`
	SBOM           CommandSpec `json:"sbom" yaml:"sbom"`
	SBOMPath       string      `json:"sbom_path" yaml:"sbom_path"`
	Provenance     CommandSpec `json:"provenance" yaml:"provenance"`
	ProvenancePath string      `json:"provenance_path" yaml:"provenance_path"`
}

type DeploymentWorkflow struct {
	Staging           CommandSpec   `json:"staging" yaml:"staging"`
	Production        CommandSpec   `json:"production" yaml:"production"`
	Rollback          CommandSpec   `json:"rollback" yaml:"rollback"`
	HealthChecks      []CommandSpec `json:"health_checks" yaml:"health_checks"`
	ObservationChecks []CommandSpec `json:"observation_checks" yaml:"observation_checks"`
	CanaryPercentages []int         `json:"canary_percentages" yaml:"canary_percentages"`
}

type ReleaseSchedule struct {
	Cron     string `json:"cron" yaml:"cron"`
	Timezone string `json:"timezone" yaml:"timezone"`
}

type WorkflowConfig struct {
	Enabled         bool               `json:"enabled" yaml:"enabled"`
	AdoptionPhase   string             `json:"adoption_phase,omitempty" yaml:"adoption_phase,omitempty"`
	StaticGuards    GuardSet           `json:"static_guards" yaml:"static_guards"`
	TestGuards      GuardSet           `json:"test_guards" yaml:"test_guards"`
	Reviewers       []ReviewerConfig   `json:"reviewers" yaml:"reviewers"`
	Correction      CorrectionConfig   `json:"correction" yaml:"correction"`
	Artifact        ArtifactWorkflow   `json:"artifact,omitempty" yaml:"artifact,omitempty"`
	Deployment      DeploymentWorkflow `json:"deployment,omitempty" yaml:"deployment,omitempty"`
	Migration       []CommandSpec      `json:"migration,omitempty" yaml:"migration,omitempty"`
	ReleaseSchedule ReleaseSchedule    `json:"release_schedule,omitempty" yaml:"release_schedule,omitempty"`
}

type Authority struct {
	WriteRepository bool `json:"write_repository" yaml:"write_repository"`
	Network         bool `json:"network" yaml:"network"`
	Commit          bool `json:"commit" yaml:"commit"`
	Push            bool `json:"push" yaml:"push"`
	Release         bool `json:"release" yaml:"release"`
	Deploy          bool `json:"deploy" yaml:"deploy"`
}

type Evidence struct {
	ReceiptDirectory string   `json:"receipt_directory" yaml:"receipt_directory"`
	RequiredStates   []string `json:"required_states" yaml:"required_states"`
}

type SetupCommand struct {
	Workdir string   `json:"workdir" yaml:"workdir"`
	Command []string `json:"command" yaml:"command"`
}

type CISecretBinding struct {
	Scope       string `json:"scope" yaml:"scope"`
	Environment string `json:"environment" yaml:"environment"`
	Secret      string `json:"secret" yaml:"secret"`
}

type AgentControlPlane struct {
	Mode                string `json:"mode" yaml:"mode"`
	RequiredCheck       string `json:"required_check" yaml:"required_check"`
	AppIDSecret         string `json:"app_id_secret,omitempty" yaml:"app_id_secret,omitempty"`
	AppPrivateKeySecret string `json:"app_private_key_secret,omitempty" yaml:"app_private_key_secret,omitempty"`
	ExternalProject     string `json:"external_project,omitempty" yaml:"external_project,omitempty"`
}

type CIConfig struct {
	Providers                []string                     `json:"providers" yaml:"providers"`
	Managed                  bool                         `json:"managed" yaml:"managed"`
	BranchProtectionRequired bool                         `json:"branch_protection_required" yaml:"branch_protection_required"`
	SetupCommands            map[string][]SetupCommand    `json:"setup_commands,omitempty" yaml:"setup_commands,omitempty"`
	SetupWaivers             map[string]string            `json:"setup_waivers,omitempty" yaml:"setup_waivers,omitempty"`
	SecretBindings           map[string][]CISecretBinding `json:"secret_bindings,omitempty" yaml:"secret_bindings,omitempty"`
	SecretWaivers            map[string]string            `json:"secret_waivers,omitempty" yaml:"secret_waivers,omitempty"`
	AgentSecretEnvironments  map[string]string            `json:"agent_secret_environments,omitempty" yaml:"agent_secret_environments,omitempty"`
	AgentControlPlanes       map[string]AgentControlPlane `json:"agent_control_planes,omitempty" yaml:"agent_control_planes,omitempty"`
	AgentRuntime             *CIAgentRuntime              `json:"agent_runtime,omitempty" yaml:"agent_runtime,omitempty"`
	GitLabImage              string                       `json:"gitlab_image,omitempty" yaml:"gitlab_image,omitempty"`
}

type ReleaseConfig struct {
	ImmutableArtifact     bool   `json:"immutable_artifact" yaml:"immutable_artifact"`
	SBOM                  bool   `json:"sbom" yaml:"sbom"`
	Provenance            bool   `json:"provenance" yaml:"provenance"`
	PromotionRequired     bool   `json:"promotion_required" yaml:"promotion_required"`
	RollbackOwner         string `json:"rollback_owner,omitempty" yaml:"rollback_owner,omitempty"`
	ObservationWindow     string `json:"observation_window,omitempty" yaml:"observation_window,omitempty"`
	ProductionEnvironment string `json:"production_environment,omitempty" yaml:"production_environment,omitempty"`
}

type MigrationConfig struct {
	Required           bool `json:"required" yaml:"required"`
	ReconciliationGate bool `json:"reconciliation_gate" yaml:"reconciliation_gate"`
	RestoreTest        bool `json:"restore_test" yaml:"restore_test"`
}

type DesignConfig struct {
	Applicable           bool     `json:"applicable" yaml:"applicable"`
	SourceOfTruth        string   `json:"source_of_truth,omitempty" yaml:"source_of_truth,omitempty"`
	BrowserProof         bool     `json:"browser_proof" yaml:"browser_proof"`
	HumanLabels          bool     `json:"human_labels" yaml:"human_labels"`
	Accessibility        bool     `json:"accessibility" yaml:"accessibility"`
	Localization         bool     `json:"localization" yaml:"localization"`
	BrowserCommand       []string `json:"browser_command,omitempty" yaml:"browser_command,omitempty"`
	BrowserWaiver        string   `json:"browser_waiver,omitempty" yaml:"browser_waiver,omitempty"`
	AccessibilityCommand []string `json:"accessibility_command,omitempty" yaml:"accessibility_command,omitempty"`
	AccessibilityWaiver  string   `json:"accessibility_waiver,omitempty" yaml:"accessibility_waiver,omitempty"`
}

type GovernanceConfig struct {
	Approvers          []string          `json:"approvers" yaml:"approvers"`
	Criticality        string            `json:"criticality" yaml:"criticality"`
	DataSensitivity    string            `json:"data_sensitivity" yaml:"data_sensitivity"`
	RiskAcceptance     string            `json:"risk_acceptance,omitempty" yaml:"risk_acceptance,omitempty"`
	CommandWaivers     map[string]string `json:"command_waivers,omitempty" yaml:"command_waivers,omitempty"`
	StandardizeCommits *bool             `json:"standardize_commits,omitempty" yaml:"standardize_commits,omitempty"`
}

type Config struct {
	SchemaVersion  string            `json:"schema_version" yaml:"schema_version"`
	HarnessVersion string            `json:"harness_version" yaml:"harness_version"`
	Profile        Profile           `json:"profile" yaml:"profile"`
	Repository     string            `json:"repository" yaml:"repository"`
	Stacks         []Stack           `json:"stacks" yaml:"stacks"`
	Gates          []Gate            `json:"gates" yaml:"gates"`
	Authority      Authority         `json:"authority" yaml:"authority"`
	Evidence       Evidence          `json:"evidence" yaml:"evidence"`
	CI             CIConfig          `json:"ci" yaml:"ci"`
	Release        ReleaseConfig     `json:"release" yaml:"release"`
	Migration      MigrationConfig   `json:"migration" yaml:"migration"`
	Design         DesignConfig      `json:"design" yaml:"design"`
	Governance     GovernanceConfig  `json:"governance" yaml:"governance"`
	Workflow       *WorkflowConfig   `json:"workflow,omitempty" yaml:"workflow,omitempty"`
	Metadata       map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type Operation struct {
	Path          string `json:"path"`
	Action        string `json:"action"`
	Content       string `json:"content"`
	ContentSHA256 string `json:"content_sha256"`
}

type Plan struct {
	PlanVersion                  string                 `json:"plan_version"`
	ID                           string                 `json:"id"`
	CreatedAt                    time.Time              `json:"created_at"`
	ExpiresAt                    time.Time              `json:"expires_at"`
	Root                         string                 `json:"root"`
	Fingerprint                  string                 `json:"fingerprint"`
	RequestedProfile             Profile                `json:"requested_profile"`
	RecommendedProfile           Profile                `json:"recommended_profile"`
	AppliedProfile               Profile                `json:"applied_profile"`
	Answers                      Answers                `json:"answers"`
	Unresolved                   []string               `json:"unresolved"`
	Deferred                     []string               `json:"deferred,omitempty"`
	ProposedGuardDefaults        map[string]CommandSpec `json:"proposed_guard_defaults,omitempty"`
	ProposedReviewerHost         string                 `json:"proposed_reviewer_host,omitempty"`
	ProposedReviewerCommand      []string               `json:"proposed_reviewer_command,omitempty"`
	ProposedBrowserCommand       []string               `json:"proposed_browser_command,omitempty"`
	ProposedAccessibilityCommand []string               `json:"proposed_accessibility_command,omitempty"`
	Operations                   []Operation            `json:"operations"`
}

type GateResult struct {
	Name       string        `json:"name"`
	Stage      string        `json:"stage"`
	Command    []string      `json:"command"`
	Workdir    string        `json:"workdir"`
	Required   bool          `json:"required"`
	Passed     bool          `json:"passed"`
	Skipped    bool          `json:"skipped"`
	ExitCode   int           `json:"exit_code"`
	Duration   time.Duration `json:"duration"`
	Output     string        `json:"output"`
	StartedAt  time.Time     `json:"started_at"`
	FinishedAt time.Time     `json:"finished_at"`
}

type CheckReport struct {
	HarnessVersion string       `json:"harness_version"`
	Root           string       `json:"root"`
	Profile        Profile      `json:"profile"`
	Fingerprint    string       `json:"fingerprint"`
	Passed         bool         `json:"passed"`
	Results        []GateResult `json:"results"`
	CreatedAt      time.Time    `json:"created_at"`
}
