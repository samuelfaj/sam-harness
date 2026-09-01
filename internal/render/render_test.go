package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	harnessconfig "github.com/samuelfaj/sam-harness/internal/config"
	"github.com/samuelfaj/sam-harness/internal/model"
	"github.com/samuelfaj/sam-harness/internal/pipeline"
	harnessschema "github.com/samuelfaj/sam-harness/schema"
	"gopkg.in/yaml.v3"
)

func TestBuildGeneratesStrictReviewerOutputSchema(t *testing.T) {
	t.Parallel()
	operations, err := Build(model.ScanResult{Root: t.TempDir()}, model.ProfileBaseline, answers())
	if err != nil {
		t.Fatal(err)
	}
	content := operationContent(t, operations, ".sam-harness/reviewer-output.schema.json")
	var schema map[string]any
	if err := json.Unmarshal([]byte(content), &schema); err != nil {
		t.Fatalf("reviewer output schema is not JSON: %v", err)
	}
	if content == string(harnessschema.ReviewerOutputJSON) || !strings.Contains(content, `"id": {`) {
		t.Fatalf("generated reviewer schema does not add the nullable convergence id:\n%s", content)
	}
	var generatedFindings map[string]any
	if err := json.Unmarshal([]byte(content), &generatedFindings); err != nil {
		t.Fatal(err)
	}
	generatedProperties := generatedFindings["properties"].(map[string]any)
	findingsProperties := generatedProperties["findings"].(map[string]any)
	itemProperties := findingsProperties["items"].(map[string]any)["properties"].(map[string]any)
	idSchema, ok := itemProperties["id"].(map[string]any)
	if !ok || !reflect.DeepEqual(idSchema["type"], []any{"string", "null"}) || idSchema["minLength"] != float64(1) {
		t.Fatalf("generated reviewer schema has incorrect nullable finding id: %#v", itemProperties["id"])
	}
	itemSchema := findingsProperties["items"].(map[string]any)
	if !containsString(itemSchema["required"].([]any), "id") {
		t.Fatal("finding id must be required for initial and convergence reviews")
	}
	assertStrictObjectSchemas(t, schema, "generated")
	checkedIn, err := os.ReadFile(filepath.Join("..", "..", ".sam-harness", "reviewer-output.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var checkedInSchema map[string]any
	if err := json.Unmarshal(checkedIn, &checkedInSchema); err != nil {
		t.Fatal(err)
	}
	assertStrictObjectSchemas(t, checkedInSchema, "checked-in")
	if !reflect.DeepEqual(schema, checkedInSchema) {
		t.Fatal("checked-in reviewer schema differs structurally from canonical schema")
	}
}

func containsString(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertStrictObjectSchemas(t *testing.T, value any, path string) {
	t.Helper()
	switch node := value.(type) {
	case map[string]any:
		if properties, ok := node["properties"].(map[string]any); ok {
			required, ok := node["required"].([]any)
			if !ok {
				t.Fatalf("%s has properties but no required array", path)
			}
			for property := range properties {
				if !containsString(required, property) {
					t.Fatalf("%s does not require property %q", path, property)
				}
			}
		}
		for key, child := range node {
			assertStrictObjectSchemas(t, child, path+"."+key)
		}
	case []any:
		for index, child := range node {
			assertStrictObjectSchemas(t, child, fmt.Sprintf("%s[%d]", path, index))
		}
	}
}

func TestBuildPreservesExistingInstructions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# Existing rules\n\nKeep this line.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	operations, err := Build(model.ScanResult{Root: root}, model.ProfileBaseline, answers())
	if err != nil {
		t.Fatal(err)
	}
	agents := operationContent(t, operations, "AGENTS.md")
	if !strings.Contains(agents, "Keep this line.") || !strings.Contains(agents, markdownStart) {
		t.Fatalf("managed AGENTS.md did not preserve existing content:\n%s", agents)
	}
}

func TestManagedRootAgentsPinsCurrentHarnessVersion(t *testing.T) {
	t.Parallel()
	want := "This repository uses sam-harness " + model.HarnessVersion + " with the production profile."
	operations := buildProductionOperations(t, t.TempDir())
	generated := operationContent(t, operations, "AGENTS.md")
	if !strings.Contains(generated, want) {
		t.Fatalf("generated AGENTS.md is not pinned to the current harness version:\n%s", generated)
	}
	checkedIn, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(checkedIn), want) {
		t.Fatalf("checked-in AGENTS.md is not pinned to the current harness version:\n%s", checkedIn)
	}
}

func TestCloneWorkflowPreservesTrustedConfigArgumentOwnership(t *testing.T) {
	t.Parallel()
	workflow := &model.WorkflowConfig{
		Reviewers:  []model.ReviewerConfig{{Command: []string{"review-agent", "schema.json"}, TrustedConfigArguments: []int{1}}},
		Correction: model.CorrectionConfig{Command: []string{"repair-agent", "policy.json"}, TrustedConfigArguments: []int{1}},
	}
	cloned := cloneWorkflow(workflow)
	workflow.Reviewers[0].TrustedConfigArguments[0] = 99
	workflow.Correction.TrustedConfigArguments[0] = 99
	if cloned.Reviewers[0].TrustedConfigArguments[0] != 1 || cloned.Correction.TrustedConfigArguments[0] != 1 {
		t.Fatalf("trusted config argv positions alias caller-owned slices: %#v", cloned)
	}
}

func TestMergeGitLabIncludePreservesExistingSequence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".gitlab-ci.yml")
	existing := "include:\n  - local: '.gitlab/common.yml'\n\ntest:\n  script: echo test\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	merged, err := mergeGitLabInclude(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(merged, ".gitlab/common.yml") || !strings.Contains(merged, ".sam-harness/ci/gitlab.yml") || !strings.Contains(merged, "script: echo test") {
		t.Fatalf("merge lost existing GitLab content:\n%s", merged)
	}
	if err := os.WriteFile(path, []byte(merged), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := mergeGitLabInclude(path)
	if err != nil {
		t.Fatal(err)
	}
	if second != merged {
		t.Fatalf("second GitLab merge changed content:\n%s", second)
	}
}

func TestBuildGeneratesParseableCIWithApprovedSetup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitlab-ci.yml"), []byte("include:\n  - local: '.gitlab/common.yml'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	approved := answers()
	allowCI := true
	approved.AllowCIChanges = &allowCI
	approved.CISetupCommands = map[string][]model.SetupCommand{
		"github": {{Workdir: ".", Command: []string{"npm", "ci"}}},
		"gitlab": {{Workdir: ".", Command: []string{"npm", "ci"}}},
	}
	approved.GitLabImage = "registry.example.test/go-node:1"
	operations, err := Build(model.ScanResult{
		Root:        root,
		CIProviders: []string{"github", "gitlab"},
		Stacks:      []model.Stack{{Kind: "typescript", Path: ".", Commands: map[string][]string{"test": {"npm", "test"}}}},
	}, model.ProfileBaseline, approved)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".github/workflows/sam-harness.yml", ".sam-harness/ci/gitlab.yml", ".gitlab-ci.yml"} {
		content := operationContent(t, operations, path)
		var document any
		if err := yaml.Unmarshal([]byte(content), &document); err != nil {
			t.Fatalf("generated %s is not YAML: %v\n%s", path, err, content)
		}
		if path != ".gitlab-ci.yml" && !strings.Contains(content, "npm") {
			t.Fatalf("generated %s lost approved setup commands", path)
		}
	}
}

func TestBuildSelfRepositoryUsesLocalHarnessCommand(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "sam-harness")
	operations := buildProductionOperations(t, root)
	for _, path := range []string{".github/workflows/sam-harness.yml", ".sam-harness/ci/gitlab.yml"} {
		content := operationContent(t, operations, path)
		if !strings.Contains(content, "go run ./cmd/sam-harness pipeline .") {
			t.Fatalf("self-hosted credential-free phases in %s do not use the checked-out harness command:\n%s", path, content)
		}
	}
	dispatcher := operationContent(t, operations, ".github/workflows/sam-harness-merge-queue-dispatch.yml")
	if !strings.Contains(dispatcher, "merge_group:") || !strings.Contains(dispatcher, "sam_harness_merge_group_review") || strings.Contains(dispatcher, "pull_request_target:") {
		t.Fatalf("merge-queue dispatcher is incomplete:\n%s", dispatcher)
	}
	agents := operationContent(t, operations, ".github/workflows/sam-harness-agents.yml")
	if strings.Contains(agents, "go run ./cmd/sam-harness") || !strings.Contains(agents, "go run github.com/samuelfaj/sam-harness/cmd/sam-harness@v"+model.HarnessVersion) {
		t.Fatalf("self-hosted secret-bearing control plane does not use the pinned trusted release:\n%s", agents)
	}
}

func TestBuildCustomerRepositoryUsesPinnedHarnessCommand(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "customer-app")
	operations := buildProductionOperations(t, root)
	executable := "go run github.com/samuelfaj/sam-harness/cmd/sam-harness@v" + model.HarnessVersion
	for _, path := range []string{".github/workflows/sam-harness.yml", ".github/workflows/sam-harness-agents.yml", ".sam-harness/ci/gitlab.yml"} {
		content := operationContent(t, operations, path)
		if !strings.Contains(content, executable) {
			t.Fatalf("customer %s does not pin the published harness version:\n%s", path, content)
		}
		if strings.Contains(content, "go run ./cmd/sam-harness") {
			t.Fatalf("customer %s incorrectly uses the repository-local harness command:\n%s", path, content)
		}
	}
}

func TestBuildGitHubScopesReleaseTokenToProductionAndRollback(t *testing.T) {
	t.Parallel()
	withRelease := operationContent(t, buildProductionOperations(t, t.TempDir()), ".github/workflows/sam-harness.yml")
	production := contentSection(t, withRelease, "  production:\n", "  observe:\n")
	rollback := contentSectionToEnd(t, withRelease, "  rollback:\n")
	for name, section := range map[string]string{"production": production, "rollback": rollback} {
		if !strings.Contains(section, "contents: write") || !strings.Contains(section, "GH_TOKEN: ${{ github.token }}") {
			t.Fatalf("%s job is missing release-scoped write authority:\n%s", name, section)
		}
		if !strings.Contains(section, "persist-credentials: false") {
			t.Fatalf("%s job persists the shared checkout credential:\n%s", name, section)
		}
	}
	for _, bounds := range [][3]string{
		{"static", "  static:\n", "  test:\n"},
		{"test", "  test:\n", "  artifact:\n"},
		{"artifact", "  artifact:\n", "  staging:\n"},
		{"staging", "  staging:\n", "  migration:\n"},
		{"migration", "  migration:\n", "  production:\n"},
		{"observe", "  observe:\n", "  rollback:\n"},
	} {
		section := contentSection(t, withRelease, bounds[1], bounds[2])
		if strings.Contains(section, "contents: write") || strings.Contains(section, "GH_TOKEN:") {
			t.Fatalf("%s lifecycle job received release authority:\n%s", bounds[0], section)
		}
		if !strings.Contains(section, "persist-credentials: false") {
			t.Fatalf("%s lifecycle job persists the checkout credential:\n%s", bounds[0], section)
		}
	}

	withoutReleaseAnswers := productionAnswers()
	withoutReleaseActions := withoutAction(*withoutReleaseAnswers.AllowedActions, "release")
	withoutReleaseAnswers.AllowedActions = &withoutReleaseActions
	withoutRelease := operationContent(t, buildProductionOperationsWithAnswers(t, t.TempDir(), withoutReleaseAnswers), ".github/workflows/sam-harness.yml")
	for name, section := range map[string]string{
		"production": contentSection(t, withoutRelease, "  production:\n", "  observe:\n"),
		"rollback":   contentSectionToEnd(t, withoutRelease, "  rollback:\n"),
	} {
		if strings.Contains(section, "contents: write") || strings.Contains(section, "GH_TOKEN:") {
			t.Fatalf("%s job received release authority while release=false:\n%s", name, section)
		}
		if !strings.Contains(section, "if: ${{ false }}") {
			t.Fatalf("%s job remains executable while release=false:\n%s", name, section)
		}
	}
	config := operationContent(t, buildProductionOperationsWithAnswers(t, t.TempDir(), withoutReleaseAnswers), ".sam-harness/config.yaml")
	if !strings.Contains(config, "release: false") {
		t.Fatalf("rendered runtime configuration did not retain release=false:\n%s", config)
	}
}

func TestBuildDisablesDeploymentJobsWithoutDeployAuthority(t *testing.T) {
	t.Parallel()
	approved := productionAnswers()
	withoutDeployActions := withoutAction(*approved.AllowedActions, "deploy")
	approved.AllowedActions = &withoutDeployActions
	operations := buildProductionOperationsWithAnswers(t, t.TempDir(), approved)
	github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	for name, section := range map[string]string{
		"staging":    contentSection(t, github, "  staging:\n", "  migration:\n"),
		"migration":  contentSection(t, github, "  migration:\n", "  production:\n"),
		"production": contentSection(t, github, "  production:\n", "  observe:\n"),
		"observe":    contentSection(t, github, "  observe:\n", "  rollback:\n"),
		"rollback":   contentSectionToEnd(t, github, "  rollback:\n"),
	} {
		if !strings.Contains(section, "if: ${{ false }}") {
			t.Fatalf("GitHub %s job is active without deploy authority:\n%s", name, section)
		}
	}

	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	for name, section := range map[string]string{
		"staging":    contentSection(t, gitlab, "sam-harness-staging:\n", "sam-harness-migration:\n"),
		"migration":  contentSection(t, gitlab, "sam-harness-migration:\n", "sam-harness-production:\n"),
		"production": contentSection(t, gitlab, "sam-harness-production:\n", "sam-harness-observe:\n"),
		"observe":    contentSection(t, gitlab, "sam-harness-observe:\n", "sam-harness-rollback:\n"),
		"rollback":   contentSectionToEnd(t, gitlab, "sam-harness-rollback:\n"),
	} {
		if !strings.Contains(section, "rules:\n    - when: never") {
			t.Fatalf("GitLab %s job is active without deploy authority:\n%s", name, section)
		}
	}
}

func TestBuildDisablesAuthorityBoundJobsBeforeRuntimeFailure(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name   string
		denied string
	}{
		{name: "network", denied: "network"},
		{name: "release", denied: "release"},
	} {
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			answers := productionAnswers()
			if scenario.denied == "network" {
				answers.Workflow.Correction.OpenChangeRequest = false
			}
			allowed := withoutAction(*answers.AllowedActions, scenario.denied)
			answers.AllowedActions = &allowed
			operations := buildProductionOperationsWithAnswers(t, t.TempDir(), answers)
			github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
			gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
			inactive := []string{"production", "rollback"}
			if scenario.denied == "network" {
				inactive = []string{"staging", "migration", "production", "observe", "rollback"}
				agents := operationContent(t, operations, ".github/workflows/sam-harness-agents.yml")
				review := contentSection(t, agents, "  review:\n", "  conclude_review_check:\n")
				if !strings.Contains(review, "if: ${{ false }}") {
					t.Fatalf("GitHub trusted review remains executable without network authority:\n%s", review)
				}
			}
			githubBounds := map[string][2]string{
				"staging":    {"  staging:\n", "  migration:\n"},
				"migration":  {"  migration:\n", "  production:\n"},
				"production": {"  production:\n", "  observe:\n"},
				"observe":    {"  observe:\n", "  rollback:\n"},
				"rollback":   {"  rollback:\n", ""},
			}
			gitlabBounds := map[string][2]string{
				"staging":    {"sam-harness-staging:\n", "sam-harness-migration:\n"},
				"migration":  {"sam-harness-migration:\n", "sam-harness-production:\n"},
				"production": {"sam-harness-production:\n", "sam-harness-observe:\n"},
				"observe":    {"sam-harness-observe:\n", "sam-harness-rollback:\n"},
				"rollback":   {"sam-harness-rollback:\n", ""},
			}
			for _, job := range inactive {
				githubSection := contentSectionToEnd(t, github, githubBounds[job][0])
				if githubBounds[job][1] != "" {
					githubSection = contentSection(t, github, githubBounds[job][0], githubBounds[job][1])
				}
				if !strings.Contains(githubSection, "if: ${{ false }}") {
					t.Fatalf("GitHub %s remains executable without %s authority:\n%s", job, scenario.denied, githubSection)
				}
				gitlabSection := contentSectionToEnd(t, gitlab, gitlabBounds[job][0])
				if gitlabBounds[job][1] != "" {
					gitlabSection = contentSection(t, gitlab, gitlabBounds[job][0], gitlabBounds[job][1])
				}
				if !strings.Contains(gitlabSection, "rules:\n    - when: never") {
					t.Fatalf("GitLab %s remains executable without %s authority:\n%s", job, scenario.denied, gitlabSection)
				}
			}
		})
	}
}

func TestBuildTransportsImmutableArtifactAsPathAndModePreservingArchive(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	archiveCommand := `tar -cf "${RUNNER_TEMP}/sam-harness-immutable-artifact.tar" -- 'dist/application.tar' 'dist/application.sbom.json' 'dist/application.provenance.json'`
	extractCommand := `tar -xf "${RUNNER_TEMP}/sam-harness-artifact-transfer/sam-harness-immutable-artifact.tar" -C .`
	github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	for _, expected := range []string{archiveCommand, "path: ${{ runner.temp }}/sam-harness-immutable-artifact.tar", extractCommand, "name: sam-harness-receipts-artifact"} {
		if !strings.Contains(github, expected) {
			t.Fatalf("GitHub immutable artifact transport missing %q:\n%s", expected, github)
		}
	}
	staging := contentSection(t, github, "  staging:\n", "  migration:\n")
	if strings.Index(staging, extractCommand) < 0 || strings.Index(staging, extractCommand) > strings.Index(staging, "pipeline . --phase staging") {
		t.Fatalf("GitHub staging does not restore the archive before promotion:\n%s", staging)
	}

	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	gitlabArchiveCommand := "tar -cf '.sam-harness/evidence/transport/sam-harness-immutable-artifact.tar' -- 'dist/application.tar' 'dist/application.sbom.json' 'dist/application.provenance.json'"
	for _, expected := range []string{"mkdir -p '.sam-harness/evidence/transport'", gitlabArchiveCommand, "tar -xf '.sam-harness/evidence/transport/sam-harness-immutable-artifact.tar' -C ."} {
		if !strings.Contains(gitlab, expected) {
			t.Fatalf("GitLab immutable artifact transport missing %q:\n%s", expected, gitlab)
		}
	}
	artifactJob := contentSection(t, gitlab, "sam-harness-artifact:\n", "sam-harness-staging:\n")
	if strings.Contains(artifactJob, "      - 'dist/application.tar'") || strings.Contains(artifactJob, "      - 'dist/application.sbom.json'") || strings.Contains(artifactJob, "      - 'dist/application.provenance.json'") || strings.Contains(artifactJob, "      - '.sam-harness/immutable-artifact.tar'") {
		t.Fatalf("GitLab uploads raw artifact paths instead of the preserving archive:\n%s", artifactJob)
	}
}

func TestArtifactArchiveRoundTripPreservesPathsContentsAndModes(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	destination := t.TempDir()
	archive := filepath.Join(t.TempDir(), "immutable-artifact.tar")
	workflow := productionAnswers().Workflow
	workflow.Artifact.ArtifactPath = "dist/bin/application"
	workflow.Artifact.SBOMPath = "dist/metadata/application.sbom.json"
	workflow.Artifact.ProvenancePath = "dist/metadata/application.provenance.json"
	fixtures := map[string]struct {
		content string
		mode    os.FileMode
	}{
		workflow.Artifact.ArtifactPath:   {content: "executable artifact\n", mode: 0o755},
		workflow.Artifact.SBOMPath:       {content: "sbom\n", mode: 0o640},
		workflow.Artifact.ProvenancePath: {content: "provenance\n", mode: 0o600},
	}
	for path, fixture := range fixtures {
		target := filepath.Join(source, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(fixture.content), fixture.mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(target, fixture.mode); err != nil {
			t.Fatal(err)
		}
	}
	archiveProcess := exec.Command("sh", "-c", artifactArchiveCommand(model.Config{Workflow: workflow}, archive))
	archiveProcess.Dir = source
	if output, err := archiveProcess.CombinedOutput(); err != nil {
		t.Fatalf("archive command failed: %v\n%s", err, output)
	}
	extractProcess := exec.Command("sh", "-c", artifactExtractCommand(archive))
	extractProcess.Dir = destination
	if output, err := extractProcess.CombinedOutput(); err != nil {
		t.Fatalf("extract command failed: %v\n%s", err, output)
	}
	for path, fixture := range fixtures {
		target := filepath.Join(destination, filepath.FromSlash(path))
		data, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != fixture.content || info.Mode().Perm() != fixture.mode {
			t.Fatalf("archive identity changed for %s: content=%q mode=%#o", path, data, info.Mode().Perm())
		}
	}
}

func TestArtifactArchivePromotesFromCleanCheckoutWithReceipt(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(t.TempDir(), "promotions.log")
	writeTestExecutable(t, filepath.Join(root, "build-artifact.sh"), "#!/bin/sh\nmkdir -p dist/bin\nprintf 'application-v1\\n' > dist/bin/application\nchmod 755 dist/bin/application\n")
	writeTestExecutable(t, filepath.Join(root, "build-sbom.sh"), "#!/bin/sh\nmkdir -p dist/metadata\nprintf '{\"sbom\":true}\\n' > dist/metadata/application.sbom.json\n")
	writeTestExecutable(t, filepath.Join(root, "build-provenance.sh"), "#!/bin/sh\nmkdir -p dist/metadata\nprintf '{\"provenance\":true}\\n' > dist/metadata/application.provenance.json\n")
	writeTestExecutable(t, filepath.Join(root, "promote.sh"), "#!/bin/sh\ntest -x \"$SAM_HARNESS_ARTIFACT_PATH\"\nprintf '%s\\n' \"$SAM_HARNESS_PIPELINE_PHASE\" >> \"$1\"\n")
	writeTestExecutable(t, filepath.Join(root, "health.sh"), "#!/bin/sh\ntest -x \"$SAM_HARNESS_ARTIFACT_PATH\"\n")

	approved := productionAnswers()
	approved.Workflow.Artifact.Build = commandSpec("build artifact", "./build-artifact.sh")
	approved.Workflow.Artifact.ArtifactPath = "dist/bin/application"
	approved.Workflow.Artifact.SBOM = commandSpec("build SBOM", "./build-sbom.sh")
	approved.Workflow.Artifact.SBOMPath = "dist/metadata/application.sbom.json"
	approved.Workflow.Artifact.Provenance = commandSpec("build provenance", "./build-provenance.sh")
	approved.Workflow.Artifact.ProvenancePath = "dist/metadata/application.provenance.json"
	approved.Workflow.Deployment.Staging = model.CommandSpec{Name: "staging", Workdir: ".", Command: []string{"./promote.sh", marker}, Required: true, TimeoutSeconds: 120}
	approved.Workflow.Deployment.Production = model.CommandSpec{Name: "production", Workdir: ".", Command: []string{"./promote.sh", marker}, Required: true, TimeoutSeconds: 120}
	approved.Workflow.Deployment.HealthChecks = []model.CommandSpec{commandSpec("health", "./health.sh")}
	approved.Workflow.Deployment.CanaryPercentages = []int{100}
	cfg := buildConfig(model.ScanResult{Root: root, CIProviders: []string{"github", "gitlab"}}, model.ProfileProduction, approved)
	configData, err := harnessconfig.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, ".sam-harness", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, configData, 0o644); err != nil {
		t.Fatal(err)
	}

	artifactReceipt, artifactReceiptPath, err := pipeline.Run(root, model.PhaseArtifact, true)
	if err != nil || !artifactReceipt.Passed || artifactReceiptPath == "" {
		t.Fatalf("artifact phase failed: err=%v receipt=%#v path=%q", err, artifactReceipt, artifactReceiptPath)
	}
	archive := filepath.Join(t.TempDir(), "sam-harness-immutable-artifact.tar")
	archiveProcess := exec.Command("sh", "-c", artifactArchiveCommand(cfg, archive))
	archiveProcess.Dir = root
	if output, err := archiveProcess.CombinedOutput(); err != nil {
		t.Fatalf("archive command failed: %v\n%s", err, output)
	}
	for _, path := range artifactPaths(cfg) {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
			t.Fatal(err)
		}
	}
	extractProcess := exec.Command("sh", "-c", artifactExtractCommand(archive))
	extractProcess.Dir = root
	if output, err := extractProcess.CombinedOutput(); err != nil {
		t.Fatalf("extract command failed: %v\n%s", err, output)
	}
	if info, err := os.Stat(filepath.Join(root, "dist/bin/application")); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("extracted executable mode changed: info=%v err=%v", info, err)
	}

	stagingReceipt, stagingReceiptPath, err := pipeline.Run(root, model.PhaseStaging, true)
	if err != nil || !stagingReceipt.Passed || stagingReceiptPath == "" || stagingReceipt.SourceReceipt != artifactReceiptPath {
		t.Fatalf("staging promotion failed: err=%v receipt=%#v path=%q", err, stagingReceipt, stagingReceiptPath)
	}
	productionReceipt, productionReceiptPath, err := pipeline.Run(root, model.PhaseProduction, true)
	if err != nil || !productionReceipt.Passed || productionReceiptPath == "" || productionReceipt.SourceReceipt != artifactReceiptPath {
		t.Fatalf("production promotion failed: err=%v receipt=%#v path=%q", err, productionReceipt, productionReceiptPath)
	}
	promotions, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if string(promotions) != "staging\nproduction\n" {
		t.Fatalf("promotion commands = %q, want staging then production", promotions)
	}
}

func TestBuildGitHubRollbackIsExplicitAndDoesNotRunProductionFirst(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	for _, expected := range []string{"workflow_dispatch:", "artifact_run_id:", "github.event_name == 'workflow_dispatch'", "inputs.phase == 'rollback'", "run-id: ${{ inputs.artifact_run_id }}"} {
		if !strings.Contains(github, expected) {
			t.Fatalf("GitHub rollback dispatch is missing %q:\n%s", expected, github)
		}
	}
	production := contentSection(t, github, "  production:\n", "  observe:\n")
	if !strings.Contains(production, "github.event_name != 'workflow_dispatch'") {
		t.Fatalf("production can execute during rollback dispatch:\n%s", production)
	}
	rollback := contentSectionToEnd(t, github, "  rollback:\n")
	if strings.Contains(rollback, "needs: production") || strings.Contains(rollback, "failure()") {
		t.Fatalf("rollback is coupled to an automatic production failure:\n%s", rollback)
	}

	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	gitlabRollback := contentSectionToEnd(t, gitlab, "sam-harness-rollback:\n")
	if strings.Contains(gitlabRollback, "job: sam-harness-production") || !strings.Contains(gitlabRollback, "when: manual") {
		t.Fatalf("GitLab rollback is not independent and manual:\n%s", gitlabRollback)
	}
}

func TestBuildCorrectionDocsDescribeLocalWritesAndCumulativeBudget(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	budget := operationContent(t, operations, ".sam-harness/CHANGE_BUDGET.md")
	repairSkill := operationContent(t, operations, ".agents/skills/sam-harness-repair/SKILL.md")
	for path, content := range map[string]string{"CHANGE_BUDGET.md": budget, "repair skill": repairSkill} {
		for _, expected := range []string{"local workspace", "remote authority", "cumulative", "frozen baseline", "filesystem_sandboxed: true", "trusted_external_command: true", "trusted_config_arguments", "does not OS-sandbox arbitrary argv"} {
			if !strings.Contains(content, expected) {
				t.Fatalf("%s does not explain %q:\n%s", path, expected, content)
			}
		}
		if strings.Contains(content, "per attempt") || strings.Contains(content, "read-only repository authority") {
			t.Fatalf("%s retains the incorrect repair boundary:\n%s", path, content)
		}
	}
	reviewers := operationContent(t, operations, ".sam-harness/REVIEWERS.md")
	reviewSkill := operationContent(t, operations, ".agents/skills/sam-harness-review/SKILL.md")
	for path, content := range map[string]string{"REVIEWERS.md": reviewers, "review skill": reviewSkill} {
		for _, expected := range []string{"filesystem_read_only: true", "trusted_external_command: true", "trusted_config_arguments", "does not OS-sandbox arbitrary argv"} {
			if !strings.Contains(content, expected) {
				t.Fatalf("%s does not explain %q:\n%s", path, expected, content)
			}
		}
	}
}

func TestBuildScopesProviderSecretsToExactPhaseAndRepairSteps(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	if strings.Contains(github, "OPENAI_API_KEY") || strings.Contains(github, "REPAIR_API_KEY") || strings.Contains(github, "  review:\n") || strings.Contains(github, "  repair_review:\n") {
		t.Fatalf("ordinary pull-request workflow contains agent secrets or jobs:\n%s", github)
	}
	agents := operationContent(t, operations, ".github/workflows/sam-harness-agents.yml")
	reviewStep := contentSection(t, agents, "      - name: Run six-role review from trusted base control plane\n", "      - name: Preserve trusted review receipt")
	repairStep := contentSection(t, agents, "  repair_review:\n", "  repair_failed_phase:\n")
	if !strings.Contains(reviewStep, "REVIEW_ENV: ${{ secrets.OPENAI_API_KEY }}") || strings.Contains(reviewStep, "REPAIR_ENV") {
		t.Fatalf("GitHub review secret is not scoped to the review step:\n%s", reviewStep)
	}
	if !strings.Contains(repairStep, "REPAIR_ENV: ${{ secrets.REPAIR_API_KEY }}") || strings.Contains(repairStep, "REVIEW_ENV") {
		t.Fatalf("GitHub repair secret is not scoped to the repair step:\n%s", repairStep)
	}
	githubPublisher := contentSectionToEnd(t, agents, "  publish_repair:\n")
	if strings.Contains(githubPublisher, "OPENAI_API_KEY") || strings.Contains(githubPublisher, "REPAIR_API_KEY") {
		t.Fatalf("GitHub trusted publisher received model credentials:\n%s", githubPublisher)
	}

	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	for _, forbidden := range []string{"OPENAI_API_KEY", "REPAIR_API_KEY", "REVIEW_ENV", "REPAIR_ENV", "sam-harness-review:\n", "sam-harness-repair-static:\n", "sam-harness-publish-repair:\n", "agent-review"} {
		if strings.Contains(gitlab, forbidden) {
			t.Fatalf("GitLab MR YAML contains secret-bound control %q:\n%s", forbidden, gitlab)
		}
	}

	config := operationContent(t, operations, ".sam-harness/config.yaml")
	for _, expected := range []string{"secret_bindings:", "scope: review", "scope: repair", "environment: REVIEW_ENV", "secret: OPENAI_API_KEY", "trusted_external_command: true", "agent_control_planes:", "mode: github_app", "mode: external"} {
		if !strings.Contains(config, expected) {
			t.Fatalf("rendered config lost secret binding name %q:\n%s", expected, config)
		}
	}
}

func TestBuildKeepsManualExternalGitLabAgentWorkOutOfMergeRequestYAML(t *testing.T) {
	t.Parallel()
	answers := productionAnswers()
	delete(answers.CISecretBindings, "gitlab")
	answers.CISecretWaivers = map[string]string{"gitlab": "the external shell runner uses a pre-authenticated manual session"}

	operations := buildProductionOperationsWithAnswers(t, t.TempDir(), answers)
	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	for _, forbidden := range []string{
		"sam-harness-review:\n",
		"sam-harness-repair-static:\n",
		"sam-harness-repair-test:\n",
		"sam-harness-repair-review:\n",
		"sam-harness-repair-artifact:\n",
		"sam-harness-publish-repair:\n",
	} {
		if strings.Contains(gitlab, forbidden) {
			t.Fatalf("external GitLab control leaked local agent job %q:\n%s", forbidden, gitlab)
		}
	}
	workflow := operationContent(t, operations, ".sam-harness/WORKFLOW.md")
	for _, expected := range []string{"trusted/review-control", "sam-harness/review", "owns agent review and enabled correction", "absence blocks merge"} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("external manual GitLab contract omitted %q:\n%s", expected, workflow)
		}
	}
}

func TestBuildPreMergeReviewUsesProtectedTrustedControlPlane(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	if strings.Contains(github, "secrets.OPENAI_API_KEY") || strings.Contains(github, "secrets.REPAIR_API_KEY") || strings.Contains(github, "pull_request_target:") {
		t.Fatalf("ordinary pull-request workflow crosses the agent control plane:\n%s", github)
	}
	agents := operationContent(t, operations, ".github/workflows/sam-harness-agents.yml")
	var document any
	if err := yaml.Unmarshal([]byte(agents), &document); err != nil {
		t.Fatalf("trusted GitHub agents workflow is not YAML: %v\n%s", err, agents)
	}
	review := contentSection(t, agents, "  review:\n", "  conclude_review_check:\n")
	for _, expected := range []string{
		"pull_request_target",
		"github.event_name == 'repository_dispatch'",
		"environment:\n      name: 'agent-review'",
		"trusted-control/.sam-harness/config.yaml",
		`--review-base "${GITHUB_WORKSPACE}/trusted-control"`,
		"--review-base-sha",
		"--review-head-sha",
		"fetch-depth: 0",
		"go run github.com/samuelfaj/sam-harness/cmd/sam-harness@v" + model.HarnessVersion,
		`test -n "${REVIEW_ENV:-}"`,
		"review_patch_sha256",
		"actual_review_patch_sha256",
		"steps.phase.outputs.review_patch",
	} {
		if !strings.Contains(review, expected) {
			t.Fatalf("GitHub pre-merge review is missing %q:\n%s", expected, review)
		}
	}
	for _, expected := range []string{
		"repository_dispatch:\n    types: [sam_harness_merge_group_review]",
		"github.event.client_payload.head_sha",
		"github.event.client_payload.base_sha",
		"github.event.client_payload.merge_group_ref",
		"refs/heads/gh-readonly-queue/*",
		"merge-group head drifted before trusted agent execution",
		"merge-group base drifted before trusted agent execution",
	} {
		if !strings.Contains(agents, expected) {
			t.Fatalf("GitHub base-owned merge-queue dispatch is missing %q:\n%s", expected, agents)
		}
	}
	if strings.Contains(agents, "\n  merge_group:\n") || strings.Contains(agents, "github.event.merge_group") {
		t.Fatalf("secret-bearing workflow is directly controlled by merge-group source:\n%s", agents)
	}
	if strings.Contains(review, "Prepare repository") || strings.Contains(review, "go run ./cmd/sam-harness") || strings.Contains(review, "uses: ./") || strings.Contains(review, "actions/cache") {
		t.Fatalf("GitHub secret-bearing review executes PR-controlled setup or runtime:\n%s", review)
	}
	for _, job := range []string{"  resolve:\n", "  start_review_check:\n", "  conclude_review_check:\n", "  publish_repair:\n"} {
		section := contentSectionToEnd(t, agents, job)
		if job != "  publish_repair:\n" {
			next := map[string]string{"  resolve:\n": "  start_review_check:\n", "  start_review_check:\n": "  review:\n", "  conclude_review_check:\n": "  repair_review:\n"}[job]
			section = contentSection(t, agents, job, next)
		}
		if !strings.Contains(section, "environment:\n      name: 'agent-review'") || !strings.Contains(section, "actions/create-github-app-token@fee1f7d63c2ff003460e3d139729b119787bc349") {
			t.Fatalf("App credential job %s is not protected by the agent environment:\n%s", job, section)
		}
	}
	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	if strings.Contains(gitlab, "sam-harness-review:\n") || strings.Contains(gitlab, "OPENAI_API_KEY") || strings.Contains(gitlab, "agent-review") {
		t.Fatalf("GitLab secret-bound review leaked into merge-request YAML:\n%s", gitlab)
	}
	workflow := operationContent(t, operations, ".sam-harness/WORKFLOW.md")
	for _, expected := range []string{"trusted/review-control", "sam-harness/review", "external trusted control plane", "absence blocks merge"} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("GitLab external control-plane contract missing %q:\n%s", expected, workflow)
		}
	}
}

func TestBuildSeparatesRepairSecretsFromUntrustedPhaseJobs(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	for _, bounds := range [][3]string{
		{"static", "  static:\n", "  test:\n"},
		{"test", "  test:\n", "  artifact:\n"},
		{"artifact", "  artifact:\n", "  staging:\n"},
	} {
		phase := contentSection(t, github, bounds[1], bounds[2])
		if strings.Contains(phase, "REPAIR_ENV") || strings.Contains(phase, "Run bounded repair") {
			t.Fatalf("GitHub %s phase can receive repair credentials:\n%s", bounds[0], phase)
		}
	}
	if strings.Contains(github, "REPAIR_ENV") || strings.Contains(github, "  repair_") || strings.Contains(github, "  publish_repair:") {
		t.Fatalf("ordinary pull-request workflow contains repair agent authority:\n%s", github)
	}
	agents := operationContent(t, operations, ".github/workflows/sam-harness-agents.yml")
	for _, bounds := range [][2]string{{"  repair_review:\n", "  repair_failed_phase:\n"}, {"  repair_failed_phase:\n", "  publish_repair:\n"}} {
		repair := contentSection(t, agents, bounds[0], bounds[1])
		for _, expected := range []string{"environment:\n      name: 'agent-review'", "REPAIR_ENV: ${{ secrets.REPAIR_API_KEY }}", "trusted-control/.sam-harness/config.yaml", "--config", "persist-credentials: false"} {
			if !strings.Contains(repair, expected) {
				t.Fatalf("GitHub %s is missing trusted repair boundary %q:\n%s", bounds[0], expected, repair)
			}
		}
		if strings.Contains(repair, "Prepare repository") || strings.Contains(repair, "go run ./cmd/sam-harness") {
			t.Fatalf("GitHub %s executes PR-controlled setup or runtime:\n%s", bounds[0], repair)
		}
	}
	failedPhase := contentSection(t, agents, "  repair_failed_phase:\n", "  publish_repair:\n")
	for _, expected := range []string{"workflow_run", "run-id: ${{ needs.resolve.outputs.source_run_id }}", "exactly one failed static, test, or artifact receipt"} {
		if !strings.Contains(failedPhase, expected) {
			t.Fatalf("workflow-run repair is missing deterministic failed-receipt boundary %q:\n%s", expected, failedPhase)
		}
	}
	reviewRepair := contentSection(t, agents, "  repair_review:\n", "  repair_failed_phase:\n")
	if strings.Contains(reviewRepair, "merge_group") {
		t.Fatalf("merge-group failure can trigger automatic repair:\n%s", reviewRepair)
	}
	if !strings.Contains(reviewRepair, `test "$(jq -r '.status' "$receipt")" = blocked`) || strings.Contains(reviewRepair, `test "$(jq -r '.status' "$receipt")" = failed`) {
		t.Fatalf("review repair does not accept the blocked review status:\n%s", reviewRepair)
	}
	if !strings.Contains(reviewRepair, "!startsWith(needs.resolve.outputs.head_ref, 'sam-harness/repair-')") {
		t.Fatalf("trusted review repair can recursively repair its own branch:\n%s", reviewRepair)
	}
	resolve := contentSection(t, agents, "  resolve:\n", "  start_review_check:\n")
	for _, expected := range []string{`head_ref="$(printf '%s' "$pull" | jq -r '.head.ref')"`, `head_ref=%s\nprior_review_run_id=%s\n' "$head_sha" "$base_sha" "$pr_number" "$SOURCE_RUN_ID" "$head_ref" "$prior_review_run_id"`} {
		if !strings.Contains(resolve, expected) {
			t.Fatalf("trusted resolver does not publish the provider-owned pull-request branch %q:\n%s", expected, resolve)
		}
	}

	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	for _, forbidden := range []string{"REPAIR_ENV", "REPAIR_API_KEY", "sam-harness-repair-static:", "sam-harness-publish-repair:"} {
		if strings.Contains(gitlab, forbidden) {
			t.Fatalf("GitLab MR YAML contains secret-bound repair control %q:\n%s", forbidden, gitlab)
		}
	}
}

func TestBuildGitLabRepairReviewTransportsFrozenReceiptAndHTMLSidecar(t *testing.T) {
	t.Parallel()
	approved := productionAnswers()
	approved.CISecretBindings = nil
	approved.CISecretWaivers = map[string]string{"github": "agents need no provider secret", "gitlab": "agents need no provider secret"}
	delete(approved.AgentControlPlanes, "gitlab")
	content := operationContent(t, buildProductionOperationsWithAnswers(t, t.TempDir(), approved), ".sam-harness/ci/gitlab.yml")
	var document any
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		t.Fatalf("generated credential-free GitLab workflow is not YAML: %v\n%s", err, content)
	}
	review := contentSection(t, content, "sam-harness-review:\n", "sam-harness-artifact:\n")
	for _, expected := range []string{
		`"$CI_PROJECT_DIR/.sam-harness/evidence/prior-review"`,
		"repair-branch re-review requires exactly one frozen prior review receipt",
		`--review-base-sha "$SAM_HARNESS_REVIEW_BASE_SHA"`,
		`--review-head-sha "$CI_COMMIT_SHA"`,
		`--prior-review-receipt "$SAM_HARNESS_PRIOR_REVIEW_RECEIPT"`,
	} {
		if !strings.Contains(review, expected) {
			t.Fatalf("GitLab convergence review is missing %q:\n%s", expected, review)
		}
	}
	repair := contentSection(t, content, "sam-harness-repair-review:\n", "sam-harness-repair-artifact:\n")
	for _, expected := range []string{
		`repair_html="${repair_receipt%.json}.html"`,
		`test -f "$repair_html"`,
		`cp -p "$repair_patch" "$repair_receipt" "$repair_html" "$artifact_dir/"`,
		`cp -p "$receipt" "$artifact_dir/prior-review/"`,
	} {
		if !strings.Contains(repair, expected) {
			t.Fatalf("GitLab review repair artifact is missing %q:\n%s", expected, repair)
		}
	}
	publisher := contentSection(t, content, "sam-harness-publish-repair:\n", "sam-harness-staging:\n")
	for _, expected := range []string{
		`html_count="$(find .sam-harness/evidence/repair-artifacts -type f -name '*-repair.html'`,
		`test "$html_count" -eq 1`,
		`test "$prior_review_count" -eq 1`,
		`branch='sam-harness/repair-'"${CI_PIPELINE_ID}-${CI_JOB_ID}-${repair_phase}"`,
		`git add -f .sam-harness/evidence/prior-review/"$(basename "$prior_review_receipt")"`,
	} {
		if !strings.Contains(publisher, expected) {
			t.Fatalf("GitLab repair publisher is missing %q:\n%s", expected, publisher)
		}
	}
}

func TestBuildLimitsCredentialFreeReviewRepairToOnePass(t *testing.T) {
	t.Parallel()
	approved := productionAnswers()
	approved.CISecretBindings = nil
	approved.CISecretWaivers = map[string]string{"github": "agents need no provider secret", "gitlab": "agents need no provider secret"}
	operations := buildProductionOperationsWithAnswers(t, t.TempDir(), approved)

	github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	repair := contentSection(t, github, "  repair_review:\n", "  repair_artifact:\n")
	if !strings.Contains(repair, "!startsWith(github.head_ref || github.ref_name, 'sam-harness/repair-')") {
		t.Fatalf("credential-free GitHub review repair can recurse:\n%s", repair)
	}

	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	for _, forbidden := range []string{"sam-harness-review:\n", "sam-harness-repair-review:\n", "sam-harness-publish-repair:\n"} {
		if strings.Contains(gitlab, forbidden) {
			t.Fatalf("external GitLab control leaked local agent job %q:\n%s", forbidden, gitlab)
		}
	}
	budget := operationContent(t, operations, ".sam-harness/CHANGE_BUDGET.md")
	if !strings.Contains(budget, "Automatic review repair is limited to one pass") {
		t.Fatalf("external GitLab correction contract omitted the one-pass limit:\n%s", budget)
	}
}

func TestBuildMixedAgentBindingsPreserveCorrectionWithoutDanglingJobs(t *testing.T) {
	t.Run("review secret and credential-free repair", func(t *testing.T) {
		approved := productionAnswers()
		approved.CISecretBindings["github"] = bindingsForScope(approved.CISecretBindings["github"], model.CISecretScopeReview)
		approved.CISecretBindings["gitlab"] = bindingsForScope(approved.CISecretBindings["gitlab"], model.CISecretScopeReview)
		approved.CISecretWaivers = map[string]string{"github": "repair uses no provider secret", "gitlab": "repair uses no provider secret"}
		operations := buildProductionOperationsWithAnswers(t, t.TempDir(), approved)

		github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
		for _, expected := range []string{"  repair_static:\n", "  repair_test:\n", "  repair_artifact:\n", "  publish_repair:\n"} {
			if !strings.Contains(github, expected) {
				t.Fatalf("credential-free GitHub correction lost %q:\n%s", expected, github)
			}
		}
		if strings.Contains(github, "  review:\n") || strings.Contains(github, "  repair_review:\n") || strings.Contains(github, "REPAIR_API_KEY") {
			t.Fatalf("ordinary GitHub workflow contains a bound review or repair secret:\n%s", github)
		}
		agents := operationContent(t, operations, ".github/workflows/sam-harness-agents.yml")
		for _, expected := range []string{"  pull_request_target:\n", "  repository_dispatch:\n", "  review:\n", "  repair_review:\n", "  publish_repair:\n"} {
			if !strings.Contains(agents, expected) {
				t.Fatalf("trusted GitHub review correction lost %q:\n%s", expected, agents)
			}
		}
		for _, forbidden := range []string{"  workflow_run:\n", "  repair_failed_phase:\n", "REPAIR_API_KEY"} {
			if strings.Contains(agents, forbidden) {
				t.Fatalf("trusted GitHub review-only workflow contains %q:\n%s", forbidden, agents)
			}
		}

		gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
		for _, forbidden := range []string{"sam-harness-review:\n", "sam-harness-repair-static:\n", "sam-harness-repair-test:\n", "sam-harness-repair-review:\n", "sam-harness-repair-artifact:\n", "sam-harness-publish-repair:\n", "OPENAI_API_KEY", "REPAIR_API_KEY"} {
			if strings.Contains(gitlab, forbidden) {
				t.Fatalf("GitLab external control pipeline contains local agent control %q:\n%s", forbidden, gitlab)
			}
		}
	})

	t.Run("credential-free review and repair secret", func(t *testing.T) {
		approved := productionAnswers()
		approved.CISecretBindings["github"] = bindingsForScope(approved.CISecretBindings["github"], model.CISecretScopeRepair)
		approved.CISecretBindings["gitlab"] = bindingsForScope(approved.CISecretBindings["gitlab"], model.CISecretScopeRepair)
		approved.CISecretWaivers = map[string]string{"github": "review uses no provider secret", "gitlab": "review uses no provider secret"}
		operations := buildProductionOperationsWithAnswers(t, t.TempDir(), approved)

		github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
		if !strings.Contains(github, "  review:\n") || strings.Contains(github, "  repair_") || strings.Contains(github, "REPAIR_API_KEY") {
			t.Fatalf("ordinary GitHub workflow did not keep review credential-free and repair external:\n%s", github)
		}
		for _, expected := range []string{
			"persist-credentials: false\n          ref: ${{ github.event.pull_request.head.sha || github.event.merge_group.head_sha || github.sha }}\n          path: target\n          fetch-depth: 0",
		} {
			if !strings.Contains(github, expected) {
				t.Fatalf("credential-free GitHub target checkout is missing %q:\n%s", expected, github)
			}
		}
		agents := operationContent(t, operations, ".github/workflows/sam-harness-agents.yml")
		for _, expected := range []string{"  workflow_run:\n", "  repair_failed_phase:\n", "for phase in static test review artifact", "REPAIR_ENV: ${{ secrets.REPAIR_API_KEY }}"} {
			if !strings.Contains(agents, expected) {
				t.Fatalf("trusted GitHub repair-only workflow lost %q:\n%s", expected, agents)
			}
		}
		for _, forbidden := range []string{"  pull_request_target:\n", "  repository_dispatch:\n", "  merge_group:\n", "  start_review_check:\n", "  review:\n", "  repair_review:\n"} {
			if strings.Contains(agents, forbidden) {
				t.Fatalf("trusted GitHub repair-only workflow contains review trigger/job %q:\n%s", forbidden, agents)
			}
		}

		gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
		for _, forbidden := range []string{"sam-harness-review:\n", "sam-harness-repair-static:\n", "sam-harness-repair-test:\n", "sam-harness-repair-review:\n", "sam-harness-repair-artifact:\n", "sam-harness-publish-repair:\n", "REPAIR_API_KEY"} {
			if strings.Contains(gitlab, forbidden) {
				t.Fatalf("GitLab MR pipeline contains external agent control %q:\n%s", forbidden, gitlab)
			}
		}
	})
}

func TestBuildFailsClosedInsteadOfInjectingSecretsIntoPullRequestPhaseJobs(t *testing.T) {
	t.Parallel()
	approved := productionAnswers()
	approved.CISecretBindings["github"] = append(approved.CISecretBindings["github"], model.CISecretBinding{Scope: model.CISecretScopeStatic, Environment: "STATIC_ENV", Secret: "STATIC_API_KEY"})
	approved.CISecretBindings["gitlab"] = append(approved.CISecretBindings["gitlab"], model.CISecretBinding{Scope: model.CISecretScopeStatic, Environment: "STATIC_ENV", Secret: "STATIC_API_KEY"})
	operations := buildProductionOperationsWithAnswers(t, t.TempDir(), approved)

	github := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	githubStatic := contentSection(t, github, "  static:\n", "  test:\n")
	if strings.Contains(githubStatic, "secrets.STATIC_API_KEY") || !strings.Contains(githubStatic, "agent secrets cannot be injected into pull-request-controlled phase jobs") {
		t.Fatalf("GitHub static job exposes or silently omits a configured agent secret:\n%s", githubStatic)
	}

	gitlab := operationContent(t, operations, ".sam-harness/ci/gitlab.yml")
	gitlabStatic := contentSection(t, gitlab, "sam-harness-static:\n", "sam-harness-test:\n")
	if strings.Contains(gitlabStatic, `STATIC_ENV="${STATIC_API_KEY}"`) || !strings.Contains(gitlabStatic, "agent secrets cannot be injected into merge-request-controlled phase jobs") {
		t.Fatalf("GitLab static job exposes or silently omits a configured agent secret:\n%s", gitlabStatic)
	}
}

func TestBuildDocumentsInventoryExecutableControlsAndProviderBoundaries(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	for _, path := range []string{
		".sam-harness/WORKFLOW.md",
		".sam-harness/REVIEWERS.md",
		".sam-harness/CHANGE_BUDGET.md",
		".sam-harness/runbooks/observability.md",
		".sam-harness/runbooks/retirement.md",
	} {
		if strings.TrimSpace(operationContent(t, operations, path)) == "" {
			t.Fatalf("generated %s is empty", path)
		}
	}
	workflow := operationContent(t, operations, ".sam-harness/WORKFLOW.md")
	gates := operationContent(t, operations, ".sam-harness/GATES.md")
	for _, expected := range []string{
		"format command",
		"lint waiver: accepted lint waiver",
		"unit command",
		"integration waiver: accepted integration waiver",
		"'tools/build-artifact'",
		"Release schedule: `17 4 * * 1` in IANA timezone `America/Asuncion`",
		"GitHub branch protection",
		"remote rule",
		"sam-harness-agents.yml",
		"sam_harness_merge_group_review",
		"external merge-queue dispatcher",
		"trusted/review-control",
	} {
		if !strings.Contains(workflow, expected) {
			t.Fatalf("WORKFLOW.md missing %q:\n%s", expected, workflow)
		}
	}
	if !strings.Contains(gates, "## Static guard command-or-waiver inventory") || !strings.Contains(gates, "lint waiver: accepted lint waiver") || !strings.Contains(gates, "integration waiver: accepted integration waiver") {
		t.Fatalf("GATES.md is missing command-or-waiver evidence:\n%s", gates)
	}
}

func TestBuildGitHubRendersLifecycleArtifactReceiptAndTrustedRepairPublisher(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	content := operationContent(t, operations, ".github/workflows/sam-harness.yml")
	var document any
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		t.Fatalf("generated GitHub workflow is not YAML: %v\n%s", err, content)
	}
	for _, expected := range []string{
		"merge_group:",
		"cron: '17 4 * * 1'",
		"Configured IANA timezone: America/Asuncion",
		"  static:",
		"  test:",
		"  artifact:",
		"  staging:",
		"  migration:",
		"  production:",
		"  observe:",
		"environment: 'production'",
		"name: sam-harness-receipts-artifact",
		"sed -n 's/^Receipt: //p'",
		"github.ref_name == github.event.repository.default_branch",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("GitHub workflow missing %q:\n%s", expected, content)
		}
	}
	if strings.Count(content, "contents: write") != 2 {
		t.Fatalf("ordinary workflow write authority must be limited to production and rollback:\n%s", content)
	}
	if strings.Contains(content, "find .sam-harness/evidence -type f -name '*.json'") {
		t.Fatalf("GitHub repair guesses a receipt instead of using the exact emitted path:\n%s", content)
	}
	staticJob := contentSection(t, content, "  static:\n", "  test:\n")
	if strings.Contains(staticJob, "repair ") || strings.Contains(staticJob, "REPAIR_ENV") || strings.Contains(staticJob, "contents: write") || strings.Contains(staticJob, "git push") {
		t.Fatalf("static phase received repair or publishing authority:\n%s", staticJob)
	}
	agents := operationContent(t, operations, ".github/workflows/sam-harness-agents.yml")
	if err := yaml.Unmarshal([]byte(agents), &document); err != nil {
		t.Fatalf("generated GitHub agents workflow is not YAML: %v\n%s", err, agents)
	}
	for _, expected := range []string{"pull_request_target:", "repository_dispatch:", "types: [sam_harness_merge_group_review]", "workflow_run:", "  review:", "  repair_review:", "  repair_failed_phase:", "  publish_repair:", "git config core.hooksPath /dev/null", "sam-harness-agent-repair", "permission-checks: write"} {
		if !strings.Contains(agents, expected) {
			t.Fatalf("GitHub trusted agents workflow missing %q:\n%s", expected, agents)
		}
	}
	repairJobs := contentSection(t, agents, "  repair_review:\n", "  publish_repair:\n")
	if !strings.Contains(repairJobs, "repair \"${GITHUB_WORKSPACE}/target\"") || !strings.Contains(repairJobs, ".repair_patch") || !strings.Contains(repairJobs, ".repair_patch_sha256") || !strings.Contains(repairJobs, "actual_repair_patch_sha256") || strings.Contains(repairJobs, "contents: write") || strings.Contains(repairJobs, "git diff --binary") || strings.Contains(repairJobs, "git add --intent-to-add") {
		t.Fatalf("dedicated repairs do not emit bounded correction evidence:\n%s", repairJobs)
	}
	publisher := contentSectionToEnd(t, agents, "  publish_repair:\n")
	if strings.Contains(publisher, "pipeline .") || strings.Contains(publisher, "repair .") || strings.Contains(publisher, "Prepare repository") || !strings.Contains(publisher, "persist-credentials: false") {
		t.Fatalf("trusted publisher executes project or agent commands:\n%s", publisher)
	}
	if strings.Contains(publisher, "merge-multiple: true") || !strings.Contains(publisher, "patch_count") || !strings.Contains(publisher, "receipt_count") || !strings.Contains(publisher, `test "$patch_count" -eq 1`) || !strings.Contains(publisher, `test "$receipt_count" -eq 1`) || !strings.Contains(publisher, "*-repair.patch") || !strings.Contains(publisher, "*-repair.json") || !strings.Contains(publisher, "expected_patch_sha256") || !strings.Contains(publisher, "actual_patch_sha256") || !strings.Contains(publisher, "git apply --check") || !strings.Contains(publisher, "git apply --index") || !strings.Contains(publisher, "git push --no-verify") || strings.Contains(publisher, "git add --all") {
		t.Fatalf("trusted publisher does not apply data and publish the isolated branch:\n%s", publisher)
	}
}

func TestBuildGitLabRendersLifecycleAndManualTrustedRepairPublisher(t *testing.T) {
	t.Parallel()
	content := operationContent(t, buildProductionOperations(t, t.TempDir()), ".sam-harness/ci/gitlab.yml")
	var document any
	if err := yaml.Unmarshal([]byte(content), &document); err != nil {
		t.Fatalf("generated GitLab workflow is not YAML: %v\n%s", err, content)
	}
	for _, expected := range []string{
		"  - static",
		"  - test",
		"  - review",
		"  - artifact",
		"  - staging",
		"  - production",
		"  - observe",
		"sam-harness-production:",
		"merge_request_event",
		"name: production",
		"when: manual",
		"allow_failure: false",
		"sed -n 's/^Receipt: //p'",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("GitLab workflow missing %q:\n%s", expected, content)
		}
	}
	if strings.Count(content, "job: sam-harness-artifact") < 3 {
		t.Fatalf("GitLab promotion jobs do not receive the immutable artifact and artifact receipt directly:\n%s", content)
	}
	staticJob := contentSection(t, content, "sam-harness-static:\n", "sam-harness-test:\n")
	if strings.Contains(staticJob, "repair ") || strings.Contains(staticJob, "REPAIR_ENV") || !strings.Contains(staticJob, "artifacts:\n    when: always\n    paths:\n      - .sam-harness/evidence/") || strings.Contains(staticJob, "git push") {
		t.Fatalf("GitLab static phase received repair or publishing authority:\n%s", staticJob)
	}
	for _, expected := range []string{"needs: []", `| tee "$output_file"`, `status="$(cat "$status_file")"`, `sed -n 's/^Receipt: //p' "$output_file"`} {
		if !strings.Contains(staticJob, expected) {
			t.Fatalf("GitLab static phase is missing safe live progress control %q:\n%s", expected, staticJob)
		}
	}
	for _, forbidden := range []string{"sam-harness-review:\n", "sam-harness-repair-static:\n", "sam-harness-publish-repair:\n", "OPENAI_API_KEY", "REPAIR_API_KEY", "agent-review"} {
		if strings.Contains(content, forbidden) {
			t.Fatalf("GitLab bound-agent MR pipeline contains %q:\n%s", forbidden, content)
		}
	}
	if strings.Count(content, "  - repair\n") != 1 {
		t.Fatalf("GitLab repair stage must be declared exactly once:\n%s", content)
	}
}

func TestGitLabCapturedPhaseScriptRunsUnderPOSIXShell(t *testing.T) {
	t.Parallel()
	receipt := filepath.Join(t.TempDir(), "receipt.json")
	command := "sh -c " + shellQuote("touch "+shellQuote(receipt)+"; printf 'sam-harness: command start phase=\\\"test\\\" name=\\\"fixture\\\"\\nReceipt: "+receipt+"\\n'; exit 7")
	script := gitLabCapturedPhaseScript(command)
	if strings.Contains(script, "PIPESTATUS") || strings.Contains(script, "SAM_HARNESS_PROGRESS_FD") {
		t.Fatalf("captured phase script is not POSIX portable:\n%s", script)
	}
	process := exec.Command("/bin/sh", "-c", script)
	output, err := process.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("captured phase exit = %v, want 7; output:\n%s", err, output)
	}
	for _, expected := range []string{"sam-harness: command start", "Receipt: " + receipt} {
		if !strings.Contains(string(output), expected) {
			t.Fatalf("captured phase output missing %q:\n%s", expected, output)
		}
	}
}

func TestBuildGitLabRunsIndependentGatesInParallelAndJoinsThemAtArtifact(t *testing.T) {
	t.Parallel()
	approved := productionAnswers()
	approved.CISecretBindings = nil
	approved.CISecretWaivers = map[string]string{"github": "agents need no provider secret", "gitlab": "agents need no provider secret"}
	delete(approved.AgentControlPlanes, "gitlab")
	content := operationContent(t, buildProductionOperationsWithAnswers(t, t.TempDir(), approved), ".sam-harness/ci/gitlab.yml")

	for _, bounds := range [][2]string{
		{"sam-harness-static:\n", "sam-harness-test:\n"},
		{"sam-harness-test:\n", "sam-harness-review:\n"},
		{"sam-harness-review:\n", "sam-harness-artifact:\n"},
	} {
		section := contentSection(t, content, bounds[0], bounds[1])
		if !strings.Contains(section, "  needs: []\n") {
			t.Fatalf("GitLab gate %q is not independent:\n%s", bounds[0], section)
		}
	}

	artifact := contentSection(t, content, "sam-harness-artifact:\n", "sam-harness-repair-static:\n")
	for _, gate := range []string{"sam-harness-static", "sam-harness-test", "sam-harness-review"} {
		if !strings.Contains(artifact, "    - job: "+gate+"\n      artifacts: true\n") {
			t.Fatalf("artifact does not explicitly wait for %s:\n%s", gate, artifact)
		}
	}
	if strings.Index(artifact, "job: sam-harness-static") > strings.Index(artifact, "job: sam-harness-test") || strings.Index(artifact, "job: sam-harness-test") > strings.Index(artifact, "job: sam-harness-review") {
		t.Fatalf("artifact gate order is not deterministic:\n%s", artifact)
	}

	external := operationContent(t, buildProductionOperations(t, t.TempDir()), ".sam-harness/ci/gitlab.yml")
	externalArtifact := contentSection(t, external, "sam-harness-artifact:\n", "sam-harness-staging:\n")
	for _, gate := range []string{"sam-harness-static", "sam-harness-test"} {
		if !strings.Contains(externalArtifact, "    - job: "+gate+"\n      artifacts: true\n") {
			t.Fatalf("externally reviewed artifact does not wait for %s:\n%s", gate, externalArtifact)
		}
	}
	if strings.Contains(externalArtifact, "job: sam-harness-review") {
		t.Fatalf("externally reviewed artifact waits for a local review job:\n%s", externalArtifact)
	}
}

func TestBuildGitLabDefersMergeRequestPhasesToExternalPipelinePolicy(t *testing.T) {
	t.Parallel()
	approved := productionAnswers()
	enabled := true
	approved.GitLabExternalPipelinePolicy = &enabled
	content := operationContent(t, buildProductionOperationsWithAnswers(t, t.TempDir(), approved), ".sam-harness/ci/gitlab.yml")

	for _, bounds := range [][2]string{
		{"sam-harness-static:\n", "sam-harness-test:\n"},
		{"sam-harness-test:\n", "sam-harness-artifact:\n"},
		{"sam-harness-artifact:\n", "sam-harness-staging:\n"},
	} {
		section := contentSection(t, content, bounds[0], bounds[1])
		for _, expected := range []string{
			`if: '$CI_PIPELINE_SOURCE == "merge_request_event"'`,
			"when: never # Protected external policy owns merge-request gates.",
			`if: '$CI_COMMIT_BRANCH'`,
		} {
			if !strings.Contains(section, expected) {
				t.Fatalf("GitLab phase %q does not defer merge-request execution to the external policy %q:\n%s", bounds[0], expected, section)
			}
		}
	}
}

func TestBuildLocalSkillsInstallsSevenLifecycleContracts(t *testing.T) {
	t.Parallel()
	operations := buildProductionOperations(t, t.TempDir())
	for _, lifecycle := range []string{"classify", "context", "plan", "implement", "review", "repair", "release"} {
		path := ".agents/skills/sam-harness-" + lifecycle + "/SKILL.md"
		content := operationContent(t, operations, path)
		if !strings.Contains(content, "name: sam-harness-"+lifecycle) || !strings.Contains(content, "canonical configuration") || strings.Contains(content, "+\"") {
			t.Fatalf("local lifecycle skill %s is incomplete:\n%s", lifecycle, content)
		}
	}
}

func TestBuildNestedAgentsPreservesWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, "services", "api", "AGENTS.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# API rules\n\nKeep this workspace rule.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	content := operationContent(t, buildProductionOperations(t, root), "services/api/AGENTS.md")
	if !strings.Contains(content, "Keep this workspace rule.") || !strings.Contains(content, "Sam Harness workspace contract") || !strings.Contains(content, markdownStart) {
		t.Fatalf("nested managed AGENTS.md did not preserve workspace instructions:\n%s", content)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	second := operationContent(t, buildProductionOperations(t, root), "services/api/AGENTS.md")
	if second != content {
		t.Fatalf("nested managed AGENTS.md is not idempotent:\n%s", second)
	}
}

func TestBuildReviewTemplatesPreserveExistingContentAndAddEvidence(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	path := filepath.Join(root, ".github", "pull_request_template.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Team template\n\nKeep this checklist.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitlabPath := filepath.Join(root, ".gitlab", "merge_request_templates", "sam-harness.md")
	if err := os.MkdirAll(filepath.Dir(gitlabPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitlabPath, []byte("# GitLab team template\n\nKeep this MR checklist.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	operations := buildProductionOperations(t, root)
	github := operationContent(t, operations, ".github/pull_request_template.md")
	gitlab := operationContent(t, operations, ".gitlab/merge_request_templates/sam-harness.md")
	for path, content := range map[string]string{"github": github, "gitlab": gitlab} {
		if !strings.Contains(content, "### Evidence ladder") || !strings.Contains(content, "### Human-facing and UX checks") || !strings.Contains(content, "Live proof") {
			t.Fatalf("%s review template is incomplete:\n%s", path, content)
		}
	}
	if !strings.Contains(github, "Keep this checklist.") {
		t.Fatalf("GitHub template lost existing content:\n%s", github)
	}
	if !strings.Contains(gitlab, "Keep this MR checklist.") {
		t.Fatalf("GitLab template lost existing content:\n%s", gitlab)
	}
	for path, content := range map[string]string{"github": github, "gitlab": gitlab} {
		for _, want := range []string{"## Description", "## Type of Change", "## Behavior", "## Business Rules", "## Validation", "## Tests", "## Author Checklist"} {
			if !strings.Contains(content, want) {
				t.Fatalf("%s template omitted %q:\n%s", path, want, content)
			}
		}
	}
}

func TestBuildWritesCommitConventionWhenEnabled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	approved := answers()
	enabled := true
	approved.StandardizeCommits = &enabled
	operations, err := Build(model.ScanResult{
		Root:   root,
		Stacks: []model.Stack{{Kind: "go", Path: ".", Commands: map[string][]string{"test": {"go", "test", "./..."}}}},
	}, model.ProfileBaseline, approved)
	if err != nil {
		t.Fatal(err)
	}
	content := operationContent(t, operations, ".sam-harness/COMMIT.md")
	if !strings.Contains(content, "Conventional Commits") || !strings.Contains(content, "feat") {
		t.Fatalf("COMMIT.md missing convention:\n%s", content)
	}
	agents := operationContent(t, operations, "AGENTS.md")
	if !strings.Contains(agents, "Conventional Commits") {
		t.Fatalf("AGENTS.md omitted commit convention:\n%s", agents)
	}
}

func TestBuildOmitsCommitConventionWhenDisabled(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	operations, err := Build(model.ScanResult{
		Root:   root,
		Stacks: []model.Stack{{Kind: "go", Path: ".", Commands: map[string][]string{"test": {"go", "test", "./..."}}}},
	}, model.ProfileBaseline, answers())
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		if operation.Path == ".sam-harness/COMMIT.md" {
			t.Fatal("COMMIT.md was planned without permission")
		}
	}
}

func answers() model.Answers {
	falsehood := false
	allowCI := false
	actions := []string{"write_repository"}
	return model.Answers{
		Criticality:         "low",
		DataSensitivity:     "public",
		DeploysToProduction: &falsehood,
		PersistentData:      &falsehood,
		IrreversibleActions: &falsehood,
		Approvers:           []string{"owner"},
		AllowCIChanges:      &allowCI,
		AllowedActions:      &actions,
		StandardizeCommits:  &falsehood,
	}
}

func buildProductionOperations(t *testing.T, root string) []model.Operation {
	t.Helper()
	return buildProductionOperationsWithAnswers(t, root, productionAnswers())
}

func buildProductionOperationsWithAnswers(t *testing.T, root string, approved model.Answers) []model.Operation {
	t.Helper()
	operations, err := Build(model.ScanResult{
		Root:        root,
		CIProviders: []string{"github", "gitlab"},
		Stacks: []model.Stack{
			{Kind: "go", Path: ".", Commands: map[string][]string{"build": {"go", "build", "./..."}, "test": {"go", "test", "./..."}}},
			{Kind: "go", Path: "services/api", Commands: map[string][]string{"test": {"go", "test", "./..."}}},
		},
		HasPersistence: true,
		HasDeployment:  true,
	}, model.ProfileProduction, approved)
	if err != nil {
		t.Fatal(err)
	}
	return operations
}

func withoutAction(actions []string, denied string) []string {
	filtered := make([]string, 0, len(actions))
	for _, action := range actions {
		if action != denied {
			filtered = append(filtered, action)
		}
	}
	return filtered
}

func bindingsForScope(bindings []model.CISecretBinding, scope string) []model.CISecretBinding {
	filtered := make([]model.CISecretBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Scope == scope {
			filtered = append(filtered, binding)
		}
	}
	return filtered
}

func productionAnswers() model.Answers {
	truth := true
	actions := []string{"write_repository", "network", "commit", "push", "release", "deploy"}
	workflow := &model.WorkflowConfig{
		Enabled:      true,
		StaticGuards: guardSet(model.StaticGuardCategories, "lint"),
		TestGuards:   guardSet(model.TestGuardCategories, "integration"),
		Correction: model.CorrectionConfig{
			Enabled:                true,
			FilesystemSandboxed:    true,
			TrustedExternalCommand: true,
			Command:                []string{"repair-agent", "--bounded"},
			MaxAttempts:            2,
			MaxChangedFiles:        5,
			MaxChangedLines:        120,
			BranchPrefix:           "sam-harness/repair-",
			OpenChangeRequest:      true,
		},
		Artifact: model.ArtifactWorkflow{
			Build:          commandSpec("build artifact", "tools/build-artifact"),
			ArtifactPath:   "dist/application.tar",
			SBOM:           commandSpec("build SBOM", "tools/build-sbom"),
			SBOMPath:       "dist/application.sbom.json",
			Provenance:     commandSpec("build provenance", "tools/build-provenance"),
			ProvenancePath: "dist/application.provenance.json",
		},
		Deployment: model.DeploymentWorkflow{
			Staging:           commandSpec("deploy staging", "tools/deploy-staging"),
			Production:        commandSpec("deploy production", "tools/deploy-production"),
			Rollback:          commandSpec("rollback production", "tools/rollback-production"),
			HealthChecks:      []model.CommandSpec{commandSpec("health", "tools/check-health")},
			ObservationChecks: []model.CommandSpec{commandSpec("observe", "tools/check-observation")},
			CanaryPercentages: []int{10, 50, 100},
		},
		Migration:       []model.CommandSpec{commandSpec("migration", "tools/migrate")},
		ReleaseSchedule: model.ReleaseSchedule{Cron: "17 4 * * 1", Timezone: "America/Asuncion"},
	}
	for _, role := range model.ReviewerRoles {
		workflow.Reviewers = append(workflow.Reviewers, model.ReviewerConfig{Role: role, Command: []string{"review-agent", "--role", string(role)}, TimeoutSeconds: 120, FilesystemReadOnly: true, TrustedExternalCommand: true})
	}
	return model.Answers{
		Criticality:         "high",
		DataSensitivity:     "internal",
		DeploysToProduction: &truth,
		PersistentData:      &truth,
		IrreversibleActions: &truth,
		Approvers:           []string{"owner"},
		AllowCIChanges:      &truth,
		AllowedActions:      &actions,
		CISecretBindings: map[string][]model.CISecretBinding{
			"github": {
				{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"},
				{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "REPAIR_API_KEY"},
			},
			"gitlab": {
				{Scope: model.CISecretScopeReview, Environment: "REVIEW_ENV", Secret: "OPENAI_API_KEY"},
				{Scope: model.CISecretScopeRepair, Environment: "REPAIR_ENV", Secret: "REPAIR_API_KEY"},
			},
		},
		AgentSecretEnvironments: map[string]string{"github": "agent-review", "gitlab": "agent-review"},
		AgentControlPlanes: map[string]model.AgentControlPlane{
			"github": {Mode: model.AgentControlPlaneModeGitHubApp, RequiredCheck: "sam-harness/review", AppIDSecret: "SAM_HARNESS_APP_ID", AppPrivateKeySecret: "SAM_HARNESS_APP_PRIVATE_KEY"},
			"gitlab": {Mode: model.AgentControlPlaneModeExternal, RequiredCheck: "sam-harness/review", ExternalProject: "trusted/review-control"},
		},
		ObservationWindow:     "24 hours",
		RollbackOwner:         "release owner",
		ProductionEnvironment: "production",
		StandardizeCommits:    &truth,
		CIAgentRuntime: &model.CIAgentRuntime{
			Host:             model.AgentHostCodex,
			LoginMethod:      model.AgentLoginAPIKey,
			LoginEnvironment: "REVIEW_ENV",
			LoginSecret:      "OPENAI_API_KEY",
		},
		Workflow: workflow,
	}
}

func guardSet(categories []string, waived string) model.GuardSet {
	guards := model.GuardSet{Commands: map[string]model.CommandSpec{}, Waivers: map[string]string{}}
	for _, category := range categories {
		if category == waived {
			guards.Waivers[category] = "accepted " + category + " waiver"
			continue
		}
		guards.Commands[category] = commandSpec(category, "tools/guard-"+category)
	}
	return guards
}

func commandSpec(name, executable string) model.CommandSpec {
	return model.CommandSpec{Name: name, Workdir: ".", Command: []string{executable}, Required: true, TimeoutSeconds: 120}
}

func writeTestExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func contentSection(t *testing.T, content, start, end string) string {
	t.Helper()
	startIndex := strings.Index(content, start)
	if startIndex < 0 {
		t.Fatalf("section start %q not found", start)
	}
	endIndex := strings.Index(content[startIndex+len(start):], end)
	if endIndex < 0 {
		t.Fatalf("section end %q not found after %q", end, start)
	}
	return content[startIndex : startIndex+len(start)+endIndex]
}

func contentSectionToEnd(t *testing.T, content, start string) string {
	t.Helper()
	startIndex := strings.Index(content, start)
	if startIndex < 0 {
		t.Fatalf("section start %q not found", start)
	}
	return content[startIndex:]
}

func operationContent(t *testing.T, operations []model.Operation, path string) string {
	t.Helper()
	for _, operation := range operations {
		if operation.Path == path {
			return operation.Content
		}
	}
	t.Fatalf("operation %s not found", path)
	return ""
}
