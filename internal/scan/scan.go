package scan

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

var skippedDirectories = map[string]bool{
	".git": true, ".sam-harness": true, "node_modules": true, "vendor": true, "target": true,
	"dist": true, "build": true, ".venv": true, ".tox": true,
	".mypy_cache": true, ".pytest_cache": true, "__pycache__": true,
	"testdata": true, "fixtures": true,
}

func Run(path string) (model.ScanResult, error) {
	root, err := repo.ResolveRoot(path)
	if err != nil {
		return model.ScanResult{}, err
	}
	fingerprint, err := repo.Fingerprint(root)
	if err != nil {
		return model.ScanResult{}, err
	}
	head, dirty, remoteHost, gitOK := repo.GitState(root)
	result := model.ScanResult{
		Root:        root,
		Fingerprint: fingerprint,
		Git: model.GitState{
			Repository: gitOK,
			Head:       head,
			Dirty:      dirty,
			RemoteHost: remoteHost,
		},
		ExistingHarness: exists(filepath.Join(root, ".sam-harness", "config.yaml")),
		Questions: []string{
			"criticality",
			"data_sensitivity",
			"deploys_to_production",
			"persistent_data",
			"irreversible_actions",
			"approvers",
			"allow_ci_changes",
			"allowed_actions",
		},
	}

	if remoteHost != "" {
		result.CIProviders = append(result.CIProviders, remoteHost)
	}
	if exists(filepath.Join(root, ".github", "workflows")) {
		result.CIProviders = appendUnique(result.CIProviders, "github")
	}
	if exists(filepath.Join(root, ".gitlab-ci.yml")) {
		result.CIProviders = appendUnique(result.CIProviders, "gitlab")
	}

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != root && skippedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = "."
		}
		switch entry.Name() {
		case "package.json":
			stack, err := detectPackageJSON(path, dir, root)
			if err != nil {
				return err
			}
			result.Stacks = append(result.Stacks, stack)
		case "pyproject.toml":
			result.Stacks = append(result.Stacks, detectPython(path, dir))
		case "go.mod":
			result.Stacks = append(result.Stacks, detectGo(dir))
		case "Cargo.toml":
			result.Stacks = append(result.Stacks, detectRust(path, dir))
		}
		lower := strings.ToLower(rel)
		if strings.Contains(lower, "dockerfile") || strings.Contains(lower, "deploy") || strings.Contains(lower, "kubernetes") || strings.Contains(lower, "terraform") || strings.Contains(lower, "pulumi") {
			result.HasDeployment = true
		}
		if strings.Contains(lower, "migration") || strings.Contains(lower, "schema.sql") || strings.Contains(lower, "prisma") || strings.Contains(lower, "alembic") {
			result.HasPersistence = true
		}
		return nil
	})
	if err != nil {
		return model.ScanResult{}, err
	}

	result.Stacks = deduplicateStacks(result.Stacks)
	for i := range result.Stacks {
		result.HasUI = result.HasUI || result.Stacks[i].UI
		result.HasPersistence = result.HasPersistence || result.Stacks[i].Persistence
		if len(result.Stacks[i].Commands) == 0 {
			result.Questions = append(result.Questions, fmt.Sprintf("commands:%s:%s", result.Stacks[i].Kind, result.Stacks[i].Path))
		}
	}
	if result.HasUI {
		result.Questions = append(result.Questions, "design_source_of_truth")
	}
	sort.Strings(result.CIProviders)
	return result, nil
}

type packageJSON struct {
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"`
}

func detectPackageJSON(path, dir, root string) (model.Stack, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Stack{}, err
	}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return model.Stack{}, fmt.Errorf("parse %s: %w", path, err)
	}
	manager := strings.Split(pkg.PackageManager, "@")[0]
	if manager == "" {
		manager = detectPackageManager(filepath.Dir(path), root)
	}
	if !supportedPackageManager(manager) {
		manager = ""
	}
	commands := map[string][]string{}
	for _, gate := range []string{"format:check", "lint", "typecheck", "test", "build", "security"} {
		if _, ok := pkg.Scripts[gate]; ok && manager != "" {
			name := strings.ReplaceAll(gate, ":", "-")
			commands[name] = []string{manager, "run", gate}
		}
	}
	deps := mergeKeys(pkg.Dependencies, pkg.DevDependencies)
	ui := containsAny(deps, "react", "next", "vue", "@angular/core", "svelte", "solid-js", "astro")
	persistence := containsAny(deps, "prisma", "typeorm", "sequelize", "mongoose", "knex", "drizzle-orm", "@supabase/supabase-js")
	return model.Stack{Kind: "typescript", Path: dir, PackageManager: manager, Commands: commands, UI: ui, Persistence: persistence}, nil
}

func detectPython(path, dir string) model.Stack {
	data, _ := os.ReadFile(path)
	text := strings.ToLower(string(data))
	commands := map[string][]string{}
	python := pythonCommand()
	if strings.Contains(text, "pytest") {
		commands["test"] = []string{python, "-m", "pytest"}
	}
	if strings.Contains(text, "ruff") {
		commands["lint"] = []string{python, "-m", "ruff", "check", "."}
	}
	if strings.Contains(text, "mypy") {
		commands["typecheck"] = []string{python, "-m", "mypy", "."}
	}
	ui := strings.Contains(text, "django") || strings.Contains(text, "flask") || strings.Contains(text, "fastapi")
	persistence := containsTextAny(text, "sqlalchemy", "django.db", "alembic", "psycopg", "pymongo")
	return model.Stack{Kind: "python", Path: dir, PackageManager: "python", Commands: commands, UI: ui, Persistence: persistence}
}

func pythonCommand() string {
	if _, err := exec.LookPath("python"); err == nil {
		return "python"
	}
	return "python3"
}

func detectGo(dir string) model.Stack {
	return model.Stack{Kind: "go", Path: dir, PackageManager: "go", Commands: map[string][]string{
		"test":      {"go", "test", "./..."},
		"typecheck": {"go", "vet", "./..."},
		"build":     {"go", "test", "-run=^$", "./..."},
	}}
}

func detectRust(path, dir string) model.Stack {
	data, _ := os.ReadFile(path)
	text := strings.ToLower(string(data))
	return model.Stack{Kind: "rust", Path: dir, PackageManager: "cargo", Commands: map[string][]string{
		"format-check": {"cargo", "fmt", "--all", "--", "--check"},
		"lint":         {"cargo", "clippy", "--all-targets", "--all-features", "--", "-D", "warnings"},
		"test":         {"cargo", "test", "--all"},
		"build":        {"cargo", "build", "--all"},
	}, Persistence: containsTextAny(text, "diesel", "sqlx", "sea-orm", "rusqlite")}
}

func detectPackageManager(dir, root string) string {
	root = filepath.Clean(root)
	for current := filepath.Clean(dir); ; current = filepath.Dir(current) {
		var found []string
		for _, candidate := range []struct {
			name  string
			files []string
		}{
			{name: "pnpm", files: []string{"pnpm-lock.yaml"}},
			{name: "yarn", files: []string{"yarn.lock"}},
			{name: "bun", files: []string{"bun.lockb", "bun.lock"}},
			{name: "npm", files: []string{"package-lock.json"}},
		} {
			for _, file := range candidate.files {
				if exists(filepath.Join(current, file)) {
					found = append(found, candidate.name)
					break
				}
			}
		}
		if len(found) == 1 {
			return found[0]
		}
		if len(found) > 1 {
			return ""
		}
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current || !pathWithinRoot(root, parent) {
			break
		}
	}
	return ""
}

func pathWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func supportedPackageManager(value string) bool {
	switch value {
	case "npm", "pnpm", "yarn", "bun":
		return true
	default:
		return false
	}
}

func deduplicateStacks(stacks []model.Stack) []model.Stack {
	seen := map[string]bool{}
	var result []model.Stack
	for _, stack := range stacks {
		key := stack.Kind + ":" + stack.Path
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, stack)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Path == result[j].Path {
			return result[i].Kind < result[j].Kind
		}
		return result[i].Path < result[j].Path
	})
	return result
}

func mergeKeys(maps ...map[string]string) map[string]bool {
	result := map[string]bool{}
	for _, values := range maps {
		for key := range values {
			result[key] = true
		}
	}
	return result
}

func containsAny(values map[string]bool, names ...string) bool {
	for _, name := range names {
		if values[name] {
			return true
		}
	}
	return false
}

func containsTextAny(text string, names ...string) bool {
	for _, name := range names {
		if strings.Contains(text, name) {
			return true
		}
	}
	return false
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
