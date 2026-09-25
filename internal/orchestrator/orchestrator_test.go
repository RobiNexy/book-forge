package orchestrator

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RobiNexy/book-forge/internal/llm"
	"github.com/RobiNexy/book-forge/internal/store"
)

type fakeLLM struct {
	requests []llm.Request
}

func (f *fakeLLM) Complete(_ context.Context, req llm.Request) (llm.Response, error) {
	f.requests = append(f.requests, req)
	n := len(f.requests)
	if n == 3 {
		return llm.Response{Content: "final body\n<<<END_OF_BOOK>>>\n"}, nil
	}
	return llm.Response{Content: fmt.Sprintf("body %d\n<<<BOOKFORGE_HOOKS>>>\n# state %d\n- arbitrary format", n, n)}, nil
}

func TestGenerateRunsUntilEndMarkerWithOpaqueState(t *testing.T) {
	state := store.New(t.TempDir())
	client := &fakeLLM{}
	outline := "# Entire outline\n## This is not interpreted as a chapter\n"
	initialHooks := "not JSON, not a recognized hook format\n{{ arbitrary text }}"
	inputs := store.InputHashes{Outline: "outline", InitialHooks: "hooks", SystemPrompt: "prompt", LongBookRules: "long"}
	o := &Orchestrator{
		LLM: client, Store: state, Outline: outline,
		Options: Options{Model: "test-model", MaxTokens: 100, Auto: true, SystemPrompt: "writer", LongBookRules: "long rules", Inputs: inputs, InitialHooks: initialHooks},
	}
	if err := o.Generate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 3 {
		t.Fatalf("LLM calls = %d, want to stop at end marker after 3", len(client.requests))
	}
	for i, req := range client.requests {
		if len(req.Messages) != 2 || req.Messages[1].Role != "user" {
			t.Fatalf("round %d messages = %#v", i+1, req.Messages)
		}
		if !contains(req.Messages[1].Content, outline) {
			t.Fatalf("round %d outline was not passed verbatim: %q", i+1, req.Messages[1].Content)
		}
		wantHooks := initialHooks
		if i > 0 {
			wantHooks = fmt.Sprintf("\n# state %d\n- arbitrary format", i)
		}
		if !contains(req.Messages[1].Content, wantHooks) || contains(req.Messages[1].Content, "generation number") {
			t.Fatalf("round %d user prompt = %q", i+1, req.Messages[1].Content)
		}
		if !contains(req.Messages[0].Content, "writer") || !contains(req.Messages[0].Content, "long rules") {
			t.Fatalf("round %d system sections missing: %q", i+1, req.Messages[0].Content)
		}
	}
	latest, err := state.LatestGeneration()
	if err != nil || latest != 3 {
		t.Fatalf("latest=%d err=%v", latest, err)
	}
	last, err := state.LoadSnapshot(3)
	if err != nil || !last.Finished || last.CurrentHooks != "\n# state 2\n- arbitrary format" {
		t.Fatalf("last state=%#v err=%v", last, err)
	}
	for n := 1; n <= 3; n++ {
		if !state.HasChapter(n) {
			t.Errorf("generation %d chapter missing", n)
		}
	}
	if err := o.Generate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 3 {
		t.Fatalf("finished project called LLM again: %d calls", len(client.requests))
	}
}

type scriptedLLM struct {
	responses []string
	calls     int
}

func (f *scriptedLLM) Complete(_ context.Context, _ llm.Request) (llm.Response, error) {
	if f.calls >= len(f.responses) {
		return llm.Response{}, fmt.Errorf("unexpected extra model request")
	}
	content := f.responses[f.calls]
	f.calls++
	return llm.Response{Content: content}, nil
}

func TestInvalidOutputRetriesAreSavedWhenFullAuditIsDisabled(t *testing.T) {
	root := t.TempDir()
	client := &scriptedLLM{responses: []string{
		"first malformed response",
		"<<<BOOKFORGE_HOOKS>>>both markers?<<<END_OF_BOOK>>>",
		"last chapter\n<<<END_OF_BOOK>>>\n",
	}}
	state := store.New(root)
	var output bytes.Buffer
	o := &Orchestrator{
		LLM: client, Store: state, Outline: "opaque outline",
		Options: Options{Model: "test", Auto: true, AuditFull: false, InvalidOutputRetries: 2,
			InitialHooks: "opaque hooks", SystemPrompt: "style", LongBookRules: "rules",
			Inputs: store.InputHashes{Outline: "o", InitialHooks: "h", SystemPrompt: "s", LongBookRules: "l"}, Output: &output},
	}
	if err := o.Generate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if client.calls != 3 || !state.HasChapter(1) {
		t.Fatalf("calls=%d chapter=%v", client.calls, state.HasChapter(1))
	}
	invalidDir := filepath.Join(root, ".bookforge", "invalid-responses")
	entries, err := os.ReadDir(invalidDir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("invalid response files=%v err=%v", entries, err)
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(invalidDir, entry.Name()))
		if err != nil || !strings.Contains(string(data), "Raw response") {
			t.Fatalf("invalid response not preserved in %s: %v", entry.Name(), err)
		}
	}
	if !strings.Contains(output.String(), "first malformed response") || !strings.Contains(output.String(), "both markers") {
		t.Fatalf("invalid response text not displayed: %q", output.String())
	}
	entriesLog, err := state.ReadLogEntries()
	if err != nil {
		t.Fatal(err)
	}
	retries := 0
	for _, entry := range entriesLog {
		if entry.Action == "retry" {
			retries++
		}
	}
	if retries != 2 {
		t.Fatalf("logged retries=%d, want 2", retries)
	}
	audit, err := os.ReadFile(filepath.Join(root, ".bookforge", "audit", "run_001.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(audit), "full_prompt") || strings.Contains(string(audit), "raw_response") {
		t.Fatal("successful full prompt/response should be omitted when full audit is disabled")
	}
}

func TestRetryExhaustionDoesNotCommitGeneration(t *testing.T) {
	root := t.TempDir()
	client := &scriptedLLM{responses: []string{"bad one", "bad two"}}
	state := store.New(root)
	o := &Orchestrator{
		LLM: client, Store: state, Outline: "outline",
		Options: Options{Model: "test", Auto: true, InvalidOutputRetries: 1, InitialHooks: "hooks",
			SystemPrompt: "style", LongBookRules: "rules", Inputs: store.InputHashes{Outline: "o", InitialHooks: "h", SystemPrompt: "s", LongBookRules: "l"}},
	}
	if err := o.Generate(context.Background()); err == nil {
		t.Fatal("expected retry exhaustion error")
	}
	if client.calls != 2 || state.HasChapter(1) {
		t.Fatalf("calls=%d chapter=%v; invalid output must not commit", client.calls, state.HasChapter(1))
	}
	if latest, err := state.LatestGeneration(); err != nil || latest != 0 {
		t.Fatalf("latest=%d err=%v", latest, err)
	}
	entries, err := os.ReadDir(filepath.Join(root, ".bookforge", "invalid-responses"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("preserved responses=%v err=%v", entries, err)
	}
}

func TestAcceptAllAppliesOnlyToCurrentRun(t *testing.T) {
	root := t.TempDir()
	client := &fakeLLM{}
	var output bytes.Buffer
	inputs := store.InputHashes{Outline: "o", InitialHooks: "h", SystemPrompt: "s", LongBookRules: "l"}
	o := &Orchestrator{
		LLM: client, Store: store.New(root), Outline: "outline",
		Options: Options{Model: "test", Input: strings.NewReader("A\n"), Output: &output,
			InitialHooks: "hooks", SystemPrompt: "style", LongBookRules: "rules", Inputs: inputs},
	}
	if err := o.Generate(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 3 || strings.Count(output.String(), "[a]ccept") != 1 {
		t.Fatalf("A should accept the rest of this run, calls=%d output=%q", len(client.requests), output.String())
	}
	for generation, want := range map[int]string{1: "human", 2: "auto", 3: "auto"} {
		data, err := os.ReadFile(filepath.Join(root, ".bookforge", "audit", fmt.Sprintf("run_%03d.json", generation)))
		if err != nil || !strings.Contains(string(data), `"reviewed_by": "`+want+`"`) {
			t.Errorf("generation %d reviewed_by want %s: %v", generation, want, err)
		}
	}
}

func contains(value, substring string) bool {
	return strings.Contains(value, substring)
}
