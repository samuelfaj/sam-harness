package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	configstore "github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
)

type configEvidence struct {
	source string
	sha256 string
	file   os.FileInfo
}

func loadRuntimeConfig(root, requested string) (model.Config, configEvidence, error) {
	source, err := canonicalConfigPath(root, requested)
	if err != nil {
		return model.Config{}, configEvidence{}, err
	}
	data, info, err := readRegularConfig(source)
	if err != nil {
		return model.Config{}, configEvidence{}, err
	}
	cfg, err := configstore.Parse(data)
	if err != nil {
		return model.Config{}, configEvidence{}, err
	}
	digest := sha256.Sum256(data)
	return cfg, configEvidence{source: source, sha256: hex.EncodeToString(digest[:]), file: info}, nil
}

func canonicalConfigPath(root, requested string) (string, error) {
	relative := !filepath.IsAbs(requested)
	if strings.TrimSpace(requested) == "" {
		requested = filepath.Join(".sam-harness", "config.yaml")
		relative = true
	}

	candidate := requested
	if relative {
		clean := filepath.Clean(filepath.FromSlash(requested))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("configuration path escapes repository: %q", requested)
		}
		candidate = filepath.Join(root, clean)
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("canonicalize configuration path: %w", err)
	}
	candidate = filepath.Clean(candidate)
	info, err := os.Lstat(candidate)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("configuration file does not exist: %s", candidate)
	}
	if err != nil {
		return "", fmt.Errorf("inspect configuration file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("configuration file must not be a symbolic link: %s", candidate)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("configuration file must be a regular file: %s", candidate)
	}

	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("canonicalize configuration file: %w", err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return "", fmt.Errorf("canonicalize configuration file: %w", err)
	}
	canonical = filepath.Clean(canonical)
	canonicalInfo, err := os.Lstat(canonical)
	if err != nil {
		return "", fmt.Errorf("inspect canonical configuration file: %w", err)
	}
	if canonicalInfo.Mode()&os.ModeSymlink != 0 || !canonicalInfo.Mode().IsRegular() || !os.SameFile(info, canonicalInfo) {
		return "", fmt.Errorf("configuration file changed while it was canonicalized: %s", candidate)
	}
	if relative {
		canonicalRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return "", fmt.Errorf("canonicalize repository root: %w", err)
		}
		contained, err := filepath.Rel(canonicalRoot, canonical)
		if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("configuration path escapes repository: %q", requested)
		}
	}
	return canonical, nil
}

func readRegularConfig(path string) ([]byte, os.FileInfo, error) {
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("configuration file does not exist: %s", path)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("inspect configuration file: %w", err)
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("configuration file must not be a symbolic link: %s", path)
	}
	if !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("configuration file must be a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open configuration file: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect opened configuration file: %w", err)
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, nil, fmt.Errorf("configuration file changed before it was opened: %s", path)
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, fmt.Errorf("read configuration file: %w", err)
	}
	return data, opened, nil
}

func bindConfigEvidence(receipt *Receipt, evidence configEvidence) {
	receipt.ConfigSource = evidence.source
	receipt.ConfigSHA256 = evidence.sha256
}

func verifyConfigEvidence(evidence configEvidence) error {
	data, info, err := readRegularConfig(evidence.source)
	if err != nil {
		return fmt.Errorf("configuration source changed: %w", err)
	}
	if evidence.file == nil || !os.SameFile(evidence.file, info) {
		return fmt.Errorf("configuration source changed: file identity mismatch: %s", evidence.source)
	}
	digest := sha256.Sum256(data)
	current := hex.EncodeToString(digest[:])
	if current != evidence.sha256 {
		return fmt.Errorf("configuration source changed: config digest mismatch: loaded %s, current %s", evidence.sha256, current)
	}
	return nil
}

func containedConfigPath(root string, evidence configEvidence) (string, bool, error) {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false, fmt.Errorf("canonicalize repository root: %w", err)
	}
	relative, err := filepath.Rel(canonicalRoot, evidence.source)
	if err != nil {
		return "", false, err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false, nil
	}
	return filepath.ToSlash(relative), true, nil
}
