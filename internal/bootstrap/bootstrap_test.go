package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const (
	ordinaryGitHubPRJob = `name: sam-harness
on:
  pull_request:
  merge_group:
jobs:
  static:
    runs-on: ubuntu-latest
    steps:
      - run: go test ./...
`
	ordinaryGitLabMRJob = `static:
  stage: test
  script:
    - go test ./...
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
`
)

func TestCreatePlanHashesCanonicalJSONWithoutID(t *testing.T) {
	t.Parallel()
	desired := validDesired(providerGitHub)
	plan, err := CreatePlan(providerGitHub, "repo-fingerprint-a", desired)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ID == "" {
		t.Fatal("CreatePlan() left ID empty")
	}
	if plan.ID != mustHashPlan(t, plan) {
		t.Fatalf("plan ID %q does not match sha256 of canonical JSON without ID", plan.ID)
	}
	again, err := CreatePlan(providerGitHub, "repo-fingerprint-a", desired)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != plan.ID {
		t.Fatalf("CreatePlan() ID = %q, want stable %q", again.ID, plan.ID)
	}
	other, err := CreatePlan(providerGitHub, "repo-fingerprint-b", desired)
	if err != nil {
		t.Fatal(err)
	}
	if other.ID == plan.ID {
		t.Fatal("CreatePlan() reused an ID across different fingerprints")
	}
	assertCredentialFree(t, plan)
}

func TestCreatePlanCoversGitHubAndGitLabPolicy(t *testing.T) {
	t.Parallel()
	t.Run("github", func(t *testing.T) {
		t.Parallel()
		input := validDesired(providerGitHub)
		input.DirectPushesAllowed = true
		input.AdministratorBypassAllowed = true
		input.AllowForcePush = true
		input.AllowDeletions = true
		input.AllowSquash = false
		input.AllowMergeCommit = false
		input.AllowRebase = false
		input.MergeQueue = false
		input.RequiredChecks = []string{"custom-lint"}
		input.RequiredApprovals = 0
		plan, err := CreatePlan(providerGitHub, "github-fingerprint", input)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Provider != providerGitHub {
			t.Fatalf("Provider = %q, want github", plan.Provider)
		}
		got := plan.Desired
		if got.DirectPushesAllowed || got.AdministratorBypassAllowed || got.AllowForcePush || got.AllowDeletions {
			t.Fatalf("github desired still allows unprotected default-branch writes: %#v", got)
		}
		if !got.AllowSquash || got.AllowMergeCommit || got.AllowRebase {
			t.Fatalf("github merge methods = squash=%v merge=%v rebase=%v, want squash-only", got.AllowSquash, got.AllowMergeCommit, got.AllowRebase)
		}
		if !got.MergeQueue || !got.DismissStaleApprovals || !got.CodeOwnersRequired || !got.RequireConversationResolution || !got.RequireCurrentBase {
			t.Fatalf("github policy flags incomplete: %#v", got)
		}
		if got.RequiredApprovals < 1 {
			t.Fatal("github required approvals is 0")
		}
		assertRequiredChecks(t, got.RequiredChecks, got.ControlPlaneCheck)
		if containsString(got.RequiredChecks, "custom-lint") == false {
			t.Fatalf("RequiredChecks = %v, want to keep extra custom-lint", got.RequiredChecks)
		}
		assertField(t, got.Fields, "branch_protection", "required")
		assertField(t, got.Fields, "merge_queue", "required")
		assertField(t, got.Fields, "control_plane", got.ControlPlaneCheck)
		assertMutation(t, plan.Mutations, "direct_pushes_allowed", "false")
		assertMutation(t, plan.Mutations, "allow_force_push", "false")
		assertMutation(t, plan.Mutations, "allow_deletions", "false")
		assertMutation(t, plan.Mutations, "merge_queue", "true")
		assertMutation(t, plan.Mutations, "code_owners_required", "true")
		assertMutation(t, plan.Mutations, "protected_environments", "production")
		assertMutationHasPrefix(t, plan.Mutations, "fields.branch_protection")
		assertMutationHasPrefix(t, plan.Mutations, "job_texts.pull_request")
		if err := JobTextsCredentialFree(got.JobTexts); err != nil {
			t.Fatal(err)
		}
		assertCredentialFree(t, plan)
	})
	t.Run("gitlab", func(t *testing.T) {
		t.Parallel()
		input := validDesired(providerGitLab)
		input.AllowMergeCommit = true
		input.AllowSquash = true
		plan, err := CreatePlan(providerGitLab, "gitlab-fingerprint", input)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Provider != providerGitLab {
			t.Fatalf("Provider = %q, want gitlab", plan.Provider)
		}
		got := plan.Desired
		if got.DirectPushesAllowed || got.AdministratorBypassAllowed || got.AllowForcePush || got.AllowDeletions {
			t.Fatalf("gitlab desired still allows unprotected default-branch writes: %#v", got)
		}
		if !got.AllowMergeCommit || !got.AllowSquash {
			t.Fatal("gitlab dropped selected merge methods")
		}
		if got.MergeQueue {
			t.Fatal("gitlab plan enabled GitHub merge queue")
		}
		if !got.DismissStaleApprovals || !got.CodeOwnersRequired || !got.RequireConversationResolution || !got.RequireCurrentBase {
			t.Fatalf("gitlab policy flags incomplete: %#v", got)
		}
		assertRequiredChecks(t, got.RequiredChecks, got.ControlPlaneCheck)
		assertField(t, got.Fields, "protected_branches", "required")
		assertField(t, got.Fields, "approval_rules", "1")
		assertField(t, got.Fields, "control_plane", got.ControlPlaneCheck)
		assertMutation(t, plan.Mutations, "fields.approval_rules", "1")
		assertMutationHasPrefix(t, plan.Mutations, "job_texts.merge_request")
		if err := JobTextsCredentialFree(got.JobTexts); err != nil {
			t.Fatal(err)
		}
		assertCredentialFree(t, plan)
	})
}

func TestCreatePlanRejectsUnknownProviderAndMissingIdentity(t *testing.T) {
	t.Parallel()
	if _, err := CreatePlan("bitbucket", "fingerprint", validDesired(providerGitHub)); err == nil {
		t.Fatal("CreatePlan() accepted an unknown provider")
	}
	if _, err := CreatePlan(providerGitHub, "  ", validDesired(providerGitHub)); err == nil {
		t.Fatal("CreatePlan() accepted an empty fingerprint")
	}
	missingBranch := validDesired(providerGitHub)
	missingBranch.DefaultBranch = ""
	if _, err := CreatePlan(providerGitHub, "fingerprint", missingBranch); err == nil {
		t.Fatal("CreatePlan() accepted an empty default branch")
	}
	missingCheck := validDesired(providerGitHub)
	missingCheck.ControlPlaneCheck = ""
	if _, err := CreatePlan(providerGitHub, "fingerprint", missingCheck); err == nil {
		t.Fatal("CreatePlan() accepted an empty control-plane check")
	}
	missingEnvs := validDesired(providerGitHub)
	missingEnvs.ProtectedEnvironments = nil
	if _, err := CreatePlan(providerGitHub, "fingerprint", missingEnvs); err == nil {
		t.Fatal("CreatePlan() accepted empty protected environments")
	}
}

func TestCreatePlanRejectsCredentialValues(t *testing.T) {
	t.Parallel()
	marker := "ghp_THISMUSTNOTAPPEARINERRORS"
	cases := []struct {
		name   string
		mutate func(*RemoteState)
	}{
		{name: "job text", mutate: func(state *RemoteState) {
			state.JobTexts = map[string]string{"pull_request": "OPENAI_API_KEY=" + marker}
		}},
		{name: "literal token", mutate: func(state *RemoteState) {
			state.JobTexts = map[string]string{"pull_request": "token " + marker}
		}},
		{name: "field", mutate: func(state *RemoteState) {
			state.Fields = map[string]string{"token": marker}
		}},
		{name: "control plane", mutate: func(state *RemoteState) {
			state.ControlPlaneCheck = marker
		}},
		{name: "environment", mutate: func(state *RemoteState) {
			state.ProtectedEnvironments = []string{marker}
		}},
		{name: "pem", mutate: func(state *RemoteState) {
			state.JobTexts = map[string]string{"pull_request": "-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----"}
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			desired := validDesired(providerGitHub)
			tc.mutate(&desired)
			plan, err := CreatePlan(providerGitHub, "fingerprint", desired)
			if err == nil {
				t.Fatal("CreatePlan() accepted credential values")
			}
			if strings.Contains(err.Error(), marker) {
				t.Fatalf("error leaked a credential value: %v", err)
			}
			if plan.ID != "" || len(plan.Mutations) != 0 {
				t.Fatalf("rejected plan still carried contents: %#v", plan)
			}
		})
	}
}

func TestApplyRejectsMismatchedAcceptIDWithoutMutating(t *testing.T) {
	t.Parallel()
	plan, err := CreatePlan(providerGitHub, "fingerprint", validDesired(providerGitHub))
	if err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{}
	if _, err := Apply(plan, "wrong-id", transport); err == nil {
		t.Fatal("Apply() accepted a mismatched plan ID")
	}
	if _, err := Apply(plan, "", transport); err == nil {
		t.Fatal("Apply() accepted an empty accept ID")
	}
	if len(transport.calls) != 0 {
		t.Fatalf("transport.Apply called %d times before accept, want 0", len(transport.calls))
	}
	tampered := plan
	tampered.Desired.DirectPushesAllowed = true
	if _, err := Apply(tampered, tampered.ID, transport); err == nil {
		t.Fatal("Apply() accepted a plan whose contents do not match ID")
	}
	if len(transport.calls) != 0 {
		t.Fatalf("transport.Apply called for a tampered plan: %d", len(transport.calls))
	}
}

func TestApplyReadyFollowsReadbackAndReportsDrift(t *testing.T) {
	t.Parallel()
	plan, err := CreatePlan(providerGitHub, "fingerprint", validDesired(providerGitHub))
	if err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{}
	transport.onApply = func([]Mutation) {
		drifted := cloneState(plan.Desired)
		drifted.DirectPushesAllowed = true
		drifted.RequiredApprovals = 0
		drifted.RequiredChecks = []string{"static", "test"}
		transport.state = drifted
	}
	result, err := Apply(plan, plan.ID, transport)
	if err == nil {
		t.Fatal("Apply() error = nil, want readback mismatch")
	}
	if result.Ready {
		t.Fatal("Apply() Ready = true, want false until readback matches")
	}
	if !strings.Contains(err.Error(), "readback mismatch") {
		t.Fatalf("Apply() error = %v, want readback mismatch", err)
	}
	if len(transport.calls) != 1 {
		t.Fatalf("transport.Apply called %d times, want 1", len(transport.calls))
	}
	if len(result.Applied) == 0 {
		t.Fatal("Apply() Applied is empty after a mutating apply")
	}
	wantMismatches := []string{
		"direct_pushes_allowed: want false got true",
		"required_approvals: want 1 got 0",
		"required_checks: want " + formatList(plan.Desired.RequiredChecks) + " got static,test",
	}
	for _, want := range wantMismatches {
		if !containsString(result.Mismatches, want) {
			t.Fatalf("Mismatches = %v, want %q", result.Mismatches, want)
		}
	}
	assertCredentialFree(t, result)
}

func TestApplySecondCallIsIdempotent(t *testing.T) {
	t.Parallel()
	plan, err := CreatePlan(providerGitLab, "fingerprint", validDesired(providerGitLab))
	if err != nil {
		t.Fatal(err)
	}
	transport := &fakeTransport{}
	transport.onApply = func([]Mutation) {
		transport.state = cloneState(plan.Desired)
	}
	first, err := Apply(plan, plan.ID, transport)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Ready {
		t.Fatalf("first Apply() Ready = false, mismatches %v", first.Mismatches)
	}
	if len(first.Applied) == 0 {
		t.Fatal("first Apply() Applied is empty")
	}
	if len(transport.calls) != 1 {
		t.Fatalf("first Apply() transport calls = %d, want 1", len(transport.calls))
	}
	second, err := Apply(plan, plan.ID, transport)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Ready {
		t.Fatalf("second Apply() Ready = false, mismatches %v", second.Mismatches)
	}
	if len(second.Applied) != 0 {
		t.Fatalf("second Apply() Applied = %#v, want empty", second.Applied)
	}
	if len(transport.calls) != 1 {
		t.Fatalf("second Apply() called transport again (%d calls)", len(transport.calls))
	}
	if err := JobTextsCredentialFree(second.Readback.JobTexts); err != nil {
		t.Fatal(err)
	}
	assertCredentialFree(t, first)
	assertCredentialFree(t, second)
}

func TestJobTextsCredentialFree(t *testing.T) {
	t.Parallel()
	ordinary := map[string]string{
		"pull_request":  ordinaryGitHubPRJob,
		"merge_request": ordinaryGitLabMRJob,
	}
	if err := JobTextsCredentialFree(ordinary); err != nil {
		t.Fatalf("ordinary PR/MR job texts failed: %v", err)
	}
	if err := JobTextsCredentialFree(nil); err != nil {
		t.Fatalf("nil job texts failed: %v", err)
	}
	bads := []map[string]string{
		{"pr": "env:\n  OPENAI_API_KEY=secret-name-only"},
		{"pr": "SAM_HARNESS_APP_PRIVATE_KEY: ${APP_KEY}"},
		{"pr": "-----BEGIN RSA PRIVATE KEY-----\nabc\n-----END RSA PRIVATE KEY-----"},
		{"pr": "token sk-examplemodel"},
		{"pr": "auth ghp_example"},
		{"pr": "auth glpat-example"},
		{"pr": "auth xoxb-example"},
		{"pr": "id AKIAIOSFODNN7EXAMPLE"},
	}
	for _, texts := range bads {
		if err := JobTextsCredentialFree(texts); err == nil {
			t.Fatalf("JobTextsCredentialFree() accepted %#v", texts)
		} else if leak := leakedCredential(err.Error(), texts["pr"]); leak != "" {
			t.Fatalf("error leaked %q: %v", leak, err)
		}
	}
}

func TestSimulatePushRejectsDirectPushAndIncompleteAdminBypass(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	state := validDesired(providerGitHub)
	state.DirectPushesAllowed = true
	if err := SimulatePush(state, false, nil, now); err == nil || !strings.Contains(err.Error(), "direct push") {
		t.Fatalf("SimulatePush() error = %v, want direct push rejection", err)
	}
	complete := completeEmergency(now.Add(24 * time.Hour))
	if err := SimulatePush(state, false, &complete, now); err == nil {
		t.Fatal("SimulatePush() allowed a non-admin emergency direct push")
	}
	protected := validDesired(providerGitHub)
	if err := SimulatePush(protected, true, nil, now); err == nil || !strings.Contains(err.Error(), "admin bypass") {
		t.Fatalf("SimulatePush() error = %v, want admin bypass rejection", err)
	}
	incomplete := []EmergencyPolicy{
		{Reason: "outage", ExpiresAt: now.Add(time.Hour), AuditEvidence: "ticket-1", MandatoryRetrospective: true},
		{Approvers: []string{"sre"}, ExpiresAt: now.Add(time.Hour), AuditEvidence: "ticket-1", MandatoryRetrospective: true},
		{Approvers: []string{"sre"}, Reason: "outage", ExpiresAt: now, AuditEvidence: "ticket-1", MandatoryRetrospective: true},
		{Approvers: []string{"sre"}, Reason: "outage", ExpiresAt: now.Add(-time.Minute), AuditEvidence: "ticket-1", MandatoryRetrospective: true},
		{Approvers: []string{"sre"}, Reason: "outage", ExpiresAt: now.Add(time.Hour), MandatoryRetrospective: true},
		{Approvers: []string{"sre"}, Reason: "outage", ExpiresAt: now.Add(time.Hour), AuditEvidence: "ticket-1"},
	}
	for i, policy := range incomplete {
		if err := SimulatePush(protected, true, &policy, now); err == nil {
			t.Fatalf("case %d: SimulatePush() allowed incomplete emergency policy", i)
		} else if !strings.Contains(err.Error(), "admin bypass") {
			t.Fatalf("case %d: error = %v, want admin bypass", i, err)
		}
	}
	if err := SimulatePush(protected, true, &complete, now); err != nil {
		t.Fatalf("SimulatePush() rejected a complete emergency policy: %v", err)
	}
}

func TestSimulateMergeRejectsMissingChecksAndApprovals(t *testing.T) {
	t.Parallel()
	plan, err := CreatePlan(providerGitHub, "fingerprint", validDesired(providerGitHub))
	if err != nil {
		t.Fatal(err)
	}
	state := plan.Desired
	okChecks := append([]string(nil), state.RequiredChecks...)
	if err := SimulateMerge(state, okChecks, state.RequiredApprovals, true); err != nil {
		t.Fatalf("SimulateMerge() rejected a complete merge: %v", err)
	}
	missing := okChecks[1:]
	if err := SimulateMerge(state, missing, state.RequiredApprovals, true); err == nil || !strings.Contains(err.Error(), "required check") {
		t.Fatalf("SimulateMerge() error = %v, want missing required check", err)
	}
	if err := SimulateMerge(state, okChecks, 0, true); err == nil || !strings.Contains(err.Error(), "approval") {
		t.Fatalf("SimulateMerge() error = %v, want insufficient approvals", err)
	}
	withFreeze := cloneState(state)
	withFreeze.RequiredChecks = append(append([]string(nil), withFreeze.RequiredChecks...), "sam-harness/freeze")
	if err := SimulateMerge(withFreeze, withFreeze.RequiredChecks[:len(withFreeze.RequiredChecks)-1], withFreeze.RequiredApprovals, false); err == nil {
		t.Fatal("SimulateMerge() accepted a merge missing the freeze check")
	}
	if err := SimulateMerge(withFreeze, withFreeze.RequiredChecks[:len(withFreeze.RequiredChecks)-1], withFreeze.RequiredApprovals, true); err != nil {
		t.Fatalf("SimulateMerge() rejected freezeCheckPresent: %v", err)
	}
}

func TestCreatePlanDoesNotMutateCallerDesired(t *testing.T) {
	t.Parallel()
	desired := validDesired(providerGitHub)
	desired.RequiredChecks = []string{"custom"}
	original := cloneState(desired)
	if _, err := CreatePlan(providerGitHub, "fingerprint", desired); err != nil {
		t.Fatal(err)
	}
	if strings.Join(desired.RequiredChecks, ",") != strings.Join(original.RequiredChecks, ",") {
		t.Fatalf("CreatePlan() mutated caller RequiredChecks: %v", desired.RequiredChecks)
	}
	if desired.Fields != nil {
		t.Fatal("CreatePlan() mutated caller Fields")
	}
}

type fakeTransport struct {
	state    RemoteState
	calls    [][]Mutation
	onApply  func([]Mutation)
	readErr  error
	applyErr error
}

func (f *fakeTransport) Read() (RemoteState, error) {
	if f.readErr != nil {
		return RemoteState{}, f.readErr
	}
	return cloneState(f.state), nil
}

func (f *fakeTransport) Apply(mutations []Mutation) error {
	f.calls = append(f.calls, cloneMutations(mutations))
	if f.applyErr != nil {
		return f.applyErr
	}
	if f.onApply != nil {
		f.onApply(mutations)
	}
	return nil
}

func validDesired(provider Provider) RemoteState {
	check := "sam-harness/" + string(provider)
	state := RemoteState{
		DefaultBranch:                 "main",
		RequiredChecks:                []string{"static", "test", "review", "artifact", check},
		RequiredApprovals:             1,
		DismissStaleApprovals:         true,
		CodeOwnersRequired:            true,
		AllowSquash:                   true,
		RequireConversationResolution: true,
		RequireCurrentBase:            true,
		MergeQueue:                    provider == providerGitHub,
		ProtectedEnvironments:         []string{"production"},
		ControlPlaneCheck:             check,
	}
	if provider == providerGitHub {
		state.JobTexts = map[string]string{"pull_request": ordinaryGitHubPRJob, "merge_group": ordinaryGitHubPRJob}
	} else {
		state.JobTexts = map[string]string{"merge_request": ordinaryGitLabMRJob}
	}
	return state
}

func completeEmergency(expires time.Time) EmergencyPolicy {
	return EmergencyPolicy{
		Approvers:              []string{"sre-oncall"},
		Reason:                 "production outage",
		ExpiresAt:              expires,
		AuditEvidence:          "incident-42",
		MandatoryRetrospective: true,
	}
}

func mustHashPlan(t *testing.T, plan Plan) string {
	t.Helper()
	plan.ID = ""
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func assertRequiredChecks(t *testing.T, checks []string, providerCheck string) {
	t.Helper()
	for _, name := range []string{"static", "test", "review", "artifact", providerCheck} {
		if !containsString(checks, name) {
			t.Fatalf("RequiredChecks = %v, want %q", checks, name)
		}
	}
}

func assertField(t *testing.T, fields map[string]string, key, want string) {
	t.Helper()
	if fields[key] != want {
		t.Fatalf("Fields[%q] = %q, want %q (fields=%v)", key, fields[key], want, fields)
	}
}

func assertMutation(t *testing.T, mutations []Mutation, field, want string) {
	t.Helper()
	for _, mutation := range mutations {
		if mutation.Field == field {
			if mutation.Value != want {
				t.Fatalf("mutation %s = %q, want %q", field, mutation.Value, want)
			}
			return
		}
	}
	t.Fatalf("missing mutation %q in %#v", field, mutations)
}

func assertMutationHasPrefix(t *testing.T, mutations []Mutation, field string) {
	t.Helper()
	for _, mutation := range mutations {
		if mutation.Field == field {
			if mutation.Value == "" {
				t.Fatalf("mutation %s is empty", field)
			}
			return
		}
	}
	t.Fatalf("missing mutation %q", field)
}

func assertCredentialFree(t *testing.T, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if marker := credentialMarker(text); marker != "" {
		t.Fatalf("persisted value contains credential marker %q", marker)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func leakedCredential(errText, source string) string {
	for _, marker := range []string{"ghp_", "glpat-", "sk-", "AKIA", "xox", "BEGIN ", "OPENAI_API_KEY="} {
		idx := strings.Index(source, marker)
		if idx < 0 {
			continue
		}
		end := idx + len(marker)
		for end < len(source) && source[end] != ' ' && source[end] != '\n' && source[end] != '"' {
			end++
		}
		token := source[idx:end]
		if strings.Contains(errText, token) && token != marker {
			return token
		}
		if strings.Contains(errText, source) {
			return source
		}
	}
	return ""
}
