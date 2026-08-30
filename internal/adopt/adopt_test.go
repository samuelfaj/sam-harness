package adopt

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestRunFixturesOnboardAutoGuided(t *testing.T) {
	t.Parallel()
	fixtures := []string{"typescript", "python", "go", "rust", "full-flow"}
	modes := []string{ModeOnboard, ModeAuto, ModeGuided}
	for _, fixture := range fixtures {
		for _, mode := range modes {
			fixture := fixture
			mode := mode
			t.Run(fixture+"/"+mode, func(t *testing.T) {
				t.Parallel()
				root, tmp := copyFixture(t, fixture)
				answersPath := filepath.Join(tmp, "answers.json")
				if fixture == "full-flow" {
					copyFile(t, filepath.Join(fixtureRoot(t), "full-flow", "answers.production.json"), answersPath)
				} else {
					writeBaselineAnswers(t, answersPath)
				}

				plan1 := filepath.Join(tmp, "plan-1.json")
				var stdout1 bytes.Buffer
				first, err := Run(Options{
					Root:        root,
					Mode:        mode,
					AnswersPath: answersPath,
					PlanOutput:  plan1,
					Locale:      "en-US",
					Stdout:      &stdout1,
				})
				if err != nil {
					t.Fatalf("first Run() error = %v", err)
				}
				if _, err := os.Stat(filepath.Join(root, ".sam-harness", "config.yaml")); !os.IsNotExist(err) {
					t.Fatal(".sam-harness/config.yaml existed before accept")
				}
				out := stdout1.String()
				if !strings.Contains(out, "Plan ID") || !strings.Contains(out, first.Plan.ID) {
					t.Fatalf("stdout missing Plan ID:\n%s", out)
				}
				if len(first.Plan.Operations) == 0 {
					t.Fatalf("plan operations empty; unresolved=%v", first.Plan.Unresolved)
				}
				for _, operation := range first.Plan.Operations {
					if !strings.Contains(out, operation.Action) || !strings.Contains(out, operation.Path) {
						t.Fatalf("stdout missing %s %s:\n%s", operation.Action, operation.Path, out)
					}
				}
				if !strings.Contains(out, "Coverage:") {
					t.Fatalf("stdout missing coverage:\n%s", out)
				}
				assertQuestionsSubset(t, first)
				assertCoverageStates(t, first.Coverage)

				if _, err := Run(Options{
					Root:         root,
					Mode:         mode,
					AnswersPath:  answersPath,
					PlanOutput:   plan1,
					AcceptPlanID: "not-the-plan-id",
					Stdout:       io.Discard,
				}); err == nil {
					t.Fatal("Run() applied a plan without the matching accept ID")
				}

				var stdout2 bytes.Buffer
				second, err := Run(Options{
					Root:         root,
					Mode:         mode,
					AnswersPath:  answersPath,
					PlanOutput:   plan1,
					AcceptPlanID: first.Plan.ID,
					Locale:       "en-US",
					Stdout:       &stdout2,
				})
				if err != nil {
					t.Fatalf("accepted Run() error = %v", err)
				}
				if len(second.Changed) == 0 {
					t.Fatal("accepted apply changed no files")
				}
				if _, err := os.Stat(filepath.Join(root, ".sam-harness", "config.yaml")); err != nil {
					t.Fatalf("config.yaml missing after accept: %v", err)
				}

				plan2 := filepath.Join(tmp, "plan-2.json")
				third, err := Run(Options{
					Root:        root,
					Mode:        mode,
					AnswersPath: answersPath,
					PlanOutput:  plan2,
					Stdout:      io.Discard,
				})
				if err != nil {
					t.Fatalf("second plan Run() error = %v", err)
				}
				var stdout4 bytes.Buffer
				fourth, err := Run(Options{
					Root:         root,
					Mode:         mode,
					AnswersPath:  answersPath,
					PlanOutput:   plan2,
					AcceptPlanID: third.Plan.ID,
					Stdout:       &stdout4,
				})
				if err != nil {
					t.Fatalf("second apply Run() error = %v", err)
				}
				if len(fourth.Changed) != 0 {
					t.Fatalf("second apply changed %v, want none", fourth.Changed)
				}
				if !strings.Contains(stdout4.String(), "No files changed") {
					t.Fatalf("second apply stdout = %q, want No files changed", stdout4.String())
				}
			})
		}
	}
}

func TestEqualCanonicalSameRepoAndAnswers(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "go")
	answersPath := filepath.Join(tmp, "answers.json")
	writeBaselineAnswers(t, answersPath)
	first, err := Run(Options{
		Root:        root,
		Mode:        ModeOnboard,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "plan-a.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Run(Options{
		Root:        root,
		Mode:        ModeAuto,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "plan-b.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Plan.ID == "" || second.Plan.ID == "" {
		t.Fatal("plan IDs are empty")
	}
	if !EqualCanonical(CanonicalFromPlan(first.Plan), CanonicalFromPlan(second.Plan)) {
		t.Fatalf("CanonicalFromPlan mismatch:\n%#v\n%#v", CanonicalFromPlan(first.Plan), CanonicalFromPlan(second.Plan))
	}
}

func TestAnswersRejectSecretsAndStayCredentialFree(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "go")
	secretPath := filepath.Join(tmp, "secret-answers.json")
	answers := baselineAnswers()
	answers.Approvers = []string{"ghp_xxx"}
	writeJSON(t, secretPath, answers)
	if _, err := Run(Options{
		Root:        root,
		Mode:        ModeOnboard,
		AnswersPath: secretPath,
		Stdout:      io.Discard,
	}); err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("Run() error = %v, want secret rejection", err)
	}

	cleanPath := filepath.Join(tmp, "answers.json")
	outputPath := filepath.Join(tmp, "answers-out.json")
	writeBaselineAnswers(t, cleanPath)
	report, err := Run(Options{
		Root:          root,
		Mode:          ModeOnboard,
		AnswersPath:   cleanPath,
		AnswersOutput: outputPath,
		PlanOutput:    filepath.Join(tmp, "plan.json"),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("ghp_")) || bytes.Contains(data, []byte("BEGIN ")) || bytes.Contains(data, []byte("sk-")) || bytes.Contains(data, []byte("AKIA")) || bytes.Contains(data, []byte("glpat-")) || bytes.Contains(data, []byte("xox")) {
		t.Fatalf("answers output contained secret-like values:\n%s", data)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded model.Answers
	if err := decoder.Decode(&decoded); err != nil {
		t.Fatalf("answers output is not DisallowUnknownFields-compatible: %v", err)
	}
	if report.AnswersPath != outputPath {
		t.Fatalf("AnswersPath = %q, want %q", report.AnswersPath, outputPath)
	}
}

func TestProductionFixtureLeavesObservationUnresolved(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "full-flow")
	src := filepath.Join(fixtureRoot(t), "full-flow", "answers.production.json")
	answers := loadAnswersFile(t, src)
	answers.ObservationWindow = ""
	answers.RollbackOwner = ""
	answers.ProductionEnvironment = ""
	answersPath := filepath.Join(tmp, "answers.json")
	writeJSON(t, answersPath, answers)

	report, err := Run(Options{
		Root:        root,
		Mode:        ModeOnboard,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "plan.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"observation_window", "rollback_owner", "production_environment"} {
		if !containsString(report.Plan.Unresolved, id) {
			t.Fatalf("Unresolved = %v, want %s", report.Plan.Unresolved, id)
		}
	}
	if len(report.Plan.Operations) != 0 {
		t.Fatalf("Operations = %#v, want none so argv is not invented", report.Plan.Operations)
	}
	if answers.ObservationWindow != "" || answers.RollbackOwner != "" || answers.ProductionEnvironment != "" {
		t.Fatal("test mutated answers in place")
	}
}

func TestCoverageUsesOnlyFourStates(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "go")
	report, err := Run(Options{
		Root:       root,
		Mode:       ModeGuided,
		PlanOutput: filepath.Join(tmp, "plan.json"),
		Stdout:     io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertCoverageStates(t, report.Coverage)
	found := false
	for _, item := range report.Coverage {
		if item.ID == "criticality" && item.State == StateHumanDecisionRequired {
			found = true
		}
	}
	if !found {
		t.Fatalf("Coverage = %#v, want unanswered criticality as %s", report.Coverage, StateHumanDecisionRequired)
	}
}

func TestCoverageUsesDetectedCIAndInstalledFreeze(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "go")
	if err := os.MkdirAll(filepath.Join(root, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".github", "workflows", "ci.yml"), []byte("name: ci\non: push\njobs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".sam-harness"), 0o755); err != nil {
		t.Fatal(err)
	}
	freezeJSON := []byte(`{"timezone":"UTC","start":"2026-12-20T00:00:00Z","end":"2026-12-27T00:00:00Z","branches":["main"],"environments":["production"],"owner":"release","kind":"production","exceptions":["P0"]}` + "\n")
	if err := os.WriteFile(filepath.Join(root, ".sam-harness", "freeze.json"), freezeJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	answersPath := filepath.Join(tmp, "answers.json")
	writeBaselineAnswers(t, answersPath)
	report, err := Run(Options{
		Root:        root,
		Mode:        ModeGuided,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "plan.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if coverageState(report.Coverage, "ci.provider") == StateMissingImplementable {
		t.Fatalf("ci.provider = %q after detecting GitHub workflows, want not %s", coverageState(report.Coverage, "ci.provider"), StateMissingImplementable)
	}
	if coverageState(report.Coverage, "ci.provider") != StateExternalProvider {
		t.Fatalf("ci.provider = %q, want %s", coverageState(report.Coverage, "ci.provider"), StateExternalProvider)
	}
	if coverageState(report.Coverage, "freeze") != StateExistingValidated {
		t.Fatalf("freeze = %q, want %s with installed policy", coverageState(report.Coverage, "freeze"), StateExistingValidated)
	}

	incomplete := []byte(`{"timezone":"UTC","start":"2026-12-20T00:00:00Z","end":"2026-12-27T00:00:00Z"}` + "\n")
	if err := os.WriteFile(filepath.Join(root, ".sam-harness", "freeze.json"), incomplete, 0o644); err != nil {
		t.Fatal(err)
	}
	incompleteReport, err := Run(Options{
		Root:        root,
		Mode:        ModeGuided,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "plan-incomplete-freeze.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if coverageState(incompleteReport.Coverage, "freeze") == StateExistingValidated {
		t.Fatal("incomplete freeze policy was reported existing-and-validated")
	}
}

func TestConfirmGuardDefaultYesRecordsCategory(t *testing.T) {
	t.Parallel()
	answers := setAnswerField(model.Answers{}, "confirm_guard_default:unit", "yes")
	if len(answers.ConfirmGuardDefaults) != 1 || answers.ConfirmGuardDefaults[0] != "unit" {
		t.Fatalf("ConfirmGuardDefaults = %#v", answers.ConfirmGuardDefaults)
	}
	answers = setAnswerField(answers, "confirm_runtime_reviewers", "yes")
	if answers.ConfirmRuntimeReviewers == nil || !*answers.ConfirmRuntimeReviewers {
		t.Fatal("confirm_runtime_reviewers was not recorded")
	}
}

func TestImplementGuardFormatOnGoFixture(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "go")
	answersPath := filepath.Join(tmp, "answers.json")
	writeBaselineAnswers(t, answersPath)
	harnessPlan := filepath.Join(tmp, "harness.json")
	proposed, err := Run(Options{Root: root, Mode: ModeGuided, AnswersPath: answersPath, PlanOutput: harnessPlan, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{Root: root, Mode: ModeGuided, AnswersPath: answersPath, PlanOutput: harnessPlan, AcceptPlanID: proposed.Plan.ID, Stdout: io.Discard}); err != nil {
		t.Fatal(err)
	}
	taskPlan := filepath.Join(tmp, "format.json")
	task, err := Run(Options{Root: root, Mode: ModeGuided, AnswersPath: answersPath, PlanOutput: taskPlan, ImplementControl: "guard:format", Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if task.Task == nil || task.Task.ControlID != "guard:format" {
		t.Fatalf("Task = %#v", task.Task)
	}
	applied, err := Run(Options{Root: root, Mode: ModeGuided, AnswersPath: answersPath, PlanOutput: taskPlan, ImplementControl: "guard:format", AcceptPlanID: task.Plan.ID, Stdout: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Changed) == 0 {
		t.Fatal("format implementation changed no files")
	}
}

func TestImplementGuardSecurityOnGoFixture(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "go")
	answersPath := filepath.Join(tmp, "answers.json")
	writeBaselineAnswers(t, answersPath)
	unrelatedPath := filepath.Join(root, "UNRELATED.txt")
	unrelated := []byte("leave this file unchanged\n")
	if err := os.WriteFile(unrelatedPath, unrelated, 0o644); err != nil {
		t.Fatal(err)
	}

	harnessPlan := filepath.Join(tmp, "harness.json")
	proposed, err := Run(Options{
		Root:        root,
		Mode:        ModeGuided,
		AnswersPath: answersPath,
		PlanOutput:  harnessPlan,
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{
		Root:         root,
		Mode:         ModeGuided,
		AnswersPath:  answersPath,
		PlanOutput:   harnessPlan,
		AcceptPlanID: proposed.Plan.ID,
		Stdout:       io.Discard,
	}); err != nil {
		t.Fatal(err)
	}
	harnessID := proposed.Plan.ID

	taskPlan := filepath.Join(tmp, "security.json")
	task, err := Run(Options{
		Root:             root,
		Mode:             ModeGuided,
		AnswersPath:      answersPath,
		PlanOutput:       taskPlan,
		ImplementControl: "guard:security",
		Stdout:           io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if task.Plan.ID == "" || task.Plan.ID == harnessID {
		t.Fatalf("bounded task plan ID %q did not differ from harness plan ID %q", task.Plan.ID, harnessID)
	}
	if task.Task == nil || task.Task.ControlID != "guard:security" {
		t.Fatalf("Task = %#v, want guard:security", task.Task)
	}
	if _, err := os.Stat(filepath.Join(root, ".sam-harness", "guards", "security.sh")); !os.IsNotExist(err) {
		t.Fatal("security script was written before the bounded task was accepted")
	}

	if _, err := Run(Options{
		Root:             root,
		Mode:             ModeGuided,
		AnswersPath:      answersPath,
		PlanOutput:       taskPlan,
		ImplementControl: "guard:security",
		AcceptPlanID:     harnessID,
		Stdout:           io.Discard,
	}); err == nil {
		t.Fatal("old harness plan ID was accepted for the bounded task")
	}

	applied, err := Run(Options{
		Root:             root,
		Mode:             ModeGuided,
		AnswersPath:      answersPath,
		PlanOutput:       taskPlan,
		ImplementControl: "guard:security",
		AcceptPlanID:     task.Plan.ID,
		Stdout:           io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := coverageState(applied.Coverage, "guard:security")
	if got != StateExistingValidated {
		t.Fatalf("guard:security state = %q, want %s", got, StateExistingValidated)
	}
	script := filepath.Join(root, ".sam-harness", "guards", "security.sh")
	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("security script mode = %s, want executable", info.Mode())
	}
	cmd := exec.Command(script)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("security script exit = %v\n%s", err, output)
	}
	gotUnrelated, err := os.ReadFile(unrelatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotUnrelated, unrelated) {
		t.Fatalf("UNRELATED.txt mutated:\n%s", gotUnrelated)
	}
	if err := os.WriteFile(filepath.Join(root, "leaked.env"), []byte("TOKEN=ghp_exampletokenvalue0001\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	afterLeak, err := Run(Options{
		Root:        root,
		Mode:        ModeGuided,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "after-leak.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if coverageState(afterLeak.Coverage, "guard:security") == StateExistingValidated {
		t.Fatal("guard:security stayed existing-and-validated after planted secret-like content")
	}
	legacy := []byte("#!/bin/sh\ntest -f go.mod -o -f package.json -o -f pyproject.toml -o -f Cargo.toml\n")
	if err := os.WriteFile(filepath.Join(root, ".sam-harness", "guards", "security.sh"), legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyReport, err := Run(Options{
		Root:        root,
		Mode:        ModeGuided,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "legacy-guard.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if coverageState(legacyReport.Coverage, "guard:security") == StateExistingValidated {
		t.Fatal("legacy go.mod-only security script validated a tree with planted secret-like content")
	}
}

func TestWaiverRequiresRiskAndReason(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "go")
	_, err := Run(Options{
		Root:          root,
		Mode:          ModeGuided,
		WaiverControl: "guard:security",
		PlanOutput:    filepath.Join(tmp, "plan.json"),
		Stdout:        io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "explicit risk and reason required") {
		t.Fatalf("Run() error = %v, want explicit risk and reason required", err)
	}
	_, err = Run(Options{
		Root:          root,
		Mode:          ModeGuided,
		WaiverControl: "guard:security",
		WaiverRisk:    "medium",
		PlanOutput:    filepath.Join(tmp, "plan.json"),
		Stdout:        io.Discard,
	})
	if err == nil || !strings.Contains(err.Error(), "explicit risk and reason required") {
		t.Fatalf("Run() error = %v, want explicit risk and reason required", err)
	}

	answersPath := filepath.Join(tmp, "answers.json")
	writeBaselineAnswers(t, answersPath)
	before, err := Run(Options{
		Root:        root,
		Mode:        ModeGuided,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "before-waiver.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if coverageState(before.Coverage, "guard:security") != StateMissingImplementable {
		t.Fatalf("pre-waiver guard:security = %q, want %s", coverageState(before.Coverage, "guard:security"), StateMissingImplementable)
	}
	waived, err := Run(Options{
		Root:          root,
		Mode:          ModeGuided,
		AnswersPath:   answersPath,
		WaiverControl: "guard:security",
		WaiverRisk:    "medium",
		WaiverReason:  "fixture has no network or credential surface",
		WaiverOwner:   "fixture-owner",
		PlanOutput:    filepath.Join(tmp, "waived.json"),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if coverageState(waived.Coverage, "guard:security") == StateMissingImplementable {
		t.Fatal("complete waiver left guard:security missing-but-implementable")
	}
	if coverageState(waived.Coverage, "guard:security") != StateHumanDecisionRequired {
		t.Fatalf("waived guard:security = %q, want %s", coverageState(waived.Coverage, "guard:security"), StateHumanDecisionRequired)
	}
	var waivedItem CoverageItem
	for _, item := range waived.Coverage {
		if item.ID == "guard:security" {
			waivedItem = item
		}
	}
	if !strings.Contains(waivedItem.Reason, "explicit waiver") || !strings.Contains(waivedItem.Reason, "fixture-owner") {
		t.Fatalf("waiver reason = %q", waivedItem.Reason)
	}
}

func TestSetAnswerFieldRecordsAgentHostLoginAndCommitConvention(t *testing.T) {
	t.Parallel()
	answers := setAnswerField(model.Answers{}, "ci_agent_host", "claude-code")
	answers = setAnswerField(answers, "ci_agent_login", "api_key ANTHROPIC_API_KEY ANTHROPIC_API_KEY")
	answers = setAnswerField(answers, "standardize_commits", "true")
	if answers.CIAgentRuntime == nil || answers.CIAgentRuntime.Host != model.AgentHostClaudeCode {
		t.Fatalf("agent host = %#v", answers.CIAgentRuntime)
	}
	if answers.CIAgentRuntime.LoginMethod != model.AgentLoginAPIKey || answers.CIAgentRuntime.LoginEnvironment != "ANTHROPIC_API_KEY" || answers.CIAgentRuntime.LoginSecret != "ANTHROPIC_API_KEY" {
		t.Fatalf("agent login = %#v", answers.CIAgentRuntime)
	}
	other := setAnswerField(model.Answers{}, "ci_agent_host", "other:cursor")
	if other.CIAgentRuntime.Host != model.AgentHostOther || other.CIAgentRuntime.HostOther != "cursor" {
		t.Fatalf("other host = %#v", other.CIAgentRuntime)
	}
	if answers.StandardizeCommits == nil || !*answers.StandardizeCommits {
		t.Fatalf("standardize_commits = %#v", answers.StandardizeCommits)
	}
}

func TestResumeDoesNotReaskCompletedIDs(t *testing.T) {
	t.Parallel()
	root, tmp := copyFixture(t, "go")
	answersPath := filepath.Join(tmp, "answers.json")
	writeJSON(t, answersPath, model.Answers{Criticality: "low"})
	first, err := Run(Options{
		Root:        root,
		Mode:        ModeOnboard,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "plan-partial.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if questionID(first.Questions, "criticality") {
		t.Fatalf("Questions = %#v, re-asked completed criticality", first.Questions)
	}
	if !questionID(first.Questions, "data_sensitivity") {
		t.Fatalf("Questions = %#v, want data_sensitivity still asked", first.Questions)
	}

	writeBaselineAnswers(t, answersPath)
	second, err := Run(Options{
		Root:        root,
		Mode:        ModeOnboard,
		AnswersPath: answersPath,
		PlanOutput:  filepath.Join(tmp, "plan-full.json"),
		Stdout:      io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"criticality", "data_sensitivity", "deploys_to_production", "approvers", "allowed_actions"} {
		if questionID(second.Questions, id) {
			t.Fatalf("Questions = %#v, re-asked completed %s", second.Questions, id)
		}
	}
}

func TestTLocales(t *testing.T) {
	t.Parallel()
	keys := []string{
		"question.criticality", "impact.criticality", "default.criticality",
		"question.data_sensitivity", "impact.data_sensitivity", "default.data_sensitivity",
		"question.deploys_to_production", "impact.deploys_to_production", "default.deploys_to_production",
		"question.approvers", "impact.approvers", "default.approvers",
		"question.allowed_actions", "impact.allowed_actions", "default.allowed_actions",
		"question.ci_agent_host", "impact.ci_agent_host",
		"question.ci_agent_login", "impact.ci_agent_login",
		"question.standardize_commits", "impact.standardize_commits", "default.standardize_commits",
		"finish.source", "finish.local_checks", "finish.remote", "finish.ci", "finish.artifact",
		"finish.deployment", "finish.live_observation", "finish.freeze", "finish.production_stability",
	}
	for _, locale := range []string{"en-US", "pt-BR", "es"} {
		for _, key := range keys {
			if got := T(locale, key); got == "" {
				t.Fatalf("T(%q, %q) is empty", locale, key)
			}
		}
	}
	if got, want := T("fr", "question.criticality"), T("en-US", "question.criticality"); got != want {
		t.Fatalf("unknown locale T() = %q, want en-US %q", got, want)
	}
}

func assertQuestionsSubset(t *testing.T, report Report) {
	t.Helper()
	unresolved := map[string]bool{}
	for _, id := range report.Plan.Unresolved {
		unresolved[id] = true
	}
	for _, question := range report.Questions {
		if !unresolved[question.ID] {
			t.Fatalf("question %q is not in unresolved %v", question.ID, report.Plan.Unresolved)
		}
		if strings.TrimSpace(question.Prompt) == "" || strings.TrimSpace(question.Impact) == "" {
			t.Fatalf("question %q missing prompt or impact: %#v", question.ID, question)
		}
	}
}

func assertCoverageStates(t *testing.T, items []CoverageItem) {
	t.Helper()
	allowed := map[string]bool{
		StateExistingValidated:     true,
		StateMissingImplementable:  true,
		StateHumanDecisionRequired: true,
		StateExternalProvider:      true,
	}
	for _, item := range items {
		if !allowed[item.State] {
			t.Fatalf("coverage %s has unknown state %q", item.ID, item.State)
		}
	}
}

func coverageState(items []CoverageItem, id string) string {
	for _, item := range items {
		if item.ID == id {
			return item.State
		}
	}
	return ""
}

func questionID(questions []Question, id string) bool {
	for _, question := range questions {
		if question.ID == id {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func baselineAnswers() model.Answers {
	falsehood := false
	actions := []string{"write_repository"}
	return model.Answers{
		Criticality:         "low",
		DataSensitivity:     "public",
		DeploysToProduction: &falsehood,
		PersistentData:      &falsehood,
		IrreversibleActions: &falsehood,
		DesignSourceOfTruth: "repository",
		Approvers:           []string{"owner"},
		AllowCIChanges:      &falsehood,
		AllowedActions:      &actions,
		StandardizeCommits:  &falsehood,
	}
}

func writeBaselineAnswers(t *testing.T, path string) {
	t.Helper()
	writeJSON(t, path, baselineAnswers())
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadAnswersFile(t *testing.T, path string) model.Answers {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var answers model.Answers
	if err := decoder.Decode(&answers); err != nil {
		t.Fatal(err)
	}
	return answers
}

func copyFixture(t *testing.T, name string) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	root := filepath.Join(tmp, "repo")
	copyTree(t, filepath.Join(fixtureRoot(t), name), root)
	initGit(t, root)
	return root, tmp
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" && path != src {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func initGit(t *testing.T, root string) {
	t.Helper()
	run(t, root, "git", "init", "-b", "main")
	run(t, root, "git", "config", "user.email", "test@example.com")
	run(t, root, "git", "config", "user.name", "Test")
	run(t, root, "git", "add", ".")
	run(t, root, "git", "commit", "-m", "fixture")
}

func run(t *testing.T, root, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures"))
}
