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
