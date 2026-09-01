package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDetectGitLabCommandsTracksLiteralWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitlab-ci.yml")
	content := `stages: [build]
build-client:
  stage: build
  script:
    - cd frontend-client
    - bun run build
dynamic:
  script:
    - export TARGET=frontend
    - cd "$TARGET" && bun run build
guarded:
  script:
    - test -f package.json && bun run build
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	commands, err := detectGitLabCommands(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %#v, want one conservative literal command", commands)
	}
	command := commands[0]
	if command.Provider != "gitlab" || command.Job != "build-client" || command.Workdir != "frontend-client" || !reflect.DeepEqual(command.Command, []string{"bun", "run", "build"}) {
		t.Fatalf("command = %#v", command)
	}
}

func TestDetectGitLabCommandsResolvesInheritedWorkingDirectoryAndRejectsConditionalBlocks(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitlab-ci.yml")
	content := `.frontend:
  before_script:
    - cd frontend
build:
  extends: .frontend
  script:
    - bun run build
conditional:
  script:
    - |
      if [ "$TARGET" = frontend ]; then
        bun run build
      fi
dynamic-directory:
  script:
    - cd "$TARGET"
    - bun run build
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	commands, err := detectGitLabCommands(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Job != "build" || commands[0].Workdir != "frontend" || !reflect.DeepEqual(commands[0].Command, []string{"bun", "run", "build"}) {
		t.Fatalf("commands = %#v, want only inherited literal build", commands)
	}
}

func TestDetectGitHubCommandsUsesStepAndJobWorkingDirectories(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workflowDir, "ci.yml")
	content := `name: CI
jobs:
  checks:
    defaults:
      run:
        working-directory: backend
    steps:
      - run: go test ./...
      - working-directory: frontend
        run: bun run test
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	commands, err := detectGitHubCommands(root, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %#v, want two", commands)
	}
	if commands[0].Workdir != "backend" || !reflect.DeepEqual(commands[0].Command, []string{"go", "test", "./..."}) {
		t.Fatalf("first command = %#v", commands[0])
	}
	if commands[1].Workdir != "frontend" || !reflect.DeepEqual(commands[1].Command, []string{"bun", "run", "test"}) {
		t.Fatalf("second command = %#v", commands[1])
	}
}

func TestDetectGitHubCommandsRejectsConditionalAndDynamicSteps(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workflowDir, "ci.yaml")
	content := `jobs:
  checks:
    steps:
      - if: github.ref == 'refs/heads/main'
        run: go test ./...
      - run: ${{ matrix.command }}
      - continue-on-error: true
        run: go test ./...
      - run: go test -run=^$ ./...
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	commands, err := detectGitHubCommands(root, path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"go", "test", "-run=^$", "./..."}
	if len(commands) != 1 || !reflect.DeepEqual(commands[0].Command, want) {
		t.Fatalf("commands = %#v, want only unconditional literal command %#v", commands, want)
	}
}

func TestDetectCICommandsIgnoresManagedHarnessWorkflow(t *testing.T) {
	root := t.TempDir()
	workflowDir := filepath.Join(root, ".github", "workflows")
	if err := os.MkdirAll(workflowDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "jobs:\n  static:\n    steps:\n      - run: go test ./...\n"
	if err := os.WriteFile(filepath.Join(workflowDir, "sam-harness.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	commands, err := detectCICommands(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 0 {
		t.Fatalf("managed workflow commands = %#v, want none", commands)
	}
}
