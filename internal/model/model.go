package model

import (
	"strings"
	"time"
)

var HarnessVersion = "0.1.0"

const SchemaVersion = "1"

type Profile string

const (
	ProfileAuto       Profile = "auto"
	ProfileBaseline   Profile = "baseline"
	ProfileProduction Profile = "production"
	ProfileRegulated  Profile = "regulated"
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
	Criticality           string                         `json:"criticality"`
	DataSensitivity       string                         `json:"data_sensitivity"`
	DeploysToProduction   *bool                          `json:"deploys_to_production"`
	PersistentData        *bool                          `json:"persistent_data"`
	IrreversibleActions   *bool                          `json:"irreversible_actions"`
	DesignSourceOfTruth   string                         `json:"design_source_of_truth,omitempty"`
	Approvers             []string                       `json:"approvers"`
	AllowCIChanges        *bool                          `json:"allow_ci_changes"`
	CIProviders           []string                       `json:"ci_providers,omitempty"`
	AllowedActions        *[]string                      `json:"allowed_actions"`
	CommandOverrides      map[string]map[string][]string `json:"command_overrides,omitempty"`
	CommandWaivers        map[string]string              `json:"command_waivers,omitempty"`
	CISetupCommands       map[string][]SetupCommand      `json:"ci_setup_commands,omitempty"`
	CISetupWaivers        map[string]string              `json:"ci_setup_waivers,omitempty"`
	GitLabImage           string                         `json:"gitlab_image,omitempty"`
	RiskAcceptance        string                         `json:"risk_acceptance,omitempty"`
	ObservationWindow     string                         `json:"observation_window,omitempty"`
	RollbackOwner         string                         `json:"rollback_owner,omitempty"`
	ProductionEnvironment string                         `json:"production_environment,omitempty"`
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
	Workdir  string   `json:"workdir" yaml:"workdir"`
	Command  []string `json:"command" yaml:"command"`
	Required bool     `json:"required" yaml:"required"`
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

type CIConfig struct {
	Providers                []string                  `json:"providers" yaml:"providers"`
	Managed                  bool                      `json:"managed" yaml:"managed"`
	BranchProtectionRequired bool                      `json:"branch_protection_required" yaml:"branch_protection_required"`
	SetupCommands            map[string][]SetupCommand `json:"setup_commands,omitempty" yaml:"setup_commands,omitempty"`
	SetupWaivers             map[string]string         `json:"setup_waivers,omitempty" yaml:"setup_waivers,omitempty"`
	GitLabImage              string                    `json:"gitlab_image,omitempty" yaml:"gitlab_image,omitempty"`
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
	Applicable    bool   `json:"applicable" yaml:"applicable"`
	SourceOfTruth string `json:"source_of_truth,omitempty" yaml:"source_of_truth,omitempty"`
	BrowserProof  bool   `json:"browser_proof" yaml:"browser_proof"`
	HumanLabels   bool   `json:"human_labels" yaml:"human_labels"`
	Accessibility bool   `json:"accessibility" yaml:"accessibility"`
	Localization  bool   `json:"localization" yaml:"localization"`
}

type GovernanceConfig struct {
	Approvers       []string          `json:"approvers" yaml:"approvers"`
	Criticality     string            `json:"criticality" yaml:"criticality"`
	DataSensitivity string            `json:"data_sensitivity" yaml:"data_sensitivity"`
	RiskAcceptance  string            `json:"risk_acceptance,omitempty" yaml:"risk_acceptance,omitempty"`
	CommandWaivers  map[string]string `json:"command_waivers,omitempty" yaml:"command_waivers,omitempty"`
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
	Metadata       map[string]string `json:"metadata,omitempty" yaml:"metadata,omitempty"`
}

type Operation struct {
	Path          string `json:"path"`
	Action        string `json:"action"`
	Content       string `json:"content"`
	ContentSHA256 string `json:"content_sha256"`
}

type Plan struct {
	PlanVersion        string      `json:"plan_version"`
	ID                 string      `json:"id"`
	CreatedAt          time.Time   `json:"created_at"`
	ExpiresAt          time.Time   `json:"expires_at"`
	Root               string      `json:"root"`
	Fingerprint        string      `json:"fingerprint"`
	RequestedProfile   Profile     `json:"requested_profile"`
	RecommendedProfile Profile     `json:"recommended_profile"`
	AppliedProfile     Profile     `json:"applied_profile"`
	Answers            Answers     `json:"answers"`
	Unresolved         []string    `json:"unresolved"`
	Operations         []Operation `json:"operations"`
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
