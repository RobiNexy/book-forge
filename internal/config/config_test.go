package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadResolvesPathsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bookforge.yaml")
	data := []byte("apiVersion: bookforge/v1\nproject:\n  outline: manuscript/outline.md\n  initial_hooks: source/hooks.md\n  long_book_rules: prompts/long.md\nllm:\n  request_timeout: 2m\n  parameters:\n    reasoning_effort: high\n    custom_toggle: true\n  headers:\n    X-Api-Client: bookforge-test\ngeneration:\n  api_error_retries: 5\n  retry_initial_delay: 45s\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Project.Outline != filepath.Join(dir, "manuscript/outline.md") {
		t.Fatalf("outline path = %q", cfg.Project.Outline)
	}
	if cfg.Project.InitialHooks != filepath.Join(dir, "source/hooks.md") {
		t.Fatalf("initial hooks path = %q", cfg.Project.InitialHooks)
	}
	if cfg.Project.LongBookRules != filepath.Join(dir, "prompts/long.md") {
		t.Fatalf("long book rules path = %q", cfg.Project.LongBookRules)
	}
	if cfg.LLM.Model != "gpt-4.1" || cfg.LLM.APIFormat != "chat_completions" || cfg.LLM.RequestTimeout.String() != "2m0s" || !cfg.Generation.StorePromptAndResponse || cfg.Generation.Review != "interactive" || cfg.Generation.InvalidOutputRetries != 3 {
		t.Fatalf("defaults or timeout not applied: %#v", cfg.LLM)
	}
	if cfg.LLM.Parameters["reasoning_effort"] != "high" || cfg.LLM.Parameters["custom_toggle"] != true {
		t.Fatalf("model parameters not preserved: %#v", cfg.LLM.Parameters)
	}
	if cfg.LLM.Headers["X-Api-Client"] != "bookforge-test" {
		t.Fatalf("request headers not preserved: %#v", cfg.LLM.Headers)
	}
	if cfg.Generation.APIErrorRetries != 5 || cfg.Generation.RetryInitialDelay != 45*time.Second {
		t.Fatalf("retry settings not loaded: %#v", cfg.Generation)
	}
}

func TestValidateAcceptsResponsesAPIFormat(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.APIFormat = "responses"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("responses API format rejected: %v", err)
	}
}

func TestValidateRejectsUnsupportedAPIFormat(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.APIFormat = "completion"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected unsupported API format error")
	}
}

func TestLoadRejectsUnsupportedAPIVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bookforge.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: bookforge/v2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unsupported apiVersion error")
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bookforge.yaml")
	data := []byte("apiVersion: bookforge/v1\nllm:\n  model: gpt-4.1\n  temprature: 0.2\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadRequiresAPIVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bookforge.yaml")
	if err := os.WriteFile(path, []byte("project:\n  outline: outline.md\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected missing apiVersion error")
	}
}

func TestValidateRejectsInvalidEndpoint(t *testing.T) {
	cfg := Defaults()
	cfg.LLM.BaseURL = "openai.local/v1"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected absolute HTTP endpoint validation error")
	}
}

func TestValidateRejectsNegativeInvalidOutputRetries(t *testing.T) {
	cfg := Defaults()
	cfg.Generation.InvalidOutputRetries = -1
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected negative retry count validation error")
	}
}
