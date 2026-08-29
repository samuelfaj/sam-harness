package freeze

import (
	"fmt"
	"strings"
	"time"
)

const (
	KindFeature          = "feature"
	KindReleaseCandidate = "release-candidate"
	KindProduction       = "production"
	KindIncident         = "incident"
)

const (
	ReceiptEntry     = "entry"
	ReceiptException = "exception"
	ReceiptApproval  = "approval"
	ReceiptMerge     = "merge"
	ReceiptRelease   = "release"
	ReceiptExit      = "exit"
)

const (
	classReleaseFix     = "release-fix"
	requiredCheckName   = "sam-harness/freeze"
	errMissingEvidence  = "freeze: missing evidence"
	errStaleApproval    = "freeze: stale approval"
	errStaleHead        = "freeze: stale head"
	errExpiredWindow    = "freeze: expired window"
	errOrdinaryFeature  = "freeze: ordinary feature changes are blocked"
	errScheduledRelease = "freeze: scheduled release requires class release-fix"
)

type Policy struct {
	Timezone     string   `json:"timezone"`
	Start        string   `json:"start"`
	End          string   `json:"end"`
	Cadence      string   `json:"cadence,omitempty"`
	Branches     []string `json:"branches"`
	Environments []string `json:"environments"`
	Owner        string   `json:"owner"`
	Kind         string   `json:"kind"`
	Exceptions   []string `json:"exceptions"`
}

type Exception struct {
	Class        string    `json:"class"`
	Severity     string    `json:"severity"`
	Reference    string    `json:"reference"`
	Scope        []string  `json:"scope"`
	RollbackPlan string    `json:"rollback_plan"`
	Approvers    []string  `json:"approvers"`
	ExpiresAt    time.Time `json:"expires_at"`
	HeadSHA      string    `json:"head_sha"`
	ApprovedAt   time.Time `json:"approved_at"`
}

type CheckRequest struct {
	HeadSHA            string
	BaseSHA            string
	Branch             string
	Kind               string
	Exception          *Exception
	WorkflowCanDisable bool
	ScheduledRelease   bool
}

type Receipt struct {
	Kind       string    `json:"kind"`
	At         time.Time `json:"at"`
	HeadSHA    string    `json:"head_sha,omitempty"`
	PolicyKind string    `json:"policy_kind"`
}

type Clock func() time.Time

func Active(policy Policy, now time.Time) (bool, error) {
	now = resolveNow(now)
	_, start, end, err := parseWindow(policy)
	if err != nil {
		return false, err
	}
	return !now.Before(start) && !now.After(end), nil
}

func Evaluate(policy Policy, req CheckRequest, now time.Time) error {
	now = resolveNow(now)
	active, err := Active(policy, now)
	if err != nil {
		return err
	}
	if !active {
		return nil
	}
	if len(policy.Branches) > 0 && strings.TrimSpace(req.Branch) != "" && !contains(policy.Branches, req.Branch) {
		return fmt.Errorf("freeze: branch %q is outside the freeze policy", req.Branch)
	}
	if req.ScheduledRelease {
		return exceptionAllowed(policy, req, now, classReleaseFix)
	}
	if req.Exception != nil {
		return exceptionAllowed(policy, req, now, "")
	}
	return fmt.Errorf("%s", errOrdinaryFeature)
}

func Record(kind string, now time.Time, headSHA, policyKind string) Receipt {
	return Receipt{
		Kind:       kind,
		At:         now,
		HeadSHA:    headSHA,
		PolicyKind: policyKind,
	}
}

func RequiredCheckName() string {
	return requiredCheckName
}

func parseWindow(policy Policy) (*time.Location, time.Time, time.Time, error) {
	if policy.Timezone == "" {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("freeze: timezone is required")
	}
	loc, err := time.LoadLocation(policy.Timezone)
	if err != nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("freeze: invalid timezone %q: %w", policy.Timezone, err)
	}
	if policy.Start == "" || policy.End == "" {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("freeze: start and end are required")
	}
	start, err := time.Parse(time.RFC3339, policy.Start)
	if err != nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("freeze: invalid start: %w", err)
	}
	end, err := time.Parse(time.RFC3339, policy.End)
	if err != nil {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("freeze: invalid end: %w", err)
	}
	if !start.Before(end) {
		return nil, time.Time{}, time.Time{}, fmt.Errorf("freeze: invalid window: start is not before end")
	}
	return loc, start, end, nil
}

func exceptionAllowed(policy Policy, req CheckRequest, now time.Time, requireClass string) error {
	if req.Exception == nil {
		if requireClass == classReleaseFix {
			return fmt.Errorf("%s", errScheduledRelease)
		}
		return fmt.Errorf("%s", errMissingEvidence)
	}
	ex := *req.Exception
	if ex.Class == "" || ex.Severity == "" || ex.Reference == "" || ex.RollbackPlan == "" || ex.HeadSHA == "" || !hasText(ex.Scope) || !hasText(ex.Approvers) {
		return fmt.Errorf("%s", errMissingEvidence)
	}
	if requireClass != "" && ex.Class != requireClass {
		return fmt.Errorf("%s", errScheduledRelease)
	}
	if !contains(policy.Exceptions, ex.Class) {
		return fmt.Errorf("freeze: exception class %q is not configured", ex.Class)
	}
	if ex.ApprovedAt.IsZero() {
		return fmt.Errorf("%s", errMissingEvidence)
	}
	if ex.ApprovedAt.After(now) {
		return fmt.Errorf("%s", errStaleApproval)
	}
	if ex.ExpiresAt.IsZero() || !ex.ExpiresAt.After(now) {
		return fmt.Errorf("%s", errExpiredWindow)
	}
	if ex.HeadSHA != req.HeadSHA {
		return fmt.Errorf("%s", errStaleHead)
	}
	return nil
}

func hasText(values []string) bool {
	for _, value := range values {
		if value != "" {
			return true
		}
	}
	return false
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// Zero now falls back to time.Now; callers must inject a fixed time in tests.
func resolveNow(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now
}
