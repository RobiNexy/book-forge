package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/RobiNexy/book-forge/internal/domain"
	"github.com/RobiNexy/book-forge/internal/outline"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type AuditRecord struct {
	N            int               `json:"n"`
	StartedAt    time.Time         `json:"started_at"`
	FinishedAt   time.Time         `json:"finished_at"`
	Model        string            `json:"model"`
	PromptHash   string            `json:"prompt_hash"`
	ResponseHash string            `json:"response_hash"`
	Usage        any               `json:"usage,omitempty"`
	Ops          domain.ChapterOps `json:"ops"`
	ReviewedBy   string            `json:"reviewed_by"`
}
type Store interface {
	LoadOutline(string) (outline.Outline, error)
	LoadInitialHooks(string) ([]domain.Hook, error)
	LoadSnapshot(int) (domain.HookPool, error)
	SaveSnapshot(domain.HookPool) error
	LatestSnapshotN() (int, error)
	SaveChapter(int, string) error
	LoadChapter(int) (string, error)
	HasChapter(int) bool
	SaveAudit(int, AuditRecord, []byte, string, bool) error
	TruncateFrom(int) error
	Acquire() (func(), error)
	Root() string
}
type FileStore struct{ Dir string }

func New(dir string) *FileStore   { return &FileStore{Dir: dir} }
func (s *FileStore) Root() string { return s.Dir }
func (s *FileStore) bf(p ...string) string {
	return filepath.Join(append([]string{s.Dir, ".bookforge"}, p...)...)
}
func (s *FileStore) chapters(p ...string) string {
	return filepath.Join(append([]string{s.Dir, "chapters"}, p...)...)
}
func (s *FileStore) LoadOutline(name string) (outline.Outline, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil {
		return outline.Outline{}, err
	}
	return outline.Parse(strings.NewReader(string(b)))
}
func (s *FileStore) LoadInitialHooks(name string) ([]domain.Hook, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, name))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []domain.Hook
	seq := 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		typ := domain.HookCallback
		for _, t := range []domain.HookType{domain.HookFact, domain.HookTension, domain.HookCallback} {
			prefix := string(t) + ":"
			if strings.HasPrefix(strings.ToLower(line), prefix) {
				typ = t
				line = strings.TrimSpace(line[len(prefix):])
			}
		}
		seq++
		out = append(out, domain.Hook{ID: fmt.Sprintf("h_000_%02d", seq), Src: 0, Type: typ, Content: line})
	}
	return out, nil
}
func (s *FileStore) LoadSnapshot(n int) (domain.HookPool, error) {
	b, err := os.ReadFile(s.bf("state", fmt.Sprintf("H_%d.json", n)))
	if err != nil {
		return domain.HookPool{}, err
	}
	var p domain.HookPool
	err = json.Unmarshal(b, &p)
	return p, err
}
func (s *FileStore) SaveSnapshot(p domain.HookPool) error {
	return atomicJSON(s.bf("state", fmt.Sprintf("H_%d.json", p.N)), p)
}
func (s *FileStore) LatestSnapshotN() (int, error) {
	es, err := os.ReadDir(s.bf("state"))
	if err != nil {
		return 0, err
	}
	max := 0
	found := false
	for _, e := range es {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "H_%d.json", &n); err == nil && n > max {
			max = n
			found = true
		} else if e.Name() == "H_0.json" {
			found = true
		}
	}
	if !found {
		return 0, os.ErrNotExist
	}
	return max, nil
}
func (s *FileStore) filename(n int) string {
	return filepath.Join(s.chapters(fmt.Sprintf("%02d.qmd", n)))
}
func (s *FileStore) SaveChapter(n int, content string) error {
	return atomicBytes(s.filename(n), []byte(content))
}
func (s *FileStore) LoadChapter(n int) (string, error) {
	b, err := os.ReadFile(s.filename(n))
	return string(b), err
}
func (s *FileStore) HasChapter(n int) bool { _, err := os.Stat(s.filename(n)); return err == nil }
func (s *FileStore) SaveAudit(n int, r AuditRecord, prompt []byte, response string, full bool) error {
	dir := s.bf("audit", fmt.Sprintf("ch_%02d", n))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if full {
		if err := atomicBytes(filepath.Join(dir, "prompt.json"), prompt); err != nil {
			return err
		}
		if err := atomicBytes(filepath.Join(dir, "response.txt"), []byte(response)); err != nil {
			return err
		}
	}
	return atomicJSON(filepath.Join(dir, "ops.json"), r)
}
func (s *FileStore) TruncateFrom(from int) error {
	for _, d := range []string{s.bf("state"), s.bf("audit")} {
		es, _ := os.ReadDir(d)
		for _, e := range es {
			var n int
			if _, err := fmt.Sscanf(e.Name(), "H_%d.json", &n); err != nil {
				_, _ = fmt.Sscanf(e.Name(), "ch_%d", &n)
			}
			if n >= from {
				_ = os.RemoveAll(filepath.Join(d, e.Name()))
			}
		}
	}
	es, _ := os.ReadDir(s.chapters())
	for _, e := range es {
		n, _ := strconv.Atoi(strings.TrimSuffix(e.Name(), ".qmd"))
		if n >= from {
			_ = os.Remove(filepath.Join(s.chapters(), e.Name()))
		}
	}
	return nil
}
func (s *FileStore) Acquire() (func(), error) {
	if err := os.MkdirAll(s.bf(), 0755); err != nil {
		return nil, err
	}
	p := s.bf("lock")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("project is locked")
		}
		return nil, err
	}
	_, _ = f.WriteString(fmt.Sprintf("pid=%d\nstarted=%s\n", os.Getpid(), time.Now().Format(time.RFC3339)))
	_ = f.Close()
	return func() { _ = os.Remove(p) }, nil
}
func atomicBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func atomicJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return atomicBytes(path, append(b, '\n'))
}
