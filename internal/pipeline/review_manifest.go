package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

const repairManifestSchemaVersion = "2"

const (
	reviewConvergenceInitial = "initial"
	reviewConvergencePassed  = "converged"
	reviewConvergenceBlocked = "blocked"

	findingStatusOpen       = "open"
	findingStatusUnresolved = "unresolved"
	findingStatusRecorded   = "recorded"
	findingStatusRegression = "regression"
)

func attachRepairManifest(receipt *Receipt) error {
	if len(receipt.Findings) == 0 {
		return errors.New("repair manifest requires at least one finding")
	}
	if receipt.ReviewLineageSHA256 == "" {
		receipt.ReviewLineageSHA256 = reviewLineageDigest(receipt)
	}
	normalizeFindings(receipt.Findings, findingStatusOpen, receipt.ReviewLineageSHA256)
	manifest := RepairManifest{
		SchemaVersion:         repairManifestSchemaVersion,
		Repository:            receipt.Repository,
		ReviewBaseSHA:         receipt.ReviewBaseSHA,
		ReviewBaseFingerprint: receipt.ReviewBaseFingerprint,
		ReviewHeadSHA:         receipt.ReviewHeadSHA,
		ReviewHeadFingerprint: receipt.ReviewHeadFingerprint,
		ReviewPatchSHA256:     receipt.ReviewPatchSHA256,
		LineageSHA256:         receipt.ReviewLineageSHA256,
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
	if manifest.SchemaVersion != repairManifestSchemaVersion || manifest.Repository != receipt.Repository || manifest.ReviewBaseSHA != receipt.ReviewBaseSHA || manifest.ReviewBaseFingerprint != receipt.ReviewBaseFingerprint || manifest.ReviewHeadSHA != receipt.ReviewHeadSHA || manifest.ReviewHeadFingerprint != receipt.ReviewHeadFingerprint || manifest.ReviewPatchSHA256 != receipt.ReviewPatchSHA256 || manifest.LineageSHA256 != receipt.ReviewLineageSHA256 {
		return errors.New("failed review receipt repair manifest lineage does not match the review")
	}
	if len(manifest.Actions) == 0 || len(manifest.Actions) != len(receipt.Findings) {
		return errors.New("failed review receipt repair manifest actions do not match findings")
	}
	hasBlockingFinding := false
	seenIDs := make(map[string]bool, len(manifest.Actions))
	for index, action := range manifest.Actions {
		if action != receipt.Findings[index] {
			return errors.New("failed review receipt repair manifest actions do not match findings")
		}
		if err := validatePersistedFinding(action, findingStatusOpen, manifest.LineageSHA256); err != nil {
			return fmt.Errorf("failed review receipt repair manifest action %d is invalid: %w", index, err)
		}
		if seenIDs[action.ID] {
			return fmt.Errorf("failed review receipt repair manifest action %d duplicates finding identity %s", index, action.ID)
		}
		seenIDs[action.ID] = true
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

func normalizeFindings(findings []Finding, status, lineage string) {
	for index := range findings {
		if findings[index].ID == "" {
			findings[index].ID = findingIdentity(findings[index])
		}
		findings[index].Status = status
		findings[index].Lineage = lineage
	}
}

func findingIdentity(finding Finding) string {
	canonical := struct {
		Role           model.ReviewerRole `json:"role"`
		Severity       string             `json:"severity"`
		Summary        string             `json:"summary"`
		Evidence       string             `json:"evidence"`
		Path           string             `json:"path"`
		Line           int                `json:"line"`
		RequiredChange string             `json:"required_change"`
		Acceptance     string             `json:"acceptance"`
	}{
		Role: finding.Role, Severity: finding.Severity, Summary: finding.Summary,
		Evidence: finding.Evidence, Path: finding.Path, Line: finding.Line,
		RequiredChange: finding.RequiredChange, Acceptance: finding.Acceptance,
	}
	encoded, _ := json.Marshal(canonical)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func validatePersistedFinding(finding Finding, expectedStatus, expectedLineage string) error {
	if err := validateFinding(finding); err != nil {
		return err
	}
	if finding.ID != findingIdentity(finding) {
		return errors.New("finding identity does not match its content")
	}
	switch finding.Status {
	case findingStatusOpen, findingStatusUnresolved, findingStatusRecorded, findingStatusRegression:
	default:
		return fmt.Errorf("invalid finding status %q", finding.Status)
	}
	if expectedStatus != "" && finding.Status != expectedStatus {
		return fmt.Errorf("finding status %q does not match expected %q", finding.Status, expectedStatus)
	}
	if finding.Lineage == "" || finding.Lineage != expectedLineage {
		return errors.New("finding lineage does not match the review")
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

func reviewLineageDigest(receipt *Receipt) string {
	canonical := struct {
		Repository            string `json:"repository"`
		ReviewBaseSHA         string `json:"review_base_sha,omitempty"`
		ReviewBaseFingerprint string `json:"review_base_fingerprint,omitempty"`
		ReviewHeadSHA         string `json:"review_head_sha,omitempty"`
		ReviewHeadFingerprint string `json:"review_head_fingerprint"`
		ReviewPatchSHA256     string `json:"review_patch_sha256,omitempty"`
	}{
		Repository: receipt.Repository, ReviewBaseSHA: receipt.ReviewBaseSHA,
		ReviewBaseFingerprint: receipt.ReviewBaseFingerprint, ReviewHeadSHA: receipt.ReviewHeadSHA,
		ReviewHeadFingerprint: receipt.ReviewHeadFingerprint, ReviewPatchSHA256: receipt.ReviewPatchSHA256,
	}
	encoded, _ := json.Marshal(canonical)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}
