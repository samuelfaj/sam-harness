package model

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	AgentHostClaudeCode = "claude-code"
	AgentHostCodex      = "codex"
	AgentHostGrok       = "grok"
	AgentHostOther      = "other"
)

const (
	AgentLoginAPIKey    = "api_key"
	AgentLoginGitHubApp = "github_app"
	AgentLoginOIDC      = "oidc"
	AgentLoginCLIToken  = "cli_token"
	AgentLoginManual    = "manual"
)

var (
	agentHostOtherPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	agentSecretNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// CIAgentRuntime records which agent host runs CI review/repair and how it
// authenticates. Fields are identifiers only; never store credential values.
type CIAgentRuntime struct {
	Host             string `json:"host" yaml:"host"`
	HostOther        string `json:"host_other,omitempty" yaml:"host_other,omitempty"`
	LoginMethod      string `json:"login_method" yaml:"login_method"`
	LoginEnvironment string `json:"login_environment,omitempty" yaml:"login_environment,omitempty"`
	LoginSecret      string `json:"login_secret,omitempty" yaml:"login_secret,omitempty"`
	LoginReason      string `json:"login_reason,omitempty" yaml:"login_reason,omitempty"`
}

func ParseAgentHost(value string) (host, other string, ok bool) {
	value = strings.TrimSpace(value)
	switch value {
	case AgentHostClaudeCode, AgentHostCodex, AgentHostGrok:
		return value, "", true
	}
	if rest, found := strings.CutPrefix(value, AgentHostOther+":"); found {
		rest = strings.TrimSpace(rest)
		if agentHostOtherPattern.MatchString(rest) {
			return AgentHostOther, rest, true
		}
	}
	return "", "", false
}

func ParseAgentLogin(value string) (method, env, secret, reason string, ok bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", "", "", false
	}
	method, rest, _ := strings.Cut(value, " ")
	rest = strings.TrimSpace(rest)
	switch method {
	case AgentLoginAPIKey, AgentLoginOIDC, AgentLoginCLIToken:
		fields := strings.Fields(rest)
		if len(fields) != 2 || !agentSecretNamePattern.MatchString(fields[0]) || !agentSecretNamePattern.MatchString(fields[1]) {
			return "", "", "", "", false
		}
		return method, fields[0], fields[1], "", true
	case AgentLoginGitHubApp:
		if rest != "" {
			return "", "", "", "", false
		}
		return method, "", "", "", true
	case AgentLoginManual:
		if rest == "" {
			return "", "", "", "", false
		}
		return method, "", "", rest, true
	default:
		return "", "", "", "", false
	}
}

func (r CIAgentRuntime) HostComplete() bool {
	switch r.Host {
	case AgentHostClaudeCode, AgentHostCodex, AgentHostGrok:
		return r.HostOther == ""
	case AgentHostOther:
		return agentHostOtherPattern.MatchString(r.HostOther)
	default:
		return false
	}
}

func (r CIAgentRuntime) LoginComplete() bool {
	switch r.LoginMethod {
	case AgentLoginAPIKey, AgentLoginOIDC, AgentLoginCLIToken:
		return agentSecretNamePattern.MatchString(r.LoginEnvironment) && agentSecretNamePattern.MatchString(r.LoginSecret) && strings.TrimSpace(r.LoginReason) == ""
	case AgentLoginGitHubApp:
		return r.LoginEnvironment == "" && r.LoginSecret == "" && strings.TrimSpace(r.LoginReason) == ""
	case AgentLoginManual:
		return strings.TrimSpace(r.LoginReason) != "" && r.LoginEnvironment == "" && r.LoginSecret == ""
	default:
		return false
	}
}

func (r *CIAgentRuntime) Validate() error {
	if r == nil {
		return nil
	}
	if r.Host == "" && r.LoginMethod == "" && r.HostOther == "" && r.LoginEnvironment == "" && r.LoginSecret == "" && r.LoginReason == "" {
		return nil
	}
	if r.Host != "" && !r.HostComplete() {
		return fmt.Errorf("ci_agent_host must be claude-code, codex, grok, or other:<name>")
	}
	if r.LoginMethod != "" && !r.LoginComplete() {
		return fmt.Errorf("ci_agent_login must be 'api_key ENV SECRET', 'oidc ENV SECRET', 'cli_token ENV SECRET', 'github_app', or 'manual <reason>'")
	}
	return nil
}

func (r *CIAgentRuntime) Clone() *CIAgentRuntime {
	if r == nil {
		return nil
	}
	cloned := *r
	return &cloned
}
