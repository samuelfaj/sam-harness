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
	"time"

	"github.com/samuelfaj/sam-harness/internal/adopt"
	applyplan "github.com/samuelfaj/sam-harness/internal/apply"
	"github.com/samuelfaj/sam-harness/internal/bootstrap"
	checkrun "github.com/samuelfaj/sam-harness/internal/check"
	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/doctor"
	"github.com/samuelfaj/sam-harness/internal/freeze"
	"github.com/samuelfaj/sam-harness/internal/model"
	pipelinerun "github.com/samuelfaj/sam-harness/internal/pipeline"
	"github.com/samuelfaj/sam-harness/internal/planner"
	"github.com/samuelfaj/sam-harness/internal/scan"
	"github.com/samuelfaj/sam-harness/internal/stage"
)

type CLI struct {
	Stdout             io.Writer
	Stderr             io.Writer
	BootstrapTransport bootstrap.Transport
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
	case "pipeline":
		return c.pipeline(args[1:])
	case "repair":
		return c.repair(args[1:])
	case "doctor":
		return c.doctor(args[1:])
	case "upgrade":
		return c.upgrade(args[1:])
	case "onboard":
		return c.onboard(args[1:])
	case "adopt":
		return c.adopt(args[1:])
	case "bootstrap":
		return c.bootstrap(args[1:])
	case "stage":
		return c.stage(args[1:])
	case "freeze":
		return c.freeze(args[1:])
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
	writeReceipt, err := boolOption(options, "receipt", true)
	if err != nil {
		return err
	}
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
			if result.Skipped {
				status = "SKIP"
			} else if !result.Passed {
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

func (c *CLI) pipeline(args []string) error {
	options, err := parseOptions(args, map[string]bool{"config": true, "format": true, "phase": true, "receipt": true, "review-base": true, "review-base-sha": true, "review-head-sha": true})
	if err != nil {
		return err
	}
	phase := model.Phase(options.values["phase"])
	if !phase.Valid() {
		return errors.New("--phase must be one of static, test, review, artifact, staging, production, observe, rollback, migration, or all")
	}
	writeReceipt, err := boolOption(options, "receipt", true)
	if err != nil {
		return err
	}
	report, receipt, runErr := pipelinerun.RunWithOptions(options.path(), phase, writeReceipt, pipelinerun.RunOptions{
		ConfigPath:    options.values["config"],
		ReviewBase:    options.values["review-base"],
		ReviewBaseSHA: options.values["review-base-sha"],
		ReviewHeadSHA: options.values["review-head-sha"],
	})
	if options.value("format", "human") == "json" {
		if err := writeJSON(c.Stdout, struct {
			Receipt string              `json:"receipt,omitempty"`
			Report  pipelinerun.Receipt `json:"report"`
		}{Receipt: receipt, Report: report}); err != nil {
			return err
		}
	} else {
		writePipelineReport(c.Stdout, report, receipt)
	}
	return runErr
}

func (c *CLI) repair(args []string) error {
	options, err := parseOptions(args, map[string]bool{"config": true, "format": true, "receipt": true, "receipt-output": true})
	if err != nil {
		return err
	}
	failedReceipt := options.values["receipt"]
	if failedReceipt == "" {
		return errors.New("--receipt is required")
	}
	writeReceipt, err := boolOption(options, "receipt-output", true)
	if err != nil {
		return err
	}
	report, receipt, runErr := pipelinerun.RepairWithConfig(options.path(), options.values["config"], failedReceipt, writeReceipt)
	if options.value("format", "human") == "json" {
		if err := writeJSON(c.Stdout, struct {
			Receipt string              `json:"receipt,omitempty"`
			Report  pipelinerun.Receipt `json:"report"`
		}{Receipt: receipt, Report: report}); err != nil {
			return err
		}
	} else {
		writePipelineReport(c.Stdout, report, receipt)
	}
	return runErr
}

func writePipelineReport(writer io.Writer, report pipelinerun.Receipt, receipt string) {
	status := strings.ToUpper(string(report.Status))
	if status == "" {
		status = "FAILED"
	}
	label := string(report.Phase)
	if label == "" {
		label = report.Kind
	}
	fmt.Fprintf(writer, "%s %s\n", status, label)
	for _, result := range report.Commands {
		commandStatus := "PASS"
		if result.Skipped {
			commandStatus = "SKIP"
		} else if !result.Passed {
			commandStatus = "FAIL"
		}
		fmt.Fprintf(writer, "%s %s (%s)\n", commandStatus, result.Name, result.Duration)
		if !result.Passed && result.Output != "" {
			fmt.Fprintln(writer, strings.TrimSpace(result.Output))
		}
	}
	for _, finding := range report.Findings {
		fmt.Fprintf(writer, "%s %s: %s\n", finding.Severity, finding.Role, finding.Summary)
	}
	if report.Artifact != nil {
		fmt.Fprintf(writer, "Artifact: %s sha256:%s\n", report.Artifact.Path, report.Artifact.SHA256)
	}
	if receipt != "" {
		fmt.Fprintf(writer, "Receipt: %s\n", receipt)
	}
}

func boolOption(options parsedOptions, key string, fallback bool) (bool, error) {
	value := options.values[key]
	if value == "" {
		return fallback, nil
	}
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("--%s must be true or false", key)
	}
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

func (c *CLI) onboard(args []string) error {
	return c.runAdopt(adopt.ModeOnboard, args, false)
}

func (c *CLI) adopt(args []string) error {
	return c.runAdopt("", args, true)
}

func (c *CLI) runAdopt(mode string, args []string, requireMode bool) error {
	options, err := parseOptionsExt(args, map[string]bool{
		"answers": true, "answers-output": true, "locale": true, "accept": true,
		"output": true, "format": true, "interactive": true, "implement": true,
		"waiver-control": true, "waiver-risk": true, "waiver-reason": true, "waiver-owner": true,
		"auto": true, "guided": true,
	}, map[string]bool{"auto": true, "guided": true, "interactive": true})
	if err != nil {
		return err
	}
	if requireMode {
		auto := options.values["auto"] == "true"
		guided := options.values["guided"] == "true"
		if auto == guided {
			return errors.New("adopt requires exactly one of --auto or --guided")
		}
		if auto {
			mode = adopt.ModeAuto
		} else {
			mode = adopt.ModeGuided
		}
	}
	interactive, err := boolOption(options, "interactive", false)
	if err != nil {
		return err
	}
	var stdin io.Reader
	if interactive {
		stdin = os.Stdin
	}
	report, runErr := adopt.Run(adopt.Options{
		Root:             options.path(),
		Mode:             mode,
		AnswersPath:      options.values["answers"],
		AnswersOutput:    options.values["answers-output"],
		Locale:           options.value("locale", "en-US"),
		AcceptPlanID:     options.values["accept"],
		PlanOutput:       options.values["output"],
		ImplementControl: options.values["implement"],
		WaiverControl:    options.values["waiver-control"],
		WaiverRisk:       options.values["waiver-risk"],
		WaiverReason:     options.values["waiver-reason"],
		WaiverOwner:      options.values["waiver-owner"],
		Stdin:            stdin,
		Stdout:           c.Stdout,
		Interactive:      interactive,
	})
	if options.value("format", "human") == "json" {
		if err := writeJSON(c.Stdout, report); err != nil {
			return err
		}
	}
	return runErr
}

func (c *CLI) bootstrap(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return errors.New("bootstrap provider is required: github or gitlab")
	}
	provider := bootstrap.Provider(args[0])
	if provider != "github" && provider != "gitlab" {
		return fmt.Errorf("unknown bootstrap provider %q", args[0])
	}
	options, err := parseOptions(args[1:], map[string]bool{"accept": true, "format": true, "plan": true})
	if err != nil {
		return err
	}
	scanResult, err := scan.Run(options.path())
	if err != nil {
		return err
	}
	desired := bootstrap.RemoteState{
		DefaultBranch:         "main",
		ControlPlaneCheck:     "sam-harness/trusted-review",
		ProtectedEnvironments: []string{"production"},
		AllowSquash:           true,
		RequiredChecks:        []string{freeze.RequiredCheckName()},
		JobTexts: map[string]string{
			"pull_request":  "sam-harness pipeline --phase static",
			"merge_request": "sam-harness pipeline --phase static",
		},
	}
	plan, err := bootstrap.CreatePlan(provider, scanResult.Fingerprint, desired)
	if err != nil {
		return err
	}
	if options.value("format", "human") == "json" && options.values["accept"] == "" {
		return writeJSON(c.Stdout, plan)
	}
	fmt.Fprintf(c.Stdout, "Plan ID: %s\nProvider: %s\n", plan.ID, plan.Provider)
	fmt.Fprintln(c.Stdout, "Mutations:")
	for _, mutation := range plan.Mutations {
		fmt.Fprintf(c.Stdout, "  - %s %s\n", mutation.Field, mutation.Value)
	}
	if options.values["accept"] == "" {
		fmt.Fprintf(c.Stdout, "Apply only after approval: sam-harness bootstrap %s --accept %s\n", provider, plan.ID)
		return nil
	}
	if c.BootstrapTransport == nil {
		return errors.New("bootstrap apply requires an injected provider transport")
	}
	result, err := bootstrap.Apply(plan, options.values["accept"], c.BootstrapTransport)
	if err != nil {
		return err
	}
	if options.value("format", "human") == "json" {
		return writeJSON(c.Stdout, result)
	}
	fmt.Fprintf(c.Stdout, "Ready: %t\n", result.Ready)
	for _, mismatch := range result.Mismatches {
		fmt.Fprintf(c.Stdout, "  mismatch %s\n", mismatch)
	}
	if len(result.Applied) == 0 {
		fmt.Fprintln(c.Stdout, "No provider mutations applied.")
	}
	if !result.Ready {
		return fmt.Errorf("bootstrap not ready: %s", strings.Join(result.Mismatches, "; "))
	}
	return nil
}

func (c *CLI) stage(args []string) error {
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		return errors.New("stage name is required")
	}
	name := args[0]
	options, err := parseOptions(args[1:], map[string]bool{"input": true, "format": true, "plan": true})
	if err != nil {
		return err
	}
	inputPath := options.values["input"]
	if inputPath == "" {
		return errors.New("--input is required")
	}
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	var req stage.Request
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return fmt.Errorf("parse stage input: %w", err)
	}
	req.Stage = name
	if strings.TrimSpace(req.Root) == "" {
		req.Root = options.path()
	}
	if planPath := options.values["plan"]; planPath != "" {
		plan, err := planner.Load(planPath)
		if err != nil {
			return err
		}
		if req.PlanID != plan.ID || req.Fingerprint != plan.Fingerprint {
			return fmt.Errorf("stage request is not bound to approved plan %s", plan.ID)
		}
	}
	receipt, err := stage.Run(req)
	if err != nil {
		return err
	}
	if options.value("format", "human") == "json" {
		return writeJSON(c.Stdout, receipt)
	}
	fmt.Fprintf(c.Stdout, "Stage: %s\nPlan ID: %s\nFingerprint: %s\nProof: %t\n", receipt.Stage, receipt.PlanID, receipt.Fingerprint, receipt.Proof)
	if receipt.Summary != "" {
		fmt.Fprintf(c.Stdout, "Summary: %s\n", receipt.Summary)
	}
	return nil
}

func (c *CLI) freeze(args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return errors.New("freeze check is required")
	}
	options, err := parseOptionsExt(args[1:], map[string]bool{
		"policy": true, "now": true, "exception": true, "head": true, "base": true,
		"branch": true, "kind": true, "format": true, "scheduled-release": true, "workflow-can-disable": true,
	}, map[string]bool{"scheduled-release": true, "workflow-can-disable": true})
	if err != nil {
		return err
	}
	policyPath := options.values["policy"]
	if policyPath == "" {
		return errors.New("--policy is required")
	}
	data, err := os.ReadFile(policyPath)
	if err != nil {
		return err
	}
	var policy freeze.Policy
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return fmt.Errorf("parse freeze policy: %w", err)
	}
	now := time.Time{}
	if options.values["now"] != "" {
		now, err = time.Parse(time.RFC3339, options.values["now"])
		if err != nil {
			return fmt.Errorf("--now must be RFC3339: %w", err)
		}
	}
	req := freeze.CheckRequest{
		HeadSHA:            options.values["head"],
		BaseSHA:            options.values["base"],
		Branch:             options.value("branch", "main"),
		Kind:               options.value("kind", freeze.KindFeature),
		ScheduledRelease:   options.values["scheduled-release"] == "true",
		WorkflowCanDisable: options.values["workflow-can-disable"] == "true",
	}
	if options.values["exception"] != "" {
		exData, err := os.ReadFile(options.values["exception"])
		if err != nil {
			return err
		}
		var exception freeze.Exception
		exDecoder := json.NewDecoder(strings.NewReader(string(exData)))
		exDecoder.DisallowUnknownFields()
		if err := exDecoder.Decode(&exception); err != nil {
			return fmt.Errorf("parse freeze exception: %w", err)
		}
		req.Exception = &exception
	}
	if err := freeze.Evaluate(policy, req, now); err != nil {
		return err
	}
	if options.value("format", "human") == "json" {
		return writeJSON(c.Stdout, map[string]any{"allowed": true, "check": freeze.RequiredCheckName()})
	}
	fmt.Fprintf(c.Stdout, "PASS %s\n", freeze.RequiredCheckName())
	return nil
}

func (c *CLI) upgrade(args []string) error {
	options, err := parseOptions(args, map[string]bool{"to": true, "output": true, "format": true, "answers": true})
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
	providedAnswers, err := planner.LoadAnswers(options.values["answers"])
	if err != nil {
		return err
	}
	answers = mergeAnswers(answers, providedAnswers)
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
	if len(plan.Unresolved) > 0 {
		fmt.Fprintln(c.Stdout, "Unresolved decisions:")
		for _, item := range plan.Unresolved {
			fmt.Fprintf(c.Stdout, "  - %s\n", item)
		}
		fmt.Fprintln(c.Stdout, "No repository files were planned. Collect answers and run upgrade again.")
		return nil
	}
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
	return parseOptionsExt(args, accepted, nil)
}

func parseOptionsExt(args []string, accepted, flags map[string]bool) (parsedOptions, error) {
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
		if !accepted[key] && (flags == nil || !flags[key]) {
			return parsedOptions{}, fmt.Errorf("unknown option --%s", key)
		}
		if flags[key] && value == "" {
			if i+1 < len(args) && (args[i+1] == "true" || args[i+1] == "false") {
				i++
				result.values[key] = args[i]
				continue
			}
			result.values[key] = "true"
			continue
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
		Criticality:             cfg.Governance.Criticality,
		DataSensitivity:         cfg.Governance.DataSensitivity,
		DeploysToProduction:     &production,
		PersistentData:          &cfg.Migration.Required,
		IrreversibleActions:     &irreversible,
		DesignSourceOfTruth:     cfg.Design.SourceOfTruth,
		Approvers:               append([]string(nil), cfg.Governance.Approvers...),
		AllowCIChanges:          &allowCI,
		CIProviders:             append([]string(nil), cfg.CI.Providers...),
		AllowedActions:          &actions,
		CommandOverrides:        commandOverrides,
		CommandWaivers:          cloneStringMap(cfg.Governance.CommandWaivers),
		CISetupCommands:         cloneSetupCommands(cfg.CI.SetupCommands),
		CISetupWaivers:          cloneStringMap(cfg.CI.SetupWaivers),
		CISecretBindings:        cloneCISecretBindings(cfg.CI.SecretBindings),
		CISecretWaivers:         cloneStringMap(cfg.CI.SecretWaivers),
		AgentSecretEnvironments: cloneStringMap(cfg.CI.AgentSecretEnvironments),
		AgentControlPlanes:      cloneAgentControlPlanes(cfg.CI.AgentControlPlanes),
		GitLabImage:             cfg.CI.GitLabImage,
		RiskAcceptance:          cfg.Governance.RiskAcceptance,
		ObservationWindow:       cfg.Release.ObservationWindow,
		RollbackOwner:           cfg.Release.RollbackOwner,
		ProductionEnvironment:   cfg.Release.ProductionEnvironment,
		Workflow:                cloneWorkflow(cfg.Workflow),
	}
}

func cloneWorkflow(workflow *model.WorkflowConfig) *model.WorkflowConfig {
	if workflow == nil {
		return nil
	}
	cloned := *workflow
	cloned.StaticGuards = cloneGuardSet(workflow.StaticGuards)
	cloned.TestGuards = cloneGuardSet(workflow.TestGuards)
	cloned.Reviewers = append([]model.ReviewerConfig(nil), workflow.Reviewers...)
	for index := range cloned.Reviewers {
		cloned.Reviewers[index].Command = append([]string(nil), workflow.Reviewers[index].Command...)
	}
	cloned.Correction.Command = append([]string(nil), workflow.Correction.Command...)
	cloned.Artifact.Build.Command = append([]string(nil), workflow.Artifact.Build.Command...)
	cloned.Artifact.SBOM.Command = append([]string(nil), workflow.Artifact.SBOM.Command...)
	cloned.Artifact.Provenance.Command = append([]string(nil), workflow.Artifact.Provenance.Command...)
	cloned.Deployment.Staging.Command = append([]string(nil), workflow.Deployment.Staging.Command...)
	cloned.Deployment.Production.Command = append([]string(nil), workflow.Deployment.Production.Command...)
	cloned.Deployment.Rollback.Command = append([]string(nil), workflow.Deployment.Rollback.Command...)
	cloned.Deployment.HealthChecks = cloneCommandSpecs(workflow.Deployment.HealthChecks)
	cloned.Deployment.ObservationChecks = cloneCommandSpecs(workflow.Deployment.ObservationChecks)
	cloned.Deployment.CanaryPercentages = append([]int(nil), workflow.Deployment.CanaryPercentages...)
	cloned.Migration = cloneCommandSpecs(workflow.Migration)
	return &cloned
}

func cloneGuardSet(guards model.GuardSet) model.GuardSet {
	cloned := model.GuardSet{Commands: make(map[string]model.CommandSpec, len(guards.Commands)), Waivers: cloneStringMap(guards.Waivers)}
	for category, command := range guards.Commands {
		command.Command = append([]string(nil), command.Command...)
		cloned.Commands[category] = command
	}
	if len(cloned.Commands) == 0 {
		cloned.Commands = nil
	}
	return cloned
}

func cloneCommandSpecs(commands []model.CommandSpec) []model.CommandSpec {
	cloned := append([]model.CommandSpec(nil), commands...)
	for index := range cloned {
		cloned[index].Command = append([]string(nil), commands[index].Command...)
	}
	return cloned
}

func mergeAnswers(base, provided model.Answers) model.Answers {
	if provided.Criticality != "" {
		base.Criticality = provided.Criticality
	}
	if provided.DataSensitivity != "" {
		base.DataSensitivity = provided.DataSensitivity
	}
	if provided.DeploysToProduction != nil {
		base.DeploysToProduction = provided.DeploysToProduction
	}
	if provided.PersistentData != nil {
		base.PersistentData = provided.PersistentData
	}
	if provided.IrreversibleActions != nil {
		base.IrreversibleActions = provided.IrreversibleActions
	}
	if provided.DesignSourceOfTruth != "" {
		base.DesignSourceOfTruth = provided.DesignSourceOfTruth
	}
	if provided.Approvers != nil {
		base.Approvers = append([]string(nil), provided.Approvers...)
	}
	if provided.AllowCIChanges != nil {
		base.AllowCIChanges = provided.AllowCIChanges
	}
	if provided.CIProviders != nil {
		base.CIProviders = append([]string(nil), provided.CIProviders...)
	}
	if provided.AllowedActions != nil {
		actions := append([]string(nil), (*provided.AllowedActions)...)
		base.AllowedActions = &actions
	}
	if provided.CommandOverrides != nil {
		base.CommandOverrides = provided.CommandOverrides
	}
	if provided.CommandWaivers != nil {
		base.CommandWaivers = provided.CommandWaivers
	}
	if provided.CISetupCommands != nil {
		base.CISetupCommands = provided.CISetupCommands
	}
	if provided.CISetupWaivers != nil {
		base.CISetupWaivers = provided.CISetupWaivers
	}
	if provided.CISecretBindings != nil {
		base.CISecretBindings = cloneCISecretBindings(provided.CISecretBindings)
	}
	if provided.CISecretWaivers != nil {
		base.CISecretWaivers = cloneStringMap(provided.CISecretWaivers)
	}
	if provided.AgentSecretEnvironments != nil {
		base.AgentSecretEnvironments = cloneStringMap(provided.AgentSecretEnvironments)
	}
	if provided.AgentControlPlanes != nil {
		base.AgentControlPlanes = cloneAgentControlPlanes(provided.AgentControlPlanes)
	}
	if provided.GitLabImage != "" {
		base.GitLabImage = provided.GitLabImage
	}
	if provided.RiskAcceptance != "" {
		base.RiskAcceptance = provided.RiskAcceptance
	}
	if provided.ObservationWindow != "" {
		base.ObservationWindow = provided.ObservationWindow
	}
	if provided.RollbackOwner != "" {
		base.RollbackOwner = provided.RollbackOwner
	}
	if provided.ProductionEnvironment != "" {
		base.ProductionEnvironment = provided.ProductionEnvironment
	}
	if provided.Workflow != nil {
		base.Workflow = cloneWorkflow(provided.Workflow)
	}
	return base
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

func cloneAgentControlPlanes(values map[string]model.AgentControlPlane) map[string]model.AgentControlPlane {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]model.AgentControlPlane, len(values))
	for provider, controlPlane := range values {
		cloned[provider] = controlPlane
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

func cloneCISecretBindings(values map[string][]model.CISecretBinding) map[string][]model.CISecretBinding {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string][]model.CISecretBinding, len(values))
	for provider, bindings := range values {
		cloned[provider] = append([]model.CISecretBinding(nil), bindings...)
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
  sam-harness onboard [path] [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id] [--output file] [--format human|json] [--interactive true|false]
  sam-harness adopt --auto [path] [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id] [--output file] [--format human|json]
  sam-harness adopt --guided [path] [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id] [--implement control] [--waiver-control id --waiver-risk text --waiver-reason text] [--output file] [--format human|json]
  sam-harness bootstrap github [path] [--accept plan-id] [--format human|json]
  sam-harness bootstrap gitlab [path] [--accept plan-id] [--format human|json]
  sam-harness stage classifier|context|planning|implementation|review|repair --input file [--format human|json]
  sam-harness freeze check [path] [--policy file] [--now rfc3339] [--exception file] [--head sha] [--base sha] [--branch name] [--kind feature] [--scheduled-release true|false]
  sam-harness check [path] [--format human|json] [--receipt true|false]
  sam-harness pipeline [path] [--config absolute-or-contained-file] [--review-base absolute-directory --review-base-sha hex --review-head-sha hex] --phase static|test|review|artifact|staging|production|observe|rollback|migration|all [--receipt true|false]
  sam-harness repair [path] [--config absolute-or-contained-file] --receipt file [--receipt-output true|false]
  sam-harness doctor [path] [--format human|json]
  sam-harness upgrade [path] --to version [--answers file] [--output file]
  sam-harness version

For pipeline and repair, --config defaults to <path>/.sam-harness/config.yaml.
An override must be an absolute regular file or a relative regular file contained by <path>.
Secret-bearing review requires all three review-base flags; both SHAs are verified against Git HEAD before and after review.`)
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
