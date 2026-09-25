package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RobiNexy/book-forge/internal/domain"
)

func TestChapterStatusesAndClearPreserveUnmanagedFiles(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if err := s.Initialize(domain.ProjectState{CurrentHooks: "initial"}, InputHashes{}); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 2; n++ {
		state := domain.ProjectState{CurrentHooks: "hooks", Finished: n == 2}
		record := AuditRecord{Generation: n, EndOfBook: state.Finished}
		if err := s.CommitGeneration(n, "chapter", state, record, "", "", false); err != nil {
			t.Fatal(err)
		}
	}
	orphan := filepath.Join(root, "chapters", "004.qmd")
	if err := os.WriteFile(orphan, []byte("uncommitted"), 0o600); err != nil {
		t.Fatal(err)
	}
	chapterNote := filepath.Join(root, "chapters", "notes.qmd")
	if err := os.WriteFile(chapterNote, []byte("user-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	customState := filepath.Join(root, ".bookforge", "custom.json")
	if err := os.WriteFile(customState, []byte("user-owned"), 0o600); err != nil {
		t.Fatal(err)
	}
	statuses, err := s.GenerationStatuses()
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 3 || !statuses[0].Committed || !statuses[1].Committed || !statuses[1].Finished || statuses[2].Committed {
		t.Fatalf("chapter statuses = %#v", statuses)
	}
	if err := s.AppendLog(LogEntry{Action: "test", Result: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveInvalidResponse(4, "run", 1, "raw bad response", "bad marker"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.state("archive", "test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.state("archive", "test", "old.qmd"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	managed, err := s.ManagedArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range managed {
		if path == "chapters/notes.qmd" || path == ".bookforge/custom.json" {
			t.Fatalf("unmanaged file listed for deletion: %s", path)
		}
	}
	if err := s.ClearManaged(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{chapterNote, customState} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("unmanaged file %s was removed: %v", path, err)
		}
	}
	for _, path := range []string{filepath.Join(root, "chapters", "001.qmd"), filepath.Join(root, ".bookforge", "manifest.json"), s.state("log.jsonl"), orphan} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("managed artifact %s remains after clear: %v", path, err)
		}
	}
}
