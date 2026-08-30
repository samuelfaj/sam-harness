package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Provider string

const (
	GitHub         Provider = "github"
	GitLab         Provider = "gitlab"
	providerGitHub          = GitHub
	providerGitLab          = GitLab
)

type Mutation struct {
	Field string `json:"field"`
	Value string `json:"value"`
}

type RemoteState struct {
	DefaultBranch                 string            `json:"default_branch"`
	DirectPushesAllowed           bool              `json:"direct_pushes_allowed"`
	AdministratorBypassAllowed    bool              `json:"administrator_bypass_allowed"`
	RequiredChecks                []string          `json:"required_checks"`
	RequiredApprovals             int               `json:"required_approvals"`
	DismissStaleApprovals         bool              `json:"dismiss_stale_approvals"`
	CodeOwnersRequired            bool              `json:"code_owners_required"`
	AllowMergeCommit              bool              `json:"allow_merge_commit"`
	AllowSquash                   bool              `json:"allow_squash"`
	AllowRebase                   bool              `json:"allow_rebase"`
	RequireConversationResolution bool              `json:"require_conversation_resolution"`
	RequireCurrentBase            bool              `json:"require_current_base"`
	MergeQueue                    bool              `json:"merge_queue"`
	AllowForcePush                bool              `json:"allow_force_push"`
	AllowDeletions                bool              `json:"allow_deletions"`
	ProtectedEnvironments         []string          `json:"protected_environments"`
	ControlPlaneCheck             string            `json:"control_plane_check"`
	JobTexts                      map[string]string `json:"job_texts,omitempty"`
	Fields                        map[string]string `json:"fields,omitempty"`
	MergeQueueDispatch            string            `json:"merge_queue_dispatch,omitempty"`
}

type Transport interface {
	Read() (RemoteState, error)
	Apply(mutations []Mutation) error
}

type EmergencyPolicy struct {
	Approvers              []string  `json:"approvers"`
	Reason                 string    `json:"reason"`
	ExpiresAt              time.Time `json:"expires_at"`
	AuditEvidence          string    `json:"audit_evidence"`
	MandatoryRetrospective bool      `json:"mandatory_retrospective"`
}

type Plan struct {
	ID          string      `json:"id"`
	Provider    Provider    `json:"provider"`
	Fingerprint string      `json:"fingerprint"`
	Mutations   []Mutation  `json:"mutations"`
	Desired     RemoteState `json:"desired"`
}

type Result struct {
	Ready      bool        `json:"ready"`
	Mismatches []string    `json:"mismatches"`
	Applied    []Mutation  `json:"applied"`
	Readback   RemoteState `json:"readback"`
}

func CreatePlan(provider Provider, fingerprint string, desired RemoteState) (Plan, error) {
	switch provider {
	case providerGitHub, providerGitLab:
	default:
		return Plan{}, fmt.Errorf("unknown provider %q", provider)
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return Plan{}, fmt.Errorf("fingerprint is required")
	}
	normalized, err := normalizeDesired(provider, desired)
	if err != nil {
		return Plan{}, err
	}
	if err := remoteStateCredentialFree(normalized); err != nil {
		return Plan{}, err
	}
	if err := valueCredentialFree(fingerprint); err != nil {
		return Plan{}, err
	}
	plan := Plan{
		Provider:    provider,
		Fingerprint: fingerprint,
		Mutations:   mutationsFrom(normalized),
		Desired:     normalized,
	}
	if err := planCredentialFree(plan); err != nil {
		return Plan{}, err
	}
	id, err := calculateID(plan)
	if err != nil {
		return Plan{}, err
	}
	plan.ID = id
	return plan, nil
}

func Apply(plan Plan, acceptID string, transport Transport) (Result, error) {
	if acceptID == "" || plan.ID == "" || acceptID != plan.ID {
		return Result{}, fmt.Errorf("bootstrap apply requires accept ID matching plan ID")
	}
	id, err := calculateID(plan)
	if err != nil {
		return Result{}, err
	}
	if id != plan.ID {
		return Result{}, fmt.Errorf("plan contents do not match plan ID")
	}
	if err := planCredentialFree(plan); err != nil {
		return Result{}, err
	}
	if transport == nil {
		return Result{}, fmt.Errorf("transport is required")
	}
	current, err := transport.Read()
	if err != nil {
		return Result{}, err
	}
	if err := remoteStateCredentialFree(current); err != nil {
		return Result{}, err
	}
	if mismatches := diffStates(plan.Desired, current); len(mismatches) == 0 {
		return Result{
			Ready:      true,
			Mismatches: []string{},
			Applied:    []Mutation{},
			Readback:   cloneState(current),
		}, nil
	}
	if err := transport.Apply(plan.Mutations); err != nil {
		return Result{}, err
	}
	readback, err := transport.Read()
	if err != nil {
		return Result{}, err
	}
	if err := remoteStateCredentialFree(readback); err != nil {
		return Result{}, err
	}
	mismatches := diffStates(plan.Desired, readback)
	if mismatches == nil {
		mismatches = []string{}
	}
	result := Result{
		Ready:      len(mismatches) == 0,
		Mismatches: mismatches,
		Applied:    cloneMutations(plan.Mutations),
		Readback:   cloneState(readback),
	}
	if !result.Ready {
		return result, fmt.Errorf("readback mismatch: %s", strings.Join(mismatches, "; "))
	}
	return result, nil
}

func SimulatePush(state RemoteState, actorIsAdmin bool, emergency *EmergencyPolicy, now time.Time) error {
	_ = state
	if !actorIsAdmin {
		return fmt.Errorf("direct pushes are not allowed")
	}
	return emergencyComplete(emergency, now)
}

func SimulateMerge(state RemoteState, checks []string, approvals int, freezeCheckPresent bool) error {
	present := make(map[string]bool, len(checks)+1)
	for _, check := range checks {
		check = strings.TrimSpace(check)
		if check == "" {
			continue
		}
		present[check] = true
	}
	if freezeCheckPresent {
		present["sam-harness/freeze"] = true
	}
	var missing []string
	for _, required := range state.RequiredChecks {
		if !present[required] {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required check %s", strings.Join(missing, ", "))
	}
	if approvals < state.RequiredApprovals {
		return fmt.Errorf("insufficient approvals: want %d got %d", state.RequiredApprovals, approvals)
	}
	return nil
}

const GitHubMergeQueueDispatch = "sam_harness_merge_group_review"

func JobTextsCredentialFree(texts map[string]string) error {
	if err := mapCredentialFree("job text", texts); err != nil {
		return err
	}
	return secretBearingJobTextsSafe(texts)
}

func secretBearingJobTextsSafe(texts map[string]string) error {
	for _, key := range sortedKeys(texts) {
		text := texts[key]
		if secretBearingJobText(text) && hasDirectMergeGroupTrigger(text) {
			return fmt.Errorf("secret-bearing job text %q must not use a direct merge_group trigger", key)
		}
	}
	return nil
}

func secretBearingJobText(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "pull_request_target:" || strings.HasPrefix(trimmed, "pull_request_target:") {
			return true
		}
	}
	return false
}

func hasDirectMergeGroupTrigger(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == "merge_group:" || strings.HasPrefix(trimmed, "merge_group:") {
			return true
		}
	}
	return false
}

func normalizeDesired(provider Provider, desired RemoteState) (RemoteState, error) {
	desired = cloneState(desired)
	desired.DefaultBranch = strings.TrimSpace(desired.DefaultBranch)
	if desired.DefaultBranch == "" {
		return RemoteState{}, fmt.Errorf("default branch is required")
	}
	desired.ControlPlaneCheck = strings.TrimSpace(desired.ControlPlaneCheck)
	if desired.ControlPlaneCheck == "" {
		return RemoteState{}, fmt.Errorf("control-plane check is required")
	}
	envs := uniqueTrimmed(desired.ProtectedEnvironments)
	if len(envs) == 0 {
		return RemoteState{}, fmt.Errorf("protected environments are required")
	}
	desired.ProtectedEnvironments = envs
	desired.DirectPushesAllowed = false
	desired.AdministratorBypassAllowed = false
	desired.DismissStaleApprovals = true
	desired.CodeOwnersRequired = true
	desired.RequireConversationResolution = true
	desired.RequireCurrentBase = true
	desired.AllowForcePush = false
	desired.AllowDeletions = false
	if !desired.AllowSquash && !desired.AllowMergeCommit && !desired.AllowRebase {
		desired.AllowSquash = true
	}
	if provider == providerGitHub {
		desired.MergeQueue = true
		if strings.TrimSpace(desired.MergeQueueDispatch) == "" {
			desired.MergeQueueDispatch = GitHubMergeQueueDispatch
		}
		if desired.Fields == nil {
			desired.Fields = map[string]string{}
		}
		if strings.TrimSpace(desired.Fields["merge_queue_dispatch"]) == "" {
			desired.Fields["merge_queue_dispatch"] = GitHubMergeQueueDispatch
		}
		if desired.JobTexts == nil {
			desired.JobTexts = map[string]string{}
		}
		if !dispatcherWorkflowComplete(desired.JobTexts["merge_queue_dispatch"]) {
			desired.JobTexts["merge_queue_dispatch"] = MergeQueueDispatcherWorkflow()
		}
	}
	if desired.RequiredApprovals < 1 {
		desired.RequiredApprovals = 1
	}
	desired.RequiredChecks = ensureRequiredChecks(desired.RequiredChecks, desired.ControlPlaneCheck)
	desired.Fields = ensurePolicyFields(provider, desired)
	return desired, nil
}

func ensureRequiredChecks(existing []string, providerCheck string) []string {
	seen := make(map[string]bool, len(existing)+5)
	var out []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	add("static")
	add("test")
	add("review")
	add("artifact")
	add(providerCheck)
	for _, name := range existing {
		add(name)
	}
	return out
}

func ensurePolicyFields(provider Provider, desired RemoteState) map[string]string {
	fields := copyMap(desired.Fields)
	if fields == nil {
		fields = map[string]string{}
	}
	set := func(key, value string) {
		if strings.TrimSpace(fields[key]) == "" {
			fields[key] = value
		}
	}
	switch provider {
	case providerGitHub:
		set("branch_protection", "required")
		set("merge_queue", "required")
	case providerGitLab:
		set("protected_branches", "required")
		set("approval_rules", strconv.Itoa(desired.RequiredApprovals))
	}
	set("control_plane", desired.ControlPlaneCheck)
	return fields
}

func mutationsFrom(desired RemoteState) []Mutation {
	mutations := []Mutation{
		{Field: "default_branch", Value: desired.DefaultBranch},
		{Field: "direct_pushes_allowed", Value: strconv.FormatBool(desired.DirectPushesAllowed)},
		{Field: "administrator_bypass_allowed", Value: strconv.FormatBool(desired.AdministratorBypassAllowed)},
		{Field: "required_checks", Value: formatList(desired.RequiredChecks)},
		{Field: "required_approvals", Value: strconv.Itoa(desired.RequiredApprovals)},
		{Field: "dismiss_stale_approvals", Value: strconv.FormatBool(desired.DismissStaleApprovals)},
		{Field: "code_owners_required", Value: strconv.FormatBool(desired.CodeOwnersRequired)},
		{Field: "allow_merge_commit", Value: strconv.FormatBool(desired.AllowMergeCommit)},
		{Field: "allow_squash", Value: strconv.FormatBool(desired.AllowSquash)},
		{Field: "allow_rebase", Value: strconv.FormatBool(desired.AllowRebase)},
		{Field: "require_conversation_resolution", Value: strconv.FormatBool(desired.RequireConversationResolution)},
		{Field: "require_current_base", Value: strconv.FormatBool(desired.RequireCurrentBase)},
		{Field: "merge_queue", Value: strconv.FormatBool(desired.MergeQueue)},
		{Field: "allow_force_push", Value: strconv.FormatBool(desired.AllowForcePush)},
		{Field: "allow_deletions", Value: strconv.FormatBool(desired.AllowDeletions)},
		{Field: "protected_environments", Value: formatList(desired.ProtectedEnvironments)},
		{Field: "control_plane_check", Value: desired.ControlPlaneCheck},
		{Field: "merge_queue_dispatch", Value: desired.MergeQueueDispatch},
	}
	for _, key := range sortedKeys(desired.Fields) {
		mutations = append(mutations, Mutation{Field: "fields." + key, Value: desired.Fields[key]})
	}
	for _, key := range sortedKeys(desired.JobTexts) {
		mutations = append(mutations, Mutation{Field: "job_texts." + key, Value: desired.JobTexts[key]})
	}
	return mutations
}

func diffStates(want, got RemoteState) []string {
	var mismatches []string
	add := func(field, wantValue, gotValue string) {
		if wantValue != gotValue {
			mismatches = append(mismatches, fmt.Sprintf("%s: want %s got %s", field, wantValue, gotValue))
		}
	}
	add("default_branch", want.DefaultBranch, got.DefaultBranch)
	add("direct_pushes_allowed", strconv.FormatBool(want.DirectPushesAllowed), strconv.FormatBool(got.DirectPushesAllowed))
	add("administrator_bypass_allowed", strconv.FormatBool(want.AdministratorBypassAllowed), strconv.FormatBool(got.AdministratorBypassAllowed))
	add("required_checks", formatList(want.RequiredChecks), formatList(got.RequiredChecks))
	add("required_approvals", strconv.Itoa(want.RequiredApprovals), strconv.Itoa(got.RequiredApprovals))
	add("dismiss_stale_approvals", strconv.FormatBool(want.DismissStaleApprovals), strconv.FormatBool(got.DismissStaleApprovals))
	add("code_owners_required", strconv.FormatBool(want.CodeOwnersRequired), strconv.FormatBool(got.CodeOwnersRequired))
	add("allow_merge_commit", strconv.FormatBool(want.AllowMergeCommit), strconv.FormatBool(got.AllowMergeCommit))
	add("allow_squash", strconv.FormatBool(want.AllowSquash), strconv.FormatBool(got.AllowSquash))
	add("allow_rebase", strconv.FormatBool(want.AllowRebase), strconv.FormatBool(got.AllowRebase))
	add("require_conversation_resolution", strconv.FormatBool(want.RequireConversationResolution), strconv.FormatBool(got.RequireConversationResolution))
	add("require_current_base", strconv.FormatBool(want.RequireCurrentBase), strconv.FormatBool(got.RequireCurrentBase))
	add("merge_queue", strconv.FormatBool(want.MergeQueue), strconv.FormatBool(got.MergeQueue))
	add("allow_force_push", strconv.FormatBool(want.AllowForcePush), strconv.FormatBool(got.AllowForcePush))
	add("allow_deletions", strconv.FormatBool(want.AllowDeletions), strconv.FormatBool(got.AllowDeletions))
	add("protected_environments", formatList(want.ProtectedEnvironments), formatList(got.ProtectedEnvironments))
	add("control_plane_check", want.ControlPlaneCheck, got.ControlPlaneCheck)
	add("merge_queue_dispatch", want.MergeQueueDispatch, got.MergeQueueDispatch)
	addMapMismatches(&mismatches, "job_texts", want.JobTexts, got.JobTexts)
	addMapMismatches(&mismatches, "fields", want.Fields, got.Fields)
	return mismatches
}

func addMapMismatches(list *[]string, prefix string, want, got map[string]string) {
	seen := map[string]bool{}
	for key := range want {
		seen[key] = true
	}
	for key := range got {
		seen[key] = true
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if want[key] != got[key] {
			*list = append(*list, fmt.Sprintf("%s.%s: want %s got %s", prefix, key, want[key], got[key]))
		}
	}
}

func emergencyComplete(policy *EmergencyPolicy, now time.Time) error {
	if policy == nil {
		return fmt.Errorf("admin bypass requires a complete emergency policy")
	}
	named := 0
	for _, approver := range policy.Approvers {
		if strings.TrimSpace(approver) != "" {
			named++
		}
	}
	if named == 0 {
		return fmt.Errorf("admin bypass requires named approvers")
	}
	if strings.TrimSpace(policy.Reason) == "" {
		return fmt.Errorf("admin bypass requires a reason")
	}
	if !policy.ExpiresAt.After(now) {
		return fmt.Errorf("admin bypass requires a future expiry")
	}
	if strings.TrimSpace(policy.AuditEvidence) == "" {
		return fmt.Errorf("admin bypass requires audit evidence")
	}
	if !policy.MandatoryRetrospective {
		return fmt.Errorf("admin bypass requires a mandatory retrospective")
	}
	return nil
}

func calculateID(plan Plan) (string, error) {
	plan.ID = ""
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func planCredentialFree(plan Plan) error {
	if err := valueCredentialFree(string(plan.Provider)); err != nil {
		return err
	}
	if err := valueCredentialFree(plan.Fingerprint); err != nil {
		return err
	}
	if err := valueCredentialFree(plan.ID); err != nil {
		return err
	}
	for _, mutation := range plan.Mutations {
		if err := valueCredentialFree(mutation.Field); err != nil {
			return err
		}
		if err := valueCredentialFree(mutation.Value); err != nil {
			return err
		}
	}
	return remoteStateCredentialFree(plan.Desired)
}

func remoteStateCredentialFree(state RemoteState) error {
	if err := valueCredentialFree(state.DefaultBranch); err != nil {
		return err
	}
	for _, check := range state.RequiredChecks {
		if err := valueCredentialFree(check); err != nil {
			return err
		}
	}
	for _, env := range state.ProtectedEnvironments {
		if err := valueCredentialFree(env); err != nil {
			return err
		}
	}
	if err := valueCredentialFree(state.ControlPlaneCheck); err != nil {
		return err
	}
	if err := JobTextsCredentialFree(state.JobTexts); err != nil {
		return err
	}
	if err := valueCredentialFree(state.MergeQueueDispatch); err != nil {
		return err
	}
	return mapCredentialFree("field", state.Fields)
}

func mapCredentialFree(kind string, values map[string]string) error {
	for _, key := range sortedKeys(values) {
		if err := valueCredentialFree(key); err != nil {
			return fmt.Errorf("%s %q is not credential-free", kind, key)
		}
		if err := valueCredentialFree(values[key]); err != nil {
			return fmt.Errorf("%s %q is not credential-free", kind, key)
		}
	}
	return nil
}

func valueCredentialFree(value string) error {
	if credentialMarker(value) == "" {
		return nil
	}
	return fmt.Errorf("credential values are not allowed")
}

func credentialMarker(value string) string {
	switch {
	case strings.Contains(value, "OPENAI_API_KEY="):
		return "OPENAI_API_KEY="
	case strings.Contains(value, "OPENAI_API_KEY"):
		return "OPENAI_API_KEY"
	case strings.Contains(value, "PRIVATE_KEY"):
		return "PRIVATE_KEY"
	case strings.Contains(value, "BEGIN "):
		return "PEM"
	case strings.Contains(value, "sk-"):
		return "sk-"
	case strings.Contains(value, "ghp_"):
		return "ghp_"
	case strings.Contains(value, "glpat-"):
		return "glpat-"
	case strings.Contains(value, "xox"):
		return "xox"
	case strings.Contains(value, "AKIA"):
		return "AKIA"
	default:
		return ""
	}
}

func formatList(values []string) string {
	return strings.Join(values, ",")
}

func uniqueTrimmed(values []string) []string {
	seen := make(map[string]bool, len(values))
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func cloneMutations(in []Mutation) []Mutation {
	if in == nil {
		return nil
	}
	out := make([]Mutation, len(in))
	copy(out, in)
	return out
}

func cloneState(in RemoteState) RemoteState {
	out := in
	if in.RequiredChecks != nil {
		out.RequiredChecks = append([]string(nil), in.RequiredChecks...)
	}
	if in.ProtectedEnvironments != nil {
		out.ProtectedEnvironments = append([]string(nil), in.ProtectedEnvironments...)
	}
	out.JobTexts = copyMap(in.JobTexts)
	out.Fields = copyMap(in.Fields)
	return out
}

func copyMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
