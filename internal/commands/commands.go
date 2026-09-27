// Package commands loads opaque project text and wires the requested project operation.
package commands

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/RobiNexy/book-forge/internal/config"
	"github.com/RobiNexy/book-forge/internal/domain"
	"github.com/RobiNexy/book-forge/internal/llm/openai"
	"github.com/RobiNexy/book-forge/internal/orchestrator"
	"github.com/RobiNexy/book-forge/internal/render"
	"github.com/RobiNexy/book-forge/internal/store"
	"gopkg.in/yaml.v3"
)

type project struct {
	config        config.Config
	store         *store.FileStore
	outline       string
	initialHooks  string
	system        string
	longBookRules string
	inputs        store.InputHashes
}

//go:embed long_book_gen.md
var defaultLongBookRules []byte

//go:embed quarto-book-template.yml
var quartoBookTemplate []byte

// Init creates a project scaffold without replacing existing human-maintained files.
func Init(dir string) error {
	return InitWithTemplate(dir, "auto")
}

// InitWithTemplate creates the scaffold using an OS default or a chosen font profile.
func InitWithTemplate(dir, selection string) error {
	return InitWithDesign(dir, selection, "")
}

// InitWithDesign combines an optional visual design with the chosen font profile.
func InitWithDesign(dir, selection, design string) error {
	template, name, err := renderBookDesign(selection, design)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	for _, p := range []string{filepath.Join(abs, "prompts"), filepath.Join(abs, "chapters"), filepath.Join(abs, ".bookforge", "snapshots"), filepath.Join(abs, ".bookforge", "audit")} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return err
		}
	}
	files := map[string]string{
		"bookforge.yaml": `# BookForge project manifest. Paths are relative to this file.
apiVersion: bookforge/v1
project:
  outline: outline.md
  initial_hooks: initial_hooks.md
  system_prompt: prompts/system.md
  long_book_rules: prompts/long_book_gen.md
  chapters_dir: chapters
  state_dir: .bookforge
llm:
  provider: openai
  model: gpt-4.1
  api_key_env: OPENAI_API_KEY
  base_url: https://api.openai.com/v1
  temperature: 0.7
  max_output_tokens: 12000
  request_timeout: 10m
  # Passed through to the model API without interpretation.
  parameters: {}
  # Additional HTTP request headers when required by the endpoint.
  headers: {}
generation:
  review: interactive
  editor: ""
  store_prompt_and_response: true
  invalid_output_retries: 3
quarto:
  project_dir: .
  command: quarto
`,
		"outline.md":               "# Add your complete outline here\n",
		"initial_hooks.md":         "",
		"index.qmd":                "",
		"_quarto.yml":              template,
		"prompts/system.md":        "You are a thoughtful book author. Follow the project owner's writing instructions and maintain continuity.\n",
		"prompts/long_book_gen.md": string(defaultLongBookRules),
		".gitignore":               ".bookforge/audit/\n.bookforge/invalid-responses/\n.bookforge/log.jsonl\n.bookforge/lock\n.bookforge/archive/\n_book/\n",
	}
	for name, content := range files {
		if err := createIfMissing(filepath.Join(abs, name), content); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stdout, "Initialized BookForge project at %s (template: %s)\n", abs, name)
	return nil
}

func createIfMissing(filePath, content string) error {
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func loadProject(configPath string) (project, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return project{}, err
	}
	s := store.NewWithLayout(filepath.Dir(cfg.File), cfg.Project.ChaptersDir, cfg.Project.StateDir)
	outline, err := os.ReadFile(cfg.Project.Outline)
	if err != nil {
		return project{}, fmt.Errorf("read outline %q: %w", cfg.Project.Outline, err)
	}
	hooks, err := os.ReadFile(cfg.Project.InitialHooks)
	if err != nil {
		return project{}, fmt.Errorf("read initial hooks %q: %w", cfg.Project.InitialHooks, err)
	}
	system, err := os.ReadFile(cfg.Project.SystemPrompt)
	if err != nil {
		return project{}, fmt.Errorf("read system prompt %q: %w", cfg.Project.SystemPrompt, err)
	}
	longBookRules, err := os.ReadFile(cfg.Project.LongBookRules)
	if err != nil {
		return project{}, fmt.Errorf("read long-book rules %q: %w", cfg.Project.LongBookRules, err)
	}
	return project{
		config: cfg, store: s, outline: string(outline), initialHooks: string(hooks), system: string(system), longBookRules: string(longBookRules),
		inputs: store.HashInputBytes(outline, hooks, system, longBookRules),
	}, nil
}

// Validate checks configuration and source-file readability without parsing document semantics.
func Validate(configPath string) error {
	p, err := loadProject(configPath)
	if err != nil {
		return err
	}
	if err := validateLoadedState(p); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, "Valid: configuration and required text files are readable.")
	return nil
}

func validateLoadedState(p project) error {
	if _, err := p.store.LoadManifest(); err == nil {
		if err := p.store.VerifyInputs(p.inputs); err != nil {
			return err
		}
		return p.store.ValidateState()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Generate runs sequentially until the model marks the final chapter.
func Generate(ctx context.Context, configPath string, auto bool, noAudit bool, model string) error {
	p, err := loadProject(configPath)
	if err != nil {
		return err
	}
	return runGenerate(ctx, p, auto, noAudit, model, false)
}

func runGenerate(ctx context.Context, p project, auto bool, noAudit bool, model string, lockHeld bool) error {
	key := os.Getenv(p.config.LLM.APIKeyEnv)
	client := &openai.Client{APIKey: key, BaseURL: p.config.LLM.BaseURL, Headers: p.config.LLM.Headers, HTTP: &http.Client{Timeout: p.config.LLM.RequestTimeout}}
	if model != "" {
		p.config.LLM.Model = model
	}
	if auto {
		p.config.Generation.Review = "auto"
	}
	if noAudit {
		p.config.Generation.StorePromptAndResponse = false
	}
	return (&orchestrator.Orchestrator{
		LLM: client, Store: p.store, Outline: p.outline,
		Options: orchestrator.Options{
			Model: p.config.LLM.Model, Temperature: p.config.LLM.Temperature, MaxTokens: p.config.LLM.MaxOutputTokens,
			Parameters: p.config.LLM.Parameters, AuditFull: p.config.Generation.StorePromptAndResponse,
			InvalidOutputRetries: p.config.Generation.InvalidOutputRetries, Auto: p.config.Generation.Review == "auto",
			InitialHooks: p.initialHooks, SystemPrompt: p.system, LongBookRules: p.longBookRules, Inputs: p.inputs,
			Output: os.Stderr, Editor: p.config.Generation.Editor, LockHeld: lockHeld,
		},
	}).Generate(ctx)
}

// Rewrite invalidates generation N onward and continues until an end-of-book marker is received.
func Rewrite(ctx context.Context, configPath string, generation int, auto bool) error {
	return RewriteWithIO(ctx, configPath, generation, auto, os.Stdin, os.Stderr)
}

// RewriteWithIO shows the resolved cascade target and requires explicit confirmation before changes.
func RewriteWithIO(ctx context.Context, configPath string, generation int, auto bool, input io.Reader, output io.Writer) error {
	p, err := loadProject(configPath)
	if err != nil {
		return err
	}
	release, err := p.store.Acquire()
	if err != nil {
		return err
	}
	defer release()
	m, err := p.store.LoadManifest()
	hasManifest := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	inputsChanged := hasManifest && m.Inputs != p.inputs
	if inputsChanged && generation != 1 {
		return fmt.Errorf("source text changed; rewrite from generation 1 to reset initial hook state")
	}
	_, initialErr := p.store.LoadSnapshot(0)
	resetState := inputsChanged || (hasManifest && errors.Is(initialErr, os.ErrNotExist))
	if hasManifest && initialErr != nil && !errors.Is(initialErr, os.ErrNotExist) {
		return initialErr
	}
	if resetState && generation != 1 {
		return fmt.Errorf("initial state is missing or source inputs changed; rewrite from generation 1")
	}
	next := 1
	if hasManifest && !resetState {
		next, err = p.store.NextGeneration()
		if err != nil {
			return err
		}
	}
	if generation < 1 || generation > next {
		return fmt.Errorf("rewrite generation %d is outside existing sequence (next is %d)", generation, next)
	}
	if hasManifest && !resetState {
		prior, err := p.store.LoadSnapshot(generation - 1)
		if err != nil {
			return err
		}
		if prior.Finished {
			return fmt.Errorf("rewrite target is after the end-of-book marker; rewrite the ending generation or an earlier one")
		}
	}
	before, err := p.store.GenerationStatuses()
	if err != nil && !resetState {
		return err
	}
	chapterPath, err := filepath.Rel(p.store.Dir, filepath.Join(p.config.Project.ChaptersDir, fmt.Sprintf("%03d.qmd", generation)))
	if err != nil {
		return err
	}
	chapterPath = filepath.ToSlash(chapterPath)
	fmt.Fprintf(output, "将重写系统中的第 %d 章：%s\n", generation, chapterPath)
	fmt.Fprintf(output, "该操作会使第 %d 章及之后的生成结果失效。\n", generation)
	fmt.Fprint(output, "确认继续？[y/N] ")
	var answer string
	if _, err := fmt.Fscanln(input, &answer); err != nil {
		return err
	}
	if answer != "y" && answer != "Y" && answer != "yes" && answer != "YES" {
		appendLog(p.store, output, store.LogEntry{Action: "rewrite", Generation: generation, Result: "cancelled", Details: map[string]any{"chapter_path": chapterPath}})
		return fmt.Errorf("rewrite cancelled")
	}
	if !hasManifest {
		if err := p.store.Initialize(domain.ProjectState{CurrentHooks: p.initialHooks}, p.inputs); err != nil {
			return err
		}
	}
	if resetState {
		if err := p.store.Reset(domain.ProjectState{CurrentHooks: p.initialHooks}, p.inputs); err != nil {
			return err
		}
	} else if err := p.store.TruncateFrom(generation); err != nil {
		return err
	}
	after, err := p.store.GenerationStatuses()
	if err != nil {
		return err
	}
	appendLog(p.store, output, store.LogEntry{Action: "rewrite", Generation: generation, Result: "invalidated", Details: map[string]any{"chapter_path": chapterPath, "before": before, "after": after}})
	return runGenerate(ctx, p, auto, false, "", true)
}

// Log prints the append-only JSONL operation history.
func Log(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	s := store.NewWithLayout(filepath.Dir(cfg.File), cfg.Project.ChaptersDir, cfg.Project.StateDir)
	text, err := s.ReadLog()
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, text)
	return err
}

// Clear requires confirmation and removes only paths BookForge reserves for generated output/state.
func Clear(configPath string) error {
	return ClearWithIO(configPath, os.Stdin, os.Stderr)
}

// ClearWithIO exposes the confirmation boundary for terminal use and deterministic tests.
func ClearWithIO(configPath string, input io.Reader, output io.Writer) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	s := store.NewWithLayout(filepath.Dir(cfg.File), cfg.Project.ChaptersDir, cfg.Project.StateDir)
	release, err := s.Acquire()
	if err != nil {
		return err
	}
	defer release()
	artifacts, err := s.ManagedArtifacts()
	if err != nil {
		return err
	}
	fmt.Fprintln(output, "BookForge will remove these generated files and state:")
	if len(artifacts) == 0 {
		fmt.Fprintln(output, "  (none)")
	}
	for _, artifact := range artifacts {
		fmt.Fprintf(output, "  %s\n", artifact)
	}
	fmt.Fprint(output, "确认清空？[y/N] ")
	var answer string
	if _, err := fmt.Fscanln(input, &answer); err != nil {
		return err
	}
	if answer != "y" && answer != "Y" && answer != "yes" && answer != "YES" {
		appendLog(s, output, store.LogEntry{Action: "clear", Result: "cancelled"})
		return fmt.Errorf("clear cancelled")
	}
	if err := s.ClearManaged(); err != nil {
		return err
	}
	appendLog(s, output, store.LogEntry{Action: "clear", Result: "completed", Details: map[string]any{"removed_files": len(artifacts)}})
	fmt.Fprintln(output, "BookForge generation state has been cleared.")
	return nil
}

func appendLog(s *store.FileStore, output io.Writer, entry store.LogEntry) {
	if err := s.AppendLog(entry); err != nil {
		fmt.Fprintf(output, "Warning: unable to append operation log: %v\n", err)
	}
}

// Hooks prints the complete, uninterpreted hook text from a committed state snapshot.
func Hooks(configPath string, generation int) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	s := store.NewWithLayout(filepath.Dir(cfg.File), cfg.Project.ChaptersDir, cfg.Project.StateDir)
	if generation == 0 {
		generation, err = s.LatestGeneration()
		if errors.Is(err, os.ErrNotExist) {
			b, readErr := os.ReadFile(cfg.Project.InitialHooks)
			if readErr != nil {
				return readErr
			}
			_, writeErr := fmt.Fprint(os.Stdout, string(b))
			return writeErr
		}
		if err != nil {
			return err
		}
	}
	state, err := s.LoadSnapshot(generation)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(os.Stdout, state.CurrentHooks)
	return err
}

// Forge synchronizes generated chapter paths into the Quarto book and compiles the PDF.
func Forge(ctx context.Context, configPath string) (forgeErr error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	state := store.NewWithLayout(filepath.Dir(cfg.File), cfg.Project.ChaptersDir, cfg.Project.StateDir)
	release, err := state.Acquire()
	if err != nil {
		return err
	}
	defer release()
	appendLog(state, os.Stderr, store.LogEntry{Action: "forge", Result: "started", Details: map[string]any{"project_dir": cfg.Quarto.ProjectDir}})
	defer func() {
		result, message := "completed", ""
		if forgeErr != nil {
			result, message = "failed", forgeErr.Error()
		}
		appendLog(state, os.Stderr, store.LogEntry{Action: "forge", Result: result, Error: message})
	}()
	if err := state.ValidateState(); err != nil {
		return fmt.Errorf("validate generated project state: %w", err)
	}
	chapters, err := state.Chapters()
	if err != nil {
		return err
	}
	if len(chapters) == 0 {
		return fmt.Errorf("no generated .qmd chapters in %s", cfg.Project.ChaptersDir)
	}
	projectConfig, err := quartoProjectConfig(cfg.Quarto.ProjectDir)
	if err != nil {
		return err
	}
	if err := updateQuartoChapterList(projectConfig, cfg.Quarto.ProjectDir, cfg.Project.ChaptersDir, chapters); err != nil {
		return err
	}
	if _, err := exec.LookPath(cfg.Quarto.Command); err != nil {
		return fmt.Errorf("Quarto command %q not found: %w", cfg.Quarto.Command, err)
	}
	return render.QuartoCommand(ctx, cfg.Quarto.ProjectDir, cfg.Quarto.Command)
}

func quartoProjectConfig(projectDir string) (string, error) {
	for _, name := range []string{"_quarto.yml", "quarto.yml"} {
		file := filepath.Join(projectDir, name)
		if _, err := os.Stat(file); err == nil {
			return file, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("Quarto project configuration not found in %s", projectDir)
}

func updateQuartoChapterList(file, projectDir, chaptersDir string, generations []int) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("parse Quarto project configuration: %w", err)
	}
	if document.Kind != yaml.DocumentNode || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("Quarto project configuration must be a YAML mapping")
	}
	root := document.Content[0]
	book := mappingValue(root, "book")
	if book == nil {
		book = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{}}
		root.Content = append(root.Content, scalarNode("book"), book)
	}
	chapters := mappingValue(book, "chapters")
	if chapters == nil {
		chapters = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		book.Content = append(book.Content, scalarNode("chapters"), chapters)
	} else if chapters.Kind != yaml.SequenceNode {
		return fmt.Errorf("Quarto book.chapters must be a list")
	}

	kept := make([]*yaml.Node, 0, len(chapters.Content)+len(generations))
	seen := make(map[string]bool)
	for _, node := range chapters.Content {
		if node.Kind == yaml.ScalarNode && numberedQMD(node.Value) {
			continue
		}
		kept = append(kept, node)
		if node.Kind == yaml.ScalarNode {
			seen[path.Clean(filepath.ToSlash(node.Value))] = true
		}
	}
	chapters.Content = kept
	for _, generation := range generations {
		name := fmt.Sprintf("%03d.qmd", generation)
		chapterFile := filepath.Join(chaptersDir, name)
		relative, err := filepath.Rel(projectDir, chapterFile)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		clean := path.Clean(strings.TrimPrefix(relative, "./"))
		if seen[clean] {
			continue
		}
		chapters.Content = append(chapters.Content, scalarNode(relative))
		seen[clean] = true
	}
	updated, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	return os.WriteFile(file, updated, 0o644)
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func numberedQMD(value string) bool {
	base := path.Base(filepath.ToSlash(value))
	if !strings.HasSuffix(base, ".qmd") {
		return false
	}
	number := strings.TrimSuffix(base, ".qmd")
	if len(number) != 3 {
		return false
	}
	for _, digit := range number {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
