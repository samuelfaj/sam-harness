package adopt

import (
	"fmt"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/planner"
	"github.com/samuelfaj/sam-harness/internal/repo"
	"os"
	"path/filepath"
	"time"
)

func knownImplementControl(control string) bool {
	switch control {
	case securityControl, "guard:format", "guard:lint", "guard:typecheck", "guard:unit":
		return true
	default:
		return false
	}
}

func resolveImplementPlan(opts Options, scanResult model.ScanResult, interview model.Plan, answers model.Answers, control string) (model.Plan, string, *BoundedTask, error) {
	if control == securityControl {
		return resolveSecurityPlan(opts, scanResult, interview, answers)
	}
	if opts.PlanOutput != "" && !fileMissing(opts.PlanOutput) {
		plan, err := planner.Load(opts.PlanOutput)
		if err != nil {
			return model.Plan{}, "", nil, err
		}
		return plan, opts.PlanOutput, implementTask(control), nil
	}
	plan, err := buildStackGuardPlan(scanResult, interview, answers, control)
	if err != nil {
		return model.Plan{}, "", nil, err
	}
	path, err := planner.Save(plan, opts.PlanOutput)
	if err != nil {
		return model.Plan{}, "", nil, err
	}
	return plan, path, implementTask(control), nil
}

func buildStackGuardPlan(scanResult model.ScanResult, interview model.Plan, answers model.Answers, control string) (model.Plan, error) {
	configPath := filepath.Join(scanResult.Root, filepath.FromSlash(configRel))
	cfg, err := config.Load(configPath)
	if err != nil {
		return model.Plan{}, fmt.Errorf("implementing %s requires an installed harness config: %w", control, err)
	}
	spec, category, phase, err := stackGuardSpec(control, scanResult.Stacks)
	if err != nil {
		return model.Plan{}, err
	}
	cfg.Gates = append(cfg.Gates, model.Gate{
		Name:     category,
		Stage:    "local",
		Phase:    phase,
		Workdir:  spec.Workdir,
		Command:  append([]string(nil), spec.Command...),
		Required: true,
	})
	if cfg.Workflow != nil && cfg.Workflow.Enabled {
		if phase == model.PhaseTest {
			ensureGuardMaps(&cfg.Workflow.TestGuards)
			delete(cfg.Workflow.TestGuards.Waivers, category)
			cfg.Workflow.TestGuards.Commands[category] = spec
		} else {
			ensureGuardMaps(&cfg.Workflow.StaticGuards)
			delete(cfg.Workflow.StaticGuards.Waivers, category)
			cfg.Workflow.StaticGuards.Commands[category] = spec
		}
	}
	data, err := config.Marshal(cfg)
	if err != nil {
		return model.Plan{}, err
	}
	operations := []model.Operation{fileOperation(scanResult.Root, configRel, string(data))}
	fingerprint, err := repo.Fingerprint(scanResult.Root)
	if err != nil {
		return model.Plan{}, err
	}
	createdAt := time.Now().UTC()
	plan := model.Plan{
		PlanVersion:        "1",
		CreatedAt:          createdAt,
		ExpiresAt:          createdAt.Add(30 * time.Minute),
		Root:               scanResult.Root,
		Fingerprint:        fingerprint,
		RequestedProfile:   interview.RequestedProfile,
		RecommendedProfile: interview.RecommendedProfile,
		AppliedProfile:     interview.AppliedProfile,
		Answers:            answers,
		Unresolved:         []string{},
		Operations:         operations,
	}
	plan.ID = planner.CalculateID(plan)
	return plan, nil
}

func stackGuardSpec(control string, stacks []model.Stack) (model.CommandSpec, string, model.Phase, error) {
	category := strings.TrimPrefix(control, "guard:")
	phase := model.PhaseStatic
	if category == model.GuardUnit {
		phase = model.PhaseTest
	}
	argv, err := stackGuardArgv(category, stacks)
	if err != nil {
		return model.CommandSpec{}, "", "", err
	}
	return model.CommandSpec{
		Name:           "implemented " + category,
		Workdir:        ".",
		Command:        argv,
		Required:       true,
		TimeoutSeconds: 600,
	}, category, phase, nil
}

func stackGuardArgv(category string, stacks []model.Stack) ([]string, error) {
	aliases := map[string][]string{
		model.GuardFormat:    {"format-check", "format"},
		model.GuardLint:      {"lint"},
		model.GuardTypecheck: {"typecheck"},
		model.GuardUnit:      {"test"},
	}
	for _, stack := range stacks {
		for _, alias := range aliases[category] {
			if command := stack.Commands[alias]; len(command) > 0 {
				return append([]string(nil), command...), nil
			}
		}
		if fallback := kindFallback(stack.Kind, category); len(fallback) > 0 {
			return fallback, nil
		}
	}
	return nil, fmt.Errorf("no existing %s command for detected stacks", category)
}

func kindFallback(kind, category string) []string {
	switch kind {
	case "go":
		switch category {
		case model.GuardFormat:
			return []string{"sh", "-c", "test -z \"$(gofmt -l .)\""}
		case model.GuardLint:
			return []string{"go", "vet", "./..."}
		case model.GuardTypecheck:
			return []string{"go", "test", "-run", "^$", "./..."}
		case model.GuardUnit:
			return []string{"go", "test", "./..."}
		}
	case "python":
		switch category {
		case model.GuardLint, model.GuardFormat:
			return []string{"python3", "-m", "ruff", "check", "."}
		case model.GuardTypecheck:
			return []string{"python3", "-m", "mypy", "."}
		case model.GuardUnit:
			return []string{"python3", "-m", "pytest"}
		}
	case "rust":
		switch category {
		case model.GuardFormat:
			return []string{"cargo", "fmt", "--all", "--", "--check"}
		case model.GuardLint:
			return []string{"cargo", "clippy", "--all-targets", "--all-features", "--", "-D", "warnings"}
		case model.GuardUnit:
			return []string{"cargo", "test", "--all"}
		case model.GuardTypecheck:
			return []string{"cargo", "check", "--all"}
		}
	}
	return nil
}

func implementTask(control string) *BoundedTask {
	if control == securityControl {
		return securityTask()
	}
	category := strings.TrimPrefix(control, "guard:")
	return &BoundedTask{
		ControlID:      control,
		Acceptance:     []string{"workflow " + category + " command exists", "the implemented command is required"},
		AffectedPaths:  []string{configRel},
		Commands:       [][]string{{"sam-harness", "doctor", "."}},
		Tests:          [][]string{{"sam-harness", "doctor", "."}},
		MaxFiles:       1,
		MaxLines:       200,
		StopConditions: []string{"repository fingerprint changed after planning"},
	}
}

func ensureGuardMaps(guards *model.GuardSet) {
	if guards.Commands == nil {
		guards.Commands = map[string]model.CommandSpec{}
	}
	if guards.Waivers == nil {
		guards.Waivers = map[string]string{}
	}
}

func chmodIfPresent(path string) error {
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	return os.Chmod(path, 0o755)
}
