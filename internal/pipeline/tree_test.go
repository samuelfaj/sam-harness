package pipeline

import (
	"os"
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
