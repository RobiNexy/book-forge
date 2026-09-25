package commands

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RobiNexy/book-forge/internal/domain"
	"github.com/RobiNexy/book-forge/internal/store"
	"gopkg.in/yaml.v3"
)

func TestInitCreatesUsableProjectWithoutOverwritingFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "book")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(dir, "outline.md")
	if err := os.WriteFile(custom, []byte("An existing outline with no required heading structure.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	if err := Validate(filepath.Join(dir, "bookforge.yaml")); err != nil {
		t.Fatal(err)
	}
	longRules, err := os.ReadFile(filepath.Join(dir, "prompts", "long_book_gen.md"))
	if err != nil || !strings.Contains(string(longRules), "<<<END_OF_BOOK>>>") {
		t.Fatalf("long-book rules scaffold is missing final-marker guidance: %v", err)
	}
	data, err := os.ReadFile(custom)
	if err != nil || string(data) != "An existing outline with no required heading structure.\n" {
		t.Fatalf("existing outline changed: %q, %v", data, err)
	}
}

func TestLoadProjectKeepsHumanMarkdownInputsOpaque(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "book")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	outline := "Any outline shape\nNo heading rules\n"
	hooks := "## Strange user heading\n- Any hook layout\n"
	if err := os.WriteFile(filepath.Join(dir, "outline.md"), []byte(outline), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "initial_hooks.md"), []byte(hooks), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := loadProject(filepath.Join(dir, "bookforge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if p.outline != outline || p.initialHooks != hooks {
		t.Fatalf("loaded inputs outline=%q hooks=%q", p.outline, p.initialHooks)
	}
}

func TestValidateRejectsChangedInputsAfterInitialization(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "book")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	p, err := loadProject(filepath.Join(dir, "bookforge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.store.Initialize(domain.ProjectState{CurrentHooks: p.initialHooks}, p.inputs); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "outline.md"), []byte("Changed opaque outline text.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Validate(filepath.Join(dir, "bookforge.yaml")); err == nil {
		t.Fatal("expected changed input rejection")
	}
}

func TestQuartoReferencesIgnoresCommentsAndAcceptsConfiguredGlob(t *testing.T) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte("project:\n  type: book\nbook:\n  chapters:\n    - index.qmd\n    - chapters/*.qmd\n# chapters/01.qmd is only a comment\n"), &document); err != nil {
		t.Fatal(err)
	}
	if !quartoReferences(&document, []string{"chapters/01.qmd", "chapters/*.qmd"}) {
		t.Fatal("expected explicit chapter wildcard reference")
	}

	if err := yaml.Unmarshal([]byte("project:\n  type: book\n# chapters/01.qmd\n"), &document); err != nil {
		t.Fatal(err)
	}
	if quartoReferences(&document, []string{"chapters/01.qmd"}) {
		t.Fatal("comment must not count as a chapter reference")
	}
}

func TestRewriteShowsResolvedTargetAndRequiresConfirmation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "book")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	p, err := loadProject(filepath.Join(dir, "bookforge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.store.Initialize(domain.ProjectState{CurrentHooks: p.initialHooks}, p.inputs); err != nil {
		t.Fatal(err)
	}
	if err := p.store.CommitGeneration(1, "chapter one", domain.ProjectState{CurrentHooks: "hooks"}, store.AuditRecord{Generation: 1}, "", "", false); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = RewriteWithIO(context.Background(), filepath.Join(dir, "bookforge.yaml"), 1, true, strings.NewReader("n\n"), &output)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("declined rewrite error = %v", err)
	}
	if !strings.Contains(output.String(), "chapters/001.qmd") || !strings.Contains(output.String(), "第 1 章") {
		t.Fatalf("rewrite preview missing resolved target: %q", output.String())
	}
	if !p.store.HasChapter(1) {
		t.Fatal("declined rewrite changed chapter files")
	}
}

func TestConfirmedRewriteInvalidatesCascadeBeforeRestart(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	dir := filepath.Join(t.TempDir(), "book")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	p, err := loadProject(filepath.Join(dir, "bookforge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.store.Initialize(domain.ProjectState{CurrentHooks: p.initialHooks}, p.inputs); err != nil {
		t.Fatal(err)
	}
	for generation := 1; generation <= 2; generation++ {
		if err := p.store.CommitGeneration(generation, "generated", domain.ProjectState{CurrentHooks: "hooks"}, store.AuditRecord{Generation: generation}, "", "", false); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	err = RewriteWithIO(context.Background(), filepath.Join(dir, "bookforge.yaml"), 1, true, strings.NewReader("y\n"), &output)
	if err == nil {
		t.Fatal("expected the restart to fail without an API key")
	}
	if p.store.HasChapter(1) || p.store.HasChapter(2) {
		t.Fatal("confirmed rewrite did not invalidate the cascade")
	}
	if latest, err := p.store.LatestGeneration(); err != nil || latest != 0 {
		t.Fatalf("latest generation=%d err=%v", latest, err)
	}
	logEntries, err := p.store.ReadLogEntries()
	if err != nil {
		t.Fatal(err)
	}
	foundMapping := false
	for _, entry := range logEntries {
		if entry.Action == "rewrite" && entry.Result == "invalidated" {
			foundMapping = true
		}
	}
	if !foundMapping {
		t.Fatal("rewrite mapping event was not logged")
	}
}

func TestClearRemovesOnlyBookForgeManagedArtifacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "book")
	if err := Init(dir); err != nil {
		t.Fatal(err)
	}
	quarto := filepath.Join(dir, "quarto.yml")
	if err := os.WriteFile(quarto, []byte("project:\n  type: book\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := loadProject(filepath.Join(dir, "bookforge.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.store.Initialize(domain.ProjectState{CurrentHooks: p.initialHooks}, p.inputs); err != nil {
		t.Fatal(err)
	}
	if err := p.store.CommitGeneration(1, "generated", domain.ProjectState{CurrentHooks: "next"}, store.AuditRecord{Generation: 1}, "prompt", "response", true); err != nil {
		t.Fatal(err)
	}
	userChapter := filepath.Join(dir, "chapters", "appendix.qmd")
	if err := os.WriteFile(userChapter, []byte("user chapter"), 0o600); err != nil {
		t.Fatal(err)
	}
	userState := filepath.Join(dir, ".bookforge", "user-file.json")
	if err := os.WriteFile(userState, []byte("user state"), 0o600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ClearWithIO(filepath.Join(dir, "bookforge.yaml"), strings.NewReader("y\n"), &output); err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{filepath.Join(dir, "outline.md"), filepath.Join(dir, "initial_hooks.md"), filepath.Join(dir, "prompts", "system.md"), filepath.Join(dir, "prompts", "long_book_gen.md"), quarto, userChapter, userState} {
		if _, err := os.Stat(preserved); err != nil {
			t.Errorf("user file %s was removed: %v", preserved, err)
		}
	}
	for _, removed := range []string{filepath.Join(dir, "chapters", "001.qmd"), filepath.Join(dir, ".bookforge", "manifest.json"), filepath.Join(dir, ".bookforge", "snapshots", "state_000.json"), filepath.Join(dir, ".bookforge", "snapshots", "state_001.json"), filepath.Join(dir, ".bookforge", "audit", "run_001.json")} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Errorf("managed file %s remains: %v", removed, err)
		}
	}
	logText, err := p.store.ReadLog()
	if err != nil || !strings.Contains(logText, `"action":"clear"`) {
		t.Fatalf("clear event missing from fresh operation log: %q err=%v", logText, err)
	}
}
