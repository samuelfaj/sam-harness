package freeze

import (
	"strings"
	"testing"
	"time"
)

func TestRequiredCheckName(t *testing.T) {
	t.Parallel()
	if got := RequiredCheckName(); got != "sam-harness/freeze" {
		t.Fatalf("RequiredCheckName() = %q, want sam-harness/freeze", got)
	}
}

func TestOrdinaryFeatureInsideWindowIsBlocked(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	err := Evaluate(testPolicy(), featureRequest(), now)
	if err == nil {
		t.Fatal("Evaluate allowed an ordinary feature inside the freeze window")
	}
	if !strings.Contains(err.Error(), "freeze") {
		t.Fatalf("error %q does not contain freeze", err)
	}
}

func TestFeatureOutsideWindowIsAllowed(t *testing.T) {
	t.Parallel()
	policy := testPolicy()
	clock := Clock(func() time.Time {
		return time.Date(2026, 12, 28, 12, 0, 0, 0, time.UTC)
	})
	if err := Evaluate(policy, featureRequest(), clock()); err != nil {
		t.Fatalf("Evaluate rejected a feature after the freeze window: %v", err)
	}
	clock = Clock(func() time.Time {
		return time.Date(2026, 12, 19, 12, 0, 0, 0, time.UTC)
	})
	if err := Evaluate(policy, featureRequest(), clock()); err != nil {
		t.Fatalf("Evaluate rejected a feature before the freeze window: %v", err)
	}
}

func TestConfiguredExceptionWithCompleteEvidenceIsAllowed(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	req := featureRequest()
	ex := completeException(now, req.HeadSHA)
	req.Exception = &ex
	if err := Evaluate(testPolicy(), req, now); err != nil {
		t.Fatalf("Evaluate rejected a configured exception with complete evidence: %v", err)
	}
}

func TestMissingEvidenceIsRejected(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	policy := testPolicy()
	req := featureRequest()
	cases := []struct {
		name string
		mut  func(*Exception)
	}{
		{"class", func(ex *Exception) { ex.Class = "" }},
		{"severity", func(ex *Exception) { ex.Severity = "" }},
		{"reference", func(ex *Exception) { ex.Reference = "" }},
		{"scope", func(ex *Exception) { ex.Scope = nil }},
		{"empty scope", func(ex *Exception) { ex.Scope = []string{""} }},
		{"rollback", func(ex *Exception) { ex.RollbackPlan = "" }},
		{"approvers", func(ex *Exception) { ex.Approvers = nil }},
		{"empty approvers", func(ex *Exception) { ex.Approvers = []string{""} }},
		{"head", func(ex *Exception) { ex.HeadSHA = "" }},
		{"approved at", func(ex *Exception) { ex.ApprovedAt = time.Time{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ex := completeException(now, req.HeadSHA)
			tc.mut(&ex)
			got := req
			got.Exception = &ex
			err := Evaluate(policy, got, now)
			if err == nil {
				t.Fatalf("Evaluate allowed an exception with missing %s", tc.name)
			}
			if !strings.Contains(err.Error(), "freeze") {
				t.Fatalf("error %q does not contain freeze", err)
			}
		})
	}
}

func TestUnconfiguredExceptionClassIsRejected(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	req := featureRequest()
	ex := completeException(now, req.HeadSHA)
	ex.Class = "P2"
	req.Exception = &ex
	if err := Evaluate(testPolicy(), req, now); err == nil {
		t.Fatal("Evaluate allowed an exception class that is not configured")
	}
}

func TestStaleApprovalIsRejected(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	req := featureRequest()
	ex := completeException(now, req.HeadSHA)
	ex.ApprovedAt = now.Add(time.Hour)
	req.Exception = &ex
	err := Evaluate(testPolicy(), req, now)
	if err == nil {
		t.Fatal("Evaluate allowed an approval from the future")
	}
	if !strings.Contains(err.Error(), "stale approval") {
		t.Fatalf("error %q does not mention stale approval", err)
	}
}

func TestStaleHeadIsRejected(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	req := featureRequest()
	ex := completeException(now, req.HeadSHA)
	ex.HeadSHA = "stale-head"
	req.Exception = &ex
	err := Evaluate(testPolicy(), req, now)
	if err == nil {
		t.Fatal("Evaluate allowed an exception bound to a different head")
	}
	if !strings.Contains(err.Error(), "stale head") {
		t.Fatalf("error %q does not mention stale head", err)
	}
}

func TestExpiredWindowIsRejected(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	req := featureRequest()
	ex := completeException(now, req.HeadSHA)
	ex.ExpiresAt = now
	req.Exception = &ex
	err := Evaluate(testPolicy(), req, now)
	if err == nil {
		t.Fatal("Evaluate allowed an exception whose window is not after now")
	}
	if !strings.Contains(err.Error(), "expired window") {
		t.Fatalf("error %q does not mention expired window", err)
	}
	ex.ExpiresAt = now.Add(-time.Minute)
	req.Exception = &ex
	if err := Evaluate(testPolicy(), req, now); err == nil {
		t.Fatal("Evaluate allowed an exception whose window already ended")
	}
}

func TestBranchOutsidePolicyIsRejected(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	req := featureRequest()
	req.Branch = "feature/out-of-scope"
	ex := completeException(now, req.HeadSHA)
	req.Exception = &ex
	if err := Evaluate(testPolicy(), req, now); err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("Evaluate() error = %v, want branch outside freeze policy", err)
	}
}

func TestWorkflowCanDisableStillEvaluates(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	req := featureRequest()
	req.WorkflowCanDisable = true
	err := Evaluate(testPolicy(), req, now)
	if err == nil {
		t.Fatal("Evaluate skipped the freeze check because the workflow could disable it")
	}
	if !strings.Contains(err.Error(), "freeze") {
		t.Fatalf("error %q does not contain freeze", err)
	}

	ex := completeException(now, req.HeadSHA)
	ex.Severity = ""
	req.Exception = &ex
	if err := Evaluate(testPolicy(), req, now); err == nil {
		t.Fatal("Evaluate skipped missing-evidence checks because the workflow could disable them")
	}

	ex = completeException(now, req.HeadSHA)
	req.Exception = &ex
	if err := Evaluate(testPolicy(), req, now); err != nil {
		t.Fatalf("Evaluate rejected a complete exception while WorkflowCanDisable was set: %v", err)
	}
}

func TestScheduledReleaseDuringFreezeRequiresReleaseFix(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	policy := testPolicy()
	req := featureRequest()
	req.ScheduledRelease = true
	if err := Evaluate(policy, req, now); err == nil {
		t.Fatal("Evaluate allowed a scheduled release during freeze without evidence")
	}

	ex := completeException(now, req.HeadSHA)
	req.Exception = &ex
	if err := Evaluate(policy, req, now); err == nil {
		t.Fatal("Evaluate allowed a scheduled release during freeze with class P0")
	}

	ex.Class = "release-fix"
	req.Exception = &ex
	if err := Evaluate(policy, req, now); err != nil {
		t.Fatalf("Evaluate rejected a scheduled release with complete release-fix evidence: %v", err)
	}

	ex.RollbackPlan = ""
	req.Exception = &ex
	if err := Evaluate(policy, req, now); err == nil {
		t.Fatal("Evaluate allowed a scheduled release-fix without complete evidence")
	}

	after := Clock(func() time.Time {
		return time.Date(2026, 12, 28, 0, 0, 1, 0, time.UTC)
	})
	req = featureRequest()
	req.ScheduledRelease = true
	if err := Evaluate(policy, req, after()); err != nil {
		t.Fatalf("Evaluate refused a scheduled release after freeze ended: %v", err)
	}
}

func TestInvalidTimezoneAndWindow(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	req := featureRequest()

	policy := testPolicy()
	policy.Timezone = ""
	if _, err := Active(policy, now); err == nil {
		t.Fatal("Active accepted an empty timezone")
	}
	if err := Evaluate(policy, req, now); err == nil {
		t.Fatal("Evaluate accepted an empty timezone")
	}

	policy = testPolicy()
	policy.Timezone = "Not/AZone"
	if _, err := Active(policy, now); err == nil {
		t.Fatal("Active accepted an invalid timezone")
	}

	policy = testPolicy()
	policy.Start = "not-rfc3339"
	if _, err := Active(policy, now); err == nil {
		t.Fatal("Active accepted an invalid start")
	}

	policy = testPolicy()
	policy.End = "2026-12-19T00:00:00Z"
	if _, err := Active(policy, now); err == nil {
		t.Fatal("Active accepted a window whose end is before start")
	}
	if err := Evaluate(policy, req, now); err == nil {
		t.Fatal("Evaluate accepted an inverted window")
	}

	policy = testPolicy()
	policy.Start = policy.End
	if _, err := Active(policy, now); err == nil {
		t.Fatal("Active accepted a zero-length window")
	}
}

func TestActiveInsideAndOutsideWindow(t *testing.T) {
	t.Parallel()
	policy := testPolicy()
	now := fixedNow()
	active, err := Active(policy, now)
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("Active() = false inside the freeze window")
	}
	active, err = Active(policy, time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("Active() = false at window start")
	}
	active, err = Active(policy, time.Date(2026, 12, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("Active() = false at window end")
	}
	active, err = Active(policy, time.Date(2026, 12, 27, 0, 0, 1, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("Active() = true after the freeze window")
	}
}

func TestActiveUsesIANATimezone(t *testing.T) {
	t.Parallel()
	policy := testPolicy()
	policy.Timezone = "America/New_York"
	policy.Start = "2026-12-24T00:00:00-05:00"
	policy.End = "2026-12-26T00:00:00-05:00"
	clock := Clock(func() time.Time {
		return time.Date(2026, 12, 24, 4, 30, 0, 0, time.UTC)
	})
	active, err := Active(policy, clock())
	if err != nil {
		t.Fatal(err)
	}
	if active {
		t.Fatal("Active() treated 23:30 in America/New_York as inside a midnight window")
	}
	clock = Clock(func() time.Time {
		return time.Date(2026, 12, 24, 5, 30, 0, 0, time.UTC)
	})
	active, err = Active(policy, clock())
	if err != nil {
		t.Fatal(err)
	}
	if !active {
		t.Fatal("Active() missed 00:30 in America/New_York")
	}
}

func TestRecordKindsAreDistinct(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	kinds := []string{ReceiptEntry, ReceiptException, ReceiptApproval, ReceiptMerge, ReceiptRelease, ReceiptExit}
	seen := map[string]struct{}{}
	for _, kind := range kinds {
		if kind == "" {
			t.Fatal("receipt kind is empty")
		}
		if _, dup := seen[kind]; dup {
			t.Fatalf("duplicate receipt kind %q", kind)
		}
		seen[kind] = struct{}{}
		got := Record(kind, now, "abc123", KindProduction)
		if got.Kind != kind || got.HeadSHA != "abc123" || got.PolicyKind != KindProduction || !got.At.Equal(now) {
			t.Fatalf("Record(%q) = %#v", kind, got)
		}
	}
	if len(seen) != 6 {
		t.Fatalf("got %d receipt kinds, want 6", len(seen))
	}
}

func TestKindConstantsMatchContract(t *testing.T) {
	t.Parallel()
	if KindFeature != "feature" || KindReleaseCandidate != "release-candidate" || KindProduction != "production" || KindIncident != "incident" {
		t.Fatalf("policy kind constants drifted: %q %q %q %q", KindFeature, KindReleaseCandidate, KindProduction, KindIncident)
	}
	if ReceiptEntry != "entry" || ReceiptException != "exception" || ReceiptApproval != "approval" || ReceiptMerge != "merge" || ReceiptRelease != "release" || ReceiptExit != "exit" {
		t.Fatalf("receipt kind constants drifted: %q %q %q %q %q %q", ReceiptEntry, ReceiptException, ReceiptApproval, ReceiptMerge, ReceiptRelease, ReceiptExit)
	}
}

func TestClockIsInjectable(t *testing.T) {
	t.Parallel()
	now := fixedNow()
	clock := Clock(func() time.Time { return now })
	if !clock().Equal(now) {
		t.Fatal("Clock did not return the injected time")
	}
	if err := Evaluate(testPolicy(), featureRequest(), clock()); err == nil {
		t.Fatal("Evaluate ignored the injected freeze instant")
	}
}

func testPolicy() Policy {
	return Policy{
		Timezone:     "UTC",
		Start:        "2026-12-20T00:00:00Z",
		End:          "2026-12-27T00:00:00Z",
		Branches:     []string{"main"},
		Environments: []string{"production"},
		Owner:        "release-managers",
		Kind:         KindProduction,
		Exceptions:   []string{"P0", "P1", "regression", "release-fix"},
	}
}

func featureRequest() CheckRequest {
	return CheckRequest{
		HeadSHA: "abc123",
		BaseSHA: "def456",
		Branch:  "main",
		Kind:    KindFeature,
	}
}

func completeException(now time.Time, headSHA string) Exception {
	return Exception{
		Class:        "P0",
		Severity:     "critical",
		Reference:    "INC-123",
		Scope:        []string{"cmd/sam-harness"},
		RollbackPlan: "revert merge abc123",
		Approvers:    []string{"sre-oncall"},
		ExpiresAt:    now.Add(2 * time.Hour),
		HeadSHA:      headSHA,
		ApprovedAt:   now.Add(-time.Hour),
	}
}

func fixedNow() time.Time {
	return time.Date(2026, 12, 24, 12, 0, 0, 0, time.UTC)
}
