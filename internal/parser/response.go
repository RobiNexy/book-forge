package parser

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/RobiNexy/book-forge/internal/domain"
)

type responseJSON struct {
	Discharged []struct {
		ID string `json:"id"`
	} `json:"discharged"`
	Rewritten []struct {
		ID         string `json:"id"`
		NewContent string `json:"new_content"`
	} `json:"rewritten"`
	Added []struct {
		Type    domain.HookType `json:"type"`
		Content string          `json:"content"`
	} `json:"added"`
}
type Result struct {
	Chapter string
	Ops     domain.ChapterOps
}

// Parse enforces the delimiter protocol and rejects unknown JSON fields.
func Parse(raw string, n int) (Result, error) {
	a := strings.Index(raw, "<<<CHAPTER>>>")
	b := strings.Index(raw, "<<<HOOKS>>>")
	e := strings.LastIndex(raw, "<<<END>>>")
	if a < 0 || b < 0 || e < 0 || !(a < b && b < e) {
		return Result{}, fmt.Errorf("invalid response delimiters")
	}
	chapter := strings.TrimSpace(raw[a+len("<<<CHAPTER>>>") : b])
	payload := strings.TrimSpace(raw[b+len("<<<HOOKS>>>") : e])
	dec := json.NewDecoder(bytes.NewBufferString(payload))
	dec.DisallowUnknownFields()
	var x responseJSON
	if err := dec.Decode(&x); err != nil {
		return Result{}, fmt.Errorf("invalid hooks JSON: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Result{}, fmt.Errorf("hooks JSON contains trailing data")
	}
	ops := domain.ChapterOps{N: n, Rewritten: map[string]string{}}
	for _, h := range x.Discharged {
		if h.ID == "" {
			return Result{}, fmt.Errorf("discharged hook has empty id")
		}
		ops.Discharged = append(ops.Discharged, h.ID)
	}
	for _, h := range x.Rewritten {
		if h.ID == "" {
			return Result{}, fmt.Errorf("rewritten hook has empty id")
		}
		ops.Rewritten[h.ID] = h.NewContent
	}
	for i, h := range x.Added {
		if h.Type == "" || h.Content == "" {
			return Result{}, fmt.Errorf("added hook %d is incomplete", i)
		}
		ops.Added = append(ops.Added, domain.Hook{ID: fmt.Sprintf("h_%03d_%02d", n, i+1), Src: n, Type: h.Type, Content: h.Content})
	}
	return Result{Chapter: chapter, Ops: ops}, nil
}
