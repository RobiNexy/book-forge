package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/RobiNexy/book-forge/internal/config"
	"github.com/RobiNexy/book-forge/internal/llm/openai"
	"github.com/RobiNexy/book-forge/internal/orchestrator"
	"github.com/RobiNexy/book-forge/internal/render"
	"github.com/RobiNexy/book-forge/internal/store"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func Init(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, ".bookforge", "state"), 0755); err != nil {
		return err
	}
	for _, p := range []string{"chapters", filepath.Join(".bookforge", "audit")} {
		if err := os.MkdirAll(filepath.Join(dir, p), 0755); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".bookforge", "config.yaml")); os.IsNotExist(err) {
		if err := os.WriteFile(filepath.Join(dir, ".bookforge", "config.yaml"), []byte("llm:\n  provider: openai\n  model: gpt-4o-2024-11-20\n  api_key_env: OPENAI_API_KEY\ngeneration:\n  audit_full: true\npaths:\n  outline: outline.md\n  initial_hooks: initial-hooks.md\n"), 0644); err != nil {
			return err
		}
	}
	gitignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignore); os.IsNotExist(err) {
		if err := os.WriteFile(gitignore, []byte(".bookforge/audit/\n.bookforge/lock\n_book/\n"), 0644); err != nil {
			return err
		}
	}
	return nil
}
func Generate(ctx context.Context, project string, cfg config.Config, from, to int, dry bool) error {
	s := store.New(project)
	o, err := s.LoadOutline(cfg.Outline)
	if err != nil {
		return err
	}
	c := &openai.Client{APIKey: os.Getenv(cfg.APIKeyEnv), BaseURL: cfg.BaseURL}
	return (&orchestrator.Orchestrator{LLM: c, Store: s, Outline: o, Options: orchestrator.Options{Model: cfg.Model, Temperature: cfg.Temperature, MaxTokens: cfg.MaxTokens, AuditFull: cfg.AuditFull, Auto: cfg.Auto, DryRun: dry, InitialHooks: cfg.InitialHooks}}).Generate(ctx, from, to)
}
func Status(project string) error {
	s := store.New(project)
	n, err := s.LatestSnapshotN()
	if err != nil {
		n = 0
	}
	out := map[string]any{"chapters_done": n, "hooks": 0}
	if p, e := s.LoadSnapshot(n); e == nil {
		out["hooks"] = len(p.Hooks)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
	return nil
}
func Hooks(project, action string, args []string) error {
	s := store.New(project)
	if action == "show" {
		n := 0
		if len(args) > 0 {
			n, _ = strconv.Atoi(args[0])
		} else {
			n, _ = s.LatestSnapshotN()
		}
		p, e := s.LoadSnapshot(n)
		if e != nil {
			return e
		}
		b, _ := json.MarshalIndent(p, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	if action == "diff" && len(args) == 2 {
		a, _ := strconv.Atoi(args[0])
		b, _ := strconv.Atoi(args[1])
		x, e := s.LoadSnapshot(a)
		if e != nil {
			return e
		}
		y, e := s.LoadSnapshot(b)
		if e != nil {
			return e
		}
		xm, ym := map[string]string{}, map[string]string{}
		for _, h := range x.Hooks {
			xm[h.ID] = h.Content
		}
		for _, h := range y.Hooks {
			ym[h.ID] = h.Content
		}
		fmt.Printf("H_%d -> H_%d\n", a, b)
		for id, content := range ym {
			if old, ok := xm[id]; !ok {
				fmt.Printf("+ %s: %s\n", id, content)
			} else if old != content {
				fmt.Printf("~ %s: %s -> %s\n", id, old, content)
			}
		}
		for id, content := range xm {
			if _, ok := ym[id]; !ok {
				fmt.Printf("- %s: %s\n", id, content)
			}
		}
		return nil
	}
	return fmt.Errorf("invalid hooks command")
}
func Rewrite(project string, from int, cfg config.Config, auto bool) error {
	s := store.New(project)
	if err := s.TruncateFrom(from); err != nil {
		return err
	}
	cfg.Auto = auto
	return Generate(context.Background(), project, cfg, from, 0, false)
}

func Render(ctx context.Context, project string) error { return render.Quarto(ctx, project) }

var _ = strings.TrimSpace
