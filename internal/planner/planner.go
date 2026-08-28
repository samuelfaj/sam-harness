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
	resolvedScan, commandQuestions, err := resolveCommands(scan, answers)
	if err != nil {
		return model.Plan{}, err
	}
	if len(answers.CIProviders) > 0 {
		resolvedScan.CIProviders = append([]string(nil), answers.CIProviders...)
		sort.Strings(resolvedScan.CIProviders)
	}
	recommended := Recommend(resolvedScan, answers)
	applied := requested
	if requested == model.ProfileAuto {
		applied = recommended
	}
	unresolved := answers.Missing(scan)
	if unresolved == nil {
		unresolved = []string{}
	}
	unresolved = append(unresolved, commandQuestions...)
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
	if applied == model.ProfileRegulated && len(answers.Approvers) < 2 {
		unresolved = append(unresolved, "separated_approvers")
	}
	if profileRank(applied) < profileRank(recommended) && strings.TrimSpace(answers.RiskAcceptance) == "" {
		unresolved = append(unresolved, "risk_acceptance")
	}
	if !allowsWrite(answers.AllowedActions) {
		unresolved = append(unresolved, "authority:write_repository")
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
	sort.Strings(unresolved)
	createdAt := time.Now().UTC()
	plan := model.Plan{
		PlanVersion:        "1",
		CreatedAt:          createdAt,
		ExpiresAt:          createdAt.Add(30 * time.Minute),
		Root:               scan.Root,
		Fingerprint:        scan.Fingerprint,
		RequestedProfile:   requested,
		RecommendedProfile: recommended,
		AppliedProfile:     applied,
		Answers:            answers,
		Unresolved:         unresolved,
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
	return nil
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

func cloneCommands(commands map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(commands))
	for gate, command := range commands {
		cloned[gate] = append([]string(nil), command...)
	}
	return cloned
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
	if actions == nil {
		return false
	}
	for _, action := range *actions {
		if action == "write_repository" {
			return true
		}
	}
	return false
}
