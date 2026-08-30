package bootstrap

import (
	"strings"
	"testing"
)

func TestMergeQueueDispatcherWorkflowIsCredentialFreeAndForwardsDispatch(t *testing.T) {
	t.Parallel()
	text := MergeQueueDispatcherWorkflow()
	if !dispatcherWorkflowComplete(text) {
		t.Fatalf("dispatcher workflow incomplete:\n%s", text)
	}
	if err := JobTextsCredentialFree(map[string]string{"merge_queue_dispatch": text}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text, "pull_request_target:") {
		t.Fatal("dispatcher used pull_request_target")
	}
}

func TestCreatePlanRequiresDispatcherJobTextOnGitHub(t *testing.T) {
	t.Parallel()
	desired := validDesired(providerGitHub)
	plan, err := CreatePlan(providerGitHub, "fingerprint", desired)
	if err != nil {
		t.Fatal(err)
	}
	text := plan.Desired.JobTexts["merge_queue_dispatch"]
	if !dispatcherWorkflowComplete(text) {
		t.Fatalf("desired dispatcher = %q", text)
	}
	incomplete := cloneState(plan.Desired)
	delete(incomplete.JobTexts, "merge_queue_dispatch")
	transport := &fakeTransport{state: incomplete}
	result, err := Apply(plan, plan.ID, transport)
	if result.Ready {
		t.Fatal("bootstrap reported ready without merge-queue dispatcher workflow")
	}
	if err == nil {
		t.Fatal("Apply() error = nil, want dispatcher mismatch")
	}
}
