package scan

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/repo"
)

func TestRunDetectsEverySupportedStackInMixedRepository(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	result, err := Run(root)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := map[string]bool{"typescript": true, "python": true, "go": true, "rust": true}
	for _, stack := range result.Stacks {
		delete(want, stack.Kind)
	}
	if len(want) != 0 {
		t.Fatalf("missing stacks: %v", want)
	}
	if !result.HasUI {
		t.Fatal("HasUI = false, want true from React fixture")
	}
	if !result.HasPersistence {
		t.Fatal("HasPersistence = false, want true from ORM fixtures")
	}
}

func TestTypeScriptDetectionUsesDeclaredScripts(t *testing.T) {
	t.Parallel()
	root := filepath.Join(fixtureRoot(t), "typescript")
	result, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stacks) != 1 {
		t.Fatalf("got %d stacks, want 1", len(result.Stacks))
	}
	stack := result.Stacks[0]
	if stack.PackageManager != "npm" {
		t.Fatalf("PackageManager = %q, want npm", stack.PackageManager)
	}
	if got := stack.Commands["typecheck"]; len(got) != 3 || got[0] != "npm" || got[2] != "typecheck" {
		t.Fatalf("typecheck command = %#v", got)
	}
}

func TestRunDoesNotTreatNestedTestFixturesAsProductionStacks(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/app\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, "testdata", "fixtures", "ui", "package.json")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture, []byte(`{"dependencies":{"react":"19.1.0"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	harnessRunbook := filepath.Join(root, ".sam-harness", "runbooks", "migration.md")
	if err := os.MkdirAll(filepath.Dir(harnessRunbook), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(harnessRunbook, []byte("# Migration runbook\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stacks) != 1 || result.Stacks[0].Kind != "go" {
		t.Fatalf("Stacks = %#v, want only root Go stack", result.Stacks)
	}
	if result.HasUI {
		t.Fatal("HasUI = true from an ignored test fixture")
	}
	if result.HasPersistence {
		t.Fatal("HasPersistence = true from the generated harness namespace")
	}
}

func TestRunAsksWhenPackageManagerIsAmbiguous(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"node --test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"package-lock.json", "pnpm-lock.yaml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("lock\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	result, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stacks) != 1 || len(result.Stacks[0].Commands) != 0 {
		t.Fatalf("Stacks = %#v, want an unresolved command runner", result.Stacks)
	}
	if !containsQuestion(result.Questions, "commands:typescript:.") {
		t.Fatalf("Questions = %v, want package-manager decision", result.Questions)
	}
}

func TestRunAsksWhenPackageManagerCannotBeProven(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	if err := os.WriteFile(filepath.Join(parent, "package-lock.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "repository")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test":"node --test"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stacks) != 1 {
		t.Fatalf("Stacks = %#v, want one TypeScript stack", result.Stacks)
	}
	stack := result.Stacks[0]
	if stack.PackageManager != "" || len(stack.Commands) != 0 {
		t.Fatalf("Stack = %#v, want unresolved package-manager command", stack)
	}
	if !containsQuestion(result.Questions, "commands:typescript:.") {
		t.Fatalf("Questions = %v, want package-manager decision", result.Questions)
	}
}

func TestDetectedGoBuildCheckDoesNotWriteSingleMainBinary(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/single-main\n\ngo 1.27.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Stacks) != 1 {
		t.Fatalf("Stacks = %#v, want one Go stack", result.Stacks)
	}
	command := result.Stacks[0].Commands["build"]
	if len(command) == 0 {
		t.Fatal("Go build verification command was not detected")
	}
	before, err := repo.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("detected Go build verification failed: %v\n%s", err, output)
	}
	after, err := repo.Fingerprint(root)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("detected Go build verification mutated the repository")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.Base(root))); !os.IsNotExist(err) {
		t.Fatalf("single-main binary was written into the repository: %v", err)
	}
}

func containsQuestion(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "fixtures"))
}
