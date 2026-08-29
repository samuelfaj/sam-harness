package architecture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRejectsForbiddenInternalImport(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeGo(t, root, filepath.Join("internal", "pipeline", "dep.go"), `package pipeline
import "github.com/samuelfaj/sam-harness/internal/cli"
`)
	err := Check(root)
	if err == nil || !strings.Contains(err.Error(), "internal/pipeline/dep.go imports github.com/samuelfaj/sam-harness/internal/cli") {
		t.Fatalf("Check() = %v, want forbidden pipeline→cli import", err)
	}
}

func TestCheckAllowsDeclaredDependencies(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeGo(t, root, filepath.Join("internal", "pipeline", "ok.go"), `package pipeline
import "github.com/samuelfaj/sam-harness/internal/model"
`)
	writeGo(t, root, filepath.Join("cmd", "sam-harness", "main.go"), `package main
import "github.com/samuelfaj/sam-harness/internal/cli"
`)
	if err := Check(root); err != nil {
		t.Fatalf("Check() rejected a permitted import: %v", err)
	}
}

func TestCheckPassesThisRepository(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	if err := Check(root); err != nil {
		t.Fatalf("this repository violates declared import boundaries: %v", err)
	}
}

func writeGo(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
