package publish

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/repo"
)

type Request struct {
	Root   string
	Branch string
	Title  string
	Body   string
	Paths  []string
	Base   string
	Runner Runner
}

type Result struct {
	Branch     string `json:"branch"`
	HeadSHA    string `json:"head_sha"`
	URL        string `json:"url,omitempty"`
	Remote     string `json:"remote,omitempty"`
	DefaultRef string `json:"default_ref,omitempty"`
}

type Runner interface {
	Run(dir string, name string, args ...string) (string, error)
}

type execRunner struct{}

func (execRunner) Run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("%s %s: %w\n%s", name, strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

// Run creates an isolated branch, commits only requested paths, pushes that
// branch, and opens a change request. It never pushes the protected default branch.
func Run(req Request) (Result, error) {
	root, err := repo.ResolveRoot(req.Root)
	if err != nil {
		return Result{}, err
	}
	cfg, err := config.Load(filepath.Join(root, ".sam-harness", "config.yaml"))
	if err != nil {
		return Result{}, err
	}
	if !cfg.Authority.Commit || !cfg.Authority.Push || !cfg.Authority.Network {
		return Result{}, fmt.Errorf("publish requires commit, push, and network authority")
	}
	branch := strings.TrimSpace(req.Branch)
	title := strings.TrimSpace(req.Title)
	if branch == "" || title == "" || len(req.Paths) == 0 {
		return Result{}, fmt.Errorf("publish requires --branch, --title, and --paths")
	}
	if strings.Contains(branch, "..") || strings.HasPrefix(branch, "-") {
		return Result{}, fmt.Errorf("invalid branch %q", branch)
	}
	for _, path := range req.Paths {
		if filepath.IsAbs(path) || strings.Contains(path, "..") {
			return Result{}, fmt.Errorf("publish path must stay inside the repository: %s", path)
		}
	}
	runner := req.Runner
	if runner == nil {
		runner = execRunner{}
	}
	base := strings.TrimSpace(req.Base)
	if base == "" {
		base = "HEAD"
	}
	defaultRef, _ := runner.Run(root, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	defaultRef = strings.TrimPrefix(strings.TrimSpace(defaultRef), "origin/")
	if defaultRef == "" {
		defaultRef = "main"
	}
	if branch == defaultRef {
		return Result{}, fmt.Errorf("publish refuses to push the default branch %s", defaultRef)
	}
	if _, err := runner.Run(root, "git", "checkout", "-B", branch, base); err != nil {
		return Result{}, err
	}
	args := append([]string{"add", "--"}, req.Paths...)
	if _, err := runner.Run(root, "git", args...); err != nil {
		return Result{}, err
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = "Evidence ladder remains unproven for CI, merge, and deployment until provider receipts exist."
	}
	if _, err := runner.Run(root, "git", "commit", "-m", title, "-m", body); err != nil {
		return Result{}, err
	}
	head, err := runner.Run(root, "git", "rev-parse", "HEAD")
	if err != nil {
		return Result{}, err
	}
	if _, err := runner.Run(root, "git", "push", "-u", "origin", branch); err != nil {
		return Result{}, err
	}
	url, err := runner.Run(root, "gh", "pr", "create", "--base", defaultRef, "--head", branch, "--title", title, "--body", body)
	if err != nil {
		return Result{}, err
	}
	return Result{Branch: branch, HeadSHA: head, URL: url, Remote: "origin", DefaultRef: defaultRef}, nil
}

func DefaultRunner() Runner {
	return execRunner{}
}
