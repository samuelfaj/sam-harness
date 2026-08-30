package planner

import (
	"fmt"
	"sort"
	"strings"

	"github.com/samuelfaj/sam-harness/internal/model"
)

var scanCommandToGuard = map[string]string{
	"format":       model.GuardFormat,
	"format-check": model.GuardFormat,
	"lint":         model.GuardLint,
	"typecheck":    model.GuardTypecheck,
	"security":     model.GuardSecurity,
	"test":         model.GuardUnit,
}

func knownGuardCategory(category string) bool {
	for _, existing := range model.StaticGuardCategories {
		if existing == category {
			return true
		}
	}
	for _, existing := range model.TestGuardCategories {
		if existing == category {
			return true
		}
	}
	return false
}

// ProposedGuardDefaults maps scan-detected stack commands onto matching guard
// categories. It never invents argv for a category the scan did not declare.
func ProposedGuardDefaults(scan model.ScanResult) map[string]model.CommandSpec {
	proposed := map[string]model.CommandSpec{}
	for _, stack := range scan.Stacks {
		workdir := stack.Path
		if strings.TrimSpace(workdir) == "" {
			workdir = "."
		}
		gates := make([]string, 0, len(stack.Commands))
		for gate := range stack.Commands {
			gates = append(gates, gate)
		}
		sort.Strings(gates)
		for _, gate := range gates {
			argv := stack.Commands[gate]
			if len(argv) == 0 {
				continue
			}
			category, ok := scanCommandToGuard[gate]
			if !ok {
				continue
			}
			if _, exists := proposed[category]; exists {
				continue
			}
			proposed[category] = model.CommandSpec{
				Name:           "detected " + category + " from " + stack.Kind,
				Workdir:        workdir,
				Command:        append([]string(nil), argv...),
				Required:       true,
				TimeoutSeconds: 900,
			}
		}
	}
	return proposed
}

func applyConfirmedGuardDefaults(workflow *model.WorkflowConfig, proposed map[string]model.CommandSpec, confirmed []string) error {
	if workflow == nil {
		if len(confirmed) > 0 {
			return nil
		}
		return nil
	}
	seen := map[string]bool{}
	for _, category := range confirmed {
		category = strings.TrimSpace(category)
		if category == "" {
			continue
		}
		if !knownGuardCategory(category) {
			if category == "browser" || category == "accessibility" {
				continue
			}
			return fmt.Errorf("confirm_guard_defaults contains unknown category %q", category)
		}
		if seen[category] {
			continue
		}
		seen[category] = true
		spec, ok := proposed[category]
		if !ok {
			continue
		}
		if staticCategory(category) {
			ensureGuardMaps(&workflow.StaticGuards)
			if _, hasCommand := workflow.StaticGuards.Commands[category]; hasCommand {
				continue
			}
			if strings.TrimSpace(workflow.StaticGuards.Waivers[category]) != "" {
				continue
			}
			workflow.StaticGuards.Commands[category] = spec
			continue
		}
		ensureGuardMaps(&workflow.TestGuards)
		if _, hasCommand := workflow.TestGuards.Commands[category]; hasCommand {
			continue
		}
		if strings.TrimSpace(workflow.TestGuards.Waivers[category]) != "" {
			continue
		}
		workflow.TestGuards.Commands[category] = spec
	}
	return nil
}

func undecidedProposedGuards(workflow *model.WorkflowConfig, proposed map[string]model.CommandSpec) map[string]model.CommandSpec {
	if len(proposed) == 0 {
		return nil
	}
	remaining := map[string]model.CommandSpec{}
	for category, spec := range proposed {
		if guardDecided(workflow, category) {
			continue
		}
		remaining[category] = spec
	}
	if len(remaining) == 0 {
		return nil
	}
	return remaining
}

func guardDecided(workflow *model.WorkflowConfig, category string) bool {
	if workflow == nil {
		return false
	}
	if staticCategory(category) {
		command, hasCommand := workflow.StaticGuards.Commands[category]
		if hasCommand && !commandSpecMissing(command) {
			return true
		}
		return strings.TrimSpace(workflow.StaticGuards.Waivers[category]) != ""
	}
	command, hasCommand := workflow.TestGuards.Commands[category]
	if hasCommand && !commandSpecMissing(command) {
		return true
	}
	return strings.TrimSpace(workflow.TestGuards.Waivers[category]) != ""
}

func staticCategory(category string) bool {
	for _, existing := range model.StaticGuardCategories {
		if existing == category {
			return true
		}
	}
	return false
}

func ensureGuardMaps(guards *model.GuardSet) {
	if guards.Commands == nil {
		guards.Commands = map[string]model.CommandSpec{}
	}
	if guards.Waivers == nil {
		guards.Waivers = map[string]string{}
	}
}

func cloneWorkflowConfig(workflow *model.WorkflowConfig) *model.WorkflowConfig {
	if workflow == nil {
		return nil
	}
	cloned := *workflow
	cloned.StaticGuards = cloneGuardSet(workflow.StaticGuards)
	cloned.TestGuards = cloneGuardSet(workflow.TestGuards)
	cloned.Reviewers = append([]model.ReviewerConfig(nil), workflow.Reviewers...)
	for index := range cloned.Reviewers {
		cloned.Reviewers[index].Command = append([]string(nil), workflow.Reviewers[index].Command...)
		cloned.Reviewers[index].TrustedConfigArguments = append([]int(nil), workflow.Reviewers[index].TrustedConfigArguments...)
	}
	cloned.Correction.Command = append([]string(nil), workflow.Correction.Command...)
	cloned.Correction.TrustedConfigArguments = append([]int(nil), workflow.Correction.TrustedConfigArguments...)
	cloned.Artifact.Build.Command = append([]string(nil), workflow.Artifact.Build.Command...)
	cloned.Artifact.SBOM.Command = append([]string(nil), workflow.Artifact.SBOM.Command...)
	cloned.Artifact.Provenance.Command = append([]string(nil), workflow.Artifact.Provenance.Command...)
	cloned.Deployment.Staging.Command = append([]string(nil), workflow.Deployment.Staging.Command...)
	cloned.Deployment.Production.Command = append([]string(nil), workflow.Deployment.Production.Command...)
	cloned.Deployment.Rollback.Command = append([]string(nil), workflow.Deployment.Rollback.Command...)
	cloned.Deployment.HealthChecks = cloneCommandSpecs(workflow.Deployment.HealthChecks)
	cloned.Deployment.ObservationChecks = cloneCommandSpecs(workflow.Deployment.ObservationChecks)
	cloned.Deployment.CanaryPercentages = append([]int(nil), workflow.Deployment.CanaryPercentages...)
	cloned.Migration = cloneCommandSpecs(workflow.Migration)
	return &cloned
}

func cloneGuardSet(guards model.GuardSet) model.GuardSet {
	cloned := model.GuardSet{
		Commands: map[string]model.CommandSpec{},
		Waivers:  map[string]string{},
	}
	for category, command := range guards.Commands {
		command.Command = append([]string(nil), command.Command...)
		cloned.Commands[category] = command
	}
	for category, reason := range guards.Waivers {
		cloned.Waivers[category] = reason
	}
	return cloned
}

func cloneCommandSpecs(commands []model.CommandSpec) []model.CommandSpec {
	cloned := make([]model.CommandSpec, len(commands))
	for index, command := range commands {
		command.Command = append([]string(nil), command.Command...)
		cloned[index] = command
	}
	return cloned
}
