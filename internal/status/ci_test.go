package status

import "testing"

func TestProveCIRequiresProviderReadbackForHead(t *testing.T) {
	t.Parallel()
	head := "abc123"
	ok, evidence := ProveCI(head, []string{"static", "test"}, []ProviderCheck{
		{Name: "static", SHA: head, Conclusion: "success"},
		{Name: "test", SHA: head, Conclusion: "success"},
	})
	if !ok {
		t.Fatalf("ProveCI() = false (%s), want provider success", evidence)
	}
	if evidence == "" {
		t.Fatal("ProveCI() left evidence empty")
	}
}

func TestProveCIDoesNotInferFromOtherSHAOrLocalReceipts(t *testing.T) {
	t.Parallel()
	head := "abc123"
	ok, _ := ProveCI(head, []string{"static"}, []ProviderCheck{
		{Name: "static", SHA: "other", Conclusion: "success"},
	})
	if ok {
		t.Fatal("ProveCI() accepted a check for a different SHA")
	}
	ok, reason := ProveCI(head, []string{"static", "test"}, nil)
	if ok {
		t.Fatal("ProveCI() treated missing provider checks as passing")
	}
	if reason == "" {
		t.Fatal("ProveCI() omitted a reason")
	}
}

func TestParseGitHubAndGitLabCheckPayloads(t *testing.T) {
	t.Parallel()
	gh, err := ParseGitHubCheckRuns("deadbeef", []byte(`{"check_runs":[{"name":"static","head_sha":"deadbeef","conclusion":"success","html_url":"https://example.test/static"}]}`))
	if err != nil || len(gh) != 1 || gh[0].Name != "static" || !checkPassed(gh[0].Conclusion) {
		t.Fatalf("ParseGitHubCheckRuns() = %#v, err=%v", gh, err)
	}
	gl, err := ParseGitLabCommitStatuses("deadbeef", []byte(`[{"name":"test","sha":"deadbeef","status":"success"}]`))
	if err != nil || len(gl) != 1 || gl[0].Name != "test" {
		t.Fatalf("ParseGitLabCommitStatuses() = %#v, err=%v", gl, err)
	}
}
