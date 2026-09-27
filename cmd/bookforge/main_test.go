package main

import (
	"bufio"
	"bytes"
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

func TestInitWizardDefaultsAndSelections(t *testing.T) {
	defaultFont, err := chooseInitOption(bufio.NewReader(strings.NewReader("\n")), &bytes.Buffer{}, "font", initFontOptions, 0)
	if err != nil || defaultFont.value != "b" {
		t.Fatalf("default font=%q err=%v", defaultFont.value, err)
	}
	defaultDesign, err := chooseInitOption(bufio.NewReader(strings.NewReader("\n")), &bytes.Buffer{}, "design", initDesignOptions, 0)
	if err != nil || defaultDesign.value != "atelier" {
		t.Fatalf("default design=%q err=%v", defaultDesign.value, err)
	}
	selectedFont, err := chooseInitOption(bufio.NewReader(strings.NewReader("invalid\n2\n")), &bytes.Buffer{}, "font", initFontOptions, 0)
	if err != nil || selectedFont.value != "a" {
		t.Fatalf("retry font=%q err=%v", selectedFont.value, err)
	}
	dir := filepath.Join(t.TempDir(), "book")
	var output bytes.Buffer
	if code := initCommand([]string{dir}, ".", strings.NewReader("2\n5\n"), &output); code != 0 {
		t.Fatalf("init exit code = %d; output=%q", code, output.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, "_quarto.yml"))
	if err != nil || !strings.Contains(string(data), "FandolSong-Regular.otf") || !strings.Contains(string(data), "bookseal") {
		t.Fatalf("chosen portable font and jade design missing: %v", err)
	}
	if !strings.Contains(output.String(), "跨平台基础字体") || !strings.Contains(output.String(), "Jade · 东方青绿") {
		t.Fatalf("selection summary missing: %q", output.String())
	}
	if code := initCommand([]string{"--template", "d"}, ".", strings.NewReader(""), &bytes.Buffer{}); code != 64 {
		t.Fatalf("removed template flag exit code = %d", code)
	}
	if code := initCommand(nil, ".", strings.NewReader(""), &bytes.Buffer{}); code != 2 {
		t.Fatalf("EOF cancellation exit code = %d", code)
	}
}
