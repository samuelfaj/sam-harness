package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	applyplan "github.com/samuelfaj/sam-harness/internal/apply"
	checkrun "github.com/samuelfaj/sam-harness/internal/check"
	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/doctor"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/planner"
	"github.com/samuelfaj/sam-harness/internal/scan"
)

type CLI struct {
	Stdout io.Writer
	Stderr io.Writer
}

func New(stdout, stderr io.Writer) *CLI {
	return &CLI{Stdout: stdout, Stderr: stderr}
}

func (c *CLI) Run(args []string) error {
	if len(args) == 0 {
		c.usage()
		return errors.New("command required")
	}
	switch args[0] {
	case "scan":
		return c.scan(args[1:])
	case "plan":
		return c.plan(args[1:])
	case "apply":
		return c.apply(args[1:])
	case "check":
		return c.check(args[1:])
	case "doctor":
		return c.doctor(args[1:])
	case "upgrade":
		return c.upgrade(args[1:])
	case "version", "--version", "-v":
		fmt.Fprintf(c.Stdout, "sam-harness %s\n", model.HarnessVersion)
		return nil
	case "help", "--help", "-h":
		c.usage()
		return nil
	default:
		c.usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func (c *CLI) scan(args []string) error {
	options, err := parseOptions(args, map[string]bool{"format": true})
	if err != nil {
		return err
	}
	result, err := scan.Run(options.path())
	if err != nil {
		return err
	}
	if options.value("format", "human") == "json" {
		return writeJSON(c.Stdout, result)
	}
	fmt.Fprintf(c.Stdout, "Repository: %s\nFingerprint: %s\n", result.Root, result.Fingerprint)
	if len(result.Stacks) == 0 {
		fmt.Fprintln(c.Stdout, "Stacks: none detected")
	} else {
		fmt.Fprintln(c.Stdout, "Stacks:")
		for _, stack := range result.Stacks {
			fmt.Fprintf(c.Stdout, "  - %s at %s\n", stack.Kind, stack.Path)
		}
	}
	fmt.Fprintf(c.Stdout, "CI providers: %s\n", valueOr(strings.Join(result.CIProviders, ", "), "none detected"))
	fmt.Fprintf(c.Stdout, "UI: %t\nPersistence: %t\nDeployment: %t\n", result.HasUI, result.HasPersistence, result.HasDeployment)
	return nil
}

func (c *CLI) plan(args []string) error {
	options, err := parseOptions(args, map[string]bool{"format": true, "profile": true, "answers": true, "output": true})
	if err != nil {
		return err
	}
	result, err := scan.Run(options.path())
	if err != nil {
		return err
	}
	answers, err := planner.LoadAnswers(options.values["answers"])
	if err != nil {
		return err
	}
	profile := model.Profile(options.value("profile", "auto"))
	plan, err := planner.Create(result, profile, answers)
	if err != nil {
		return err
	}
	path, err := planner.Save(plan, options.values["output"])
	if err != nil {
		return err
	}
	if options.value("format", "human") == "json" {
		return writeJSON(c.Stdout, struct {
			PlanFile string     `json:"plan_file"`
			Plan     model.Plan `json:"plan"`
		}{PlanFile: path, Plan: plan})
	}
	fmt.Fprintf(c.Stdout, "Plan ID: %s\nPlan file: %s\nRecommended profile: %s\nApplied profile: %s\n", plan.ID, path, plan.RecommendedProfile, plan.AppliedProfile)
	if len(plan.Unresolved) > 0 {
		fmt.Fprintln(c.Stdout, "Unresolved decisions:")
		for _, item := range plan.Unresolved {
			fmt.Fprintf(c.Stdout, "  - %s\n", item)
		}
		fmt.Fprintln(c.Stdout, "No repository files were planned. Collect answers and run plan again.")
		return nil
	}
	fmt.Fprintln(c.Stdout, "Operations:")
	for _, operation := range plan.Operations {
		fmt.Fprintf(c.Stdout, "  - %s %s\n", operation.Action, operation.Path)
	}
	fmt.Fprintf(c.Stdout, "Apply only after approval: sam-harness apply --plan %s --accept %s\n", path, plan.ID)
	return nil
}

func (c *CLI) apply(args []string) error {
	options, err := parseOptions(args, map[string]bool{"plan": true, "accept": true, "format": true})
	if err != nil {
		return err
	}
	planPath := options.values["plan"]
	if planPath == "" {
		return errors.New("--plan is required")
	}
	plan, err := planner.Load(planPath)
	if err != nil {
		return err
	}
	changed, err := applyplan.Run(plan, options.values["accept"])
	if err != nil {
		return err
	}
	if options.value("format", "human") == "json" {
		return writeJSON(c.Stdout, map[string]any{"plan_id": plan.ID, "changed": changed})
	}
	fmt.Fprintf(c.Stdout, "Applied plan %s\n", plan.ID)
	if len(changed) == 0 {
		fmt.Fprintln(c.Stdout, "No files changed.")
	} else {
		for _, path := range changed {
			fmt.Fprintf(c.Stdout, "  - %s\n", path)
		}
	}
	return nil
}

func (c *CLI) check(args []string) error {
	options, err := parseOptions(args, map[string]bool{"format": true, "receipt": true})
	if err != nil {
		return err
	}
	writeReceipt := options.value("receipt", "true") != "false"
	report, receipt, runErr := checkrun.Run(options.path(), writeReceipt)
	if options.value("format", "human") == "json" {
		if err := writeJSON(c.Stdout, struct {
			Receipt string            `json:"receipt,omitempty"`
			Report  model.CheckReport `json:"report"`
		}{Receipt: receipt, Report: report}); err != nil {
			return err
		}
	} else {
		for _, result := range report.Results {
			status := "PASS"
			if !result.Passed {
				status = "FAIL"
			}
			fmt.Fprintf(c.Stdout, "%s %s (%s)\n", status, result.Name, result.Duration)
			if !result.Passed && result.Output != "" {
				fmt.Fprintln(c.Stdout, strings.TrimSpace(result.Output))
			}
		}
		if receipt != "" {
			fmt.Fprintf(c.Stdout, "Receipt: %s\n", receipt)
		}
	}
	return runErr
}

func (c *CLI) doctor(args []string) error {
	options, err := parseOptions(args, map[string]bool{"format": true})
	if err != nil {
		return err
	}
	report, err := doctor.Run(options.path())
	if err != nil {
		return err
	}
	if options.value("format", "human") == "json" {
		if err := writeJSON(c.Stdout, report); err != nil {
			return err
		}
	} else {
		for _, warning := range report.Warnings {
			fmt.Fprintf(c.Stdout, "WARN %s\n", warning)
		}
		for _, item := range report.Errors {
			fmt.Fprintf(c.Stdout, "FAIL %s\n", item)
		}
		if report.Passed {
			fmt.Fprintln(c.Stdout, "PASS sam-harness configuration is structurally healthy")
		}
	}
	if !report.Passed {
		return errors.New("doctor found structural errors")
	}
	return nil
}

func (c *CLI) upgrade(args []string) error {
	options, err := parseOptions(args, map[string]bool{"to": true, "output": true, "format": true})
	if err != nil {
		return err
	}
	target := options.values["to"]
	if target == "" {
		return errors.New("--to is required")
	}
	if strings.TrimPrefix(target, "v") != model.HarnessVersion {
		return fmt.Errorf("this CLI can plan only version %s", model.HarnessVersion)
	}
	result, err := scan.Run(options.path())
	if err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(result.Root, ".sam-harness", "config.yaml"))
	if err != nil {
		return err
	}
	answers := answersFromConfig(cfg)
	plan, err := planner.Create(result, cfg.Profile, answers)
	if err != nil {
		return err
	}
	path, err := planner.Save(plan, options.values["output"])
	if err != nil {
		return err
	}
	if options.value("format", "human") == "json" {
		return writeJSON(c.Stdout, struct {
			PlanFile string     `json:"plan_file"`
			Plan     model.Plan `json:"plan"`
		}{PlanFile: path, Plan: plan})
	}
	fmt.Fprintf(c.Stdout, "Upgrade plan ID: %s\nPlan file: %s\n", plan.ID, path)
	for _, operation := range plan.Operations {
		fmt.Fprintf(c.Stdout, "  - %s %s\n", operation.Action, operation.Path)
	}
	return nil
}

type parsedOptions struct {
	positionals []string
	values      map[string]string
}

func (o parsedOptions) path() string {
	if len(o.positionals) == 0 {
		return "."
	}
	return o.positionals[0]
}

func (o parsedOptions) value(key, fallback string) string {
	if value := o.values[key]; value != "" {
		return value
	}
	return fallback
}

func parseOptions(args []string, accepted map[string]bool) (parsedOptions, error) {
	result := parsedOptions{values: map[string]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			result.positionals = append(result.positionals, arg)
			continue
		}
		keyValue := strings.TrimPrefix(arg, "--")
		key := keyValue
		value := ""
		if index := strings.Index(keyValue, "="); index >= 0 {
			key = keyValue[:index]
			value = keyValue[index+1:]
		}
		if !accepted[key] {
			return parsedOptions{}, fmt.Errorf("unknown option --%s", key)
		}
		if value == "" {
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return parsedOptions{}, fmt.Errorf("--%s requires a value", key)
			}
			i++
			value = args[i]
		}
		result.values[key] = value
	}
	if len(result.positionals) > 1 {
		return parsedOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(result.positionals[1:], " "))
	}
	return result, nil
}

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func answersFromConfig(cfg model.Config) model.Answers {
	production := cfg.Profile == model.ProfileProduction || cfg.Profile == model.ProfileRegulated
	irreversible := false
	allowCI := cfg.CI.Managed
	actions := make([]string, 0, 6)
	for _, item := range []struct {
		name    string
		granted bool
	}{
		{"write_repository", cfg.Authority.WriteRepository},
		{"network", cfg.Authority.Network},
		{"commit", cfg.Authority.Commit},
		{"push", cfg.Authority.Push},
		{"release", cfg.Authority.Release},
		{"deploy", cfg.Authority.Deploy},
	} {
		if item.granted {
			actions = append(actions, item.name)
		}
	}
	sort.Strings(actions)
	commandOverrides := map[string]map[string][]string{}
	for _, stack := range cfg.Stacks {
		if len(stack.Commands) == 0 {
			continue
		}
		key := stack.Kind + ":" + stack.Path
		commandOverrides[key] = map[string][]string{}
		for gate, command := range stack.Commands {
			commandOverrides[key][gate] = append([]string(nil), command...)
		}
	}
	return model.Answers{
		Criticality:           cfg.Governance.Criticality,
		DataSensitivity:       cfg.Governance.DataSensitivity,
		DeploysToProduction:   &production,
		PersistentData:        &cfg.Migration.Required,
		IrreversibleActions:   &irreversible,
		DesignSourceOfTruth:   cfg.Design.SourceOfTruth,
		Approvers:             append([]string(nil), cfg.Governance.Approvers...),
		AllowCIChanges:        &allowCI,
		CIProviders:           append([]string(nil), cfg.CI.Providers...),
		AllowedActions:        &actions,
		CommandOverrides:      commandOverrides,
		CommandWaivers:        cloneStringMap(cfg.Governance.CommandWaivers),
		CISetupCommands:       cloneSetupCommands(cfg.CI.SetupCommands),
		CISetupWaivers:        cloneStringMap(cfg.CI.SetupWaivers),
		GitLabImage:           cfg.CI.GitLabImage,
		RiskAcceptance:        cfg.Governance.RiskAcceptance,
		ObservationWindow:     cfg.Release.ObservationWindow,
		RollbackOwner:         cfg.Release.RollbackOwner,
		ProductionEnvironment: cfg.Release.ProductionEnvironment,
	}
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func cloneSetupCommands(values map[string][]model.SetupCommand) map[string][]model.SetupCommand {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string][]model.SetupCommand, len(values))
	for provider, commands := range values {
		for _, command := range commands {
			cloned[provider] = append(cloned[provider], model.SetupCommand{
				Workdir: command.Workdir,
				Command: append([]string(nil), command.Command...),
			})
		}
	}
	return cloned
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func (c *CLI) usage() {
	fmt.Fprintln(c.Stdout, `sam-harness applies repository-specific controls through an approved plan.

Usage:
  sam-harness scan [path] [--format human|json]
  sam-harness plan [path] [--profile auto|baseline|production|regulated] [--answers file] [--output file]
  sam-harness apply --plan file --accept plan-id [--format human|json]
  sam-harness check [path] [--format human|json] [--receipt true|false]
  sam-harness doctor [path] [--format human|json]
  sam-harness upgrade [path] --to version [--output file]
  sam-harness version`)
}

func Main(args []string, stdout, stderr io.Writer) int {
	command := New(stdout, stderr)
	if err := command.Run(args); err != nil {
		fmt.Fprintf(stderr, "sam-harness: %v\n", err)
		return 1
	}
	return 0
}

func RunMain() {
	os.Exit(Main(os.Args[1:], os.Stdout, os.Stderr))
}
