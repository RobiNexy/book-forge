package outline

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

type Chapter struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	Heading string `json:"heading"`
}
type Outline struct {
	Markdown string    `json:"markdown"`
	Chapters []Chapter `json:"chapters"`
}

// Parse extracts level-1/2 Markdown headings as chapters. If level-2 headings exist,
// level-1 headings are treated as book sections and are not emitted as chapters.
func Parse(r io.Reader) (Outline, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return Outline{}, err
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	var level2, level1 []Chapter
	s := bufio.NewScanner(strings.NewReader(text))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "#") {
			continue
		}
		i := 0
		for i < len(line) && line[i] == '#' {
			i++
		}
		if i > 2 || i == 0 || i == len(line) || line[i] != ' ' {
			continue
		}
		title := strings.TrimSpace(strings.TrimRight(line[i+1:], "# "))
		if title == "" {
			continue
		}
		c := Chapter{Title: title, Heading: line}
		if i == 2 {
			level2 = append(level2, c)
		} else {
			level1 = append(level1, c)
		}
	}
	chapters := level2
	if len(chapters) == 0 {
		chapters = level1
	}
	if len(chapters) == 0 {
		return Outline{}, fmt.Errorf("outline contains no Markdown chapter headings")
	}
	for i := range chapters {
		chapters[i].Number = i + 1
	}
	return Outline{Markdown: text, Chapters: chapters}, nil
}
