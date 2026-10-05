// Package jev calls the TypeSafe System One (Jev) model through OpenRouter.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	EnvKey      = "SAM_HARNESS_OPENROUTER_KEY"
	EnvModel    = "SAM_HARNESS_OPENROUTER_MODEL"
	EnvJevModel = "SAM_HARNESS_OPENROUTER_JEV_MODEL"
	// EnvBaseURL overrides DefaultBaseURL for tests and proxies.
	EnvBaseURL = "SAM_HARNESS_OPENROUTER_BASE_URL"

	DefaultModel   = "~typesafe/jev-latest"
	DefaultBaseURL = "https://openrouter.ai/api"

	maxResponseBytes = 1 << 20
	retryDelay       = time.Second
)

// Client is a minimal System One client.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

// FromEnvironment builds a client from the process environment. It returns
// false when no API key is configured.
func FromEnvironment() (*Client, bool) {
	key := strings.TrimSpace(os.Getenv(EnvKey))
	if key == "" {
		return nil, false
	}
	model := strings.TrimSpace(os.Getenv(EnvJevModel))
	if model == "" {
		model = DefaultModel
	}
	base := strings.TrimSpace(os.Getenv(EnvBaseURL))
	if base == "" {
		base = DefaultBaseURL
	}
	return &Client{BaseURL: base, APIKey: key, Model: model, HTTP: &http.Client{Timeout: 20 * time.Second}}, true
}

type noulQuestion struct {
	Type         string `json:"type"`
	Instructions string `json:"instructions"`
}

type request struct {
	Model     string                  `json:"model"`
	State     any                     `json:"state"`
	Questions map[string]noulQuestion `json:"questions"`
}

type response struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Noul *float64 `json:"noul"`
	} `json:"answers"`
}

// Nouls asks every yes/no question in one request and returns each
// probability in [0,1] keyed by question id.
func (c *Client) Nouls(ctx context.Context, state any, questions map[string]string) (map[string]float64, string, error) {
	if len(questions) == 0 {
		return nil, "", errors.New("jev: no questions")
	}
	payload := request{Model: c.Model, State: state, Questions: make(map[string]noulQuestion, len(questions))}
	for id, instruction := range questions {
		payload.Questions[id] = noulQuestion{Type: "noul", Instructions: instruction}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", fmt.Errorf("jev: encode request: %w", err)
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/v1/systemone"
	var status int
	var data []byte
	for attempt := 0; attempt < 2; attempt++ {
		status, data, err = c.post(ctx, client, url, body)
		if err != nil {
			return nil, "", err
		}
		if (status != http.StatusTooManyRequests && status != 529) || attempt == 1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	if status < 200 || status > 299 {
		return nil, "", fmt.Errorf("jev: status %d: %s", status, strings.TrimSpace(redact(string(data), c.APIKey)))
	}
	var decoded response
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, "", fmt.Errorf("jev: decode response: %w", err)
	}
	answers := make(map[string]float64, len(questions))
	for id := range questions {
		answer, ok := decoded.Answers[id]
		if !ok || answer.Noul == nil {
			return nil, "", fmt.Errorf("jev: missing answer for %q", id)
		}
		value := *answer.Noul
		if !(value >= 0 && value <= 1) {
			return nil, "", fmt.Errorf("jev: answer for %q out of range: %v", id, value)
		}
		answers[id] = value
	}
	return answers, decoded.Model, nil
}

func (c *Client) post(ctx context.Context, client *http.Client, url string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, fmt.Errorf("jev: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("jev: request failed: %s", redact(err.Error(), c.APIKey))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return 0, nil, fmt.Errorf("jev: read response: %w", err)
	}
	return resp.StatusCode, data, nil
}

func redact(text, key string) string {
	if key == "" {
		return text
	}
	return strings.ReplaceAll(text, key, "[redacted]")
}
