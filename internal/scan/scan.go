package scan

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/repo"
	"gopkg.in/yaml.v3"
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
	result.CICommands, err = detectCICommands(root)
	if err != nil {
		return model.ScanResult{}, err
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
	if containsAny(deps, "@playwright/test", "playwright") {
		commands["browser"] = []string{"npx", "playwright", "test"}
	} else if containsAny(deps, "cypress") {
		if _, ok := pkg.Scripts["cypress"]; ok && manager != "" {
			commands["browser"] = []string{manager, "run", "cypress"}
		}
	}
	if containsAny(deps, "pa11y", "@axe-core/cli") {
		if containsAny(deps, "pa11y") {
			commands["accessibility"] = []string{"npx", "pa11y"}
		} else {
			commands["accessibility"] = []string{"npx", "@axe-core/cli"}
		}
	}
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

func detectCICommands(root string) ([]model.CICommand, error) {
	var commands []model.CICommand
	gitlabPath := filepath.Join(root, ".gitlab-ci.yml")
	if exists(gitlabPath) {
		detected, err := detectGitLabCommands(root, gitlabPath)
		if err != nil {
			return nil, err
		}
		commands = append(commands, detected...)
	}
	workflowDirectory := filepath.Join(root, ".github", "workflows")
	entries, err := os.ReadDir(workflowDirectory)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		extension := strings.ToLower(filepath.Ext(entry.Name()))
		if entry.IsDir() || (extension != ".yml" && extension != ".yaml") || strings.HasPrefix(entry.Name(), "sam-harness") {
			continue
		}
		path := filepath.Join(workflowDirectory, entry.Name())
		detected, err := detectGitHubCommands(root, path)
		if err != nil {
			return nil, err
		}
		commands = append(commands, detected...)
	}
	sort.Slice(commands, func(i, j int) bool {
		left, right := commands[i], commands[j]
		return left.Provider+"\x00"+left.File+"\x00"+left.Job+"\x00"+left.Workdir+"\x00"+strings.Join(left.Command, "\x00") < right.Provider+"\x00"+right.File+"\x00"+right.Job+"\x00"+right.Workdir+"\x00"+strings.Join(right.Command, "\x00")
	})
	return commands, nil
}

func detectGitLabCommands(root, path string) ([]model.CICommand, error) {
	document, err := readYAML(path)
	if err != nil {
		return nil, err
	}
	rootNode := documentContent(document)
	if rootNode == nil || rootNode.Kind != yaml.MappingNode {
		return nil, nil
	}
	definitions := map[string]*yaml.Node{}
	for index := 0; index+1 < len(rootNode.Content); index += 2 {
		if rootNode.Content[index+1].Kind == yaml.MappingNode {
			definitions[rootNode.Content[index].Value] = rootNode.Content[index+1]
		}
	}
	defaultBeforeScript := nestedNode(rootNode, "default", "before_script")
	if defaultBeforeScript == nil {
		defaultBeforeScript = mappingValue(rootNode, "before_script")
	}
	rel, _ := filepath.Rel(root, path)
	var result []model.CICommand
	for index := 0; index+1 < len(rootNode.Content); index += 2 {
		job := rootNode.Content[index].Value
		definition := rootNode.Content[index+1]
		if strings.HasPrefix(job, ".") || definition.Kind != yaml.MappingNode || permitsFailure(mappingValue(definition, "allow_failure")) {
			continue
		}
		script := inheritedGitLabField(definitions, job, "script", map[string]bool{})
		if script == nil {
			continue
		}
		workdir := "."
		beforeScript := inheritedGitLabField(definitions, job, "before_script", map[string]bool{})
		if beforeScript == nil {
			beforeScript = defaultBeforeScript
		}
		for _, block := range append(scriptBlocks(beforeScript), scriptBlocks(script)...) {
			commands, nextWorkdir, ok := commandsFromBlock("gitlab", filepath.ToSlash(rel), job, workdir, block)
			if !ok {
				if mightChangeDirectory(block) {
					break
				}
				continue
			}
			result = append(result, commands...)
			workdir = nextWorkdir
		}
	}
	return result, nil
}

func inheritedGitLabField(definitions map[string]*yaml.Node, job, field string, seen map[string]bool) *yaml.Node {
	if seen[job] {
		return nil
	}
	seen[job] = true
	definition := definitions[job]
	if value := mappingValue(definition, field); value != nil {
		return value
	}
	parents := scalarList(mappingValue(definition, "extends"))
	for index := len(parents) - 1; index >= 0; index-- {
		if value := inheritedGitLabField(definitions, parents[index], field, seen); value != nil {
			return value
		}
	}
	return nil
}

func detectGitHubCommands(root, path string) ([]model.CICommand, error) {
	document, err := readYAML(path)
	if err != nil {
		return nil, err
	}
	rootNode := documentContent(document)
	jobs := mappingValue(rootNode, "jobs")
	if jobs == nil || jobs.Kind != yaml.MappingNode {
		return nil, nil
	}
	rel, _ := filepath.Rel(root, path)
	workflowWorkdir := nestedScalar(rootNode, "defaults", "run", "working-directory")
	var result []model.CICommand
	for index := 0; index+1 < len(jobs.Content); index += 2 {
		job := jobs.Content[index].Value
		definition := jobs.Content[index+1]
		jobWorkdir := nestedScalar(definition, "defaults", "run", "working-directory")
		if jobWorkdir == "" {
			jobWorkdir = workflowWorkdir
		}
		if jobWorkdir == "" {
			jobWorkdir = "."
		}
		jobWorkdir, ok := cleanWorkdir(jobWorkdir)
		if !ok {
			continue
		}
		steps := mappingValue(definition, "steps")
		if steps == nil || steps.Kind != yaml.SequenceNode {
			continue
		}
		for _, step := range steps.Content {
			run := mappingValue(step, "run")
			if run == nil || run.Kind != yaml.ScalarNode || mappingValue(step, "if") != nil || permitsFailure(mappingValue(step, "continue-on-error")) {
				continue
			}
			workdir := scalarValue(mappingValue(step, "working-directory"))
			if workdir == "" {
				workdir = jobWorkdir
			} else {
				workdir, ok = cleanWorkdir(workdir)
				if !ok {
					continue
				}
			}
			commands, _, ok := commandsFromBlock("github", filepath.ToSlash(rel), job, workdir, run.Value)
			if ok {
				result = append(result, commands...)
			}
		}
	}
	return result, nil
}

func commandsFromBlock(provider, file, job, initialWorkdir, block string) ([]model.CICommand, string, bool) {
	workdir, ok := cleanWorkdir(initialWorkdir)
	if !ok {
		return nil, initialWorkdir, false
	}
	var result []model.CICommand
	for _, raw := range strings.Split(block, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts, ok := splitLiteralAnd(line)
		if !ok {
			return nil, initialWorkdir, false
		}
		if len(parts) > 1 {
			prefix, prefixOK := splitSimpleShell(parts[0])
			if len(parts) != 2 || !prefixOK || len(prefix) != 2 || prefix[0] != "cd" {
				return nil, initialWorkdir, false
			}
		}
		for _, part := range parts {
			argv, ok := splitSimpleShell(part)
			if !ok || len(argv) == 0 || isShellControl(argv[0]) {
				return nil, initialWorkdir, false
			}
			if argv[0] == "cd" {
				if len(argv) != 2 {
					return nil, initialWorkdir, false
				}
				workdir, ok = joinWorkdir(workdir, argv[1])
				if !ok {
					return nil, initialWorkdir, false
				}
				continue
			}
			result = append(result, model.CICommand{Provider: provider, File: file, Job: job, Workdir: workdir, Command: argv})
		}
	}
	return result, workdir, true
}

func splitLiteralAnd(line string) ([]string, bool) {
	var parts []string
	start := 0
	quote := byte(0)
	escaped := false
	for index := 0; index < len(line); index++ {
		current := line[index]
		if escaped {
			escaped = false
			continue
		}
		if current == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if current == quote {
				quote = 0
			}
			continue
		}
		if current == '\'' || current == '"' {
			quote = current
			continue
		}
		if strings.ContainsRune("|;`<>(){}", rune(current)) {
			return nil, false
		}
		if current == '&' {
			if index+1 >= len(line) || line[index+1] != '&' {
				return nil, false
			}
			part := strings.TrimSpace(line[start:index])
			if part == "" {
				return nil, false
			}
			parts = append(parts, part)
			index++
			start = index + 1
		}
	}
	if escaped || quote != 0 {
		return nil, false
	}
	last := strings.TrimSpace(line[start:])
	if last == "" {
		return nil, false
	}
	return append(parts, last), true
}

func splitSimpleShell(line string) ([]string, bool) {
	var argv []string
	var word strings.Builder
	quote := rune(0)
	escaped := false
	started := false
	flush := func() {
		if started {
			argv = append(argv, word.String())
			word.Reset()
			started = false
		}
	}
	for index := 0; index < len(line); {
		current, size := utf8.DecodeRuneInString(line[index:])
		if current == utf8.RuneError && size == 1 {
			return nil, false
		}
		if escaped {
			word.WriteRune(current)
			started = true
			escaped = false
			index += size
			continue
		}
		if current == '\\' && quote != '\'' {
			escaped = true
			started = true
			index += size
			continue
		}
		if quote != 0 {
			if current == quote {
				quote = 0
			} else {
				if quote == '"' && (current == '`' || dynamicDollar(line[index:])) {
					return nil, false
				}
				word.WriteRune(current)
			}
			started = true
			index += size
			continue
		}
		if current == '\'' || current == '"' {
			quote = current
			started = true
			index += size
			continue
		}
		if unicode.IsSpace(current) {
			flush()
			index += size
			continue
		}
		if current == '#' && !started {
			break
		}
		if current == '`' || current == '*' || current == '?' || current == '[' || dynamicDollar(line[index:]) {
			return nil, false
		}
		word.WriteRune(current)
		started = true
		index += size
	}
	if escaped || quote != 0 {
		return nil, false
	}
	flush()
	return argv, len(argv) > 0
}

func dynamicDollar(value string) bool {
	if !strings.HasPrefix(value, "$") || len(value) == 1 {
		return false
	}
	next, _ := utf8.DecodeRuneInString(value[1:])
	return next == '{' || next == '(' || next == '$' || next == '*' || next == '@' || next == '#' || next == '?' || next == '-' || next == '!' || next == '_' || unicode.IsLetter(next) || unicode.IsDigit(next)
}

func isShellControl(value string) bool {
	switch value {
	case "if", "then", "else", "elif", "fi", "for", "while", "until", "do", "done", "case", "esac", "function", "export", "source", ".", "eval", "exec", "set", "trap":
		return true
	default:
		return false
	}
}

func cleanWorkdir(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || filepath.IsAbs(value) || strings.ContainsAny(value, "$`*?[") {
		return "", false
	}
	value = filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if value == "." {
		return value, true
	}
	if value == ".." || strings.HasPrefix(value, "../") {
		return "", false
	}
	return value, true
}

func joinWorkdir(base, child string) (string, bool) {
	if filepath.IsAbs(child) {
		return "", false
	}
	return cleanWorkdir(filepath.Join(filepath.FromSlash(base), filepath.FromSlash(child)))
}

func mightChangeDirectory(block string) bool {
	for _, raw := range strings.Split(block, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "cd ") || strings.Contains(line, "; cd ") || strings.Contains(line, "&& cd ") || strings.HasPrefix(line, "pushd ") {
			return true
		}
	}
	return false
}

func readYAML(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &document, nil
}

func documentContent(document *yaml.Node) *yaml.Node {
	if document == nil || len(document.Content) == 0 {
		return nil
	}
	return document.Content[0]
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func nestedNode(node *yaml.Node, keys ...string) *yaml.Node {
	for _, key := range keys {
		node = mappingValue(node, key)
		if node == nil {
			return nil
		}
	}
	return node
}

func nestedScalar(node *yaml.Node, keys ...string) string {
	return scalarValue(nestedNode(node, keys...))
}

func scalarValue(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode {
		return ""
	}
	return strings.TrimSpace(node.Value)
}

func permitsFailure(node *yaml.Node) bool {
	return node != nil && (node.Kind != yaml.ScalarNode || strings.TrimSpace(node.Value) != "false")
}

func scalarList(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		return []string{strings.TrimSpace(node.Value)}
	}
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	var values []string
	for _, item := range node.Content {
		if item.Kind == yaml.ScalarNode && strings.TrimSpace(item.Value) != "" {
			values = append(values, strings.TrimSpace(item.Value))
		}
	}
	return values
}

func scriptBlocks(node *yaml.Node) []string {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		return []string{node.Value}
	}
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	var blocks []string
	for _, item := range node.Content {
		if item.Kind == yaml.ScalarNode {
			blocks = append(blocks, item.Value)
		}
	}
	return blocks
}
