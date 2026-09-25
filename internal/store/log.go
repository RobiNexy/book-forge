package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LogEntry records an observable project operation without storing secrets or prompt text.
type LogEntry struct {
	Time       time.Time      `json:"time"`
	Action     string         `json:"action"`
	Generation int            `json:"generation,omitempty"`
	Result     string         `json:"result"`
	Error      string         `json:"error,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// ChapterStatus maps a discovered output sequence to its path and commit state.
type ChapterStatus struct {
	Generation int    `json:"generation"`
	Path       string `json:"path"`
	Committed  bool   `json:"committed"`
	Finished   bool   `json:"finished"`
}

// AppendLog appends one JSON line. File permissions keep logs private because inputs may be sensitive.
func (s *FileStore) AppendLog(entry LogEntry) error {
	if entry.Time.IsZero() {
		entry.Time = time.Now().UTC()
	}
	if err := os.MkdirAll(s.StateDir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(s.state("log.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(entry); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// ReadLog returns the JSONL operation log, or an empty string if no log exists.
func (s *FileStore) ReadLog() (string, error) {
	b, err := os.ReadFile(s.state("log.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// SaveInvalidResponse preserves a malformed raw response regardless of the full-audit setting.
func (s *FileStore) SaveInvalidResponse(generation int, runID string, attempt int, raw, reason string) (string, error) {
	name := fmt.Sprintf("generation_%03d_%s_attempt_%02d.txt", generation, runID, attempt)
	path := s.state("invalid-responses", name)
	content := fmt.Sprintf("Validation error: %s\n\n----- Raw response -----\n%s", reason, raw)
	if err := atomicBytes(path, []byte(content)); err != nil {
		return "", err
	}
	return path, nil
}

// GenerationStatuses lists all numbered artifacts, including incomplete/uncommitted sequences.
func (s *FileStore) GenerationStatuses() ([]ChapterStatus, error) {
	next, err := s.NextGeneration()
	if errors.Is(err, os.ErrNotExist) {
		next, err = 1, nil
	}
	if err != nil {
		return nil, err
	}
	found := make(map[int]struct{})
	for _, dir := range []string{s.ChaptersDir, s.state("snapshots"), s.state("audit")} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if generation, ok := artifactGeneration(dir, entry.Name(), s); ok && generation > 0 {
				found[generation] = struct{}{}
			}
		}
	}
	generations := make([]int, 0, len(found))
	for generation := range found {
		generations = append(generations, generation)
	}
	sort.Ints(generations)
	statuses := make([]ChapterStatus, 0, len(generations))
	for _, generation := range generations {
		complete, err := s.generationComplete(generation)
		if err != nil {
			return nil, err
		}
		chapterPath, err := filepath.Rel(s.Dir, s.chapter(generation))
		if err != nil {
			return nil, err
		}
		status := ChapterStatus{
			Generation: generation,
			Path:       filepath.ToSlash(chapterPath),
			Committed:  generation < next && complete,
		}
		if status.Committed {
			state, err := s.LoadSnapshot(generation)
			if err != nil {
				return nil, err
			}
			status.Finished = state.Finished
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// ManagedArtifacts lists only files that BookForge owns and clear is permitted to remove.
func (s *FileStore) ManagedArtifacts() ([]string, error) {
	var paths []string
	add := func(path string) error {
		if _, err := os.Stat(path); err == nil {
			relative, relErr := filepath.Rel(s.Dir, path)
			if relErr != nil {
				return relErr
			}
			paths = append(paths, filepath.ToSlash(relative))
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := add(s.state("manifest.json")); err != nil {
		return nil, err
	}
	if err := add(s.state("log.jsonl")); err != nil {
		return nil, err
	}
	rootEntries, err := os.ReadDir(s.StateDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, entry := range rootEntries {
		if strings.HasPrefix(entry.Name(), ".bookforge-") && strings.HasSuffix(entry.Name(), ".tmp") {
			if err := add(filepath.Join(s.StateDir, entry.Name())); err != nil {
				return nil, err
			}
		}
	}
	for _, dir := range []string{s.ChaptersDir, s.state("snapshots"), s.state("audit")} {
		entries, err := os.ReadDir(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			name := entry.Name()
			_, numbered := artifactGeneration(dir, name, s)
			if numbered || (strings.HasPrefix(name, ".bookforge-") && strings.HasSuffix(name, ".tmp")) {
				if err := add(filepath.Join(dir, name)); err != nil {
					return nil, err
				}
			}
		}
	}
	for _, dir := range []string{s.state("invalid-responses"), s.state("archive")} {
		err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			return add(path)
		})
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// ClearManaged removes only BookForge-managed files, preserving user documents and directories.
func (s *FileStore) ClearManaged() error {
	if err := os.Remove(s.state("manifest.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(s.state("log.jsonl")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	rootEntries, err := os.ReadDir(s.StateDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, entry := range rootEntries {
		if strings.HasPrefix(entry.Name(), ".bookforge-") && strings.HasSuffix(entry.Name(), ".tmp") {
			if err := os.RemoveAll(filepath.Join(s.StateDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	for _, dir := range []string{s.state("invalid-responses"), s.state("archive")} {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
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
			name := entry.Name()
			_, numbered := artifactGeneration(dir, name, s)
			managedTemp := strings.HasPrefix(name, ".bookforge-") && strings.HasSuffix(name, ".tmp")
			if numbered || managedTemp {
				if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ReadLogEntries decodes valid JSONL entries and reports the first malformed line.
func (s *FileStore) ReadLogEntries() ([]LogEntry, error) {
	text, err := s.ReadLog()
	if err != nil {
		return nil, err
	}
	var entries []LogEntry
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		var entry LogEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			return entries, fmt.Errorf("decode operation log: %w", err)
		}
		entries = append(entries, entry)
	}
	return entries, scanner.Err()
}
