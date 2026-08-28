package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

const outputLimit = 32 * 1024

func Run(path string, writeReceipt bool) (model.CheckReport, string, error) {
	root, err := repo.ResolveRoot(path)
	if err != nil {
		return model.CheckReport{}, "", err
	}
	cfg, err := config.Load(filepath.Join(root, ".sam-harness", "config.yaml"))
	if err != nil {
		return model.CheckReport{}, "", err
	}
	fingerprint, err := repo.Fingerprint(root)
	if err != nil {
		return model.CheckReport{}, "", err
	}
	report := model.CheckReport{
		HarnessVersion: model.HarnessVersion,
		Root:           root,
		Profile:        cfg.Profile,
		Fingerprint:    fingerprint,
		Passed:         true,
		CreatedAt:      time.Now().UTC(),
	}
	for _, gate := range cfg.Gates {
		result := runGate(root, gate)
		report.Results = append(report.Results, result)
		if gate.Required && !result.Passed {
			report.Passed = false
		}
	}
	receiptPath := ""
	if writeReceipt {
		receiptPath, err = writeReport(root, cfg.Evidence.ReceiptDirectory, report)
		if err != nil {
			return report, "", err
		}
	}
	if !report.Passed {
		return report, receiptPath, fmt.Errorf("one or more required gates failed")
	}
	return report, receiptPath, nil
}

func runGate(root string, gate model.Gate) (result model.GateResult) {
	result = model.GateResult{
		Name:      gate.Name,
		Stage:     gate.Stage,
		Command:   append([]string(nil), gate.Command...),
		Workdir:   gate.Workdir,
		Required:  gate.Required,
		StartedAt: time.Now().UTC(),
		ExitCode:  -1,
	}
	start := time.Now()
	defer func() {
		result.Duration = time.Since(start)
		result.FinishedAt = time.Now().UTC()
	}()
	if len(gate.Command) == 0 {
		result.Output = "empty command"
		return result
	}
	workdir, err := containedPath(root, gate.Workdir)
	if err != nil {
		result.Output = err.Error()
		return result
	}
	executable := gate.Command[0]
	if !filepath.IsAbs(executable) && strings.ContainsAny(executable, `/\`) {
		relative := filepath.Join(gate.Workdir, filepath.FromSlash(executable))
		executable, err = containedPath(root, relative)
		if err != nil {
			result.Output = err.Error()
			return result
		}
	} else if _, err := exec.LookPath(executable); err != nil {
		result.Output = fmt.Sprintf("command not found: %s", gate.Command[0])
		return result
	}
	cmd := exec.Command(executable, gate.Command[1:]...)
	cmd.Dir = workdir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err = cmd.Run()
	result.Output = truncate(output.String())
	if err == nil {
		result.Passed = true
		result.ExitCode = 0
		return result
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	} else {
		result.Output = strings.TrimSpace(result.Output + "\n" + err.Error())
	}
	return result
}

func writeReport(root, directory string, report model.CheckReport) (string, error) {
	targetDir, err := containedPath(root, directory)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", err
	}
	name := report.CreatedAt.Format("20060102T150405.000000000Z") + ".json"
	path := filepath.Join(targetDir, name)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func containedPath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe repository path %q", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("repository path escapes root: %q", relative)
	}
	target := filepath.Join(root, clean)
	current := root
	for _, part := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			break
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("repository path contains a symbolic link: %q", relative)
		}
	}
	return target, nil
}

func truncate(value string) string {
	if len(value) <= outputLimit {
		return value
	}
	return value[:outputLimit] + "\n[output truncated by sam-harness]"
}
