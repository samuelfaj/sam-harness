package repo

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func ResolveRoot(path string) (string, error) {
	if path == "" {
		path = "."
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository path is not a directory: %s", abs)
	}
	return filepath.Clean(abs), nil
}

func Fingerprint(root string) (string, error) {
	if isGit(root) {
		return gitFingerprint(root)
	}
	return treeFingerprint(root)
}

func GitState(root string) (head string, dirty bool, remoteHost string, ok bool) {
	if !isGit(root) {
		return "", false, "", false
	}
	head = strings.TrimSpace(runGit(root, "rev-parse", "HEAD"))
	if head == "" {
		head = "unborn"
	}
	dirty = runGit(root, "status", "--porcelain=v1", "--untracked-files=all") != ""
	remote := strings.TrimSpace(runGit(root, "remote", "get-url", "origin"))
	switch {
	case strings.Contains(remote, "github.com"):
		remoteHost = "github"
	case strings.Contains(remote, "gitlab.com"):
		remoteHost = "gitlab"
	}
	return head, dirty, remoteHost, true
}

func isGit(root string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	cmd.Dir = root
	return cmd.Run() == nil
}

func gitFingerprint(root string) (string, error) {
	h := sha256.New()
	writePart(h, runGit(root, "rev-parse", "HEAD"))
	writePart(h, runGit(root, "diff", "--binary", "--no-ext-diff", "--", "."))
	writePart(h, runGit(root, "diff", "--binary", "--no-ext-diff", "--cached", "--", "."))

	cmd := exec.Command("git", "ls-files", "--others", "--exclude-standard", "-z", "--", ".")
	cmd.Dir = root
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("list untracked files: %w", err)
	}
	paths := bytes.Split(output, []byte{0})
	for _, raw := range paths {
		if len(raw) == 0 {
			continue
		}
		path := string(raw)
		if ignoredUntrackedPath(path) {
			continue
		}
		writePart(h, path)
		if err := hashFile(h, filepath.Join(root, filepath.FromSlash(path))); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ignoredUntrackedPath(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for index, part := range parts {
		if ignoredDir(part) {
			return true
		}
		if part == ".sam-harness" && index+1 < len(parts) && parts[index+1] == "evidence" {
			return true
		}
	}
	return false
}

func treeFingerprint(root string) (string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && path != root && ignoredDir(entry.Name()) {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, rel := range paths {
		writePart(h, rel)
		if err := hashFile(h, filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func ignoredDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "target", "dist", "build", ".venv", ".tox", ".mypy_cache", ".pytest_cache", "__pycache__":
		return true
	default:
		return false
	}
}

func hashFile(dst io.Writer, path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		if err != nil {
			return fmt.Errorf("read symlink %s: %w", path, err)
		}
		writePart(dst, "symlink")
		writePart(dst, target)
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	defer file.Close()
	if _, err := io.Copy(dst, bufio.NewReader(file)); err != nil {
		return fmt.Errorf("hash %s: %w", path, err)
	}
	return nil
}

func writePart(dst io.Writer, value string) {
	_, _ = io.WriteString(dst, value)
	_, _ = io.WriteString(dst, "\x00")
}

func runGit(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, _ := cmd.Output()
	return string(output)
}
