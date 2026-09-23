package domain

import (
	"errors"
	"fmt"
)

var (
	ErrUnknownHookID = errors.New("unknown hook id")
	ErrTypeChanged   = errors.New("hook type changed")
	ErrBadSrc        = errors.New("added hook has wrong source chapter")
)

type ChapterOps struct {
	N          int               `json:"n"`
	Discharged []string          `json:"discharged"`
	Rewritten  map[string]string `json:"rewritten"`
	Added      []Hook            `json:"added"`
}

// Apply is pure: it validates and applies one chapter's operations without I/O.
func Apply(prev HookPool, ops ChapterOps) (HookPool, error) {
	byID := make(map[string]Hook, len(prev.Hooks))
	for _, h := range prev.Hooks {
		byID[h.ID] = h
	}
	done := make(map[string]bool, len(ops.Discharged))
	for _, id := range ops.Discharged {
		if _, ok := byID[id]; !ok {
			return HookPool{}, fmt.Errorf("%w: %s", ErrUnknownHookID, id)
		}
		done[id] = true
	}
	for id, content := range ops.Rewritten {
		h, ok := byID[id]
		if !ok {
			return HookPool{}, fmt.Errorf("%w: %s", ErrUnknownHookID, id)
		}
		if done[id] {
			continue
		}
		h.Content = content
		byID[id] = h
	}
	for _, h := range ops.Added {
		if h.Src != ops.N {
			return HookPool{}, fmt.Errorf("%w: %s", ErrBadSrc, h.ID)
		}
		if h.ID == "" || byID[h.ID].ID != "" {
			return HookPool{}, fmt.Errorf("duplicate or empty hook id: %s", h.ID)
		}
		byID[h.ID] = h
	}
	result := HookPool{N: ops.N, Version: prev.Version, Hooks: make([]Hook, 0, len(byID))}
	for _, h := range prev.Hooks {
		if !done[h.ID] {
			result.Hooks = append(result.Hooks, byID[h.ID])
		}
	}
	result.Hooks = append(result.Hooks, ops.Added...)
	return result, nil
}
