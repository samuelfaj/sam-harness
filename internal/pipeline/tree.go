package pipeline

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

type fileState struct {
	data []byte
	mode os.FileMode
	link string
}

func artifactOutputPaths(artifact model.ArtifactWorkflow) []string {
	return []string{artifact.ArtifactPath, artifact.SBOMPath, artifact.ProvenancePath}
}

func repositoryFingerprint(root string, cfg model.Config) (string, error) {
	return sourceFingerprint(root, repositoryExcludedPaths(cfg))
}

func repositoryExcludedPaths(cfg model.Config) []string {
	excluded := []string{cfg.Evidence.ReceiptDirectory}
	if cfg.Workflow != nil {
		excluded = append(excluded, artifactOutputPaths(cfg.Workflow.Artifact)...)
	}
	return excluded
}

func sourceFingerprint(root string, excludedPaths []string) (string, error) {
	snapshot, err := sourceSnapshot(root, excludedPaths)
	if err != nil {
		return "", err
	}
	return fingerprintSnapshot(snapshot), nil
}

func sourceSnapshot(root string, excludedPaths []string) (map[string]fileState, error) {
	excluded, err := normalizedExcludedPaths(excludedPaths)
	if err != nil {
		return nil, err
	}
	snapshot, err := snapshotRepository(root, func(relative string, entry os.DirEntry) bool {
		if relative == ".git" || relative == ".sam-harness/evidence" {
			return true
		}
		if ignoredGeneratedSourceEntry(relative, entry) {
			return true
		}
		if entry.IsDir() && ignoredSourceDirectory(entry.Name()) {
			return true
		}
		return excluded[relative]
	})
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

func ignoredSourceDirectory(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "target", "dist", "build", ".venv", ".tox", ".mypy_cache", ".pytest_cache", ".ruff_cache", "__pycache__":
		return true
	default:
		return false
	}
}

func ignoredGeneratedSourceEntry(relative string, entry os.DirEntry) bool {
	switch entry.Name() {
	case ".bun", ".ci-cache", ".turbo", ".eslintcache":
		return true
	}
	clean := filepath.ToSlash(relative)
	return clean == ".husky/_" || strings.HasSuffix(clean, "/.husky/_")
}

func snapshotRepairWorktree(root, evidenceDirectory string) (map[string]fileState, error) {
	evidence := filepath.ToSlash(filepath.Clean(filepath.FromSlash(evidenceDirectory)))
	return snapshotRepository(root, func(relative string, entry os.DirEntry) bool {
		return relative == ".git" || relative == evidence || relative == ".sam-harness/evidence" || ignoredGeneratedSourceEntry(relative, entry) || (entry.IsDir() && ignoredSourceDirectory(entry.Name()))
	})
}

func snapshotGitControl(root string) (map[string]fileState, error) {
	gitRoot := filepath.Join(root, ".git")
	info, err := os.Lstat(gitRoot)
	if os.IsNotExist(err) {
		return map[string]fileState{}, nil
	} else if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return snapshotRepository(gitRoot, ignoredGitControlEntry)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("unsupported .git control entry")
	}
	data, err := os.ReadFile(gitRoot)
	if err != nil {
		return nil, err
	}
	pointer := strings.TrimSpace(string(data))
	gitDirectory, found := strings.CutPrefix(pointer, "gitdir:")
	if !found || strings.TrimSpace(gitDirectory) == "" {
		return nil, errors.New("invalid .git file")
	}
	gitDirectory = strings.TrimSpace(gitDirectory)
	if !filepath.IsAbs(gitDirectory) {
		gitDirectory = filepath.Join(root, gitDirectory)
	}
	gitDirectory = filepath.Clean(gitDirectory)
	result := map[string]fileState{
		"gitfile": {data: data, mode: info.Mode()},
	}
	if err := mergeSnapshot(result, "gitdir", gitDirectory); err != nil {
		return nil, err
	}
	command := exec.Command("git", "rev-parse", "--git-common-dir")
	command.Dir = root
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("resolve Git common directory: %w", err)
	}
	commonDirectory := strings.TrimSpace(string(output))
	if !filepath.IsAbs(commonDirectory) {
		commonDirectory = filepath.Join(root, commonDirectory)
	}
	commonDirectory = filepath.Clean(commonDirectory)
	if commonDirectory != gitDirectory {
		if err := mergeSnapshot(result, "common", commonDirectory); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func mergeSnapshot(destination map[string]fileState, prefix, root string) error {
	snapshot, err := snapshotRepository(root, ignoredGitControlEntry)
	if err != nil {
		return err
	}
	for path, state := range snapshot {
		destination[prefix+"/"+path] = state
	}
	return nil
}

func ignoredGitControlEntry(relative string, entry os.DirEntry) bool {
	name := filepath.Base(filepath.ToSlash(relative))
	return strings.HasSuffix(name, ".lock") || strings.HasPrefix(name, "tmp_pack_")
}

func snapshotRepository(root string, skip func(string, os.DirEntry) bool) (map[string]fileState, error) {
	result := map[string]fileState{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		slash := filepath.ToSlash(relative)
		if skip != nil && skip(slash, entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		state := fileState{mode: info.Mode()}
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			state.link, err = os.Readlink(path)
		case info.Mode().IsRegular():
			state.data, err = os.ReadFile(path)
		default:
			return fmt.Errorf("unsupported repository entry %s (%s)", slash, info.Mode())
		}
		if err != nil {
			return err
		}
		result[slash] = state
		return nil
	})
	return result, err
}

func normalizedExcludedPaths(paths []string) (map[string]bool, error) {
	result := make(map[string]bool, len(paths))
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if filepath.IsAbs(path) {
			return nil, fmt.Errorf("excluded repository path must be relative: %q", path)
		}
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return nil, fmt.Errorf("excluded repository path escapes root: %q", path)
		}
		result[clean] = true
	}
	return result, nil
}

func fingerprintSnapshot(snapshot map[string]fileState) string {
	paths := make([]string, 0, len(snapshot))
	for path := range snapshot {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		state := snapshot[path]
		_, _ = io.WriteString(hash, path)
		_, _ = io.WriteString(hash, "\x00")
		_, _ = io.WriteString(hash, state.mode.String())
		_, _ = io.WriteString(hash, "\x00")
		_, _ = io.WriteString(hash, state.link)
		_, _ = io.WriteString(hash, "\x00")
		_, _ = hash.Write(state.data)
		_, _ = io.WriteString(hash, "\x00")
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func snapshotsEqual(left, right map[string]fileState) bool {
	if len(left) != len(right) {
		return false
	}
	for path, before := range left {
		after, ok := right[path]
		if !ok || before.mode != after.mode || before.link != after.link || !bytes.Equal(before.data, after.data) {
			return false
		}
	}
	return true
}
