package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

func loadPriorReviewReceipt(root string, cfg model.Config, requested string) (string, string, Receipt, error) {
	if strings.TrimSpace(requested) == "" {
		return "", "", Receipt{}, nil
	}
	path, err := receiptInputPath(root, cfg, requested, "prior review receipt")
	if err != nil {
		return "", "", Receipt{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", Receipt{}, fmt.Errorf("read prior review receipt: %w", err)
	}
	digest := sha256.Sum256(raw)
	var prior Receipt
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&prior); err != nil {
		return "", "", Receipt{}, fmt.Errorf("decode prior review receipt: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return "", "", Receipt{}, errors.New("prior review receipt contains multiple JSON values")
		}
		return "", "", Receipt{}, fmt.Errorf("decode prior review receipt: %w", err)
	}
	if prior.Kind != "pipeline" || prior.Phase != model.PhaseReview {
		return "", "", Receipt{}, errors.New("prior receipt must be a pipeline review receipt")
	}
	if prior.HarnessVersion != model.HarnessVersion {
		return "", "", Receipt{}, fmt.Errorf("prior review receipt harness version %q does not match %q", prior.HarnessVersion, model.HarnessVersion)
	}
	if prior.Repository != cfg.Repository {
		return "", "", Receipt{}, errors.New("prior review receipt belongs to a different repository")
	}
	if prior.Passed || prior.Status != StatusBlocked || strings.TrimSpace(prior.Error) == "" || prior.ArbiterBlocked {
		return "", "", Receipt{}, errors.New("prior review receipt must be a blocked review with failure evidence")
	}
	if prior.StartedAt.IsZero() || prior.FinishedAt.IsZero() || prior.FinishedAt.Before(prior.StartedAt) || filepath.Base(path) != receiptFilename(prior) {
		return "", "", Receipt{}, errors.New("prior review receipt has invalid timestamps or non-canonical filename")
	}
	if prior.Root == "" || !filepath.IsAbs(prior.Root) || filepath.Clean(prior.Root) != prior.Root {
		return "", "", Receipt{}, errors.New("prior review receipt has invalid repository root provenance")
	}
	if prior.Fingerprint == "" || prior.FinalFingerprint == "" || prior.Fingerprint != prior.FinalFingerprint {
		return "", "", Receipt{}, errors.New("prior review receipt has invalid source fingerprints")
	}
	if prior.ReviewConvergence != "" && prior.ReviewConvergence != reviewConvergenceInitial {
		return "", "", Receipt{}, errors.New("prior review receipt is not an initial review receipt")
	}
	if prior.PriorReviewReceipt != "" || prior.PriorReviewManifest != nil || prior.PriorReviewManifestSHA256 != "" {
		return "", "", Receipt{}, errors.New("prior review receipt must not contain a prior convergence input")
	}
	if prior.ReviewLineageSHA256 == "" || prior.ReviewLineageSHA256 != reviewLineageDigest(&prior) {
		return "", "", Receipt{}, errors.New("prior review receipt has invalid review lineage")
	}
	if len(prior.Commands) != len(model.ReviewerRoles) {
		return "", "", Receipt{}, errors.New("prior review receipt does not prove all reviewer roles")
	}
	seenRoles := make(map[model.ReviewerRole]bool, len(prior.Commands))
	for _, command := range prior.Commands {
		if command.Phase != model.PhaseReview || !command.Required || !command.Passed || !strings.HasPrefix(command.Name, "review:") {
			return "", "", Receipt{}, errors.New("prior review receipt does not prove a complete read-only review")
		}
		role := model.ReviewerRole(strings.TrimPrefix(command.Name, "review:"))
		if !role.Valid() || seenRoles[role] {
			return "", "", Receipt{}, errors.New("prior review receipt has invalid or duplicate reviewer command evidence")
		}
		seenRoles[role] = true
	}
	for _, role := range model.ReviewerRoles {
		if !seenRoles[role] {
			return "", "", Receipt{}, errors.New("prior review receipt is missing reviewer command evidence")
		}
	}
	if err := validateRepairManifest(prior); err != nil {
		return "", "", Receipt{}, fmt.Errorf("validate prior review manifest: %w", err)
	}
	for _, action := range prior.RepairManifest.Actions {
		if action.Status != findingStatusOpen {
			return "", "", Receipt{}, errors.New("prior review manifest is not frozen")
		}
	}
	return path, hex.EncodeToString(digest[:]), prior, nil
}

func receiptInputPath(root string, cfg model.Config, requested, label string) (string, error) {
	path := requested
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s must be inside the configured evidence directory", label)
	}
	evidence := filepath.ToSlash(filepath.Clean(filepath.FromSlash(cfg.Evidence.ReceiptDirectory)))
	relativeSlash := filepath.ToSlash(filepath.Clean(relative))
	if relativeSlash == evidence || !strings.HasPrefix(relativeSlash, evidence+"/") {
		return "", fmt.Errorf("%s must be inside the configured evidence directory", label)
	}
	contained, err := containedPath(root, relativeSlash)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(contained)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", label)
	}
	return contained, nil
}

func bindPriorReview(receipt *Receipt, path, digest string, prior Receipt) error {
	if path == "" {
		return nil
	}
	if prior.RepairManifest == nil {
		return errors.New("prior review receipt has no frozen manifest")
	}
	manifest := *prior.RepairManifest
	manifest.Actions = append([]Finding(nil), prior.RepairManifest.Actions...)
	receipt.PriorReviewReceipt = path
	receipt.PriorReviewReceiptSHA256 = digest
	receipt.PriorReviewManifest = &manifest
	receipt.PriorReviewManifestSHA256 = prior.RepairManifestSHA256
	return nil
}

func validatePriorReviewLineage(root string, prior, current Receipt) error {
	if prior.Repository != current.Repository || prior.ReviewBaseSHA != current.ReviewBaseSHA || prior.ReviewBaseFingerprint != current.ReviewBaseFingerprint {
		return errors.New("prior review receipt base lineage does not match the current review")
	}
	if strings.TrimSpace(prior.ReviewHeadSHA) == "" || strings.TrimSpace(current.ReviewHeadSHA) == "" {
		return errors.New("prior and current review head SHAs are required for convergence")
	}
	if strings.EqualFold(prior.ReviewHeadSHA, current.ReviewHeadSHA) {
		return errors.New("prior review receipt points to the current reviewed head")
	}
	ancestor, err := gitAncestor(root, prior.ReviewHeadSHA, current.ReviewHeadSHA)
	if err != nil {
		return fmt.Errorf("validate prior review head lineage: %w", err)
	}
	if !ancestor {
		return errors.New("current review head is not a descendant of the prior reviewed head")
	}
	return nil
}

func classifyReviewConvergence(root string, prior Receipt, receipt *Receipt) error {
	if prior.RepairManifest == nil {
		return errors.New("cannot classify convergence without a frozen prior manifest")
	}
	frozen := make(map[string]Finding, len(prior.RepairManifest.Actions))
	for _, action := range prior.RepairManifest.Actions {
		if _, exists := frozen[action.ID]; exists {
			return fmt.Errorf("prior review manifest contains duplicate finding identity %s", action.ID)
		}
		frozen[action.ID] = action
	}
	currentIDs := make(map[string]bool, len(receipt.Findings))
	for index := range receipt.Findings {
		finding := &receipt.Findings[index]
		explicitID := finding.ID
		if explicitID != "" {
			if explicitID != strings.TrimSpace(explicitID) {
				return fmt.Errorf("review finding id %q is not canonical; IDs must not contain surrounding whitespace", explicitID)
			}
			frozenAction, exists := frozen[explicitID]
			if !exists || frozenAction.Role != finding.Role {
				return fmt.Errorf("review finding %q is not a frozen action for reviewer role %q", explicitID, finding.Role)
			}
		} else {
			finding.ID = findingIdentity(*finding)
			if frozen[finding.ID].ID != "" {
				return fmt.Errorf("review finding %q matches a frozen action but did not return its explicit exact manifest ID", finding.ID)
			}
		}
		currentIDs[finding.ID] = true
		if frozen[finding.ID].ID != "" {
			finding.Status = findingStatusUnresolved
			finding.Lineage = prior.RepairManifest.LineageSHA256
			receipt.UnresolvedFindingIDs = append(receipt.UnresolvedFindingIDs, finding.ID)
			continue
		}
		finding.Lineage = receipt.ReviewLineageSHA256
		introduced, err := findingIntroducedAfter(root, prior.ReviewHeadSHA, receipt.ReviewHeadSHA, finding.Path, finding.Line)
		if err != nil {
			return err
		}
		if (finding.Severity == "P0" || finding.Severity == "P1") && introduced {
			finding.Status = findingStatusRegression
			receipt.RegressionFindingIDs = append(receipt.RegressionFindingIDs, finding.ID)
			continue
		}
		finding.Status = findingStatusRecorded
	}
	failedRoles := make(map[model.ReviewerRole]bool)
	for _, command := range receipt.Commands {
		if command.Phase != model.PhaseReview || command.Passed || !strings.HasPrefix(command.Name, "review:") {
			continue
		}
		role := model.ReviewerRole(strings.TrimPrefix(command.Name, "review:"))
		if role.Valid() {
			failedRoles[role] = true
		}
	}
	for _, action := range prior.RepairManifest.Actions {
		if currentIDs[action.ID] {
			continue
		}
		if failedRoles[action.Role] {
			action.Status = findingStatusUnresolved
			action.Lineage = prior.RepairManifest.LineageSHA256
			receipt.Findings = append(receipt.Findings, action)
			receipt.UnresolvedFindingIDs = append(receipt.UnresolvedFindingIDs, action.ID)
			continue
		}
		receipt.ResolvedFindingIDs = append(receipt.ResolvedFindingIDs, action.ID)
	}
	receipt.ResolvedFindingIDs = sortedUnique(receipt.ResolvedFindingIDs)
	receipt.UnresolvedFindingIDs = sortedUnique(receipt.UnresolvedFindingIDs)
	receipt.RegressionFindingIDs = sortedUnique(receipt.RegressionFindingIDs)
	if len(failedRoles) > 0 {
		receipt.ReviewConvergence = reviewConvergenceBlocked
		return errors.New("review blocked by incomplete reviewer execution")
	}
	if len(receipt.UnresolvedFindingIDs) > 0 || len(receipt.RegressionFindingIDs) > 0 {
		receipt.ReviewConvergence = reviewConvergenceBlocked
		return errors.New("review blocked by unresolved frozen action or introduced P0/P1 regression")
	}
	receipt.ReviewConvergence = reviewConvergencePassed
	return nil
}

func sortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func gitAncestor(root, ancestor, descendant string) (bool, error) {
	git, err := resolveExternalExecutable(root, "git")
	if err != nil {
		return false, fmt.Errorf("resolve git: %w", err)
	}
	command := exec.Command(git, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-pager", "-C", root, "merge-base", "--is-ancestor", ancestor, descendant)
	command.Env = []string{
		"HOME=" + os.TempDir(), "TMPDIR=" + os.TempDir(), "LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0",
	}
	err = command.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor: %w", err)
}

func findingIntroducedAfter(root, priorHead, currentHead, path string, line int) (bool, error) {
	if priorHead == "" || currentHead == "" {
		return false, errors.New("cannot prove a convergence finding without prior and current head SHAs")
	}
	git, err := resolveExternalExecutable(root, "git")
	if err != nil {
		return false, fmt.Errorf("resolve git: %w", err)
	}
	command := exec.Command(git, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "--no-pager", "-C", root, "diff", "--unified=0", "--no-ext-diff", "--find-renames", priorHead, currentHead, "--")
	command.Env = []string{
		"HOME=" + os.TempDir(), "TMPDIR=" + os.TempDir(), "LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_OPTIONAL_LOCKS=0",
	}
	output, err := command.Output()
	if err == nil {
		return diffContainsChangedLine(string(output), path, line), nil
	}
	return false, fmt.Errorf("git diff hunk proof for %s: %w", path, err)
}

func diffContainsChangedLine(diff, path string, line int) bool {
	target := filepath.ToSlash(path)
	for _, scope := range parseDiffScopes(diff) {
		matches := scope.newPath == target || ((scope.deleted || scope.renamed) && scope.oldPath == target)
		if !matches {
			continue
		}
		if line == 0 {
			return len(scope.addedLines) == 0 && (scope.removedLine || scope.deleted || scope.renamed)
		}
		if scope.addedLines[line] {
			return true
		}
	}
	return false
}

type diffFileScope struct {
	oldPath     string
	newPath     string
	addedLines  map[int]bool
	removedLine bool
	renamed     bool
	deleted     bool
}

func parseDiffScopes(diff string) []diffFileScope {
	var scopes []diffFileScope
	var current *diffFileScope
	newLine := 0
	inHunk := false
	finish := func() {
		if current != nil {
			scopes = append(scopes, *current)
		}
	}
	for _, raw := range strings.Split(diff, "\n") {
		if strings.HasPrefix(raw, "diff --git ") {
			finish()
			oldPath, newPath, ok := parseGitDiffHeader(strings.TrimPrefix(raw, "diff --git "))
			current = nil
			inHunk = false
			if ok {
				current = &diffFileScope{oldPath: oldPath, newPath: newPath, addedLines: map[int]bool{}}
			}
			continue
		}
		if current == nil {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "rename from "):
			if value, ok := parseGitPath(strings.TrimPrefix(raw, "rename from ")); ok {
				current.oldPath = value
				current.renamed = true
			}
			continue
		case strings.HasPrefix(raw, "rename to "):
			if value, ok := parseGitPath(strings.TrimPrefix(raw, "rename to ")); ok {
				current.newPath = value
				current.renamed = true
			}
			continue
		case strings.HasPrefix(raw, "deleted file mode "):
			current.deleted = true
			continue
		case strings.HasPrefix(raw, "+++ "):
			if value, ok := parseGitPath(strings.TrimPrefix(raw, "+++ ")); ok {
				if value == "/dev/null" {
					current.deleted = true
				} else {
					current.newPath = trimDiffPathPrefix(value)
				}
			}
			continue
		case strings.HasPrefix(raw, "@@ "):
			start, ok := parseNewDiffRange(raw)
			inHunk = ok
			newLine = start
			continue
		}
		if !inHunk || raw == "" {
			continue
		}
		switch raw[0] {
		case '+':
			current.addedLines[newLine] = true
			newLine++
		case '-':
			current.removedLine = true
		case ' ':
			newLine++
		}
	}
	finish()
	return scopes
}

func parseGitDiffHeader(value string) (string, string, bool) {
	oldToken, rest, ok := nextGitPathToken(value)
	if !ok {
		return "", "", false
	}
	newToken, rest, ok := nextGitPathToken(rest)
	if !ok || strings.TrimSpace(rest) != "" {
		return "", "", false
	}
	return trimDiffPathPrefix(oldToken), trimDiffPathPrefix(newToken), true
}

func nextGitPathToken(value string) (string, string, bool) {
	value = strings.TrimLeft(value, " \t")
	if value == "" {
		return "", "", false
	}
	if value[0] != '"' {
		end := strings.IndexAny(value, " \t")
		if end < 0 {
			return value, "", true
		}
		return value[:end], value[end:], true
	}
	escaped := false
	for index := 1; index < len(value); index++ {
		switch {
		case escaped:
			escaped = false
		case value[index] == '\\':
			escaped = true
		case value[index] == '"':
			decoded, err := strconv.Unquote(value[:index+1])
			return decoded, value[index+1:], err == nil
		}
	}
	return "", "", false
}

func parseGitPath(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if value[0] != '"' {
		return filepath.ToSlash(value), true
	}
	path, rest, ok := nextGitPathToken(value)
	return filepath.ToSlash(path), ok && strings.TrimSpace(rest) == ""
}

func trimDiffPathPrefix(path string) string {
	path = filepath.ToSlash(path)
	if strings.HasPrefix(path, "a/") || strings.HasPrefix(path, "b/") {
		return path[2:]
	}
	return path
}

func parseNewDiffRange(header string) (int, bool) {
	marker := strings.Index(header, " +")
	if marker < 0 {
		return 0, false
	}
	value := header[marker+2:]
	if end := strings.IndexAny(value, " @"); end >= 0 {
		value = value[:end]
	}
	start, _, ok := parseDiffRange(value)
	return start, ok
}

func scopeInitialFindings(findings []Finding, change reviewChangeEvidence, lineage string) ([]Finding, []Finding, error) {
	if len(findings) == 0 {
		return nil, nil, nil
	}
	if change.baseRoot == "" || len(change.patch) == 0 {
		return nil, nil, errors.New("initial review findings require a base-to-head patch for changed-hunk proof")
	}
	inScope := make([]Finding, 0, len(findings))
	excluded := make([]Finding, 0)
	for _, finding := range findings {
		if diffContainsChangedLine(string(change.patch), finding.Path, finding.Line) {
			inScope = append(inScope, finding)
			continue
		}
		if finding.Severity == "P0" || finding.Severity == "P1" {
			return nil, excluded, fmt.Errorf("finding %s:%d is outside an added or modified base-to-head hunk", finding.Path, finding.Line)
		}
		finding.ID = findingIdentity(finding)
		finding.Status = findingStatusExcluded
		finding.Lineage = lineage
		excluded = append(excluded, finding)
	}
	return inScope, excluded, nil
}

func parseDiffRange(value string) (int, int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, 0, false
	}
	fields := strings.SplitN(value, ",", 2)
	start, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false
	}
	count := 1
	if len(fields) == 2 {
		count, err = strconv.Atoi(fields[1])
		if err != nil {
			return 0, 0, false
		}
	}
	return start, count, true
}
