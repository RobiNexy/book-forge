package domain

import (
	"errors"
	"testing"
)

func TestApply(t *testing.T) {
	prev := HookPool{Version: "v1", Hooks: []Hook{{ID: "h_000_01", Src: 0, Type: HookFact, Content: "old"}}}
	got, err := Apply(prev, ChapterOps{N: 1, Rewritten: map[string]string{"h_000_01": "new"}, Added: []Hook{{ID: "h_001_01", Src: 1, Type: HookTension, Content: "x"}}})
	if err != nil || len(got.Hooks) != 2 || got.Hooks[0].Content != "new" {
		t.Fatalf("got %#v, %v", got, err)
	}
	_, err = Apply(prev, ChapterOps{N: 1, Discharged: []string{"missing"}})
	if !errors.Is(err, ErrUnknownHookID) {
		t.Fatalf("expected unknown id, got %v", err)
	}
	_, err = Apply(prev, ChapterOps{N: 1, Added: []Hook{{ID: "h_001_01", Src: 2}}})
	if !errors.Is(err, ErrBadSrc) {
		t.Fatalf("expected bad src, got %v", err)
	}
}
