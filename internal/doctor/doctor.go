package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

type Report struct {
	Passed   bool     `json:"passed"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

func Run(path string) (Report, error) {
	root, err := repo.ResolveRoot(path)
	if err != nil {
		return Report{}, err
	}
	cfg, err := config.Load(filepath.Join(root, ".sam-harness", "config.yaml"))
	if err != nil {
		return Report{Errors: []string{err.Error()}}, nil
	}
	report := Report{Passed: true}
	if cfg.HarnessVersion != model.HarnessVersion {
		report.Warnings = append(report.Warnings, fmt.Sprintf("config uses harness %s; CLI is %s", cfg.HarnessVersion, model.HarnessVersion))
	}
	for _, path := range []string{"AGENTS.md", ".sam-harness/GATES.md", ".sam-harness/DELEGATION.md", ".sam-harness/UX_GATES.md", ".sam-harness/INVARIANTS.md"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			report.Errors = append(report.Errors, fmt.Sprintf("missing %s", path))
		}
	}
	for _, path := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md", ".github/copilot-instructions.md"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err == nil && (!strings.Contains(string(data), "<!-- sam-harness:start -->") || !strings.Contains(string(data), "<!-- sam-harness:end -->")) {
			report.Errors = append(report.Errors, fmt.Sprintf("invalid managed block in %s", path))
		}
	}
	for _, gate := range cfg.Gates {
		if len(gate.Command) == 0 {
			report.Errors = append(report.Errors, fmt.Sprintf("empty command for %s", gate.Name))
			continue
		}
		if _, err := exec.LookPath(gate.Command[0]); err != nil {
			report.Warnings = append(report.Warnings, fmt.Sprintf("command unavailable for %s: %s", gate.Name, gate.Command[0]))
		}
	}
	if cfg.Design.Applicable && strings.TrimSpace(cfg.Design.SourceOfTruth) == "" {
		report.Errors = append(report.Errors, "design source of truth is required for a user interface")
	}
	if cfg.Profile == model.ProfileProduction || cfg.Profile == model.ProfileRegulated {
		for _, path := range []string{".sam-harness/runbooks/release.md", ".sam-harness/runbooks/migration.md", ".sam-harness/runbooks/incident.md"} {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("missing %s", path))
			}
		}
	}
	if cfg.Profile == model.ProfileRegulated {
		for _, path := range []string{".sam-harness/runbooks/threat-model.md", ".sam-harness/runbooks/data-governance.md"} {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); err != nil {
				report.Errors = append(report.Errors, fmt.Sprintf("missing %s", path))
			}
		}
	}
	report.Passed = len(report.Errors) == 0
	return report, nil
}
