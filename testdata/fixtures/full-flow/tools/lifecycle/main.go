package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const artifactPath = "dist/application.bin"
const fixtureStateDirectory = ".sam-harness/evidence/fixture-state"

func main() {
	if len(os.Args) != 2 {
		fail(errors.New("usage: lifecycle <check-format|preflight|review|repair|artifact|sbom|provenance|staging|production|health|observe|rollback|migration>"))
	}
	var err error
	switch os.Args[1] {
	case "check-format":
		err = checkFormat()
	case "preflight":
		err = preflight()
	case "review":
		err = review()
	case "repair":
		err = repair()
	case "artifact":
		err = writeFile(artifactPath, []byte("immutable full-flow fixture\n"))
	case "sbom":
		err = writeJSON("dist/sbom.cdx.json", map[string]any{"bomFormat": "CycloneDX", "specVersion": "1.6", "components": []any{}})
	case "provenance":
		err = provenance()
	case "staging":
		err = recordDigest("staging")
	case "production":
		err = promoteProduction()
	case "health":
		err = health()
	case "observe":
		err = observe()
	case "rollback":
		err = writeFile(filepath.Join(fixtureStateDirectory, "rollback"), []byte("requested\n"))
	case "migration":
		err = writeFile(filepath.Join(fixtureStateDirectory, "migration"), []byte("reconciled\n"))
	default:
		err = fmt.Errorf("unknown lifecycle operation %q", os.Args[1])
	}
	if err != nil {
		fail(err)
	}
}

func checkFormat() error {
	return filepath.WalkDir(".", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == ".sam-harness" || entry.Name() == "dist") {
			return filepath.SkipDir
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		formatted, err := format.Source(data)
		if err != nil {
			return fmt.Errorf("format %s: %w", path, err)
		}
		if string(formatted) != string(data) {
			return fmt.Errorf("%s is not gofmt formatted", path)
		}
		return nil
	})
}

func preflight() error {
	for _, path := range []string{"go.mod", "main.go", "answers.production.json"} {
		if _, err := os.Stat(path); err != nil {
			return err
		}
	}
	return nil
}

func review() error {
	prompt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	role := os.Getenv("SAM_HARNESS_REVIEW_ROLE")
	if role == "" || len(strings.TrimSpace(string(prompt))) == 0 {
		return errors.New("review requires a role and structured prompt")
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"findings": []any{}})
}

func repair() error {
	receipt, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(receipt))) == 0 {
		return errors.New("repair requires a failed receipt")
	}
	return writeFile("repair.marker", []byte("bounded correction executed\n"))
}

func provenance() error {
	digest, err := artifactDigest()
	if err != nil {
		return err
	}
	return writeJSON("dist/provenance.json", map[string]string{"subject": artifactPath, "sha256": digest})
}

func recordDigest(environment string) error {
	digest, err := artifactDigest()
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(fixtureStateDirectory, environment+".sha256"), []byte(digest+"\n"))
}

func promoteProduction() error {
	digest, err := artifactDigest()
	if err != nil {
		return err
	}
	staging, err := os.ReadFile(filepath.Join(fixtureStateDirectory, "staging.sha256"))
	if err != nil {
		return fmt.Errorf("staging proof: %w", err)
	}
	if strings.TrimSpace(string(staging)) != digest {
		return errors.New("staging and production artifact digests differ")
	}
	return writeFile(filepath.Join(fixtureStateDirectory, "production.sha256"), []byte(digest+"\n"))
}

func observe() error {
	if err := requireState("production"); err != nil {
		return err
	}
	return writeFile(filepath.Join(fixtureStateDirectory, "observed"), []byte("healthy\n"))
}

func health() error {
	switch os.Getenv("SAM_HARNESS_PIPELINE_PHASE") {
	case "staging":
		return requireState("staging")
	case "production":
		return requireState("production")
	case "rollback":
		_, err := os.Stat(filepath.Join(fixtureStateDirectory, "rollback"))
		return err
	default:
		if err := requireState("production"); err == nil {
			return nil
		}
		return requireState("staging")
	}
}

func requireState(environment string) error {
	_, err := os.Stat(filepath.Join(fixtureStateDirectory, environment+".sha256"))
	if err != nil {
		return fmt.Errorf("%s state: %w", environment, err)
	}
	return nil
}

func artifactDigest() (string, error) {
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(path, append(data, '\n'))
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
