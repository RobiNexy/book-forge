package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/RobiNexy/book-forge/internal/domain"
	"github.com/RobiNexy/book-forge/internal/llm"
	"github.com/RobiNexy/book-forge/internal/outline"
	"github.com/RobiNexy/book-forge/internal/parser"
	"github.com/RobiNexy/book-forge/internal/prompt"
	"github.com/RobiNexy/book-forge/internal/store"
	"os"
	"time"
)

type Options struct {
	Model        string
	Temperature  float64
	MaxTokens    int
	AuditFull    bool
	Auto         bool
	DryRun       bool
	InitialHooks string
}
type Orchestrator struct {
	LLM     llm.Client
	Store   store.Store
	Outline outline.Outline
	Options Options
}

func (o *Orchestrator) Generate(ctx context.Context, from, to int) error {
	release, err := o.Store.Acquire()
	if err != nil {
		return err
	}
	defer release()
	pool := domain.HookPool{Version: "v1"}
	if from <= 0 {
		if latest, e := o.Store.LatestSnapshotN(); e == nil {
			from = latest + 1
			pool, err = o.Store.LoadSnapshot(latest)
		} else {
			from = 1
			name := o.Options.InitialHooks
			if name == "" {
				name = "initial-hooks.md"
			}
			hs, e := o.Store.LoadInitialHooks(name)
			if e != nil {
				return e
			}
			pool = domain.HookPool{N: 0, Version: "v1", Hooks: hs}
			if err := o.Store.SaveSnapshot(pool); err != nil {
				return err
			}
		}
	} else if from > 1 {
		pool, err = o.Store.LoadSnapshot(from - 1)
	} else {
		name := o.Options.InitialHooks
		if name == "" {
			name = "initial-hooks.md"
		}
		hs, e := o.Store.LoadInitialHooks(name)
		if e != nil {
			return e
		}
		pool = domain.HookPool{N: 0, Version: "v1", Hooks: hs}
		if err := o.Store.SaveSnapshot(pool); err != nil {
			return err
		}
	}
	if err != nil {
		return err
	}
	if to == 0 || to > len(o.Outline.Chapters) {
		to = len(o.Outline.Chapters)
	}
	for n := from; n <= to; n++ {
		if err := o.chapter(ctx, n, pool); err != nil {
			return err
		}
		pool, _ = o.Store.LoadSnapshot(n)
	}
	return nil
}
func (o *Orchestrator) chapter(ctx context.Context, n int, pool domain.HookPool) error {
	messages, err := prompt.Build(o.Outline.Markdown, pool, n)
	if err != nil {
		return err
	}
	if o.Options.DryRun {
		fmt.Printf("chapter %d: %s\n", n, o.Outline.Chapters[n-1].Title)
		return nil
	}
	b, _ := json.Marshal(messages)
	r, err := o.LLM.Complete(ctx, llm.Request{Model: o.Options.Model, Messages: messages, Temperature: o.Options.Temperature, MaxTokens: o.Options.MaxTokens})
	if err != nil {
		return err
	}
	parsed, err := parser.Parse(r.Content, n)
	if err != nil {
		return err
	}
	next, err := domain.Apply(pool, parsed.Ops)
	if err != nil {
		return err
	}
	if !o.Options.Auto {
		fmt.Fprintf(os.Stderr, "Chapter %d generated (%d characters). [a]ccept or [q]uit: ", n, len(parsed.Chapter))
		var choice string
		if _, err := fmt.Fscanln(os.Stdin, &choice); err != nil || (choice != "a" && choice != "accept") {
			return fmt.Errorf("chapter %d not committed", n)
		}
	}
	if err = o.Store.SaveChapter(n, parsed.Chapter); err != nil {
		return err
	}
	if err = o.Store.SaveSnapshot(next); err != nil {
		return err
	}
	h := sha256.Sum256(b)
	rh := sha256.Sum256([]byte(r.Content))
	return o.Store.SaveAudit(n, store.AuditRecord{N: n, StartedAt: time.Now(), FinishedAt: time.Now(), Model: o.Options.Model, PromptHash: "sha256:" + hex.EncodeToString(h[:]), ResponseHash: "sha256:" + hex.EncodeToString(rh[:]), Usage: r.Usage, Ops: parsed.Ops, ReviewedBy: func() string {
		if o.Options.Auto {
			return "auto"
		}
		return "human"
	}()}, b, r.Content, o.Options.AuditFull)
}
