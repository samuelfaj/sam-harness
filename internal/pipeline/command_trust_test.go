package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTrustedCommandUsesOnlyExternalExecutableAndConfigFiles(t *testing.T) {
	root := t.TempDir()
	trusted := t.TempDir()
	executable := filepath.Join(trusted, "reviewer")
	writeExecutable(t, trusted, "reviewer", "#!/bin/sh\nexit 0\n")
	writeFile(t, trusted, "schema.json", "trusted\n")
	writeFile(t, root, "schema.json", "target-controlled\n")
	configPath := filepath.Join(trusted, "config.yaml")
	writeFile(t, trusted, "config.yaml", "trusted\n")
	writeFile(t, root, "config.yaml", "target\n")

	command, err := resolveTrustedCommand(root, configEvidence{source: configPath}, []string{executable, "schema.json", "review"}, true, []int{1})
	if err != nil {
		t.Fatalf("trusted command rejected: %v", err)
	}
	canonicalExecutable, err := filepath.EvalSymlinks(executable)
	if err != nil {
		t.Fatal(err)
	}
	if command[0] != canonicalExecutable || command[1] != filepath.Join(trusted, "schema.json") || command[2] != "review" {
		t.Fatalf("trusted command resolved incorrectly: %#v", command)
	}

	outside := filepath.Join(t.TempDir(), "outside.json")
	writeFile(t, filepath.Dir(outside), filepath.Base(outside), "outside\n")
	if err := os.Symlink(outside, filepath.Join(trusted, "linked.json")); err != nil {
		t.Fatal(err)
	}
	for _, argument := range []string{"missing.json", "linked.json"} {
		if _, err := resolveTrustedCommand(root, configEvidence{source: configPath}, []string{executable, argument}, true, []int{1}); err == nil {
			t.Fatalf("unsafe trusted config argument %q was accepted", argument)
		}
	}
	if _, err := resolveTrustedCommand(root, configEvidence{source: filepath.Join(root, "config.yaml")}, []string{executable}, true, nil); err == nil {
		t.Fatal("target-contained trusted configuration was accepted")
	}
}

func TestResolveTrustedCommandAcceptsOnlyPinnedNPXPackage(t *testing.T) {
	root := t.TempDir()
	trusted := t.TempDir()
	writeExecutable(t, trusted, "npx", "#!/bin/sh\nexit 0\n")
	writeFile(t, trusted, "reviewer-output.schema.json", "{}\n")
	writeFile(t, trusted, "config.yaml", "trusted\n")
	t.Setenv("PATH", trusted+string(os.PathListSeparator)+os.Getenv("PATH"))
	valid := []string{"npx", "--yes", "@openai/codex@0.150.1", "exec", "--sandbox", "read-only", "--ephemeral", "--output-schema", "reviewer-output.schema.json", "-"}
	command, err := resolveTrustedCommand(root, configEvidence{source: filepath.Join(trusted, "config.yaml")}, valid, true, []int{8})
	if err != nil {
		t.Fatalf("pinned npx command rejected: %v", err)
	}
	if command[8] != filepath.Join(trusted, "reviewer-output.schema.json") {
		t.Fatalf("trusted npx schema was not resolved from config root: %#v", command)
	}

	for _, command := range [][]string{
		{"npx", "--yes", "@openai/codex", "exec"},
		{"npx", "--yes", "@openai/codex@latest", "exec"},
		{"npx", "--yes", "@openai/codex@0.150.1", "--package", "target-package"},
		{"npx", "--yes", "@openai/codex@0.150.1", "--package=target-package"},
	} {
		if _, err := resolveTrustedCommand(root, configEvidence{source: filepath.Join(trusted, "config.yaml")}, command, true, nil); err == nil {
			t.Fatalf("unsafe npx command was accepted: %s", strings.Join(command, " "))
		}
	}
}
