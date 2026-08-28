package cli

import (
	"testing"

	"github.com/samuelfaj/sam-harness/internal/model"
)

func TestAnswersFromConfigPreservesProductionUpgradeDecisions(t *testing.T) {
	t.Parallel()
	cfg := model.Config{
		Profile: model.ProfileProduction,
		Release: model.ReleaseConfig{
			RollbackOwner:         "release-owner",
			ObservationWindow:     "30 minutes",
			ProductionEnvironment: "production-us",
		},
		Governance: model.GovernanceConfig{
			Criticality:     "medium",
			DataSensitivity: "internal",
			Approvers:       []string{"release-owner"},
		},
	}
	answers := answersFromConfig(cfg)
	if answers.ProductionEnvironment != "production-us" || answers.RollbackOwner != "release-owner" || answers.ObservationWindow != "30 minutes" {
		t.Fatalf("answersFromConfig() lost production decisions: %#v", answers)
	}
}
