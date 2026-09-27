package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestInitTemplateOption(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "book")
	if code := run([]string{"init", "--template", "d", dir}); code != 0 {
		t.Fatalf("init exit code = %d", code)
	}
	data, err := os.ReadFile(filepath.Join(dir, "_quarto.yml"))
	if err != nil || !strings.Contains(string(data), "IBM Plex Sans") {
		t.Fatalf("modern template not selected: %v", err)
	}
	designDir := filepath.Join(t.TempDir(), "jade")
	if code := run([]string{"init", "--template", "b", "--design", "jade", designDir}); code != 0 {
		t.Fatalf("init with layered design exit code = %d", code)
	}
	designData, err := os.ReadFile(filepath.Join(designDir, "_quarto.yml"))
	if err != nil || !strings.Contains(string(designData), "bookseal") || !strings.Contains(string(designData), `\setCJKmainfont{`) {
		t.Fatalf("selected design/font combination missing: %v", err)
	}
	directDir := filepath.Join(t.TempDir(), "nocturne")
	if code := run([]string{"init", "--template", "nocturne", directDir}); code != 0 {
		t.Fatalf("direct designer preset exit code = %d", code)
	}
	directData, err := os.ReadFile(filepath.Join(directDir, "_quarto.yml"))
	if err != nil || !strings.Contains(string(directData), "booknight") || !strings.Contains(string(directData), "FandolSong-Regular.otf") {
		t.Fatalf("direct preset did not select portable font design: %v", err)
	}
	if code := run([]string{"init", "--template", "invalid", filepath.Join(t.TempDir(), "invalid")}); code != 64 {
		t.Fatalf("invalid template exit code = %d", code)
	}
	if code := run([]string{"init", "--design", "invalid", filepath.Join(t.TempDir(), "invalid-design")}); code != 64 {
		t.Fatalf("invalid design exit code = %d", code)
	}
}
