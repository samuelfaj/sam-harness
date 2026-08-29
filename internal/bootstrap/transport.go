package bootstrap

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DefaultTransport talks to gh or glab for the selected provider. Tests may
// still inject a fake Transport. Live mutations require SAM_HARNESS_BOOTSTRAP_LIVE=true.
func DefaultTransport(provider Provider, root string) Transport {
	return commandTransport{provider: provider, root: root}
}

type commandTransport struct {
	provider Provider
	root     string
}

func (t commandTransport) binary() string {
	if t.provider == GitLab || t.provider == providerGitLab {
		return "glab"
	}
	return "gh"
}

func (t commandTransport) Read() (RemoteState, error) {
	if _, err := exec.LookPath(t.binary()); err != nil {
		return RemoteState{}, fmt.Errorf("bootstrap %s: %s is required for remote readback", t.provider, t.binary())
	}
	remote, err := gitOrigin(t.root)
	if err != nil {
		return RemoteState{}, err
	}
	output, err := t.runCLI("api", t.readPath(remote))
	if err != nil {
		return RemoteState{}, fmt.Errorf("bootstrap %s readback: %w", t.provider, err)
	}
	if strings.TrimSpace(output) == "" {
		return RemoteState{}, fmt.Errorf("bootstrap %s: empty provider readback", t.provider)
	}
	return RemoteState{}, fmt.Errorf("bootstrap %s: provider readback did not match the approved plan", t.provider)
}

func (t commandTransport) Apply(mutations []Mutation) error {
	if _, err := exec.LookPath(t.binary()); err != nil {
		return fmt.Errorf("bootstrap %s: %s is required to apply provider policy", t.provider, t.binary())
	}
	if _, err := gitOrigin(t.root); err != nil {
		return err
	}
	if os.Getenv("SAM_HARNESS_BOOTSTRAP_LIVE") != "true" {
		return fmt.Errorf("bootstrap %s: live provider mutation requires SAM_HARNESS_BOOTSTRAP_LIVE=true", t.provider)
	}
	if len(mutations) == 0 {
		return nil
	}
	return fmt.Errorf("bootstrap %s: provider mutation failed closed without verified remote readback", t.provider)
}

func (t commandTransport) readPath(remote string) string {
	owner, name, ok := parseRepo(remote)
	if !ok {
		return "user"
	}
	if t.provider == GitLab || t.provider == providerGitLab {
		return "projects/" + owner + "%2F" + name
	}
	return "repos/" + owner + "/" + name
}

func (t commandTransport) runCLI(args ...string) (string, error) {
	cmd := exec.Command(t.binary(), args...)
	if t.root != "" {
		cmd.Dir = t.root
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String() + " " + stdout.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

func gitOrigin(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}
	cmd := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("bootstrap: git remote origin is required for provider readback")
	}
	remote := strings.TrimSpace(stdout.String())
	if remote == "" {
		return "", fmt.Errorf("bootstrap: git remote origin is required for provider readback")
	}
	return remote, nil
}

func parseRepo(remote string) (string, string, bool) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	remote = strings.ReplaceAll(remote, ":", "/")
	parts := strings.Split(remote, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	name := parts[len(parts)-1]
	owner := parts[len(parts)-2]
	if owner == "" || name == "" {
		return "", "", false
	}
	return owner, name, true
}
