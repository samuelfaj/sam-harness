package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
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

const reviewerPatchFilename = "review.patch"

type reviewSandboxRoot struct {
	path     string
	root     *os.Root
	identity os.FileInfo
}

func openReviewSandboxRoot(path string) (*reviewSandboxRoot, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("review sandbox path must be absolute: %q", path)
	}
	identity, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect review sandbox: %w", err)
	}
	if identity.Mode()&os.ModeSymlink != 0 || !identity.IsDir() {
		return nil, errors.New("review sandbox must be a non-symlink directory")
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open review sandbox root: %w", err)
	}
	sandbox := &reviewSandboxRoot{path: path, root: root, identity: identity}
	if err := sandbox.confirmIdentity(); err != nil {
		_ = root.Close()
		return nil, err
	}
	return sandbox, nil
}

func (sandbox *reviewSandboxRoot) confirmIdentity() error {
	if sandbox == nil || sandbox.root == nil {
		return errors.New("review sandbox root is unavailable")
	}
	rootInfo, err := sandbox.root.Stat(".")
	if err != nil {
		return fmt.Errorf("inspect held review sandbox root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() || !os.SameFile(sandbox.identity, rootInfo) {
		return errors.New("held review sandbox root identity changed")
	}
	textInfo, err := os.Lstat(sandbox.path)
	if err != nil {
		return fmt.Errorf("inspect review sandbox path: %w", err)
	}
	if textInfo.Mode()&os.ModeSymlink != 0 || !textInfo.IsDir() || !os.SameFile(sandbox.identity, textInfo) {
		return errors.New("review sandbox path identity changed")
	}
	return nil
}

func (sandbox *reviewSandboxRoot) Close() error {
	if sandbox == nil || sandbox.root == nil {
		return nil
	}
	root := sandbox.root
	sandbox.root = nil
	return root.Close()
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

func materializeReviewPatch(sandbox *reviewSandboxRoot, patch []byte, expectedDigest string) (destination string, err error) {
	if sandbox == nil || sandbox.root == nil {
		return "", errors.New("review sandbox root is unavailable")
	}
	if err := sandbox.confirmIdentity(); err != nil {
		return "", err
	}
	patchDigest := sha256.Sum256(patch)
	actualDigest := hex.EncodeToString(patchDigest[:])
	if expectedDigest == "" || actualDigest != expectedDigest {
		return "", fmt.Errorf("review patch bytes digest mismatch: expected %s, current %s", expectedDigest, actualDigest)
	}

	const name = ".sam-harness-" + reviewerPatchFilename
	destination = reviewerPatchPath(sandbox.path)
	file, err := sandbox.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o400)
	if err != nil {
		return "", fmt.Errorf("create exclusive review patch: %w", err)
	}
	removeDestination := true
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close review patch: %w", closeErr)
		}
		if removeDestination || err != nil {
			if removeErr := sandbox.root.RemoveAll(name); err == nil && !os.IsNotExist(removeErr) && removeErr != nil {
				err = fmt.Errorf("remove review patch after failure: %w", removeErr)
			}
		}
	}()

	if _, err := file.Write(patch); err != nil {
		return "", fmt.Errorf("write review patch: %w", err)
	}
	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync review patch: %w", err)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("stat open review patch: %w", err)
	}
	if !openedInfo.Mode().IsRegular() {
		return "", errors.New("open review patch must be a regular file")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind review patch: %w", err)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash open review patch: %w", err)
	}
	if current := hex.EncodeToString(hash.Sum(nil)); current != expectedDigest {
		return "", fmt.Errorf("review patch digest changed while it was written: expected %s, current %s", expectedDigest, current)
	}
	pathInfo, err := sandbox.root.Lstat(name)
	if err != nil {
		return "", fmt.Errorf("inspect created review patch: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() || !os.SameFile(openedInfo, pathInfo) {
		return "", errors.New("review patch changed while it was written")
	}
	if err := sandbox.confirmIdentity(); err != nil {
		return "", err
	}
	removeDestination = false
	return destination, nil
}

func reviewerPatchPath(sandbox string) string {
	return filepath.Join(sandbox, ".sam-harness-"+reviewerPatchFilename)
}

func verifyReviewSandboxPatch(sandbox *reviewSandboxRoot, expectedDigest string) error {
	if sandbox == nil || sandbox.root == nil {
		return errors.New("review sandbox root is unavailable")
	}
	if err := sandbox.confirmIdentity(); err != nil {
		return err
	}
	const name = ".sam-harness-" + reviewerPatchFilename
	info, err := sandbox.root.Lstat(name)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("review sandbox patch must be a regular file")
	}
	file, err := sandbox.root.Open(name)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return errors.New("review sandbox patch changed while it was opened")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	current := hex.EncodeToString(hash.Sum(nil))
	if expectedDigest == "" || current != expectedDigest {
		return fmt.Errorf("review sandbox patch digest mismatch: expected %s, current %s", expectedDigest, current)
	}
	finalInfo, err := sandbox.root.Lstat(name)
	if err != nil {
		return err
	}
	if finalInfo.Mode()&os.ModeSymlink != 0 || !finalInfo.Mode().IsRegular() || !os.SameFile(opened, finalInfo) {
		return errors.New("review sandbox patch changed while it was verified")
	}
	return sandbox.confirmIdentity()
}

func removeReviewSandboxPatch(sandbox *reviewSandboxRoot) error {
	if sandbox == nil || sandbox.root == nil {
		return errors.New("review sandbox root is unavailable")
	}
	const name = ".sam-harness-" + reviewerPatchFilename
	if err := sandbox.root.RemoveAll(name); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
