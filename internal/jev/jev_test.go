package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testClient(url string) *Client {
	return &Client{BaseURL: url, APIKey: "secret-key", Model: DefaultModel, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

func TestNoulsReturnsProbabilitiesAndSendsAuth(t *testing.T) {
	var auth, path string
	var sent request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&sent)
		_, _ = w.Write([]byte(`{"model":"typesafe/jev-1","answers":{"a":{"type":"noul","noul":0.02},"b":{"type":"noul","noul":0.9}}}`))
	}))
	defer server.Close()
	answers, model, err := testClient(server.URL).Nouls(context.Background(), map[string]string{"k": "v"}, map[string]string{"a": "qa?", "b": "qb?"})
	if err != nil {
		t.Fatal(err)
	}
	if answers["a"] != 0.02 || answers["b"] != 0.9 || model != "typesafe/jev-1" {
		t.Fatalf("unexpected result %v %q", answers, model)
	}
	if auth != "Bearer secret-key" || path != "/v1/systemone" {
		t.Fatalf("auth=%q path=%q", auth, path)
	}
	if sent.Model != DefaultModel || sent.Questions["a"].Type != "noul" || sent.Questions["a"].Instructions != "qa?" {
		t.Fatalf("unexpected request %#v", sent)
	}
}

func TestNoulsRejectsMissingAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","answers":{"a":{"type":"noul","noul":0.5}}}`))
	}))
	defer server.Close()
	if _, _, err := testClient(server.URL).Nouls(context.Background(), "s", map[string]string{"a": "x", "b": "y"}); err == nil || !strings.Contains(err.Error(), "missing answer") {
		t.Fatalf("expected missing answer error, got %v", err)
	}
}

func TestNoulsRejectsOutOfRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"model":"m","answers":{"a":{"type":"noul","noul":1.5}}}`))
	}))
	defer server.Close()
	if _, _, err := testClient(server.URL).Nouls(context.Background(), "s", map[string]string{"a": "x"}); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected range error, got %v", err)
	}
}

func TestNoulsServerErrorOmitsKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom secret-key"}`))
	}))
	defer server.Close()
	_, _, err := testClient(server.URL).Nouls(context.Background(), "s", map[string]string{"a": "x"})
	if err == nil || !strings.Contains(err.Error(), "500") || strings.Contains(err.Error(), "secret-key") {
		t.Fatalf("expected key-free 500 error, got %v", err)
	}
}

func TestNoulsRetriesRateLimitOnce(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"model":"m","answers":{"a":{"type":"noul","noul":0.5}}}`))
	}))
	defer server.Close()
	if _, _, err := testClient(server.URL).Nouls(context.Background(), "s", map[string]string{"a": "x"}); err != nil || calls.Load() != 2 {
		t.Fatalf("err=%v calls=%d", err, calls.Load())
	}
}

func TestFromEnvironment(t *testing.T) {
	t.Setenv(EnvKey, "")
	if client, ok := FromEnvironment(); ok || client != nil {
		t.Fatal("expected no client without key")
	}
	t.Setenv(EnvKey, "k")
	t.Setenv(EnvJevModel, "")
	t.Setenv(EnvBaseURL, "")
	client, ok := FromEnvironment()
	if !ok || client.Model != DefaultModel || client.BaseURL != DefaultBaseURL || client.HTTP.Timeout != 20*time.Second {
		t.Fatalf("unexpected defaults %#v", client)
	}
	t.Setenv(EnvJevModel, "custom")
	if client, _ := FromEnvironment(); client.Model != "custom" {
		t.Fatalf("model override ignored: %q", client.Model)
	}
}
