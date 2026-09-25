package orchestrator

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestChapterSummaryShowsCommitAndOnlyFirstThirtyLines(t *testing.T) {
	var output bytes.Buffer
	var chapter strings.Builder
	for line := 1; line <= 33; line++ {
		fmt.Fprintf(&chapter, "line %02d\n", line)
	}
	writeChapterSummary(&output, 7, "chapters/007.qmd", chapter.String(), "next hooks", false)
	text := output.String()
	if !strings.Contains(text, "第 007 章已提交") || !strings.Contains(text, "chapters/007.qmd") || !strings.Contains(text, "line 01") || !strings.Contains(text, "line 30") {
		t.Fatalf("summary missing expected content: %q", text)
	}
	if strings.Contains(text, "line 31") || !strings.Contains(text, "其余 3 行未显示") || !strings.Contains(text, "继续生成") {
		t.Fatalf("summary line limit/state incorrect: %q", text)
	}
}
