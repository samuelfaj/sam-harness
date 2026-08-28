package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
	configschema "github.com/samuelfaj/sam-harness/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
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
	if err := validateSemantics(cfg); err != nil {
		return model.Config{}, err
	}
	return cfg, nil
}

func Marshal(cfg model.Config) ([]byte, error) {
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
		if strings.TrimSpace(cfg.Release.RollbackOwner) == "" || strings.TrimSpace(cfg.Release.ObservationWindow) == "" || strings.TrimSpace(cfg.Release.ProductionEnvironment) == "" {
			return fmt.Errorf("validate config: %s requires rollback owner, observation window, and production environment", cfg.Profile)
		}
	}
	if cfg.Profile == model.ProfileRegulated && len(cfg.Governance.Approvers) < 2 {
		return fmt.Errorf("validate config: regulated profile requires at least two approvers")
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
