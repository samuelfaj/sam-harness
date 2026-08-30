package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const repairManifestSchemaVersion = "1"

func attachRepairManifest(receipt *Receipt) error {
	if len(receipt.Findings) == 0 {
		return errors.New("repair manifest requires at least one finding")
	}
	manifest := RepairManifest{
		SchemaVersion:         repairManifestSchemaVersion,
		Repository:            receipt.Repository,
		ReviewBaseSHA:         receipt.ReviewBaseSHA,
		ReviewBaseFingerprint: receipt.ReviewBaseFingerprint,
		ReviewHeadSHA:         receipt.ReviewHeadSHA,
		ReviewHeadFingerprint: receipt.ReviewHeadFingerprint,
		ReviewPatchSHA256:     receipt.ReviewPatchSHA256,
		Actions:               append([]Finding(nil), receipt.Findings...),
	}
	digest, err := repairManifestDigest(manifest)
	if err != nil {
		return err
	}
	receipt.RepairManifest = &manifest
	receipt.RepairManifestSHA256 = digest
	return nil
}

func validateRepairManifest(receipt Receipt) error {
	if receipt.RepairManifest == nil || receipt.RepairManifestSHA256 == "" {
		return errors.New("failed review receipt has no complete repair manifest")
	}
	manifest := *receipt.RepairManifest
	if manifest.SchemaVersion != repairManifestSchemaVersion || manifest.Repository != receipt.Repository || manifest.ReviewBaseSHA != receipt.ReviewBaseSHA || manifest.ReviewBaseFingerprint != receipt.ReviewBaseFingerprint || manifest.ReviewHeadSHA != receipt.ReviewHeadSHA || manifest.ReviewHeadFingerprint != receipt.ReviewHeadFingerprint || manifest.ReviewPatchSHA256 != receipt.ReviewPatchSHA256 {
		return errors.New("failed review receipt repair manifest lineage does not match the review")
	}
	if len(manifest.Actions) == 0 || len(manifest.Actions) != len(receipt.Findings) {
		return errors.New("failed review receipt repair manifest actions do not match findings")
	}
	hasBlockingFinding := false
	for index, action := range manifest.Actions {
		if err := validateFinding(action); err != nil {
			return fmt.Errorf("failed review receipt repair manifest action %d is invalid: %w", index, err)
		}
		if action != receipt.Findings[index] {
			return errors.New("failed review receipt repair manifest actions do not match findings")
		}
		if action.Severity == "P0" || action.Severity == "P1" {
			hasBlockingFinding = true
		}
	}
	if receipt.Status != StatusPassed && !hasBlockingFinding {
		return errors.New("failed review receipt repair manifest has no blocking finding")
	}
	digest, err := repairManifestDigest(manifest)
	if err != nil {
		return err
	}
	if digest != receipt.RepairManifestSHA256 {
		return errors.New("failed review receipt repair manifest digest does not match")
	}
	return nil
}

func validateFinding(finding Finding) error {
	if !finding.Role.Valid() {
		return fmt.Errorf("invalid role %q", finding.Role)
	}
	switch finding.Severity {
	case "P0", "P1", "P2", "P3":
	default:
		return fmt.Errorf("invalid severity %q", finding.Severity)
	}
	if strings.TrimSpace(finding.Summary) == "" || strings.TrimSpace(finding.Evidence) == "" || strings.TrimSpace(finding.RequiredChange) == "" || strings.TrimSpace(finding.Acceptance) == "" {
		return errors.New("required text is empty")
	}
	path := filepath.ToSlash(finding.Path)
	if path == "" || path != finding.Path || path != strings.TrimSpace(path) || strings.Contains(path, `\`) || filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, "../") || filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path {
		return errors.New("path must be a clean repository-relative path")
	}
	if finding.Line < 0 {
		return errors.New("line must be zero or greater")
	}
	return nil
}

func repairManifestDigest(manifest RepairManifest) (string, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encode repair manifest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
