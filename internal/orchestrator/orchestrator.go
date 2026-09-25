// Package orchestrator generates opaque-text chapters until the model returns the end marker.
package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/RobiNexy/book-forge/internal/domain"
	"github.com/RobiNexy/book-forge/internal/llm"
	"github.com/RobiNexy/book-forge/internal/parser"
	"github.com/RobiNexy/book-forge/internal/prompt"
	"github.com/RobiNexy/book-forge/internal/store"
)

// Store is the minimal persistence contract required by generation and cascade rewrites.
type Store interface {
	Initialize(domain.ProjectState, store.InputHashes) error
	Reset(domain.ProjectState, store.InputHashes) error
	LoadManifest() (store.Manifest, error)
	VerifyInputs(store.InputHashes) error
	Recover() error
	LoadSnapshot(int) (domain.ProjectState, error)
	NextGeneration() (int, error)
	CommitGeneration(int, string, domain.ProjectState, store.AuditRecord, string, string, bool) error
	ChapterPath(int) string
	TruncateFrom(int) error
	Acquire() (func(), error)
	AppendLog(store.LogEntry) error
	SaveInvalidResponse(int, string, int, string, string) (string, error)
}

// Options contains run-specific generation and review settings.
type Options struct {
	Model                string
	Temperature          float64
	MaxTokens            int
	Parameters           map[string]any
	AuditFull            bool
	InvalidOutputRetries int
	Auto                 bool
	InitialHooks         string
	SystemPrompt         string
	LongBookRules        string
	Inputs               store.InputHashes
	StartedAt            func() time.Time
	Input                io.Reader
	Output               io.Writer
	Editor               string
	LockHeld             bool
}

// Orchestrator sends opaque inputs to the model and commits each valid split as a new state.
type Orchestrator struct {
	LLM     llm.Client
	Store   Store
	Outline string
	Options Options
}

// Generate resumes the artifact-derived sequence and continues until END_OF_BOOK is committed.
func (o *Orchestrator) Generate(ctx context.Context) error {
	if o.Store == nil {
		return fmt.Errorf("store is required")
	}
	if !o.Options.LockHeld {
		release, err := o.Store.Acquire()
		if err != nil {
			return err
		}
		defer release()
	}
	if err := o.initializeAndVerify(); err != nil {
		o.logEvent("generate", 0, "input_or_state_error", err, nil)
		return err
	}
	if err := o.Store.Recover(); err != nil {
		err = fmt.Errorf("recover interrupted generation: %w", err)
		o.logEvent("generate", 0, "recovery_error", err, nil)
		return err
	}
	next, err := o.Store.NextGeneration()
	if err != nil {
		o.logEvent("generate", 0, "sequence_error", err, nil)
		return err
	}
	autoReview := o.Options.Auto
	o.logEvent("generate", next, "started", nil, map[string]any{"review": map[bool]string{true: "auto", false: "interactive"}[autoReview]})
	state, err := o.Store.LoadSnapshot(next - 1)
	if err != nil {
		err = fmt.Errorf("load state_%03d: %w", next-1, err)
		o.logEvent("generate", next, "state_error", err, nil)
		return err
	}
	if state.Finished {
		fmt.Fprintln(o.output(), "The book is already complete.")
		o.logEvent("generate", next-1, "already_complete", nil, nil)
		return nil
	}
	for {
		var ended bool
		var acceptAll bool
		state, ended, acceptAll, err = o.generateOne(ctx, next, state, autoReview)
		if err != nil {
			o.logEvent("generate", next, "failed", err, nil)
			return err
		}
		if acceptAll {
			autoReview = true
		}
		if ended {
			fmt.Fprintf(o.output(), "Book completed at generation %03d.\n", next)
			o.logEvent("generate", next, "book_completed", nil, nil)
			return nil
		}
		next++
	}
}

func (o *Orchestrator) initializeAndVerify() error {
	initial := domain.ProjectState{CurrentHooks: o.Options.InitialHooks}
	if _, err := o.Store.LoadManifest(); errors.Is(err, os.ErrNotExist) {
		return o.Store.Initialize(initial, o.Options.Inputs)
	} else if err != nil {
		return err
	}
	if err := o.Store.VerifyInputs(o.Options.Inputs); err != nil {
		return err
	}
	return o.Store.Initialize(initial, o.Options.Inputs)
}

func (o *Orchestrator) generateOne(ctx context.Context, generation int, previous domain.ProjectState, autoReview bool) (domain.ProjectState, bool, bool, error) {
	if o.LLM == nil {
		return domain.ProjectState{}, false, false, fmt.Errorf("LLM client is required")
	}
	messages := prompt.Build(o.Options.SystemPrompt, o.Options.LongBookRules, o.Outline, previous.CurrentHooks)
	promptBytes, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return domain.ProjectState{}, false, false, err
	}
	started := time.Now().UTC()
	if o.Options.StartedAt != nil {
		started = o.Options.StartedAt().UTC()
	}
	requestAttempt, invalidCount := 0, 0
	runID := fmt.Sprintf("%d-%d", started.UnixNano(), os.Getpid())
	for {
		requestAttempt++
		writeGenerationRequest(o.output(), generation, requestAttempt, o.Options.InvalidOutputRetries, o.Options.Model)
		response, err := o.LLM.Complete(ctx, llm.Request{Model: o.Options.Model, Messages: messages, Temperature: o.Options.Temperature, MaxTokens: o.Options.MaxTokens, Parameters: o.Options.Parameters})
		if err != nil {
			failure := fmt.Errorf("generate chapter %d: %w", generation, err)
			o.logEvent("generate", generation, "api_error", failure, nil)
			return domain.ProjectState{}, false, false, failure
		}
		blocks, err := parser.Split(response.Content)
		if err != nil {
			invalidCount++
			responsePath, saveErr := o.Store.SaveInvalidResponse(generation, runID, requestAttempt, response.Content, err.Error())
			if saveErr != nil {
				return domain.ProjectState{}, false, false, fmt.Errorf("save invalid chapter %d response: %w", generation, saveErr)
			}
			writeInvalidResponse(o.output(), generation, requestAttempt, responsePath, response.Content)
			if invalidCount <= o.Options.InvalidOutputRetries {
				o.logEvent("retry", generation, "invalid_output", err, map[string]any{"attempt": requestAttempt, "response_file": responsePath})
				continue
			}
			failure := fmt.Errorf("chapter %d remained invalid after %d retries; raw response saved to %s: %w", generation, o.Options.InvalidOutputRetries, responsePath, err)
			o.logEvent("invalid_response", generation, "retry_exhausted", failure, map[string]any{"attempt": requestAttempt, "response_file": responsePath})
			return domain.ProjectState{}, false, false, failure
		}
		chapter := blocks.Chapter
		acceptAll := false
		if !autoReview {
			choice, edited, err := o.review(generation, chapter, blocks.Hooks, blocks.EndOfBook)
			if err != nil {
				o.logEvent("review", generation, "error", err, nil)
				return domain.ProjectState{}, false, false, err
			}
			if choice == "r" {
				invalidCount = 0
				o.logEvent("retry", generation, "reviewer_requested_regeneration", nil, map[string]any{"attempt": requestAttempt})
				continue
			}
			if choice != "a" && choice != "A" {
				err := fmt.Errorf("generation %d review aborted; no state committed", generation)
				o.logEvent("review", generation, "quit", err, nil)
				return domain.ProjectState{}, false, false, err
			}
			chapter = edited
			acceptAll = choice == "A"
			o.logEvent("review", generation, map[bool]string{true: "accept_all_remaining", false: "accepted"}[acceptAll], nil, nil)
		}
		next := domain.ProjectState{CurrentHooks: previous.CurrentHooks, Finished: blocks.EndOfBook}
		if !blocks.EndOfBook {
			next.CurrentHooks = blocks.Hooks
		}
		promptHash := sha256.Sum256(promptBytes)
		responseHash := sha256.Sum256([]byte(response.Content))
		hooksBefore := sha256.Sum256([]byte(previous.CurrentHooks))
		hooksAfter := sha256.Sum256([]byte(next.CurrentHooks))
		finished := time.Now().UTC()
		record := store.AuditRecord{
			Generation: generation, RunID: runID,
			StartedAt: started, FinishedAt: finished, Model: o.Options.Model,
			Temperature: o.Options.Temperature, MaxTokens: o.Options.MaxTokens, Parameters: o.Options.Parameters, Usage: response.Usage,
			Inputs: o.Options.Inputs, PromptHash: "sha256:" + hex.EncodeToString(promptHash[:]),
			ResponseHash: "sha256:" + hex.EncodeToString(responseHash[:]),
			HooksBefore:  "sha256:" + hex.EncodeToString(hooksBefore[:]),
			HooksAfter:   "sha256:" + hex.EncodeToString(hooksAfter[:]),
			EndOfBook:    blocks.EndOfBook,
			ReviewedBy:   map[bool]string{true: "auto", false: "human"}[autoReview],
		}
		if err := o.Store.CommitGeneration(generation, chapter, next, record, string(promptBytes), response.Content, o.Options.AuditFull); err != nil {
			failure := fmt.Errorf("commit generation %d: %w", generation, err)
			o.logEvent("generate", generation, "commit_error", failure, nil)
			return domain.ProjectState{}, false, false, failure
		}
		o.logEvent("generate", generation, "committed", nil, map[string]any{"chapter_path": record.ChapterPath, "end_of_book": blocks.EndOfBook})
		writeChapterSummary(o.output(), generation, o.Store.ChapterPath(generation), chapter, next.CurrentHooks, next.Finished)
		return next, blocks.EndOfBook, acceptAll, nil
	}
}

func (o *Orchestrator) review(generation int, chapter, hooks string, endOfBook bool) (string, string, error) {
	input := o.Options.Input
	if input == nil {
		input = os.Stdin
	}
	out := o.output()
	fmt.Fprintf(out, "Chapter %d: %d characters\n", generation, len(chapter))
	if endOfBook {
		fmt.Fprintln(out, "The model marked this as the final chapter.")
	} else {
		fmt.Fprintf(out, "Next hook state: %d characters\n", len(hooks))
	}
	fmt.Fprint(out, "[a]ccept, [A]ccept all remaining, [v]iew, [e]dit chapter, [r]egenerate, [q]uit: ")
	for {
		var choice string
		if _, err := fmt.Fscanln(input, &choice); err != nil {
			return "q", chapter, err
		}
		switch choice {
		case "a":
			return "a", chapter, nil
		case "A":
			return "A", chapter, nil
		case "v":
			o.logEvent("review", generation, "viewed", nil, nil)
			fmt.Fprintf(out, "--- Chapter ---\n%s\n", chapter)
			if !endOfBook {
				fmt.Fprintf(out, "--- Next hook state ---\n%s\n", hooks)
			}
			fmt.Fprint(out, "[a]ccept, [A]ccept all remaining, [v]iew, [e]dit chapter, [r]egenerate, [q]uit: ")
		case "e":
			edited, err := editChapter(o.Options.Editor, chapter)
			if err != nil {
				return "q", chapter, err
			}
			for {
				fmt.Fprint(out, "Accept edited chapter? [a]ccept, [e]dit again, [q]uit: ")
				var confirm string
				if _, err := fmt.Fscanln(input, &confirm); err != nil {
					return "q", chapter, err
				}
				if confirm == "a" {
					o.logEvent("review", generation, "edited_and_accepted", nil, nil)
					return "a", edited, nil
				}
				if confirm == "q" {
					return "q", chapter, nil
				}
				if confirm == "e" {
					edited, err = editChapter(o.Options.Editor, edited)
					if err != nil {
						return "q", chapter, err
					}
					continue
				}
				fmt.Fprintln(out, "Choose a, e, or q.")
			}
		case "r":
			return "r", chapter, nil
		default:
			return "q", chapter, nil
		}
	}
}

func (o *Orchestrator) logEvent(action string, generation int, result string, eventErr error, details map[string]any) {
	entry := store.LogEntry{Action: action, Generation: generation, Result: result, Details: details}
	if eventErr != nil {
		entry.Error = eventErr.Error()
	}
	if err := o.Store.AppendLog(entry); err != nil {
		fmt.Fprintf(o.output(), "Warning: unable to append operation log: %v\n", err)
	}
}

func editChapter(editor, chapter string) (string, error) {
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return "", fmt.Errorf("set $EDITOR to edit chapter text")
	}
	f, err := os.CreateTemp("", "bookforge-chapter-*.qmd")
	if err != nil {
		return "", err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.WriteString(chapter); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	command := strings.Fields(editor)
	if len(command) == 0 {
		return "", fmt.Errorf("editor command is empty")
	}
	cmd := exec.Command(command[0], append(command[1:], name)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("run editor: %w", err)
	}
	b, err := os.ReadFile(name)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", fmt.Errorf("edited chapter is empty")
	}
	return string(b), nil
}

func (o *Orchestrator) output() io.Writer {
	if o.Options.Output != nil {
		return o.Options.Output
	}
	return os.Stdout
}
