// Package distillagent runs Distill as a Sam Harness reviewer or correction
// command, backed by OpenRouter models and Jev.
package distillagent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/samuelfaj/sam-harness/schema"
)

const (
	keyEnv          = "SAM_HARNESS_OPENROUTER_KEY"
	modelEnv        = "SAM_HARNESS_OPENROUTER_MODEL"
	jevModelEnv     = "SAM_HARNESS_OPENROUTER_JEV_MODEL"
	defaultJevModel = "~typesafe/jev-latest"
	modelAlias      = "sam-harness-main"
)

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9._:/~@-]+$`)

// Run executes Distill headlessly. mode is "review" or "repair"; the prompt is read from stdin.
func Run(mode string, stdin io.Reader, stdout, stderr io.Writer) error {
	if mode != "review" && mode != "repair" {
		return fmt.Errorf("distill mode must be review or repair, got %q", mode)
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		return fmt.Errorf("%s is required", keyEnv)
	}
	mainModel := os.Getenv(modelEnv)
	if mainModel == "" {
		return fmt.Errorf("%s is required", modelEnv)
	}
	jevModel := os.Getenv(jevModelEnv)
	if jevModel == "" {
		jevModel = defaultJevModel
	}
	for name, value := range map[string]string{modelEnv: mainModel, jevModelEnv: jevModel} {
		if !modelPattern.MatchString(value) {
			return fmt.Errorf("%s must match %s", name, modelPattern)
		}
	}
	binary, err := exec.LookPath("distill")
	if err != nil {
		return fmt.Errorf("distill executable not found: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	home, err := os.MkdirTemp("", "sam-harness-distill-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(home)
	configDir := filepath.Join(home, ".distill")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.toml"), []byte(configTOML(mainModel, jevModel)), 0o600); err != nil {
		return err
	}

	args := []string{"-p", "-", "-m", modelAlias, "--cwd", cwd, "--output-format", "json", "--no-subagents"}
	if mode == "review" {
		var schemaJSON bytes.Buffer
		if err := json.Compact(&schemaJSON, schema.ReviewerOutputJSON); err != nil {
			return fmt.Errorf("compact reviewer schema: %w", err)
		}
		args = append(args, "--json-schema", schemaJSON.String(), "--tools", "read_file,grep,list_dir")
	} else {
		args = append(args, "--tools", "read_file,grep,list_dir,search_replace,write", "--permission-mode", "bypassPermissions")
	}

	var out, errOut bytes.Buffer
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = childEnv(home, key)
	cmd.Stdin = stdin
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	runErr := cmd.Run()

	redact := func(text string) string { return strings.ReplaceAll(text, key, "[redacted]") }
	var result struct {
		Type             string          `json:"type"`
		Message          string          `json:"message"`
		Text             string          `json:"text"`
		StructuredOutput json.RawMessage `json:"structuredOutput"`
	}
	parseErr := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &result)
	if parseErr == nil && result.Type == "error" {
		return fmt.Errorf("distill error: %s", redact(result.Message))
	}
	if runErr != nil {
		detail := strings.TrimSpace(errOut.String())
		if detail == "" && parseErr == nil {
			detail = result.Message
		}
		return fmt.Errorf("distill failed: %v: %s", runErr, redact(detail))
	}
	if parseErr != nil {
		return errors.New("distill output is not a JSON object")
	}

	if mode == "repair" {
		if result.Text != "" {
			fmt.Fprintln(stderr, redact(result.Text))
		}
		return nil
	}
	structured := bytes.TrimSpace(result.StructuredOutput)
	if len(structured) == 0 || structured[0] != '{' {
		return errors.New("distill output has no structuredOutput object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, structured); err != nil {
		return fmt.Errorf("distill structuredOutput is invalid: %w", err)
	}
	compact.WriteByte('\n')
	_, err = io.WriteString(stdout, redact(compact.String()))
	return err
}

func configTOML(mainModel, jevModel string) string {
	return fmt.Sprintf(`[model.%s]
model = %q
base_url = "https://openrouter.ai/api/v1"
env_key = "OPENROUTER_API_KEY"
name = "sam-harness main"

[jev]
provider = "openrouter_decisions"
base_url = "https://openrouter.ai/api"
api_key_env = "OPENROUTER_API_KEY"
model = %q
timeout_ms = 20000
effort_auto = true

[jev.ladder]
e_crushers = true
e_retention = true
e_importance = true
e_read_reuse = true
e_breaker = true
e_cheap_compress = true
e_cheap_task = true
e_lane_choice = true
`, modelAlias, mainModel, jevModel)
}

func childEnv(home, key string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		if name == "HOME" || name == "OPENROUTER_API_KEY" || name == keyEnv {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "HOME="+home, "OPENROUTER_API_KEY="+key)
}
