package planner

import (
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

func splitWorkflowAnswers(missing []string, phase string) (blocking, deferred []string) {
	blocking = []string{}
	deferred = []string{}
	rank := model.AdoptionPhaseRank(phase)
	for _, item := range missing {
		if deferrableWorkflowAnswer(item, rank) {
			deferred = append(deferred, item)
			continue
		}
		blocking = append(blocking, item)
	}
	return blocking, deferred
}

func deferrableWorkflowAnswer(item string, rank int) bool {
	switch {
	case strings.HasPrefix(item, "workflow.artifact."), item == "workflow.artifact.build", item == "workflow.artifact.path", item == "workflow.artifact.sbom", item == "workflow.artifact.sbom_path", item == "workflow.artifact.provenance", item == "workflow.artifact.provenance_path":
		return rank < model.AdoptionPhaseRank(model.AdoptionPhaseArtifact)
	case strings.HasPrefix(item, "workflow.deployment."), item == "workflow.migration", item == "workflow.release_schedule.cron", item == "workflow.release_schedule.timezone":
		return rank < model.AdoptionPhaseRank(model.AdoptionPhaseDelivery)
	case item == "observation_window", item == "rollback_owner", item == "production_environment":
		return rank < model.AdoptionPhaseRank(model.AdoptionPhaseDelivery)
	default:
		return false
	}
}

func adoptionPhaseFrom(answers model.Answers) string {
	if answers.Workflow != nil && strings.TrimSpace(answers.Workflow.AdoptionPhase) != "" {
		normalized, err := model.NormalizeAdoptionPhase(answers.Workflow.AdoptionPhase)
		if err == nil {
			return normalized
		}
	}
	normalized, err := model.NormalizeAdoptionPhase(answers.AdoptionPhase)
	if err != nil {
		return model.AdoptionPhaseDelivery
	}
	return normalized
}
