package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

type reviewChangeEvidence struct {
	baseRoot        string
	baseSHA         string
	baseFingerprint string
	headSHA         string
	headFingerprint string
	patch           []byte
	patchSHA256     string
	baseSnapshot    map[string]fileState
	headSnapshot    map[string]fileState
}

func prepareReviewChange(root string, cfg model.Config, requestedBase, expectedBaseSHA, expectedHeadSHA string, receipt *Receipt) (reviewChangeEvidence, error) {
	headSnapshot, err := sourceSnapshot(root, repositoryExcludedPaths(cfg))
	if err != nil {
		return reviewChangeEvidence{}, fmt.Errorf("snapshot review head: %w", err)
	}
	if err := validateReviewSnapshotSymlinks(headSnapshot); err != nil {
		return reviewChangeEvidence{}, err
	}
	change := reviewChangeEvidence{
		baseSHA:         expectedBaseSHA,
		headSHA:         expectedHeadSHA,
		headFingerprint: fingerprintSnapshot(headSnapshot),
		headSnapshot:    headSnapshot,
	}
	receipt.ReviewBaseSHA = change.baseSHA
	receipt.ReviewHeadSHA = change.headSHA
	receipt.ReviewHeadFingerprint = change.headFingerprint
	if strings.TrimSpace(requestedBase) == "" {
		return change, nil
	}
	change.baseRoot, err = canonicalReviewBase(requestedBase)
	if err != nil {
		return reviewChangeEvidence{}, err
	}
	if change.baseSHA != "" {
		if err := verifyGitHEAD(change.baseRoot, root, change.baseSHA, "review base"); err != nil {
			return reviewChangeEvidence{}, err
		}
		if err := verifyGitHEAD(root, root, change.headSHA, "review head"); err != nil {
			return reviewChangeEvidence{}, err
		}
	}
	change.baseSnapshot, err = sourceSnapshot(change.baseRoot, repositoryExcludedPaths(cfg))
	if err != nil {
		return reviewChangeEvidence{}, fmt.Errorf("snapshot review base: %w", err)
	}
	if err := validateReviewSnapshotSymlinks(change.baseSnapshot); err != nil {
		return reviewChangeEvidence{}, err
	}
	change.baseFingerprint = fingerprintSnapshot(change.baseSnapshot)
	change.patch, err = canonicalSnapshotPatch(change.baseSnapshot, change.headSnapshot)
	if err != nil {
		return reviewChangeEvidence{}, fmt.Errorf("render review change: %w", err)
	}
	digest := sha256.Sum256(change.patch)
	change.patchSHA256 = hex.EncodeToString(digest[:])
	patchPath, patchDigest, err := writeEvidencePatch(root, cfg.Evidence.ReceiptDirectory, receipt.StartedAt, "review", change.patch)
	if err != nil {
		return reviewChangeEvidence{}, fmt.Errorf("write review patch: %w", err)
	}
	if patchDigest != change.patchSHA256 {
		_ = os.Remove(patchPath)
		return reviewChangeEvidence{}, errors.New("review patch digest changed while it was written")
	}
	receipt.ReviewBaseRoot = change.baseRoot
	receipt.ReviewBaseSHA = change.baseSHA
	receipt.ReviewBaseFingerprint = change.baseFingerprint
	receipt.ReviewHeadSHA = change.headSHA
	receipt.ReviewHeadFingerprint = change.headFingerprint
	receipt.ReviewPatch = patchPath
	receipt.ReviewPatchSHA256 = change.patchSHA256
	return change, nil
}

func normalizeReviewIdentities(reviewBase, baseSHA, headSHA string) (string, string, error) {
	baseSHA = strings.ToLower(strings.TrimSpace(baseSHA))
	headSHA = strings.ToLower(strings.TrimSpace(headSHA))
	if baseSHA == "" && headSHA == "" {
		return "", "", nil
	}
	if strings.TrimSpace(reviewBase) == "" {
		return "", "", errors.New("--review-base-sha and --review-head-sha require --review-base")
	}
	if baseSHA == "" || headSHA == "" {
		return "", "", errors.New("--review-base-sha and --review-head-sha must be provided together")
	}
	for _, identity := range []struct {
		name  string
		value string
	}{{name: "--review-base-sha", value: baseSHA}, {name: "--review-head-sha", value: headSHA}} {
		name, value := identity.name, identity.value
		if len(value) != 40 && len(value) != 64 {
			return "", "", fmt.Errorf("%s must be 40 or 64 hexadecimal characters", name)
		}
		if _, err := hex.DecodeString(value); err != nil {
			return "", "", fmt.Errorf("%s must be 40 or 64 hexadecimal characters", name)
		}
	}
	return baseSHA, headSHA, nil
}

func verifyReviewIdentities(root string, change reviewChangeEvidence) error {
	if change.baseSHA == "" {
		return nil
	}
	if err := verifyGitHEAD(change.baseRoot, root, change.baseSHA, "review base after reviewers"); err != nil {
		return err
	}
	return verifyGitHEAD(root, root, change.headSHA, "review head after reviewers")
}

func verifyGitHEAD(repository, targetRoot, expected, label string) error {
	git, err := resolveExternalExecutable(targetRoot, "git")
	if err != nil {
		return fmt.Errorf("verify %s identity: %w", label, err)
	}
	canonicalRepository, err := filepath.EvalSymlinks(repository)
	if err != nil {
		return fmt.Errorf("verify %s identity: canonicalize repository: %w", label, err)
	}
	if pathWithin(git, canonicalRepository) {
		return fmt.Errorf("verify %s identity: git executable resolves inside reviewed repository", label)
	}
	command := exec.Command(git,
		"-c", "core.fsmonitor=false",
		"-c", "core.hooksPath=/dev/null",
		"--no-pager",
		"-C", canonicalRepository,
		"rev-parse", "--verify", "HEAD",
	)
	command.Env = []string{
		"HOME=" + os.TempDir(),
		"TMPDIR=" + os.TempDir(),
		"LC_ALL=C",
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_OPTIONAL_LOCKS=0",
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("verify %s identity: git rev-parse HEAD: %w: %s", label, err, strings.TrimSpace(string(output)))
	}
	actual := strings.ToLower(strings.TrimSpace(string(output)))
	if actual != expected {
		return fmt.Errorf("%s SHA mismatch: expected %s, current %s", label, expected, actual)
	}
	return nil
}

func canonicalReviewBase(requested string) (string, error) {
	if !filepath.IsAbs(requested) {
		return "", errors.New("--review-base must be an absolute directory")
	}
	candidate := filepath.Clean(requested)
	info, err := os.Lstat(candidate)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("review base does not exist: %s", candidate)
	}
	if err != nil {
		return "", fmt.Errorf("inspect review base: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("review base must not be a symbolic link: %s", candidate)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("review base must be a directory: %s", candidate)
	}
	canonical, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", fmt.Errorf("canonicalize review base: %w", err)
	}
	canonicalInfo, err := os.Lstat(canonical)
	if err != nil || !canonicalInfo.IsDir() || !os.SameFile(info, canonicalInfo) {
		return "", fmt.Errorf("review base changed while it was canonicalized: %s", candidate)
	}
	return filepath.Clean(canonical), nil
}

func prepareReviewSandbox(root, sandbox string, change reviewChangeEvidence) error {
	if change.baseRoot == "" {
		if err := copyRepository(root, sandbox, copyForReview); err != nil {
			return err
		}
		return initializeSandboxGit(sandbox)
	}
	if err := materializeSnapshot(sandbox, change.baseSnapshot); err != nil {
		return err
	}
	if err := initializeSandboxGit(sandbox); err != nil {
		return err
	}
	return applySnapshotDelta(sandbox, change.baseSnapshot, change.headSnapshot, changedPaths(change.baseSnapshot, change.headSnapshot))
}

func verifyReviewBase(change reviewChangeEvidence, cfg model.Config) error {
	if change.baseRoot == "" {
		return nil
	}
	current, err := sourceFingerprint(change.baseRoot, repositoryExcludedPaths(cfg))
	if err != nil {
		return fmt.Errorf("fingerprint review base after reviewers: %w", err)
	}
	if current != change.baseFingerprint {
		return errors.New("review base changed during review")
	}
	return nil
}

func validateReviewSnapshotSymlinks(snapshot map[string]fileState) error {
	for relative, state := range snapshot {
		if state.mode&os.ModeSymlink == 0 {
			continue
		}
		if filepath.IsAbs(state.link) {
			return fmt.Errorf("review source symlink escapes repository: %s -> %s", relative, state.link)
		}
		resolved := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(relative)), state.link)))
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			return fmt.Errorf("review source symlink escapes repository: %s -> %s", relative, state.link)
		}
	}
	return nil
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
