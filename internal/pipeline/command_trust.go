package pipeline

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

var trustedRuntimeInterpreters = map[string]bool{
	"bash": true, "bun": true, "dash": true, "deno": true,
	"env": true, "go": true, "ksh": true, "node": true, "nodejs": true,
	"npm": true, "npx": true, "osascript": true, "perl": true,
	"php": true, "pnpm": true, "powershell": true, "pwsh": true,
	"python": true, "python3": true, "ruby": true, "sh": true,
	"uv": true, "xargs": true, "yarn": true, "zsh": true,
}

var trustedRuntimeFileExtensions = map[string]bool{
	".bash": true, ".cjs": true, ".js": true, ".json": true,
	".mjs": true, ".ps1": true, ".py": true, ".rb": true,
	".schema": true, ".sh": true, ".toml": true, ".ts": true,
	".tsx": true, ".xml": true, ".yaml": true, ".yml": true,
	".zsh": true,
}

var pinnedNPMPackage = regexp.MustCompile(`^(?:@[A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9][A-Za-z0-9._-]*|[A-Za-z0-9][A-Za-z0-9._-]*)@[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?(?:\+[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)

var forbiddenRuntimeNPXOptions = map[string]bool{
	"--call": true, "--package": true, "--shell": true,
	"-c": true, "-p": true,
}

func secretScopeBound(cfg model.Config, scope string) bool {
	for _, bindings := range cfg.CI.SecretBindings {
		for _, binding := range bindings {
			if binding.Scope == scope {
				return true
			}
		}
	}
	return false
}

func requireExternalConfig(root string, evidence configEvidence, scope string) error {
	if evidence.source == "" {
		return fmt.Errorf("secret-bearing %s requires a canonical trusted configuration", scope)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("canonicalize target repository for %s: %w", scope, err)
	}
	canonicalSource, err := filepath.EvalSymlinks(evidence.source)
	if err != nil {
		return fmt.Errorf("canonicalize trusted configuration for %s: %w", scope, err)
	}
	if pathWithin(canonicalSource, canonicalRoot) {
		return fmt.Errorf("secret-bearing %s requires --config outside the target repository", scope)
	}
	return nil
}

func resolveTrustedCommand(root string, evidence configEvidence, command []string, attested bool, trustedArguments []int) ([]string, error) {
	if !attested {
		return nil, errors.New("trusted_external_command attestation is required")
	}
	if len(command) == 0 {
		return nil, errors.New("trusted command must contain argv")
	}
	if err := requireExternalConfig(root, evidence, "agent command"); err != nil {
		return nil, err
	}

	resolved := append([]string(nil), command...)
	executable, err := resolveExternalExecutable(root, command[0])
	if err != nil {
		return nil, err
	}
	resolved[0] = executable

	trusted := make(map[int]bool, len(trustedArguments))
	for _, index := range trustedArguments {
		if index <= 0 || index >= len(command) || trusted[index] {
			return nil, fmt.Errorf("trusted_config_arguments index %d is invalid", index)
		}
		trusted[index] = true
	}
	configRoot := filepath.Dir(evidence.source)
	executableName := strings.ToLower(filepath.Base(executable))
	interpreter := trustedRuntimeInterpreters[executableName]
	npxPackageIndex := -1
	if executableName == "npx" {
		interpreter = false
		var err error
		npxPackageIndex, err = validateTrustedNPX(command, trusted)
		if err != nil {
			return nil, err
		}
	}
	for index := 1; index < len(command); index++ {
		if trusted[index] {
			argument, err := resolveTrustedConfigArgument(configRoot, command[index])
			if err != nil {
				return nil, fmt.Errorf("trusted command argument %d: %w", index, err)
			}
			resolved[index] = argument
			continue
		}
		if index == npxPackageIndex {
			continue
		}
		if runtimeArgumentMayReferenceTarget(root, command[index], interpreter, npxPackageIndex < 0) {
			return nil, fmt.Errorf("trusted command argument %d %q may reference target-controlled content", index, command[index])
		}
	}
	return resolved, nil
}

func validateTrustedNPX(command []string, trusted map[int]bool) (int, error) {
	for index := 1; index < len(command); index++ {
		argument := command[index]
		if argument == "--yes" || argument == "-y" {
			continue
		}
		if strings.HasPrefix(argument, "-") {
			return -1, fmt.Errorf("npx option %q is not allowed before the pinned package", argument)
		}
		if trusted[index] {
			return -1, fmt.Errorf("trusted npx package argument %d must not be a trusted_config_arguments path", index)
		}
		if !pinnedNPMPackage.MatchString(argument) {
			return -1, fmt.Errorf("trusted npx package %q must use an exact pinned version", argument)
		}
		for _, remaining := range command[index+1:] {
			option := remaining
			if name, _, found := strings.Cut(option, "="); found {
				option = name
			}
			if forbiddenRuntimeNPXOptions[option] {
				return -1, fmt.Errorf("npx option %q can add or replace executable package content", remaining)
			}
		}
		return index, nil
	}
	return -1, errors.New("trusted npx command requires an exact pinned package")
}

func resolveExternalExecutable(root, configured string) (string, error) {
	if configured == "." || configured == ".." || (!filepath.IsAbs(configured) && strings.ContainsAny(configured, `/\\`)) {
		return "", fmt.Errorf("trusted executable %q is relative to the target repository", configured)
	}
	resolved, err := exec.LookPath(configured)
	if err != nil {
		return "", fmt.Errorf("resolve trusted executable %q: %w", configured, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("canonicalize trusted executable %q: %w", configured, err)
	}
	resolved, err = filepath.EvalSymlinks(resolved)
	if err != nil {
		return "", fmt.Errorf("canonicalize trusted executable %q: %w", configured, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect trusted executable %q: %w", configured, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("trusted executable %q is not a regular file", configured)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("canonicalize target repository: %w", err)
	}
	if pathWithin(resolved, canonicalRoot) {
		return "", fmt.Errorf("trusted executable %q resolves inside the target repository", configured)
	}
	return filepath.Clean(resolved), nil
}

func pathsOverlap(left, right string) (bool, error) {
	canonicalLeft, err := filepath.EvalSymlinks(left)
	if err != nil {
		return false, err
	}
	canonicalRight, err := filepath.EvalSymlinks(right)
	if err != nil {
		return false, err
	}
	return pathWithin(canonicalLeft, canonicalRight) || pathWithin(canonicalRight, canonicalLeft), nil
}

func resolveTrustedConfigArgument(configRoot, argument string) (string, error) {
	if argument == "" || filepath.IsAbs(filepath.FromSlash(argument)) || strings.ContainsRune(argument, '\x00') {
		return "", errors.New("must be a safe relative path")
	}
	clean := filepath.Clean(filepath.FromSlash(argument))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", errors.New("escapes the trusted configuration directory")
	}
	candidate := filepath.Join(configRoot, clean)
	if !pathWithin(candidate, configRoot) {
		return "", errors.New("escapes the trusted configuration directory")
	}
	current := configRoot
	for _, component := range strings.Split(clean, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return "", fmt.Errorf("inspect %s: %w", argument, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%s must not contain symbolic links", argument)
		}
	}
	info, err := os.Lstat(candidate)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", argument, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s must be a regular file", argument)
	}
	return filepath.Clean(candidate), nil
}

func runtimeArgumentMayReferenceTarget(root, argument string, interpreter, checkExisting bool) bool {
	value := argument
	if strings.HasPrefix(value, "-") {
		_, optionValue, found := strings.Cut(value, "=")
		if !found {
			return false
		}
		value = optionValue
	}
	if value == "" || value == "-" {
		return false
	}
	if interpreter && !strings.HasPrefix(argument, "-") {
		return true
	}
	pathValue := filepath.FromSlash(value)
	if filepath.IsAbs(pathValue) || strings.ContainsAny(value, `/\\`) || strings.HasPrefix(value, ".") || trustedRuntimeFileExtensions[strings.ToLower(filepath.Ext(value))] {
		return true
	}
	if !checkExisting {
		return false
	}
	_, err := os.Lstat(filepath.Join(root, pathValue))
	return err == nil || !os.IsNotExist(err)
}
