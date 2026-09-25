package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RobiNexy/book-forge/internal/domain"
)

func TestCommitAndRecoveryUseManifestAsCommitMarker(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	inputs := InputHashes{Outline: "outline", InitialHooks: "hooks", SystemPrompt: "prompt"}
	initial := domain.ProjectState{CurrentHooks: "unparsed initial hook text\n"}
	if err := s.Initialize(initial, inputs); err != nil {
		t.Fatal(err)
	}
	state := domain.ProjectState{CurrentHooks: "anything goes: [a, b]"}
	if err := s.CommitGeneration(1, "chapter", state, AuditRecord{Generation: 1, StartedAt: time.Now()}, "prompt", "response", false); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LatestGeneration(); err != nil || got != 1 {
		t.Fatalf("latest=%d err=%v", got, err)
	}
	loaded, err := s.LoadSnapshot(1)
	if err != nil || loaded.CurrentHooks != state.CurrentHooks {
		t.Fatalf("snapshot=%#v err=%v", loaded, err)
	}
	if !s.HasChapter(1) {
		t.Fatal("generation 1 chapter missing")
	}

	orphan := filepath.Join(root, "chapters", "002.qmd")
	if err := os.WriteFile(orphan, []byte("uncommitted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan chapter remains active: %v", err)
	}
	if err := os.Remove(filepath.Join(root, "chapters", "001.qmd")); err != nil {
		t.Fatal(err)
	}
	if err := s.Recover(); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LatestGeneration(); err != nil || got != 0 {
		t.Fatalf("recovered latest=%d err=%v", got, err)
	}
}

func TestGenerationPositionIsDerivedAndNotStoredAsStateCounter(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if err := s.Initialize(domain.ProjectState{CurrentHooks: "initial"}, InputHashes{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, ".bookforge", "manifest.json"), filepath.Join(root, ".bookforge", "snapshots", "state_000.json")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			t.Fatal(err)
		}
		if _, exists := fields["next_generation"]; exists {
			t.Fatalf("generation counter persisted in %s", path)
		}
	}
}

func TestInitializeRepairsMissingInitialSnapshotOnlyForSameInputs(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	inputs := InputHashes{Outline: "o", InitialHooks: "h", SystemPrompt: "s", LongBookRules: "l"}
	initial := domain.ProjectState{CurrentHooks: "original initial text"}
	if err := s.Initialize(initial, inputs); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.snapshot(0)); err != nil {
		t.Fatal(err)
	}
	if err := s.Initialize(initial, inputs); err != nil {
		t.Fatalf("repair same-input state zero: %v", err)
	}
	loaded, err := s.LoadSnapshot(0)
	if err != nil || loaded.CurrentHooks != initial.CurrentHooks {
		t.Fatalf("state zero=%#v err=%v", loaded, err)
	}
	if err := os.Remove(s.snapshot(0)); err != nil {
		t.Fatal(err)
	}
	changed := inputs
	changed.InitialHooks = "changed"
	if err := s.Initialize(initial, changed); err == nil {
		t.Fatal("must not silently repair state zero from changed source inputs")
	}
}

func TestAcquireRejectsLiveLockAndRecoversDeadPID(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	release, err := s.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Acquire(); err == nil {
		t.Fatal("expected an active lock to block a second writer")
	}
	release()

	if err := os.MkdirAll(filepath.Join(root, ".bookforge"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(root, ".bookforge", "lock")
	if err := os.WriteFile(lock, []byte("pid=2147483647\nstarted=old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	release, err = s.Acquire()
	if err != nil {
		t.Fatalf("recover stale lock: %v", err)
	}
	release()
}

func TestTruncateFromArchivesCascadeAndPreservesPreviousOpaqueState(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	inputs := InputHashes{Outline: "o", InitialHooks: "h", SystemPrompt: "s"}
	if err := s.Initialize(domain.ProjectState{CurrentHooks: "state zero"}, inputs); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= 3; n++ {
		state := domain.ProjectState{CurrentHooks: "state", Finished: n == 3}
		if err := s.CommitGeneration(n, "chapter", state, AuditRecord{Generation: n, Model: "model", EndOfBook: state.Finished}, "", "", false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.TruncateFrom(2); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LatestGeneration(); err != nil || got != 1 {
		t.Fatalf("latest=%d err=%v", got, err)
	}
	prior, err := s.LoadSnapshot(1)
	if err != nil || prior.CurrentHooks != "state" || prior.Finished {
		t.Fatalf("preserved state=%#v err=%v", prior, err)
	}
	if !s.HasChapter(1) || s.HasChapter(2) || s.HasChapter(3) {
		t.Fatal("truncate did not preserve only generations before 2")
	}
	if _, err := os.Stat(filepath.Join(root, ".bookforge", "archive")); err != nil {
		t.Fatalf("archive missing: %v", err)
	}
}
