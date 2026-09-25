// Package parser splits model output at the one protocol boundary and never interprets either block.
package parser

import (
	"fmt"
	"strings"
)

const (
	HooksDelimiter     = "<<<BOOKFORGE_HOOKS>>>"
	EndOfBookDelimiter = "<<<END_OF_BOOK>>>"
)

// Result contains the two opaque text blocks returned by the model.
type Result struct {
	Chapter   string
	Hooks     string
	EndOfBook bool
}

// Split accepts exactly one of the hook delimiter or end marker. It returns raw text sections,
// validating only ambiguity and the required non-empty body/hook block.
func Split(raw string) (Result, error) {
	hooksCount := strings.Count(raw, HooksDelimiter)
	endCount := strings.Count(raw, EndOfBookDelimiter)
	if hooksCount+endCount != 1 {
		return Result{}, fmt.Errorf("response must contain exactly one of %s or %s", HooksDelimiter, EndOfBookDelimiter)
	}
	if hooksCount == 1 {
		chapter, hooks, _ := strings.Cut(raw, HooksDelimiter)
		if strings.TrimSpace(chapter) == "" {
			return Result{}, fmt.Errorf("chapter text before delimiter is empty")
		}
		if strings.TrimSpace(hooks) == "" {
			return Result{}, fmt.Errorf("hook text after delimiter is empty")
		}
		return Result{Chapter: chapter, Hooks: hooks}, nil
	}
	chapter, trailing, _ := strings.Cut(raw, EndOfBookDelimiter)
	if strings.TrimSpace(chapter) == "" {
		return Result{}, fmt.Errorf("chapter text before end marker is empty")
	}
	if strings.TrimSpace(trailing) != "" {
		return Result{}, fmt.Errorf("text after end-of-book marker is not allowed")
	}
	return Result{Chapter: chapter, EndOfBook: true}, nil
}
