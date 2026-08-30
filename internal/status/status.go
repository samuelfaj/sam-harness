package status

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/pipeline"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

const (
	StateSource      = "source"
	StateLocalChecks = "local_checks"
	StateCommit      = "commit"
	StateRemote      = "remote"
	StateReview      = "review"
	StateCI          = "ci"
	StateArtifact    = "artifact"
	StateDeployment  = "deployment"
	StateLiveProof   = "live_proof"
)

var Ladder = []string{
	StateSource,
	StateLocalChecks,
	StateCommit,
	StateRemote,
	StateReview,
	StateCI,
	StateArtifact,
	StateDeployment,
	StateLiveProof,
}

type StateReport struct {
	Name     string `json:"name"`
	Proven   bool   `json:"proven"`
	Evidence string `json:"evidence,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type Report struct {
	Root        string        `json:"root"`
	Head        string        `json:"head,omitempty"`
	Fingerprint string        `json:"fingerprint"`
	States      []StateReport `json:"states"`
}

type Options struct {
	Checks []ProviderCheck
}

// Evaluate reports the evidence ladder for root. A later state is never marked
// proven from an earlier receipt.
func Evaluate(path string) (Report, error) {
	return EvaluateWithOptions(path, Options{})
}

func EvaluateWithOptions(path string, options Options) (Report, error) {
	root, err := repo.ResolveRoot(path)
	if err != nil {
		return Report{}, err
	}
	cfg, err := config.Load(filepath.Join(root, ".sam-harness", "config.yaml"))
	if err != nil {
		return Report{}, err
	}
	fingerprint, err := repo.Fingerprint(root)
	if err != nil {
		return Report{}, err
	}
	head, dirty, _, gitOK := repo.GitState(root)
	receipts, err := loadReceipts(root, cfg.Evidence.ReceiptDirectory)
	if err != nil {
		return Report{}, err
	}
	report := Report{Root: root, Head: head, Fingerprint: fingerprint}
	for _, name := range Ladder {
		report.States = append(report.States, evaluateState(name, root, fingerprint, head, dirty, gitOK, receipts, cfg, options.Checks))
	}
	return report, nil
}

type storedReceipt struct {
	Path          string
	Kind          string
	Phase         string
	Passed        bool
	Fingerprint   string
	ReviewHeadSHA string
}

func evaluateState(name, root, fingerprint, head string, dirty, gitOK bool, receipts []storedReceipt, cfg model.Config, checks []ProviderCheck) StateReport {
	unproven := func(reason string) StateReport {
		return StateReport{Name: name, Reason: reason}
	}
	switch name {
	case StateSource:
		if fingerprint == "" {
			return unproven("repository fingerprint is missing")
		}
		return StateReport{Name: name, Proven: true, Evidence: "fingerprint " + fingerprint}
	case StateLocalChecks:
		for _, receipt := range receipts {
			if receipt.Passed && receipt.Fingerprint == fingerprint && (receipt.Kind == "check" || receipt.Phase == string(model.PhaseStatic) || receipt.Phase == string(model.PhaseTest)) {
				return StateReport{Name: name, Proven: true, Evidence: receipt.Path}
			}
		}
		return unproven("no passed local-check receipt matches the current fingerprint")
	case StateCommit:
		if !gitOK || strings.TrimSpace(head) == "" {
			return unproven("git HEAD is missing")
		}
		if dirty {
			return unproven("worktree is dirty relative to HEAD")
		}
		return StateReport{Name: name, Proven: true, Evidence: head}
	case StateRemote:
		if !gitOK || strings.TrimSpace(head) == "" {
			return unproven("git HEAD is missing")
		}
		if !remoteContains(root, head) {
			return unproven("no upstream ref contains HEAD")
		}
		return StateReport{Name: name, Proven: true, Evidence: head}
	case StateReview:
		for _, receipt := range receipts {
			if receipt.Kind == "pipeline" && receipt.Phase == string(model.PhaseReview) && receipt.Passed && (receipt.ReviewHeadSHA == head || receipt.Fingerprint == fingerprint) {
				return StateReport{Name: name, Proven: true, Evidence: receipt.Path}
			}
		}
		return unproven("no passed review receipt matches the current head")
	case StateCI:
		for _, receipt := range receipts {
			if receipt.Kind == "ci" && receipt.Passed && receipt.Fingerprint == fingerprint {
				return StateReport{Name: name, Proven: true, Evidence: receipt.Path}
			}
		}
		ok, evidence := ProveCI(head, requiredCIChecks(cfg), checks)
		if ok {
			return StateReport{Name: name, Proven: true, Evidence: evidence}
		}
		if evidence == "" {
			evidence = "CI is not proven from local receipts"
		}
		return unproven(evidence)
	case StateArtifact:
		for _, receipt := range receipts {
			if receipt.Kind == "pipeline" && receipt.Phase == string(model.PhaseArtifact) && receipt.Passed && receipt.Fingerprint == fingerprint {
				return StateReport{Name: name, Proven: true, Evidence: receipt.Path}
			}
		}
		return unproven("no passed artifact receipt matches the current fingerprint")
	case StateDeployment:
		for _, receipt := range receipts {
			if receipt.Kind == "pipeline" && receipt.Phase == string(model.PhaseProduction) && receipt.Passed && receipt.Fingerprint == fingerprint {
				return StateReport{Name: name, Proven: true, Evidence: receipt.Path}
			}
		}
		return unproven("no passed production receipt matches the current fingerprint")
	case StateLiveProof:
		for _, receipt := range receipts {
			if receipt.Kind == "pipeline" && receipt.Phase == string(model.PhaseObserve) && receipt.Passed && receipt.Fingerprint == fingerprint {
				return StateReport{Name: name, Proven: true, Evidence: receipt.Path}
			}
		}
		return unproven("no passed observation receipt matches the current fingerprint")
	default:
		return unproven("unknown evidence state")
	}
}

func loadReceipts(root, directory string) ([]storedReceipt, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, nil
	}
	target := filepath.Join(root, filepath.FromSlash(directory))
	info, err := os.Stat(target)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("evidence path is not a directory: %s", directory)
	}
	var receipts []storedReceipt
	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		receipts = append(receipts, parseReceipt(path, data))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(receipts, func(i, j int) bool { return receipts[i].Path < receipts[j].Path })
	return receipts, nil
}

func parseReceipt(path string, data []byte) storedReceipt {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return storedReceipt{Path: path}
	}
	receipt := storedReceipt{Path: path}
	kind, _ := raw["kind"].(string)
	phase, _ := raw["phase"].(string)
	fingerprint, _ := raw["repository_fingerprint"].(string)
	if fingerprint == "" {
		fingerprint, _ = raw["fingerprint"].(string)
	}
	passed, _ := raw["passed"].(bool)
	head, _ := raw["review_head_sha"].(string)
	if kind == "" {
		if _, hasResults := raw["results"]; hasResults {
			kind = "check"
		}
	}
	if kind == "pipeline" {
		var typed pipeline.Receipt
		if err := json.Unmarshal(data, &typed); err == nil {
			passed = typed.Passed && typed.Status == pipeline.StatusPassed
			fingerprint = typed.Fingerprint
			phase = string(typed.Phase)
			head = typed.ReviewHeadSHA
		}
	}
	receipt.Kind = kind
	receipt.Phase = phase
	receipt.Passed = passed
	receipt.Fingerprint = fingerprint
	receipt.ReviewHeadSHA = head
	return receipt
}

func remoteContains(root, head string) bool {
	if strings.TrimSpace(head) == "" {
		return false
	}
	cmd := exec.Command("git", "branch", "-r", "--contains", head)
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

func requiredCIChecks(cfg model.Config) []string {
	required := []string{"static", "test"}
	for _, plane := range cfg.CI.AgentControlPlanes {
		if name := strings.TrimSpace(plane.RequiredCheck); name != "" {
			required = append(required, name)
		}
	}
	return required
}

func StateByName(report Report, name string) StateReport {
	for _, state := range report.States {
		if state.Name == name {
			return state
		}
	}
	return StateReport{Name: name}
}
