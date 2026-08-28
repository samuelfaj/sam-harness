package config

import (
	"strings"
	"testing"

	"github.com/samuelfaj/sam-harness/internal/model"
	"gopkg.in/yaml.v3"
)

func TestMarshalAndParseValidateThePublicSchema(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	parsed, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Profile != model.ProfileBaseline {
		t.Fatalf("Profile = %q, want baseline", parsed.Profile)
	}
}

func TestParseRejectsUnknownProfile(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	data, err := Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "profile: baseline", "profile: imaginary", 1))
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() accepted a profile outside the JSON Schema")
	}
}

func TestParseRejectsProductionWithoutOperationalOwnership(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Profile = model.ProfileProduction
	cfg.Release = model.ReleaseConfig{ImmutableArtifact: true, SBOM: true, Provenance: true}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() accepted production without rollback, observation, and environment ownership")
	}
}

func TestParseRejectsPathsOutsideTheRepository(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Evidence.ReceiptDirectory = "../../outside"
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err == nil {
		t.Fatal("Parse() accepted a receipt directory outside the repository")
	}
}

func validConfig() model.Config {
	return model.Config{
		SchemaVersion:  model.SchemaVersion,
		HarnessVersion: model.HarnessVersion,
		Profile:        model.ProfileBaseline,
		Repository:     "fixture",
		Stacks:         []model.Stack{},
		Gates:          []model.Gate{},
		Authority:      model.Authority{},
		Evidence:       model.Evidence{ReceiptDirectory: ".sam-harness/evidence", RequiredStates: []string{"source"}},
		CI:             model.CIConfig{},
		Release:        model.ReleaseConfig{},
		Migration:      model.MigrationConfig{},
		Design:         model.DesignConfig{},
		Governance: model.GovernanceConfig{
			Approvers:       []string{"owner"},
			Criticality:     "low",
			DataSensitivity: "public",
		},
	}
}
