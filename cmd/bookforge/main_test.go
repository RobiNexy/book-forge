package main

import (
	"errors"
	"testing"
)

func TestExtractGlobalOptionsPreservesSubcommandArguments(t *testing.T) {
	project, config := ".", ""
	got, err := extractGlobal([]string{"generate", "--auto", "--project", "./book", "--config=custom.yaml"}, &project, &config)
	if err != nil {
		t.Fatal(err)
	}
	if project != "./book" || config != "custom.yaml" || len(got) != 2 || got[0] != "generate" || got[1] != "--auto" {
		t.Fatalf("remaining=%v project=%q config=%q", got, project, config)
	}
}

func TestClassifyUserAbortAndServiceErrors(t *testing.T) {
	if got := classify(errors.New("chapter 5 review aborted; no state committed")); got != 2 {
		t.Fatalf("review abort exit code = %d", got)
	}
	if got := classify(errors.New("generation 2: OpenAI HTTP 429")); got != 69 {
		t.Fatalf("service error exit code = %d", got)
	}
}

func TestParseRewriteArgsSupportsAutoAfterPositionAndRejectsCount(t *testing.T) {
	generation, auto, err := parseRewriteArgs([]string{"5", "--auto"})
	if err != nil || generation != 5 || !auto {
		t.Fatalf("parsed rewrite = %d %v, err=%v", generation, auto, err)
	}
	if _, _, err := parseRewriteArgs([]string{"5", "--count", "3"}); err == nil {
		t.Fatal("generation count options must be rejected")
	}
}

func TestGenerateRejectsRemovedCountOption(t *testing.T) {
	if code := run([]string{"generate", "--count", "3"}); code != 64 {
		t.Fatalf("generate --count exit code = %d, want usage error", code)
	}
}
