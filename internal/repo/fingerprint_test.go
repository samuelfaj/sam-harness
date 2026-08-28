package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTreeFingerprintChangesWithFileContent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	first, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("Fingerprint() did not change after content changed")
	}
}

func TestIgnoredUntrackedPathExcludesBuildAndEvidenceArtifacts(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"target/debug/app", "node_modules/pkg/index.js", ".venv/bin/python", ".ruff_cache/0.14.9/cache", ".sam-harness/evidence/receipt.json"} {
		if !ignoredUntrackedPath(path) {
			t.Fatalf("ignoredUntrackedPath(%q) = false", path)
		}
	}
	if ignoredUntrackedPath("src/targeting.go") {
		t.Fatal("ignoredUntrackedPath excluded a normal source file")
	}
}

func TestTreeFingerprintIgnoresRuffCacheCreatedByAFirstCheck(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte("value = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".ruff_cache", "0.14.9"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".ruff_cache", "0.14.9", "cache"), []byte("generated\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("Fingerprint() treated Ruff's rebuildable cache as source mutation")
	}
}

func TestGitFingerprintTracksIndexAndWorktreeBeforeFirstCommit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runTestGit(t, root, "init")
	path := filepath.Join(root, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "main.go")
	staged, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	worktree, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if worktree == staged {
		t.Fatal("Fingerprint() ignored an unborn worktree change")
	}
	runTestGit(t, root, "add", "main.go")
	restaged, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if restaged == staged || restaged == worktree {
		t.Fatal("Fingerprint() ignored an unborn index change")
	}
}

func TestGitFingerprintScopesExplicitNestedRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	runTestGit(t, root, "init")
	nested := filepath.Join(root, "service")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, root, "add", "service/main.go")
	first, err := Fingerprint(nested)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(nested)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("Fingerprint() ignored a change inside the explicit nested root")
	}
	if err := os.WriteFile(filepath.Join(root, "sibling.txt"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	third, err := Fingerprint(nested)
	if err != nil {
		t.Fatal(err)
	}
	if second != third {
		t.Fatal("Fingerprint() included a change outside the explicit nested root")
	}
}

func TestFingerprintHashesSymlinkTargetWithoutFollowingIt(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	target := t.TempDir()
	link := filepath.Join(root, "linked-directory")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	first, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "outside.txt"), []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("Fingerprint() followed a directory symlink outside the root")
	}
}

func runTestGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
