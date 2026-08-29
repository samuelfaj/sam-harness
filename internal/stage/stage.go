package stage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

const (
	Classifier     = "classifier"
	Context        = "context"
	Planning       = "planning"
	Implementation = "implementation"
	Review         = "review"
	Repair         = "repair"
)

const disclosureLimit = 32

type Request struct {
	Stage         string          `json:"stage"`
	PlanID        string          `json:"plan_id"`
	Fingerprint   string          `json:"fingerprint"`
	Root          string          `json:"root"`
	Risk          string          `json:"risk,omitempty"`
	AffectedPaths []string        `json:"affected_paths,omitempty"`
	Authority     model.Authority `json:"authority"`
	Input         json.RawMessage `json:"input"`
}

type Receipt struct {
	Stage         string          `json:"stage"`
	PlanID        string          `json:"plan_id"`
	Fingerprint   string          `json:"fingerprint"`
	StartedAt     time.Time       `json:"started_at"`
	FinishedAt    time.Time       `json:"finished_at"`
	Risk          string          `json:"risk"`
	AffectedPaths []string        `json:"affected_paths"`
	Authority     model.Authority `json:"authority"`
	Output        json.RawMessage `json:"output"`
	Summary       string          `json:"summary,omitempty"`
	Proof         bool            `json:"proof"` // always false unless a later check receipt is attached; summaries are never proof
}

type classifierInput struct {
	Paths []string        `json:"paths"`
	Hints json.RawMessage `json:"hints"`
	Risk  string          `json:"risk"`
}

type classifierOutput struct {
	Risk          string   `json:"risk"`
	AffectedPaths []string `json:"affected_paths"`
}

type contextOutput struct {
	Instructions []string `json:"instructions"`
	Skills       []string `json:"skills"`
	Source       []string `json:"source"`
	Contracts    []string `json:"contracts"`
	Tests        []string `json:"tests"`
	Schemas      []string `json:"schemas"`
	Evidence     []string `json:"evidence"`
}

type planningInput struct {
	Task               string   `json:"task"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	AffectedFiles      []string `json:"affected_files"`
	Commands           []string `json:"commands"`
	Risks              []string `json:"risks"`
	Mitigations        []string `json:"mitigations"`
	Budgets            []string `json:"budgets"`
	StopConditions     []string `json:"stop_conditions"`
	ProofRequirements  []string `json:"proof_requirements"`
}

type planningOutput struct {
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	AffectedFiles      []string `json:"affected_files"`
	Commands           []string `json:"commands"`
	Risks              []string `json:"risks"`
	Mitigations        []string `json:"mitigations"`
	Budgets            []string `json:"budgets"`
	StopConditions     []string `json:"stop_conditions"`
	ProofRequirements  []string `json:"proof_requirements"`
}

type implementationInput struct {
	Action string `json:"action"`
}

type implementationOutput struct {
	Allowed bool     `json:"allowed"`
	Scope   []string `json:"scope"`
}

type reviewInput struct {
	Status string `json:"status"`
}

type reviewOutput struct {
	Findings []map[string]any `json:"findings"`
	Status   string           `json:"status"`
}

type repairInput struct {
	FailedReceipt     string `json:"failed_receipt"`
	ReceiptPath       string `json:"receipt_path"`
	FailedReceiptPath string `json:"failed_receipt_path"`
}

type repairOutput struct {
	Attempted bool   `json:"attempted"`
	Reason    string `json:"reason"`
}

func Run(req Request) (Receipt, error) {
	started := time.Now().UTC()
	if err := validateRequest(req); err != nil {
		return Receipt{}, err
	}

	risk := req.Risk
	paths := cloneStrings(req.AffectedPaths)
	var (
		output  json.RawMessage
		summary string
		err     error
	)
	switch req.Stage {
	case Classifier:
		output, risk, paths, summary, err = runClassifier(req)
	case Context:
		output, summary, err = runContext(req)
	case Planning:
		output, summary, err = runPlanning(req)
	case Implementation:
		output, summary, err = runImplementation(req)
	case Review:
		output, summary, err = runReview(req)
	case Repair:
		output, summary, err = runRepair(req)
	default:
		return Receipt{}, fmt.Errorf("unknown stage %q", req.Stage)
	}
	if err != nil {
		return Receipt{}, err
	}

	finished := time.Now().UTC()
	if finished.Before(started) {
		finished = started
	}
	return Receipt{
		Stage:         req.Stage,
		PlanID:        req.PlanID,
		Fingerprint:   req.Fingerprint,
		StartedAt:     started,
		FinishedAt:    finished,
		Risk:          risk,
		AffectedPaths: paths,
		Authority:     req.Authority,
		Output:        output,
		Summary:       summary,
		Proof:         false,
	}, nil
}

func ValidateChain(previous, next Receipt) error {
	if strings.TrimSpace(previous.PlanID) == "" || strings.TrimSpace(next.PlanID) == "" {
		return fmt.Errorf("plan id is required")
	}
	if previous.PlanID != next.PlanID {
		return fmt.Errorf("substituted plan id")
	}
	if strings.TrimSpace(previous.Fingerprint) == "" || strings.TrimSpace(next.Fingerprint) == "" {
		return fmt.Errorf("fingerprint is required")
	}
	if previous.Fingerprint != next.Fingerprint {
		return fmt.Errorf("mutated fingerprint")
	}
	if next.StartedAt.Before(previous.FinishedAt) {
		return fmt.Errorf("next receipt started before previous receipt finished")
	}
	return nil
}

func validateRequest(req Request) error {
	if strings.TrimSpace(req.Stage) == "" {
		return fmt.Errorf("stage is required")
	}
	if strings.TrimSpace(req.PlanID) == "" {
		return fmt.Errorf("plan id is required")
	}
	if strings.TrimSpace(req.Fingerprint) == "" {
		return fmt.Errorf("fingerprint is required")
	}
	switch req.Stage {
	case Classifier, Context, Planning, Implementation, Review, Repair:
	default:
		return fmt.Errorf("unknown stage %q", req.Stage)
	}
	root := strings.TrimSpace(req.Root)
	if root == "" {
		return fmt.Errorf("root is required")
	}
	fingerprint, err := repo.Fingerprint(root)
	if err != nil {
		return err
	}
	if fingerprint != strings.TrimSpace(req.Fingerprint) {
		return fmt.Errorf("fingerprint mismatch")
	}
	return nil
}

func runClassifier(req Request) (json.RawMessage, string, []string, string, error) {
	var input classifierInput
	if err := decodeInput(req.Input, &input); err != nil {
		return nil, "", nil, "", err
	}
	paths := cloneStrings(input.Paths)
	if len(paths) == 0 {
		paths = cloneStrings(req.AffectedPaths)
	}
	hints := parseHints(input.Hints)
	risk := classifyRisk(input.Risk, paths, hints, req.Risk)
	output, err := marshalJSON(classifierOutput{Risk: risk, AffectedPaths: paths})
	if err != nil {
		return nil, "", nil, "", err
	}
	return output, risk, paths, "classified change risk", nil
}

func runContext(req Request) (json.RawMessage, string, error) {
	output, err := collectContext(strings.TrimSpace(req.Root))
	if err != nil {
		return nil, "", err
	}
	raw, err := marshalJSON(output)
	if err != nil {
		return nil, "", err
	}
	return raw, "selected existing context", nil
}

func runPlanning(req Request) (json.RawMessage, string, error) {
	var input planningInput
	if err := decodeInput(req.Input, &input); err != nil {
		return nil, "", err
	}
	task := strings.TrimSpace(input.Task)
	acceptance := cloneStrings(input.AcceptanceCriteria)
	if input.AcceptanceCriteria == nil && task != "" {
		acceptance = []string{task}
	}
	affected := cloneStrings(input.AffectedFiles)
	if input.AffectedFiles == nil {
		affected = cloneStrings(req.AffectedPaths)
	}
	commands := cloneStrings(input.Commands)
	risks := cloneStrings(input.Risks)
	if input.Risks == nil && strings.TrimSpace(req.Risk) != "" {
		risks = []string{req.Risk}
	}
	mitigations := providedOrDefault(input.Mitigations, []string{
		"do not exceed authority",
		"do not treat summary as proof",
	})
	budgets := providedOrDefault(input.Budgets, []string{
		"bounded files",
		"bounded lines",
	})
	stops := providedOrDefault(input.StopConditions, []string{
		"stop when evidence is missing",
		"stop when the repository changed after planning",
		"stop when a requested action exceeds authority",
	})
	proof := providedOrDefault(input.ProofRequirements, []string{
		"independent check receipt",
		"summary is not proof",
	})
	raw, err := marshalJSON(planningOutput{
		AcceptanceCriteria: acceptance,
		AffectedFiles:      affected,
		Commands:           commands,
		Risks:              risks,
		Mitigations:        mitigations,
		Budgets:            budgets,
		StopConditions:     stops,
		ProofRequirements:  proof,
	})
	if err != nil {
		return nil, "", err
	}
	return raw, "planned bounded work", nil
}

func runImplementation(req Request) (json.RawMessage, string, error) {
	var input implementationInput
	if err := decodeInput(req.Input, &input); err != nil {
		return nil, "", err
	}
	if err := authorizeAction(input.Action, req.Authority); err != nil {
		return nil, "", err
	}
	raw, err := marshalJSON(implementationOutput{
		Allowed: true,
		Scope:   cloneStrings(req.AffectedPaths),
	})
	if err != nil {
		return nil, "", err
	}
	return raw, "authorized repository edit scope", nil
}

func runReview(req Request) (json.RawMessage, string, error) {
	var input reviewInput
	if err := decodeInput(req.Input, &input); err != nil {
		return nil, "", err
	}
	status := "pass"
	if strings.EqualFold(strings.TrimSpace(input.Status), "fail") {
		status = "fail"
	}
	raw, err := marshalJSON(reviewOutput{
		Findings: []map[string]any{},
		Status:   status,
	})
	if err != nil {
		return nil, "", err
	}
	return raw, "review receipt without independent proof", nil
}

func runRepair(req Request) (json.RawMessage, string, error) {
	var input repairInput
	if err := decodeInput(req.Input, &input); err != nil {
		return nil, "", err
	}
	path := firstNonEmpty(input.FailedReceipt, input.ReceiptPath, input.FailedReceiptPath)
	out := repairOutput{
		Attempted: false,
		Reason:    "no failed receipt path",
	}
	if path != "" {
		out.Attempted = true
		out.Reason = "repair does not write or commit"
	}
	raw, err := marshalJSON(out)
	if err != nil {
		return nil, "", err
	}
	return raw, "repair did not commit", nil
}

func authorizeAction(action string, auth model.Authority) error {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "", "edit", "write":
		if !auth.WriteRepository {
			return fmt.Errorf("edit requires write_repository authority")
		}
		return nil
	case "commit":
		if !auth.Commit {
			return fmt.Errorf("commit requires commit authority")
		}
	case "push":
		if !auth.Push {
			return fmt.Errorf("push requires push authority")
		}
	case "pr":
		if !auth.Push {
			return fmt.Errorf("pr requires push authority")
		}
	case "mr":
		if !auth.Push {
			return fmt.Errorf("mr requires push authority")
		}
	case "release":
		if !auth.Release {
			return fmt.Errorf("release requires release authority")
		}
	case "deploy":
		if !auth.Deploy {
			return fmt.Errorf("deploy requires deploy authority")
		}
	default:
		return fmt.Errorf("unsupported action %q", action)
	}
	return nil
}

func collectContext(root string) (contextOutput, error) {
	out := contextOutput{
		Instructions: []string{},
		Skills:       []string{},
		Source:       []string{},
		Contracts:    []string{},
		Tests:        []string{},
		Schemas:      []string{},
		Evidence:     []string{},
	}
	if root == "" {
		return out, nil
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && skippedContextDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case isInstruction(rel):
			out.Instructions = append(out.Instructions, rel)
		case isSkill(rel):
			out.Skills = append(out.Skills, rel)
		case strings.HasPrefix(rel, "schema/"):
			out.Schemas = append(out.Schemas, rel)
		case strings.HasPrefix(rel, ".sam-harness/evidence/"):
			out.Evidence = append(out.Evidence, rel)
		case isContract(rel):
			out.Contracts = append(out.Contracts, rel)
		case isTest(rel):
			out.Tests = append(out.Tests, rel)
		case isSource(rel):
			out.Source = append(out.Source, rel)
		}
		return nil
	})
	if err != nil {
		return contextOutput{}, err
	}
	sort.Strings(out.Instructions)
	sort.Strings(out.Skills)
	sort.Strings(out.Source)
	sort.Strings(out.Contracts)
	sort.Strings(out.Tests)
	sort.Strings(out.Schemas)
	sort.Strings(out.Evidence)
	out.Source = limitPaths(out.Source, disclosureLimit)
	out.Tests = limitPaths(out.Tests, disclosureLimit)
	return out, nil
}

func isInstruction(rel string) bool {
	switch filepath.Base(rel) {
	case "AGENTS.md", "CLAUDE.md", "GEMINI.md":
		return true
	default:
		return false
	}
}

func isSource(rel string) bool {
	if isTest(rel) {
		return false
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs":
		return true
	default:
		return false
	}
}

func isTest(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.HasSuffix(base, "_test.py"), strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
		return true
	case strings.Contains(base, ".test.") && (strings.HasSuffix(base, ".ts") || strings.HasSuffix(base, ".js") || strings.HasSuffix(base, ".tsx") || strings.HasSuffix(base, ".jsx")):
		return true
	case strings.Contains(base, ".spec.") && (strings.HasSuffix(base, ".ts") || strings.HasSuffix(base, ".js") || strings.HasSuffix(base, ".tsx") || strings.HasSuffix(base, ".jsx")):
		return true
	case strings.HasSuffix(base, "_test.rs"), strings.HasSuffix(base, "_test.ts"):
		return true
	case strings.Contains(filepath.ToSlash(rel), "/tests/") && strings.HasSuffix(base, ".rs"):
		return true
	default:
		return false
	}
}

func isSkill(rel string) bool {
	if filepath.Base(rel) != "SKILL.md" {
		return false
	}
	return strings.HasPrefix(rel, "skills/") || strings.HasPrefix(rel, ".agents/skills/")
}

func isContract(rel string) bool {
	if strings.HasPrefix(rel, "contracts/") {
		return true
	}
	name := strings.ToLower(filepath.Base(rel))
	return strings.Contains(name, "contract")
}

func skippedContextDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "target", "dist", "build", ".venv", ".tox", ".mypy_cache", ".pytest_cache", ".ruff_cache", "__pycache__":
		return true
	default:
		return false
	}
}

func classifyRisk(explicit string, paths, hints []string, fallback string) string {
	if validRisk(explicit) {
		return strings.ToLower(strings.TrimSpace(explicit))
	}
	text := strings.ToLower(strings.Join(append(append([]string{}, paths...), hints...), " "))
	switch {
	case containsAny(text, "critical", "production", "secret", "credential", "deploy"):
		return "critical"
	case containsAny(text, "high", "security", "auth", "release", "migration"):
		return "high"
	case containsAny(text, "medium") || len(paths) > 8:
		return "medium"
	case validRisk(fallback):
		return strings.ToLower(strings.TrimSpace(fallback))
	default:
		return "low"
	}
}

func validRisk(risk string) bool {
	switch strings.ToLower(strings.TrimSpace(risk)) {
	case "low", "medium", "high", "critical":
		return true
	default:
		return false
	}
}

func parseHints(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil && strings.TrimSpace(one) != "" {
		return []string{one}
	}
	return nil
}

func decodeInput(raw json.RawMessage, dest any) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("parse input: %w", err)
	}
	return nil
}

func marshalJSON(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func cloneStrings(in []string) []string {
	if len(in) == 0 {
		return []string{}
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}

func providedOrDefault(provided, fallback []string) []string {
	if provided != nil {
		return cloneStrings(provided)
	}
	return cloneStrings(fallback)
}

func limitPaths(paths []string, n int) []string {
	if n >= 0 && len(paths) > n {
		return paths[:n]
	}
	return paths
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if path := strings.TrimSpace(value); path != "" {
			return path
		}
	}
	return ""
}
