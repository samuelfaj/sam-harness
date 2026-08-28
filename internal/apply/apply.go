package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/planner"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

type snapshot struct {
	path    string
	existed bool
	content []byte
	mode    os.FileMode
}

func Run(plan model.Plan, acceptedID string) ([]string, error) {
	if acceptedID == "" || acceptedID != plan.ID {
		return nil, fmt.Errorf("apply requires --accept %s", plan.ID)
	}
	if planner.CalculateID(plan) != plan.ID {
		return nil, fmt.Errorf("plan contents do not match plan ID")
	}
	if plan.ExpiresAt.IsZero() || time.Now().UTC().After(plan.ExpiresAt) {
		return nil, fmt.Errorf("plan expired; create and approve a new plan")
	}
	if len(plan.Unresolved) > 0 {
		return nil, fmt.Errorf("plan has unresolved decisions: %s", strings.Join(plan.Unresolved, ", "))
	}
	if hasChanges(plan.Operations) && !allowsWrite(plan.Answers.AllowedActions) {
		return nil, fmt.Errorf("plan does not grant write_repository authority")
	}
	currentFingerprint, err := repo.Fingerprint(plan.Root)
	if err != nil {
		return nil, err
	}
	if currentFingerprint != plan.Fingerprint {
		return nil, fmt.Errorf("repository changed after planning; create a new plan")
	}

	var snapshots []snapshot
	var changed []string
	for _, operation := range plan.Operations {
		if operation.Action == "noop" {
			continue
		}
		target, err := safeTarget(plan.Root, operation.Path)
		if err != nil {
			rollback(snapshots)
			return nil, err
		}
		sum := sha256.Sum256([]byte(operation.Content))
		if hex.EncodeToString(sum[:]) != operation.ContentSHA256 {
			rollback(snapshots)
			return nil, fmt.Errorf("content hash mismatch for %s", operation.Path)
		}
		item := snapshot{path: target, mode: 0o644}
		if info, statErr := os.Stat(target); statErr == nil {
			item.existed = true
			item.mode = info.Mode().Perm()
			item.content, err = os.ReadFile(target)
			if err != nil {
				rollback(snapshots)
				return nil, err
			}
		} else if !os.IsNotExist(statErr) {
			rollback(snapshots)
			return nil, statErr
		}
		snapshots = append(snapshots, item)
		if err := writeAtomic(target, []byte(operation.Content), item.mode); err != nil {
			rollback(snapshots)
			return nil, err
		}
		changed = append(changed, operation.Path)
	}
	if _, err := config.Load(filepath.Join(plan.Root, ".sam-harness", "config.yaml")); err != nil {
		rollback(snapshots)
		return nil, fmt.Errorf("generated config failed validation: %w", err)
	}
	return changed, nil
}

func hasChanges(operations []model.Operation) bool {
	for _, operation := range operations {
		if operation.Action != "noop" {
			return true
		}
	}
	return false
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

func safeTarget(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) {
		return "", fmt.Errorf("unsafe operation path %q", relative)
	}
	clean := filepath.Clean(filepath.FromSlash(relative))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe operation path %q", relative)
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("operation escapes repository: %q", relative)
	}
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
			return "", fmt.Errorf("operation path contains a symbolic link: %q", relative)
		}
	}
	return target, nil
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".sam-harness-write-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}

func rollback(items []snapshot) {
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.existed {
			_ = os.WriteFile(item.path, item.content, item.mode)
		} else {
			_ = os.Remove(item.path)
		}
	}
}
