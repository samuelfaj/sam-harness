package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
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
		ReviewHeadSHA:         receipt.ReviewHeadSHA,
		ReviewHeadFingerprint: receipt.ReviewHeadFingerprint,
		ReviewPatchSHA256:     receipt.ReviewPatchSHA256,
		Actions:               make([]RepairAction, len(receipt.Findings)),
	}
	for index, finding := range receipt.Findings {
		manifest.Actions[index] = RepairAction{
			ID:      fmt.Sprintf("%s-%03d", finding.Role, index+1),
			Finding: finding,
		}
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
	if manifest.SchemaVersion != repairManifestSchemaVersion || manifest.Repository != receipt.Repository || manifest.ReviewBaseSHA != receipt.ReviewBaseSHA || manifest.ReviewHeadSHA != receipt.ReviewHeadSHA || manifest.ReviewHeadFingerprint != receipt.ReviewHeadFingerprint || manifest.ReviewPatchSHA256 != receipt.ReviewPatchSHA256 {
		return errors.New("failed review receipt repair manifest lineage does not match the review")
	}
	if len(manifest.Actions) != len(receipt.Findings) {
		return errors.New("failed review receipt repair manifest actions do not match findings")
	}
	for index, action := range manifest.Actions {
		expectedID := fmt.Sprintf("%s-%03d", receipt.Findings[index].Role, index+1)
		if action.ID != expectedID || !reflect.DeepEqual(action.Finding, receipt.Findings[index]) {
			return errors.New("failed review receipt repair manifest actions do not match findings")
		}
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

func repairManifestDigest(manifest RepairManifest) (string, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("encode repair manifest: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}
