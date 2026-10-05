package distillagent

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDistill installs a distill script that records its invocation and prints stdoutJSON.
func fakeDistill(t *testing.T, stdoutJSON string, exitCode int) (record string) {
	t.Helper()
	dir := t.TempDir()
	record = filepath.Join(dir, "record.txt")
	script := `#!/bin/sh
{
  echo "ARGS: $*"
  echo "HOME: $HOME"
  echo "KEY: $OPENROUTER_API_KEY"
  echo "HARNESS_KEY: ${SAM_HARNESS_OPENROUTER_KEY-unset}"
  echo "STDIN: $(cat)"
  echo "CONFIG:"
  cat "$HOME/.distill/config.toml"
} > "` + record + `"
cat <<'EOF'
` + stdoutJSON + `
EOF
exit ` + string(rune('0'+exitCode)) + `
`
	if err := os.WriteFile(filepath.Join(dir, "distill"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

func setEnv(t *testing.T) {
	t.Helper()
	t.Setenv(keyEnv, "sk-or-secret")
	t.Setenv(modelEnv, "anthropic/claude-sonnet-4.5")
	t.Setenv(jevModelEnv, "")
}

func readRecord(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestReviewExtractsStructuredOutputAndIsolatesHome(t *testing.T) {
	setEnv(t)
	record := fakeDistill(t, `{"text":"x","structuredOutput":{"review_complete":true,"findings":[]},"total_cost_usd":0.01}`, 0)
	var stdout, stderr bytes.Buffer
	if err := Run("review", strings.NewReader(`{"role":"security"}`), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if got := stdout.String(); got != "{\"review_complete\":true,\"findings\":[]}\n" {
		t.Fatalf("stdout = %q", got)
	}
	rec := readRecord(t, record)
	for _, want := range []string{
		"-p - -m sam-harness-main", "--json-schema {", "--output-format json", "--tools read_file,grep,list_dir", "--no-subagents",
		"KEY: sk-or-secret", "HARNESS_KEY: unset", `STDIN: {"role":"security"}`,
		`model = "anthropic/claude-sonnet-4.5"`, `model = "~typesafe/jev-latest"`,
	} {
		if !strings.Contains(rec, want) {
			t.Errorf("record missing %q:\n%s", want, rec)
		}
	}
	if strings.Contains(rec, "bypassPermissions") {
		t.Error("review must not bypass permissions")
	}
	home := strings.SplitN(strings.SplitN(rec, "HOME: ", 2)[1], "\n", 2)[0]
	realHome, _ := os.UserHomeDir()
	if home == realHome || home == "" {
		t.Fatalf("child HOME = %q, must be isolated", home)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("temp HOME must be removed, stat err = %v", err)
	}
}

// A model echoing the key into a finding must not leak it to the caller.
func TestReviewOutputRedactsKey(t *testing.T) {
	setEnv(t)
	fakeDistill(t, `{"structuredOutput":{"review_complete":true,"findings":[{"summary":"leak sk-or-secret"}]}}`, 0)
	var stdout bytes.Buffer
	if err := Run("review", strings.NewReader("{}"), &stdout, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), "sk-or-secret") || !strings.Contains(stdout.String(), "[redacted]") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestJevModelOverride(t *testing.T) {
	setEnv(t)
	t.Setenv(jevModelEnv, "custom/jev-1")
	record := fakeDistill(t, `{"structuredOutput":{"review_complete":true,"findings":[]}}`, 0)
	if err := Run("review", strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if rec := readRecord(t, record); !strings.Contains(rec, `model = "custom/jev-1"`) {
		t.Fatalf("jev model not applied:\n%s", rec)
	}
}

func TestMissingRequiredEnvNamesVariableNotKey(t *testing.T) {
	setEnv(t)
	t.Setenv(keyEnv, "")
	err := Run("review", strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), keyEnv) {
		t.Fatalf("err = %v", err)
	}
	setEnv(t)
	t.Setenv(modelEnv, "")
	err = Run("review", strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), modelEnv) {
		t.Fatalf("err = %v", err)
	}
}

func TestInvalidModelRejected(t *testing.T) {
	setEnv(t)
	fakeDistill(t, `{}`, 0)
	t.Setenv(modelEnv, "a\"\nmalicious = true")
	err := Run("review", strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), modelEnv) {
		t.Fatalf("err = %v", err)
	}
	t.Setenv(modelEnv, "ok/model")
	t.Setenv(jevModelEnv, "bad model")
	err = Run("review", strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), jevModelEnv) {
		t.Fatalf("err = %v", err)
	}
}

func TestDistillErrorJSONFailsAndRedactsKey(t *testing.T) {
	setEnv(t)
	fakeDistill(t, `{"type":"error","message":"auth failed for sk-or-secret"}`, 1)
	var stdout bytes.Buffer
	err := Run("review", strings.NewReader("{}"), &stdout, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "auth failed") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "sk-or-secret") {
		t.Fatalf("key leaked: %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay empty on error, got %q", stdout.String())
	}
}

func TestReviewWithoutStructuredOutputFails(t *testing.T) {
	setEnv(t)
	fakeDistill(t, `{"text":"prose only"}`, 0)
	err := Run("review", strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "structuredOutput") {
		t.Fatalf("err = %v", err)
	}
}

func TestRepairPassesPermissionModeAndLogsText(t *testing.T) {
	setEnv(t)
	record := fakeDistill(t, `{"text":"fixed it","total_cost_usd":0.02}`, 0)
	var stdout, stderr bytes.Buffer
	if err := Run("repair", strings.NewReader("{}"), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	rec := readRecord(t, record)
	for _, want := range []string{"--permission-mode bypassPermissions", "search_replace,write", "--no-subagents"} {
		if !strings.Contains(rec, want) {
			t.Errorf("record missing %q:\n%s", want, rec)
		}
	}
	if strings.Contains(rec, "--json-schema") {
		t.Error("repair must not constrain output to the reviewer schema")
	}
	if !strings.Contains(stderr.String(), "fixed it") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRepairPropagatesDistillFailure(t *testing.T) {
	setEnv(t)
	fakeDistill(t, `{"text":"boom"}`, 3)
	if err := Run("repair", strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestUnknownModeRejected(t *testing.T) {
	if err := Run("other", strings.NewReader("{}"), &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected error")
	}
}
