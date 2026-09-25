// Package store persists opaque hook text, numbered chapters, snapshots, and per-generation audits.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RobiNexy/book-forge/internal/domain"
	"github.com/RobiNexy/book-forge/internal/llm"
)

// InputHashes bind the current state chain to exact user-controlled source bytes.
type InputHashes struct {
	Outline       string `json:"outline"`
	InitialHooks  string `json:"initial_hooks"`
	SystemPrompt  string `json:"system_prompt"`
	LongBookRules string `json:"long_book_rules"`
}

// Manifest contains source identity only; generation position is derived from committed artifacts.
type Manifest struct {
	Version   string      `json:"version"`
	Inputs    InputHashes `json:"inputs"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// AuditRecord describes one generation. It records sequence for file correlation, not as prompt state.
type AuditRecord struct {
	Generation   int            `json:"generation"`
	RunID        string         `json:"run_id"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   time.Time      `json:"finished_at"`
	Model        string         `json:"model"`
	Temperature  float64        `json:"temperature"`
	MaxTokens    int            `json:"max_output_tokens"`
	Parameters   map[string]any `json:"model_parameters,omitempty"`
	Usage        llm.Usage      `json:"usage"`
	Inputs       InputHashes    `json:"input_hashes"`
	PromptHash   string         `json:"prompt_hash"`
	ResponseHash string         `json:"response_hash"`
	HooksBefore  string         `json:"hooks_before_hash"`
	HooksAfter   string         `json:"hooks_after_hash"`
	ChapterPath  string         `json:"chapter_path"`
	EndOfBook    bool           `json:"end_of_book"`
	ReviewedBy   string         `json:"reviewed_by"`
	FullPrompt   string         `json:"full_prompt,omitempty"`
	RawResponse  string         `json:"raw_response,omitempty"`
}

// FileStore owns its configured chapter and state paths.
type FileStore struct {
	Dir         string
	ChaptersDir string
	StateDir    string
}

// New selects the default project layout.
func New(root string) *FileStore {
	return NewWithLayout(root, filepath.Join(root, "chapters"), filepath.Join(root, ".bookforge"))
}

// NewWithLayout applies paths normalized by project configuration.
func NewWithLayout(root, chaptersDir, stateDir string) *FileStore {
	return &FileStore{Dir: root, ChaptersDir: chaptersDir, StateDir: stateDir}
}

func (s *FileStore) state(parts ...string) string {
	return filepath.Join(append([]string{s.StateDir}, parts...)...)
}

func (s *FileStore) snapshot(generation int) string {
	return s.state("snapshots", fmt.Sprintf("state_%03d.json", generation))
}

func (s *FileStore) chapter(generation int) string {
	return filepath.Join(s.ChaptersDir, fmt.Sprintf("%03d.qmd", generation))
}

// ChapterPath returns the project-relative, numbered QMD path used in progress and audits.
func (s *FileStore) ChapterPath(generation int) string {
	relative, err := filepath.Rel(s.Dir, s.chapter(generation))
	if err != nil {
		return filepath.ToSlash(s.chapter(generation))
	}
	return filepath.ToSlash(relative)
}

func (s *FileStore) audit(generation int) string {
	return s.state("audit", fmt.Sprintf("run_%03d.json", generation))
}

// HashInputs reads and hashes the four user-maintained text files without interpreting them.
func HashInputs(outlinePath, hooksPath, systemPath, rulesPath string) (InputHashes, error) {
	outline, err := os.ReadFile(outlinePath)
	if err != nil {
		return InputHashes{}, fmt.Errorf("read outline %q: %w", outlinePath, err)
	}
	hooks, err := os.ReadFile(hooksPath)
	if err != nil {
		return InputHashes{}, fmt.Errorf("read initial hooks %q: %w", hooksPath, err)
	}
	system, err := os.ReadFile(systemPath)
	if err != nil {
		return InputHashes{}, fmt.Errorf("read system prompt %q: %w", systemPath, err)
	}
	rules, err := os.ReadFile(rulesPath)
	if err != nil {
		return InputHashes{}, fmt.Errorf("read long-book rules %q: %w", rulesPath, err)
	}
	return HashInputBytes(outline, hooks, system, rules), nil
}

// HashInputBytes fingerprints the exact bytes loaded for one generation run.
func HashInputBytes(outline, hooks, system, rules []byte) InputHashes {
	return InputHashes{Outline: hashBytes(outline), InitialHooks: hashBytes(hooks), SystemPrompt: hashBytes(system), LongBookRules: hashBytes(rules)}
}

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(h[:])
}

// LoadManifest reads source hashes; generation progress is intentionally not stored here.
func (s *FileStore) LoadManifest() (Manifest, error) {
	b, err := os.ReadFile(s.state("manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	if m.Version != "v1" {
		return Manifest{}, fmt.Errorf("unsupported manifest version %q", m.Version)
	}
	return m, nil
}

// Initialize stores the initial hook text as state_000 and writes source hashes to manifest.
func (s *FileStore) Initialize(initial domain.ProjectState, inputs InputHashes) error {
	if _, err := os.Stat(s.state("manifest.json")); err == nil {
		manifest, err := s.LoadManifest()
		if err != nil {
			return err
		}
		if manifest.Inputs != inputs {
			return fmt.Errorf("project inputs changed; refusing to rebuild initial state")
		}
		if _, err := os.Stat(s.snapshot(0)); errors.Is(err, os.ErrNotExist) {
			if initial.Finished {
				return fmt.Errorf("initial project state cannot be finished")
			}
			return atomicJSON(s.snapshot(0), initial)
		} else if err != nil {
			return err
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if initial.Finished {
		return fmt.Errorf("initial project state cannot be finished")
	}
	if err := atomicJSON(s.snapshot(0), initial); err != nil {
		return err
	}
	return atomicJSON(s.state("manifest.json"), Manifest{Version: "v1", Inputs: inputs, UpdatedAt: time.Now().UTC()})
}

// Reset archives the old chain and installs new state zero after an explicit source reset.
func (s *FileStore) Reset(initial domain.ProjectState, inputs InputHashes) error {
	if initial.Finished {
		return fmt.Errorf("initial project state cannot be finished")
	}
	if err := s.archiveFrom(0); err != nil {
		return err
	}
	if err := atomicJSON(s.snapshot(0), initial); err != nil {
		return err
	}
	return atomicJSON(s.state("manifest.json"), Manifest{Version: "v1", Inputs: inputs, UpdatedAt: time.Now().UTC()})
}

// VerifyInputs rejects continuation when any source file differs from the initialized chain.
func (s *FileStore) VerifyInputs(inputs InputHashes) error {
	m, err := s.LoadManifest()
	if err != nil {
		return err
	}
	if m.Inputs != inputs {
		return fmt.Errorf("project inputs changed; use rewrite 1 to reset and regenerate")
	}
	return nil
}

// LoadSnapshot reads the opaque hook text and completion bit after generation (zero is initial input).
func (s *FileStore) LoadSnapshot(generation int) (domain.ProjectState, error) {
	b, err := os.ReadFile(s.snapshot(generation))
	if err != nil {
		return domain.ProjectState{}, err
	}
	var state domain.ProjectState
	if err := json.Unmarshal(b, &state); err != nil {
		return domain.ProjectState{}, fmt.Errorf("decode state_%03d: %w", generation, err)
	}
	return state, nil
}

// NextGeneration derives the next sequence from contiguous chapters, snapshots, and audit files.
func (s *FileStore) NextGeneration() (int, error) {
	if _, err := s.LoadManifest(); err != nil {
		return 0, err
	}
	state, err := s.LoadSnapshot(0)
	if err != nil {
		return 0, fmt.Errorf("load initial state: %w", err)
	}
	if state.Finished {
		return 0, fmt.Errorf("initial snapshot cannot be marked finished")
	}
	next := 1
	for !state.Finished {
		complete, err := s.generationComplete(next)
		if err != nil {
			return 0, err
		}
		if !complete {
			break
		}
		state, err = s.LoadSnapshot(next)
		if err != nil {
			break
		}
		next++
	}
	return next, nil
}

// LatestGeneration returns the last complete generation, or zero for a new project.
func (s *FileStore) LatestGeneration() (int, error) {
	next, err := s.NextGeneration()
	if err != nil {
		return 0, err
	}
	return next - 1, nil
}

func (s *FileStore) generationComplete(generation int) (bool, error) {
	missing, err := missingAny(s.chapter(generation), s.snapshot(generation), s.audit(generation))
	if err != nil || missing {
		return false, err
	}
	state, err := s.LoadSnapshot(generation)
	if err != nil {
		return false, nil
	}
	b, err := os.ReadFile(s.audit(generation))
	if err != nil {
		return false, err
	}
	var record AuditRecord
	if err := json.Unmarshal(b, &record); err != nil || record.Generation != generation || record.EndOfBook != state.Finished {
		return false, nil
	}
	return true, nil
}

// CommitGeneration writes the chapter and state before the audit file, which is the generation's
// completion marker. The sequence is derived from those artifacts and is not written to ProjectState.
func (s *FileStore) CommitGeneration(generation int, chapter string, state domain.ProjectState, record AuditRecord, prompt, response string, full bool) error {
	next, err := s.NextGeneration()
	if err != nil {
		return err
	}
	if generation != next {
		return fmt.Errorf("generation %d does not match next artifact sequence %d", generation, next)
	}
	previous, err := s.LoadSnapshot(generation - 1)
	if err != nil {
		return err
	}
	if previous.Finished {
		return fmt.Errorf("book already ended after generation %d", generation-1)
	}
	if record.Generation != generation || record.EndOfBook != state.Finished {
		return fmt.Errorf("generation audit does not match completion state")
	}
	record.ChapterPath = s.ChapterPath(generation)
	if full {
		record.FullPrompt, record.RawResponse = prompt, response
	}
	if err := atomicBytes(s.chapter(generation), []byte(chapter)); err != nil {
		return err
	}
	if err := atomicJSON(s.snapshot(generation), state); err != nil {
		return err
	}
	return atomicJSON(s.audit(generation), record)
}

// ValidateState checks the initial snapshot and every active generation artifact for gaps.
func (s *FileStore) ValidateState() error {
	if _, err := s.LoadManifest(); err != nil {
		return err
	}
	initial, err := s.LoadSnapshot(0)
	if err != nil || initial.Finished {
		return fmt.Errorf("initial hook state is missing or invalid")
	}
	next, err := s.NextGeneration()
	if err != nil {
		return err
	}
	for _, dir := range []string{s.ChaptersDir, s.state("snapshots"), s.state("audit")} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			n, ok := artifactGeneration(dir, entry.Name(), s)
			if ok && n >= next {
				return fmt.Errorf("artifact %q exists beyond the contiguous committed sequence", entry.Name())
			}
		}
	}
	return nil
}

// Recover archives artifacts after the first incomplete sequence. The next generation is
// recomputed from files rather than restored from a stored counter.
func (s *FileStore) Recover() error {
	if _, err := s.LoadManifest(); err != nil {
		return err
	}
	if _, err := s.LoadSnapshot(0); err != nil {
		return fmt.Errorf("load initial state: %w", err)
	}
	next, err := s.NextGeneration()
	if err != nil {
		return err
	}
	return s.archiveFrom(next)
}

// TruncateFrom archives N and all later artifacts; the N-1 snapshot remains the restart state.
func (s *FileStore) TruncateFrom(generation int) error {
	if generation < 1 {
		return fmt.Errorf("generation must be positive")
	}
	next, err := s.NextGeneration()
	if err != nil {
		return err
	}
	if generation > next {
		return fmt.Errorf("generation %d is beyond next artifact sequence %d", generation, next)
	}
	previous, err := s.LoadSnapshot(generation - 1)
	if err != nil {
		return err
	}
	if previous.Finished {
		return fmt.Errorf("generation %d is after the end-of-book marker; rewrite the ending generation or an earlier one", generation)
	}
	return s.archiveFrom(generation)
}

func (s *FileStore) archiveFrom(generation int) error {
	archive := s.state("archive", time.Now().UTC().Format("20060102T150405.000000000"))
	for _, dir := range []string{s.ChaptersDir, s.state("snapshots"), s.state("audit")} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".bookforge-") && strings.HasSuffix(name, ".tmp") {
				_ = os.RemoveAll(filepath.Join(dir, name))
				continue
			}
			n, ok := artifactGeneration(dir, name, s)
			if !ok || n < generation {
				continue
			}
			destination := filepath.Join(archive, filepath.Base(dir), name)
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return err
			}
			if err := movePath(filepath.Join(dir, name), destination); err != nil {
				return err
			}
		}
	}
	return nil
}

func artifactGeneration(dir, name string, s *FileStore) (int, bool) {
	switch dir {
	case s.ChaptersDir:
		if !strings.HasSuffix(name, ".qmd") {
			return 0, false
		}
		n, err := strconv.Atoi(strings.TrimSuffix(name, ".qmd"))
		return n, err == nil && n > 0
	case s.state("snapshots"):
		if !strings.HasPrefix(name, "state_") || !strings.HasSuffix(name, ".json") {
			return 0, false
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "state_"), ".json"))
		return n, err == nil && n >= 0
	case s.state("audit"):
		if !strings.HasPrefix(name, "run_") || !strings.HasSuffix(name, ".json") {
			return 0, false
		}
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(name, "run_"), ".json"))
		return n, err == nil && n > 0
	default:
		return 0, false
	}
}

// Acquire creates an exclusive local project lock and reclaims it when its PID is no longer alive.
func (s *FileStore) Acquire() (func(), error) {
	if err := os.MkdirAll(s.StateDir, 0o755); err != nil {
		return nil, err
	}
	lock := s.state("lock")
	f, err := os.OpenFile(lock, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if pid, readErr := lockPID(lock); readErr == nil && pid > 0 {
				alive, probeErr := processAlive(pid)
				if probeErr == nil && !alive {
					stale := fmt.Sprintf("%s.stale-%d-%d", lock, pid, time.Now().UnixNano())
					if renameErr := os.Rename(lock, stale); renameErr == nil {
						movedPID, movedErr := lockPID(stale)
						if movedErr == nil && movedPID == pid {
							fmt.Fprintf(os.Stderr, "Warning: recovering stale BookForge lock for pid %d\n", pid)
							_ = os.Remove(stale)
							return s.Acquire()
						}
						_ = os.Rename(stale, lock)
					}
				}
			}
			return nil, fmt.Errorf("project is locked: %s", lock)
		}
		return nil, err
	}
	if _, err := fmt.Fprintf(f, "pid=%d\nstarted=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339)); err != nil {
		_ = f.Close()
		_ = os.Remove(lock)
		return nil, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(lock)
		return nil, err
	}
	return func() { _ = os.Remove(lock) }, nil
}

func lockPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "pid=") {
			return strconv.Atoi(strings.TrimPrefix(line, "pid="))
		}
	}
	return 0, fmt.Errorf("lock file has no pid")
}

// HasChapter reports whether a numbered output file exists.
func (s *FileStore) HasChapter(generation int) bool { return fileExists(s.chapter(generation)) }

// Chapters returns the sorted sequence numbers represented by numbered QMD files.
func (s *FileStore) Chapters() ([]int, error) {
	entries, err := os.ReadDir(s.ChaptersDir)
	if err != nil {
		return nil, err
	}
	var generations []int
	for _, entry := range entries {
		if n, ok := artifactGeneration(s.ChaptersDir, entry.Name(), s); ok {
			generations = append(generations, n)
		}
	}
	sort.Ints(generations)
	return generations, nil
}

func missingAny(paths ...string) (bool, error) {
	missing := false
	for _, path := range paths {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			missing = true
		} else if err != nil {
			return false, err
		}
	}
	return missing, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func atomicBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".bookforge-*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func atomicJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicBytes(path, append(b, '\n'))
}

func movePath(source, destination string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	} else if copyErr := copyPath(source, destination); copyErr != nil {
		return fmt.Errorf("rename %q failed; copy fallback: %w", source, copyErr)
	}
	return os.RemoveAll(source)
}

func copyPath(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := os.Mkdir(destination, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(source)
		if err != nil {
			_ = os.RemoveAll(destination)
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(source, entry.Name()), filepath.Join(destination, entry.Name())); err != nil {
				_ = os.RemoveAll(destination)
				return err
			}
		}
		return os.Chmod(destination, info.Mode().Perm())
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(destination)
		return err
	}
	return nil
}
