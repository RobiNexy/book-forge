package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RobiNexy/book-forge/internal/llm"
)

func TestCompleteSendsConfiguredRequestAndParsesUsage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request path=%q authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Api-Client") != "bookforge-test" {
			t.Errorf("X-Api-Client = %q", r.Header.Get("X-Api-Client"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["max_completion_tokens"] != float64(99) {
			t.Errorf("max_completion_tokens = %v", body["max_completion_tokens"])
		}
		if body["reasoning_effort"] != "high" {
			t.Errorf("reasoning_effort = %v", body["reasoning_effort"])
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"done"}}],"usage":{"prompt_tokens":10,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":6}}}`))
	}))
	defer server.Close()

	client := &Client{APIKey: "secret", BaseURL: server.URL + "/v1", Headers: map[string]string{"X-Api-Client": "bookforge-test"}, HTTP: server.Client()}
	got, err := client.Complete(context.Background(), llm.Request{Model: "gpt-4.1", MaxTokens: 99, Parameters: map[string]any{"reasoning_effort": "high"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "done" || got.Usage.PromptTokens != 10 || got.Usage.CompletionTokens != 4 || got.Usage.CachedTokens != 6 {
		t.Fatalf("response = %#v", got)
	}
}

func TestCompleteUsesResponsesAPIAndParsesOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("request path=%q authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["max_output_tokens"] != float64(99) {
			t.Errorf("max_output_tokens = %v", body["max_output_tokens"])
		}
		if _, ok := body["messages"]; ok {
			t.Error("Responses request unexpectedly included messages")
		}
		input, ok := body["input"].([]any)
		if !ok || len(input) != 1 {
			t.Errorf("input = %#v", body["input"])
		} else if message, ok := input[0].(map[string]any); !ok || message["role"] != "user" || message["content"] != "hello" {
			t.Errorf("input message = %#v", input[0])
		}
		if body["reasoning_effort"] != "high" {
			t.Errorf("reasoning_effort = %v", body["reasoning_effort"])
		}
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"done"}]}],"usage":{"input_tokens":10,"output_tokens":4,"input_tokens_details":{"cached_tokens":6}}}`))
	}))
	defer server.Close()

	client := &Client{APIKey: "secret", BaseURL: server.URL + "/v1", APIFormat: "responses", HTTP: server.Client()}
	got, err := client.Complete(context.Background(), llm.Request{
		Model: "gpt-4.1", Messages: []llm.Message{{Role: "user", Content: "hello"}}, MaxTokens: 99,
		Parameters: map[string]any{"reasoning_effort": "high"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "done" || got.Usage.PromptTokens != 10 || got.Usage.CompletionTokens != 4 || got.Usage.CachedTokens != 6 {
		t.Fatalf("response = %#v", got)
	}
}

func TestCompleteReturnsProviderErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"unsupported reasoning_effort"}}`))
	}))
	defer server.Close()
	client := &Client{APIKey: "secret", BaseURL: server.URL, HTTP: server.Client()}
	_, err := client.Complete(context.Background(), llm.Request{Model: "test", Parameters: map[string]any{"reasoning_effort": "high"}})
	if err == nil || !strings.Contains(err.Error(), "unsupported reasoning_effort") {
		t.Fatalf("provider error = %v", err)
	}
}
