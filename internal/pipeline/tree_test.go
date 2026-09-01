package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSourceFingerprintIgnoresOnlyKnownGeneratedCaches(t *testing.T) {
	root := t.TempDir()
	writeTreeTestFile(t, root, "source.go", "package fixture\n")
	writeTreeTestFile(t, root, ".env", "SECRET=one\n")
	writeTreeTestFile(t, root, "arbitrary.ignored", "one\n")
	for _, path := range []string{
		".bun/cache/item",
		".ci-cache/item",
		".turbo/item",
		".eslintcache",
		".husky/_/hook.sh",
		"nested/.husky/_/hook.sh",
	} {
		writeTreeTestFile(t, root, path, "one\n")
	}

	baseline, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		".bun/cache/item",
		".ci-cache/item",
		".turbo/item",
		".eslintcache",
		".husky/_/hook.sh",
		"nested/.husky/_/hook.sh",
	} {
		writeTreeTestFile(t, root, path, "two\n")
	}
	generatedChanged, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if generatedChanged != baseline {
		t.Fatal("known generated caches changed the source fingerprint")
	}

	writeTreeTestFile(t, root, ".env", "SECRET=two\n")
	environmentChanged, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if environmentChanged == baseline {
		t.Fatal(".env was excluded from the source fingerprint")
	}

	writeTreeTestFile(t, root, ".env", "SECRET=one\n")
	writeTreeTestFile(t, root, "arbitrary.ignored", "two\n")
	arbitraryChanged, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if arbitraryChanged == baseline {
		t.Fatal("arbitrary ignored-like input was excluded from the source fingerprint")
	}
}

func TestSourceFingerprintUsesGitSourceBoundary(t *testing.T) {
	root := t.TempDir()
	writeTreeTestFile(t, root, ".gitignore", ".generated/\n.env\n")
	writeTreeTestFile(t, root, "source.go", "package fixture\n")
	writeTreeTestFile(t, root, "tracked.env", "TRACKED=one\n")
	writeTreeTestFile(t, root, ".env", "SECRET=one\n")
	writeTreeTestFile(t, root, ".generated/cache", "one\n")
	runTreeTestGit(t, root, "init", "-q")
	runTreeTestGit(t, root, "add", ".gitignore", "source.go", "tracked.env")

	baseline, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeTreeTestFile(t, root, ".env", "SECRET=two\n")
	writeTreeTestFile(t, root, ".generated/cache", "two\n")
	ignoredChanged, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ignoredChanged != baseline {
		t.Fatal("Git-ignored workspace state changed the source fingerprint")
	}

	writeTreeTestFile(t, root, "tracked.env", "TRACKED=two\n")
	trackedChanged, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if trackedChanged == baseline {
		t.Fatal("tracked source was excluded from the source fingerprint")
	}

	writeTreeTestFile(t, root, "tracked.env", "TRACKED=one\n")
	writeTreeTestFile(t, root, "visible.untracked", "one\n")
	untrackedChanged, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if untrackedChanged == baseline {
		t.Fatal("visible untracked source was excluded from the source fingerprint")
	}
}

func TestReviewSandboxPreservesTrackedIgnoredSourceFingerprint(t *testing.T) {
	root := t.TempDir()
	writeTreeTestFile(t, root, ".gitignore", ".generated/\n")
	writeTreeTestFile(t, root, "source.go", "package fixture\n")
	writeTreeTestFile(t, root, ".generated/tracked.json", "tracked\n")
	writeTreeTestFile(t, root, ".generated/runner-cache.json", "ignored\n")
	runTreeTestGit(t, root, "init", "-q")
	runTreeTestGit(t, root, "add", ".gitignore", "source.go")
	runTreeTestGit(t, root, "add", "--force", ".generated/tracked.json")

	snapshot, err := sourceSnapshot(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot[".generated/tracked.json"]; !ok {
		t.Fatal("tracked ignored source is missing from source snapshot")
	}
	if _, ok := snapshot[".generated/runner-cache.json"]; ok {
		t.Fatal("untracked ignored runner state is present in source snapshot")
	}

	sandbox := t.TempDir()
	change := reviewChangeEvidence{
		baseRoot:     root,
		baseSnapshot: snapshot,
		headSnapshot: snapshot,
	}
	if err := prepareReviewSandbox(root, sandbox, change); err != nil {
		t.Fatal(err)
	}
	actual, err := sourceFingerprint(sandbox, nil)
	if err != nil {
		t.Fatal(err)
	}
	if expected := fingerprintSnapshot(snapshot); actual != expected {
		t.Fatalf("review sandbox fingerprint = %s, want %s", actual, expected)
	}
}

func TestSourceFingerprintUsesPortableGitFileModes(t *testing.T) {
	root := t.TempDir()
	writeTreeTestFile(t, root, "source.sh", "#!/bin/sh\n")

	baseline, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "source.sh"), 0o600); err != nil {
		t.Fatal(err)
	}
	portable, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if portable != baseline {
		t.Fatal("runner-specific read and write permissions changed the source fingerprint")
	}

	if err := os.Chmod(filepath.Join(root, "source.sh"), 0o700); err != nil {
		t.Fatal(err)
	}
	executable, err := sourceFingerprint(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if executable == baseline {
		t.Fatal("Git-tracked executable mode did not change the source fingerprint")
	}
}

func writeTreeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runTreeTestGit(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
}
