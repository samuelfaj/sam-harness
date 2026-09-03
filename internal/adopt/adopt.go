package adopt

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/samuelfaj/sam-harness/internal/apply"
	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/freeze"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/planner"
	"github.com/samuelfaj/sam-harness/internal/repo"
	"github.com/samuelfaj/sam-harness/internal/scan"
)

const (
	ModeOnboard = "onboard"
	ModeAuto    = "auto"
	ModeGuided  = "guided"
)

const (
	StateExistingValidated     = "existing-and-validated"
	StateMissingImplementable  = "missing-but-implementable"
	StateHumanDecisionRequired = "human-decision-required"
	StateExternalProvider      = "external-provider-required"
)

const (
	securityControl    = "guard:security"
	securityScript     = ".sam-harness/guards/security.sh"
	configRel          = ".sam-harness/config.yaml"
	securityScriptBody = "#!/bin/sh\n" +
		"pattern=$(printf '%s_|%s-|sk-|BEGIN %s|%s|xox' ghp glpat PRIVATE AKIA)\n" +
		"if find . \\( -name .git -o -name node_modules -o -name vendor -o -name .sam-harness -o -name dist -o -name target \\) -prune -o -type f -exec grep -I -E -q \"$pattern\" {} \\; -print | grep -q .\n" +
		"then\n" +
		"  echo \"sam-harness: secret-like content found\" >&2\n" +
		"  exit 1\n" +
		"fi\n" +
		"exit 0\n"
)

type Options struct {
	Root             string
	Mode             string
	AnswersPath      string
	AnswersOutput    string
	Locale           string
	AcceptPlanID     string
	PlanOutput       string
	ImplementControl string
	WaiverControl    string
	WaiverRisk       string
	WaiverReason     string
	WaiverOwner      string
	Stdin            io.Reader
	Stdout           io.Writer
	Interactive      bool
}

type Question struct {
	ID          string `json:"id"`
	Prompt      string `json:"prompt"`
	Impact      string `json:"impact"`
	SafeDefault string `json:"safe_default"`
}

type CoverageItem struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Reason string `json:"reason"`
}

type BoundedTask struct {
	ControlID      string     `json:"control_id"`
	Acceptance     []string   `json:"acceptance"`
	AffectedPaths  []string   `json:"affected_paths"`
	Commands       [][]string `json:"commands"`
	Tests          [][]string `json:"tests"`
	MaxFiles       int        `json:"max_files"`
	MaxLines       int        `json:"max_lines"`
	StopConditions []string   `json:"stop_conditions"`
}

type Canonical struct {
	Profile    model.Profile `json:"profile"`
	Unresolved []string      `json:"unresolved"`
	Operations []string      `json:"operations"`
}

type Report struct {
	Mode         string            `json:"mode"`
	Locale       string            `json:"locale"`
	Plan         model.Plan        `json:"plan"`
	Questions    []Question        `json:"questions"`
	Coverage     []CoverageItem    `json:"coverage"`
	Task         *BoundedTask      `json:"task,omitempty"`
	Changed      []string          `json:"changed"`
	AnswersPath  string            `json:"answers_path,omitempty"`
	PlanFile     string            `json:"plan_file,omitempty"`
	FinishReport map[string]string `json:"finish_report"`
}

func Run(opts Options) (Report, error) {
	locale := opts.Locale
	if locale == "" {
		locale = "en-US"
	}
	report := Report{
		Mode:         opts.Mode,
		Locale:       locale,
		AnswersPath:  opts.AnswersPath,
		FinishReport: finishReport(locale, false),
	}
	stdout := writer(opts.Stdout)

	switch opts.Mode {
	case ModeOnboard, ModeAuto, ModeGuided:
	default:
		return report, fmt.Errorf("unknown mode %q", opts.Mode)
	}
	if strings.TrimSpace(opts.WaiverControl) != "" && (strings.TrimSpace(opts.WaiverRisk) == "" || strings.TrimSpace(opts.WaiverReason) == "") {
		return report, fmt.Errorf("explicit risk and reason required")
	}
	if strings.TrimSpace(opts.WaiverControl) == "" && (strings.TrimSpace(opts.WaiverRisk) != "" || strings.TrimSpace(opts.WaiverReason) != "" || strings.TrimSpace(opts.WaiverOwner) != "") {
		return report, fmt.Errorf("waiver-control is required")
	}
	if strings.TrimSpace(opts.AcceptPlanID) != "" && strings.TrimSpace(opts.WaiverControl) != "" {
		return report, fmt.Errorf("waiver cannot be combined with apply of an accepted plan")
	}
	implement := strings.TrimSpace(opts.ImplementControl)
	if implement != "" && opts.Mode != ModeGuided {
		return report, fmt.Errorf("ImplementControl requires mode %s", ModeGuided)
	}
	if strings.TrimSpace(opts.WaiverControl) != "" && opts.Mode != ModeGuided {
		return report, fmt.Errorf("waiver requires mode %s", ModeGuided)
	}
	if implement != "" && !knownImplementControl(implement) {
		return report, fmt.Errorf("unknown control %q", implement)
	}

	scanResult, err := scan.Run(opts.Root)
	if err != nil {
		return report, err
	}
	answers, err := loadAnswers(opts.AnswersPath)
	if err != nil {
		return report, err
	}

	interviewPlan, err := planner.Create(scanResult, model.ProfileAuto, answers)
	if err != nil {
		return report, err
	}
	questions := questionsFor(locale, interviewPlan.Unresolved)
	questions = append(questions, confirmationQuestions(locale, interviewPlan)...)
	if opts.Interactive && opts.Stdin != nil {
		answers = applyInterview(opts.Stdin, stdout, questions, answers)
		interviewPlan, err = planner.Create(scanResult, model.ProfileAuto, answers)
		if err != nil {
			return report, err
		}
		questions = questionsFor(locale, interviewPlan.Unresolved)
		questions = append(questions, confirmationQuestions(locale, interviewPlan)...)
	}

	var plan model.Plan
	var planFile string
	var task *BoundedTask
	if implement != "" {
		plan, planFile, task, err = resolveImplementPlan(opts, scanResult, interviewPlan, answers, implement)
		if err != nil {
			return report, err
		}
	} else {
		plan, planFile, err = resolvePlan(opts, interviewPlan)
		if err != nil {
			return report, err
		}
		questions = questionsFor(locale, plan.Unresolved)
	}

	coverage := applyWaiver(buildCoverage(scanResult, plan, answers), opts)
	printProposal(stdout, plan, answers, coverage)
	if opts.AnswersOutput != "" {
		if err := writeAnswers(scanResult.Root, opts.AnswersOutput, answers); err != nil {
			return report, err
		}
		report.AnswersPath = opts.AnswersOutput
	}

	report.Plan = plan
	report.Questions = questions
	report.Coverage = coverage
	report.Task = task
	report.PlanFile = planFile
	report.FinishReport = finishReport(locale, configExists(scanResult.Root))

	if opts.AcceptPlanID == "" {
		return report, nil
	}
	if opts.AcceptPlanID != plan.ID {
		return report, fmt.Errorf("apply requires matching plan ID %s", plan.ID)
	}
	if implement == "" && len(plan.Unresolved) > 0 {
		return report, fmt.Errorf("plan has unresolved decisions: %s", strings.Join(plan.Unresolved, ", "))
	}

	changed, err := apply.Run(plan, opts.AcceptPlanID)
	if err != nil {
		return report, err
	}
	report.Changed = changed
	if len(changed) == 0 {
		fmt.Fprintln(stdout, "No files changed.")
	}
	if implement == securityControl {
		scriptPath := filepath.Join(plan.Root, filepath.FromSlash(securityScript))
		if err := os.Chmod(scriptPath, 0o755); err != nil && !os.IsNotExist(err) {
			return report, err
		}
	}

	scanResult, err = scan.Run(plan.Root)
	if err != nil {
		return report, err
	}
	report.Coverage = applyWaiver(buildCoverage(scanResult, plan, answers), opts)
	report.FinishReport = finishReport(locale, configExists(plan.Root))
	return report, nil
}

func CanonicalFromPlan(plan model.Plan) Canonical {
	unresolved := append([]string(nil), plan.Unresolved...)
	sort.Strings(unresolved)
	operations := make([]string, 0, len(plan.Operations))
	for _, operation := range plan.Operations {
		operations = append(operations, operation.Action+" "+operation.Path)
	}
	sort.Strings(operations)
	return Canonical{
		Profile:    plan.AppliedProfile,
		Unresolved: unresolved,
		Operations: operations,
	}
}

func EqualCanonical(a, b Canonical) bool {
	if a.Profile != b.Profile {
		return false
	}
	return equalSorted(a.Unresolved, b.Unresolved) && equalSorted(a.Operations, b.Operations)
}

func resolvePlan(opts Options, created model.Plan) (model.Plan, string, error) {
	if opts.PlanOutput != "" && !fileMissing(opts.PlanOutput) {
		plan, err := planner.Load(opts.PlanOutput)
		return plan, opts.PlanOutput, err
	}
	path, err := planner.Save(created, opts.PlanOutput)
	if err != nil {
		return model.Plan{}, "", err
	}
	return created, path, nil
}

func resolveSecurityPlan(opts Options, scanResult model.ScanResult, interview model.Plan, answers model.Answers) (model.Plan, string, *BoundedTask, error) {
	task := securityTask()
	if opts.PlanOutput != "" && !fileMissing(opts.PlanOutput) {
		plan, err := planner.Load(opts.PlanOutput)
		if err != nil {
			return model.Plan{}, "", nil, err
		}
		return plan, opts.PlanOutput, task, nil
	}
	plan, err := buildSecurityPlan(scanResult, interview, answers)
	if err != nil {
		return model.Plan{}, "", nil, err
	}
	path, err := planner.Save(plan, opts.PlanOutput)
	if err != nil {
		return model.Plan{}, "", nil, err
	}
	return plan, path, task, nil
}

func buildSecurityPlan(scanResult model.ScanResult, interview model.Plan, answers model.Answers) (model.Plan, error) {
	configPath := filepath.Join(scanResult.Root, filepath.FromSlash(configRel))
	if _, err := os.Stat(configPath); err != nil {
		return model.Plan{}, fmt.Errorf("implementing %s requires an installed harness config", securityControl)
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return model.Plan{}, err
	}
	existingConfig, err := os.ReadFile(configPath)
	if err != nil {
		return model.Plan{}, err
	}
	configContent := string(existingConfig)
	if !hasSecurityGate(cfg) {
		cfg.Gates = append(cfg.Gates, model.Gate{
			Name:     "security",
			Stage:    "local",
			Phase:    model.PhaseStatic,
			Workdir:  ".",
			Command:  []string{"sh", securityScript},
			Required: true,
		})
		data, err := config.Marshal(cfg)
		if err != nil {
			return model.Plan{}, err
		}
		configContent = string(data)
	}

	operations := []model.Operation{
		fileOperation(scanResult.Root, securityScript, securityScriptBody),
		fileOperation(scanResult.Root, configRel, configContent),
	}
	sort.Slice(operations, func(i, j int) bool { return operations[i].Path < operations[j].Path })

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
	if plan.Answers.AllowedActions == nil && interview.Answers.AllowedActions != nil {
		plan.Answers.AllowedActions = interview.Answers.AllowedActions
	}
	plan.ID = planner.CalculateID(plan)
	return plan, nil
}

func securityTask() *BoundedTask {
	return &BoundedTask{
		ControlID: securityControl,
		Acceptance: []string{
			"the security script exists",
			"the security script is executable",
			"the security script exits 0",
			"a required local static security gate runs the script",
		},
		AffectedPaths: []string{configRel, securityScript},
		Commands:      [][]string{{"sh", securityScript}},
		Tests:         [][]string{{"sh", securityScript}},
		MaxFiles:      2,
		MaxLines:      80,
		StopConditions: []string{
			"repository fingerprint changed after planning",
			"security script exits non-zero",
		},
	}
}

func fileOperation(root, rel, content string) model.Operation {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	sum := sha256.Sum256([]byte(content))
	action := "create"
	existing, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err == nil {
		if string(existing) == content {
			action = "noop"
		} else {
			action = "update"
		}
	}
	return model.Operation{
		Path:          rel,
		Action:        action,
		Content:       content,
		ContentSHA256: hex.EncodeToString(sum[:]),
	}
}

func hasSecurityGate(cfg model.Config) bool {
	for _, gate := range cfg.Gates {
		if gate.Name == "security" && gate.Stage == "local" && gate.Required && gate.Phase == model.PhaseStatic && commandMentions(gate.Command, securityScript) {
			return true
		}
	}
	return false
}

func buildCoverage(scanResult model.ScanResult, plan model.Plan, answers model.Answers) []CoverageItem {
	root := scanResult.Root
	if root == "" {
		root = plan.Root
	}
	cfg, cfgErr := config.Load(filepath.Join(root, filepath.FromSlash(configRel)))
	items := []CoverageItem{
		harnessCoverage(root, cfgErr),
		securityCoverage(root, cfg, cfgErr),
		testCoverage(cfg, cfgErr),
		ciProviderCoverage(scanResult),
		{
			ID:     "branch_protection",
			State:  StateExternalProvider,
			Reason: "branch protection must be read back from the provider",
		},
		freezeCoverage(root),
	}
	seen := map[string]bool{}
	for _, item := range items {
		seen[item.ID] = true
	}
	for _, id := range plan.Unresolved {
		if seen[id] {
			continue
		}
		items = append(items, CoverageItem{
			ID:     id,
			State:  StateHumanDecisionRequired,
			Reason: "scan and plan cannot prove this fact",
		})
		seen[id] = true
	}
	if answers.Criticality == "" && !seen["criticality"] {
		items = append(items, CoverageItem{
			ID:     "criticality",
			State:  StateHumanDecisionRequired,
			Reason: "scan and plan cannot prove this fact",
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func harnessCoverage(root string, cfgErr error) CoverageItem {
	if cfgErr == nil {
		return CoverageItem{
			ID:     "harness.config",
			State:  StateExistingValidated,
			Reason: "config.yaml loaded and validated",
		}
	}
	if !configExists(root) {
		return CoverageItem{
			ID:     "harness.config",
			State:  StateMissingImplementable,
			Reason: "no validated .sam-harness/config.yaml",
		}
	}
	return CoverageItem{
		ID:     "harness.config",
		State:  StateHumanDecisionRequired,
		Reason: "config.yaml exists but is not valid",
	}
}

func securityCoverage(root string, cfg model.Config, cfgErr error) CoverageItem {
	script := filepath.Join(root, filepath.FromSlash(securityScript))
	_, scriptErr := os.Stat(script)
	if scriptErr == nil && cfgErr == nil && hasSecurityGate(cfg) && commandPasses(root, []string{"sh", securityScript}) && !secretLikePresent(root) {
		return CoverageItem{
			ID:     securityControl,
			State:  StateExistingValidated,
			Reason: "security script exits 0 under the required local gate",
		}
	}
	return CoverageItem{
		ID:     securityControl,
		State:  StateMissingImplementable,
		Reason: "no local security guard is installed",
	}
}

func testCoverage(cfg model.Config, cfgErr error) CoverageItem {
	if cfgErr == nil {
		for _, gate := range cfg.Gates {
			if gate.Phase == model.PhaseTest && gate.Required && len(gate.Command) > 0 {
				return CoverageItem{
					ID:     "guard:test",
					State:  StateExistingValidated,
					Reason: "a required local test gate is present",
				}
			}
		}
	}
	return CoverageItem{
		ID:     "guard:test",
		State:  StateMissingImplementable,
		Reason: "no test guard is installed",
	}
}

func ciProviderCoverage(scanResult model.ScanResult) CoverageItem {
	if len(scanResult.CIProviders) == 0 {
		return CoverageItem{
			ID:     "ci.provider",
			State:  StateMissingImplementable,
			Reason: "CI provider settings are not proven from local files",
		}
	}
	return CoverageItem{
		ID:     "ci.provider",
		State:  StateExternalProvider,
		Reason: "CI provider " + strings.Join(scanResult.CIProviders, ",") + " detected; remote readback is required",
	}
}

func freezeCoverage(root string) CoverageItem {
	path := filepath.Join(root, ".sam-harness", "freeze.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return CoverageItem{
			ID:     "freeze",
			State:  StateMissingImplementable,
			Reason: "no freeze policy is installed",
		}
	}
	var policy freeze.Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return CoverageItem{
			ID:     "freeze",
			State:  StateHumanDecisionRequired,
			Reason: "freeze policy exists but is not valid JSON",
		}
	}
	if strings.TrimSpace(policy.Owner) == "" || strings.TrimSpace(policy.Kind) == "" || len(policy.Branches) == 0 || len(policy.Environments) == 0 || len(policy.Exceptions) == 0 {
		return CoverageItem{
			ID:     "freeze",
			State:  StateHumanDecisionRequired,
			Reason: "freeze policy exists but is missing owner, kind, branches, environments, or exceptions",
		}
	}
	if _, err := freeze.Active(policy, time.Date(2026, 12, 22, 12, 0, 0, 0, time.UTC)); err != nil {
		return CoverageItem{
			ID:     "freeze",
			State:  StateHumanDecisionRequired,
			Reason: "freeze policy exists but is not executable",
		}
	}
	return CoverageItem{
		ID:     "freeze",
		State:  StateExistingValidated,
		Reason: "freeze policy loaded and parsed",
	}
}

func applyWaiver(items []CoverageItem, opts Options) []CoverageItem {
	control := strings.TrimSpace(opts.WaiverControl)
	if control == "" {
		return items
	}
	owner := strings.TrimSpace(opts.WaiverOwner)
	if owner == "" {
		owner = "unspecified"
	}
	reason := fmt.Sprintf("explicit waiver: risk %s; %s; owner %s", strings.TrimSpace(opts.WaiverRisk), strings.TrimSpace(opts.WaiverReason), owner)
	found := false
	out := append([]CoverageItem(nil), items...)
	for i, item := range out {
		if item.ID == control {
			out[i].State = StateHumanDecisionRequired
			out[i].Reason = reason
			found = true
		}
	}
	if !found {
		out = append(out, CoverageItem{ID: control, State: StateHumanDecisionRequired, Reason: reason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func commandMentions(command []string, needle string) bool {
	for _, argument := range command {
		if argument == needle || strings.Contains(argument, needle) {
			return true
		}
	}
	return false
}

func commandPasses(root string, command []string) bool {
	if len(command) == 0 {
		return false
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = root
	return cmd.Run() == nil
}

func secretLikePresent(root string) bool {
	skip := map[string]bool{
		".git": true, "node_modules": true, "vendor": true, ".sam-harness": true,
		"dist": true, "target": true, ".venv": true, "__pycache__": true,
	}
	found := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if entry.IsDir() {
			if path != root && skip[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if bytesContainSecret(data) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

func bytesContainSecret(data []byte) bool {
	text := string(data)
	for _, marker := range []string{"ghp_", "glpat-", "AKIA", "xoxb-", "xoxp-", "BEGIN RSA PRIVATE", "BEGIN PRIVATE KEY", "BEGIN OPENSSH PRIVATE"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func confirmationQuestions(locale string, plan model.Plan) []Question {
	var questions []Question
	categories := make([]string, 0, len(plan.ProposedGuardDefaults))
	for category := range plan.ProposedGuardDefaults {
		categories = append(categories, category)
	}
	sort.Strings(categories)
	for _, category := range categories {
		spec := plan.ProposedGuardDefaults[category]
		questions = append(questions, Question{
			ID:          "confirm_guard_default:" + category,
			Prompt:      fmt.Sprintf("Confirm detected %s command `%s`?", category, strings.Join(spec.Command, " ")),
			Impact:      "Yes records the scan-detected argv. No leaves the category unresolved until you supply a command or waiver.",
			SafeDefault: "yes",
		})
	}
	if len(plan.ProposedBrowserCommand) > 0 {
		questions = append(questions, Question{
			ID:          "confirm_guard_default:browser",
			Prompt:      fmt.Sprintf("Confirm detected browser command `%s`?", strings.Join(plan.ProposedBrowserCommand, " ")),
			Impact:      "A user interface needs executable browser proof or an explicit waiver.",
			SafeDefault: "yes",
		})
	}
	if len(plan.ProposedAccessibilityCommand) > 0 {
		questions = append(questions, Question{
			ID:          "confirm_guard_default:accessibility",
			Prompt:      fmt.Sprintf("Confirm detected accessibility command `%s`?", strings.Join(plan.ProposedAccessibilityCommand, " ")),
			Impact:      "A user interface needs executable accessibility proof or an explicit waiver.",
			SafeDefault: "yes",
		})
	}
	if plan.ProposedReviewerHost != "" && len(plan.ProposedReviewerCommand) > 0 {
		questions = append(questions, Question{
			ID:          "confirm_runtime_reviewers",
			Prompt:      fmt.Sprintf("Use default %s reviewer commands for all six roles?", plan.ProposedReviewerHost),
			Impact:      "Yes installs the host recipe with filesystem_read_only attestation still required.",
			SafeDefault: "yes",
		})
	}
	_ = locale
	return questions
}

func questionsFor(locale string, unresolved []string) []Question {
	questions := make([]Question, 0, len(unresolved))
	for _, id := range unresolved {
		prompt := T(locale, "question."+id)
		if prompt == "" {
			prompt = strings.ReplaceAll(id, "_", " ")
		}
		impact := T(locale, "impact."+id)
		if impact == "" {
			impact = "This decision remains unresolved until you answer. The scan cannot invent it."
		}
		questions = append(questions, Question{
			ID:          id,
			Prompt:      prompt,
			Impact:      impact,
			SafeDefault: T(locale, "default."+id),
		})
	}
	return questions
}

func printProposal(w io.Writer, plan model.Plan, answers model.Answers, coverage []CoverageItem) {
	fmt.Fprintf(w, "Plan ID: %s\n", plan.ID)
	fmt.Fprintln(w, "Operations:")
	if len(plan.Operations) == 0 {
		fmt.Fprintln(w, "  - none")
	}
	for _, operation := range plan.Operations {
		fmt.Fprintf(w, "  - %s %s\n", operation.Action, operation.Path)
	}
	fmt.Fprint(w, "Authority:")
	if answers.AllowedActions != nil {
		fmt.Fprintf(w, " %s", strings.Join(*answers.AllowedActions, ", "))
	} else if plan.Answers.AllowedActions != nil {
		fmt.Fprintf(w, " %s", strings.Join(*plan.Answers.AllowedActions, ", "))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Gates:")
	if len(plan.Unresolved) == 0 {
		fmt.Fprintln(w, "  - none unresolved")
	}
	for _, item := range plan.Unresolved {
		fmt.Fprintf(w, "  - unresolved %s\n", item)
	}
	fmt.Fprintln(w, "Unresolved:")
	if len(plan.Unresolved) == 0 {
		fmt.Fprintln(w, "  - none")
	}
	for _, item := range plan.Unresolved {
		fmt.Fprintf(w, "  - %s\n", item)
	}
	fmt.Fprintln(w, "Coverage:")
	for _, item := range coverage {
		fmt.Fprintf(w, "  - %s %s %s\n", item.ID, item.State, item.Reason)
	}
	fmt.Fprintf(w, "Suggested CI stages (happy path): %s\n", model.DeliveryHappyPath)
	fmt.Fprintf(w, "Exception path: %s\n", model.DeliveryExceptionPath)
	fmt.Fprintln(w, "After apply, unify redundant host CI: keep sam-harness-* jobs; remove host jobs that only repeat those gates.")
}

func finishReport(locale string, sourcePresent bool) map[string]string {
	keys := []string{
		"source", "local_checks", "remote", "ci", "artifact",
		"deployment", "live_observation", "freeze", "production_stability",
	}
	report := make(map[string]string, len(keys))
	for _, key := range keys {
		if key == "source" && sourcePresent {
			report[key] = T(locale, "finish.source.present")
			continue
		}
		report[key] = T(locale, "finish."+key+".unproven")
	}
	return report
}

func loadAnswers(path string) (model.Answers, error) {
	answers, err := planner.LoadAnswers(path)
	if err != nil {
		return model.Answers{}, err
	}
	if path == "" {
		return answers, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Answers{}, err
	}
	if err := rejectSecrets(data); err != nil {
		return model.Answers{}, err
	}
	return answers, nil
}

func writeAnswers(root, path string, answers model.Answers) error {
	if err := validateOutsideRoot(root, path); err != nil {
		return err
	}
	data, err := json.MarshalIndent(answers, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := rejectSecrets(data); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func rejectSecrets(data []byte) error {
	text := string(data)
	if strings.Contains(text, "BEGIN ") {
		return fmt.Errorf("answers must not contain secret values")
	}
	for _, pattern := range []string{"sk-", "ghp_", "glpat-", "xox", "AKIA"} {
		if strings.Contains(text, pattern) {
			return fmt.Errorf("answers must not contain secret values")
		}
	}
	return nil
}

func validateOutsideRoot(root, path string) error {
	if path == "" {
		return nil
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	parentReal, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return fmt.Errorf("resolve answers output directory: %w", err)
	}
	target := filepath.Join(parentReal, filepath.Base(abs))
	relative, err := filepath.Rel(rootReal, target)
	if err != nil {
		return err
	}
	if relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		return fmt.Errorf("answers output must stay outside the repository: %s", path)
	}
	return nil
}

func applyInterview(stdin io.Reader, stdout io.Writer, questions []Question, answers model.Answers) model.Answers {
	scanner := bufio.NewScanner(stdin)
	for _, question := range questions {
		fmt.Fprintf(stdout, "%s\n%s\n", question.Prompt, question.Impact)
		if !scanner.Scan() {
			break
		}
		value := strings.TrimSpace(scanner.Text())
		if value == "" {
			continue
		}
		answers = setAnswerField(answers, question.ID, value)
	}
	return answers
}

func setAnswerField(answers model.Answers, id, value string) model.Answers {
	switch id {
	case "criticality":
		answers.Criticality = value
	case "data_sensitivity":
		answers.DataSensitivity = value
	case "deploys_to_production":
		answers.DeploysToProduction = parseBoolPtr(value)
	case "persistent_data":
		answers.PersistentData = parseBoolPtr(value)
	case "irreversible_actions":
		answers.IrreversibleActions = parseBoolPtr(value)
	case "allow_ci_changes":
		answers.AllowCIChanges = parseBoolPtr(value)
	case "approvers":
		answers.Approvers = splitComma(value)
	case "allowed_actions":
		actions := splitComma(value)
		answers.AllowedActions = &actions
	case "design_source_of_truth":
		answers.DesignSourceOfTruth = value
	case "observation_window":
		answers.ObservationWindow = value
	case "rollback_owner":
		answers.RollbackOwner = value
	case "production_environment":
		answers.ProductionEnvironment = value
	case "risk_acceptance":
		answers.RiskAcceptance = value
	case "ci_agent_host":
		if answers.CIAgentRuntime == nil {
			answers.CIAgentRuntime = &model.CIAgentRuntime{}
		}
		if host, other, ok := model.ParseAgentHost(value); ok {
			answers.CIAgentRuntime.Host = host
			answers.CIAgentRuntime.HostOther = other
		} else {
			answers.CIAgentRuntime.Host = value
			answers.CIAgentRuntime.HostOther = ""
		}
	case "ci_agent_login":
		if answers.CIAgentRuntime == nil {
			answers.CIAgentRuntime = &model.CIAgentRuntime{}
		}
		if method, env, secret, reason, ok := model.ParseAgentLogin(value); ok {
			answers.CIAgentRuntime.LoginMethod = method
			answers.CIAgentRuntime.LoginEnvironment = env
			answers.CIAgentRuntime.LoginSecret = secret
			answers.CIAgentRuntime.LoginReason = reason
		} else {
			answers.CIAgentRuntime.LoginMethod = value
			answers.CIAgentRuntime.LoginEnvironment = ""
			answers.CIAgentRuntime.LoginSecret = ""
			answers.CIAgentRuntime.LoginReason = ""
		}
	case "standardize_commits":
		answers.StandardizeCommits = parseBoolPtr(value)
	case "confirm_runtime_reviewers":
		answers.ConfirmRuntimeReviewers = parseBoolPtr(value)
	default:
		if category, ok := strings.CutPrefix(id, "confirm_guard_default:"); ok {
			if parsed := parseBoolPtr(value); parsed != nil && *parsed {
				answers.ConfirmGuardDefaults = appendUnique(answers.ConfirmGuardDefaults, category)
			}
		}
	}
	return answers
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func parseBoolPtr(value string) *bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1":
		v := true
		return &v
	case "false", "no", "0":
		v := false
		return &v
	default:
		if parsed, err := strconv.ParseBool(value); err == nil {
			return &parsed
		}
	}
	return nil
}

func splitComma(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func fileMissing(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

func configExists(root string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(configRel)))
	return err == nil
}

func writer(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

func equalSorted(a, b []string) bool {
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
