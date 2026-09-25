package orchestrator

import (
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	previewLineLimit = 30
	progressWidth    = 88
)

func writeGenerationRequest(out io.Writer, generation, attempt, retryLimit int, model string) {
	fmt.Fprintln(out)
	writeBoxTop(out)
	writeBoxLine(out, fmt.Sprintf("正在生成第 %03d 章", generation))
	writeBoxLine(out, "模型："+model)
	if attempt > 1 {
		writeBoxLine(out, fmt.Sprintf("本次请求：第 %d 次（无效输出额外重试上限 %d 次）", attempt, retryLimit))
	}
	writeBoxBottom(out)
}

func writeInvalidResponse(out io.Writer, generation, attempt int, path, response string) {
	fmt.Fprintln(out)
	writeBoxTop(out)
	writeBoxLine(out, fmt.Sprintf("⚠ 第 %03d 章输出格式无效（第 %d 次请求）", generation, attempt))
	writeBoxLine(out, "原始响应已保存："+path)
	writeBoxSection(out, "无效响应原文")
	for _, line := range strings.Split(strings.TrimRight(response, "\r\n"), "\n") {
		fmt.Fprintln(out, line)
	}
	writeBoxBottom(out)
}

func writeChapterSummary(out io.Writer, generation int, path, chapter, hooks string, finished bool) {
	state := "继续生成"
	if finished {
		state = "全书完成"
	}
	fmt.Fprintln(out)
	writeBoxTop(out)
	writeBoxLine(out, fmt.Sprintf("✓ 第 %03d 章已提交", generation))
	writeBoxLine(out, "文件："+path)
	writeBoxLine(out, fmt.Sprintf("正文：%d 字符　钩子：%d 字符　状态：%s", utf8.RuneCountInString(chapter), utf8.RuneCountInString(hooks), state))
	writeBoxSection(out, "正文预览（最多 30 行）")
	lines := strings.Split(strings.TrimSpace(chapter), "\n")
	shown := len(lines)
	if shown > previewLineLimit {
		shown = previewLineLimit
	}
	for _, line := range lines[:shown] {
		line = strings.TrimRight(line, "\r")
		writeBoxLine(out, line)
	}
	if len(lines) > shown {
		writeBoxLine(out, fmt.Sprintf("……（其余 %d 行未显示）", len(lines)-shown))
	}
	writeBoxBottom(out)
}

func writeBoxTop(out io.Writer) {
	fmt.Fprintf(out, "╭%s╮\n", strings.Repeat("─", progressWidth-2))
}

func writeBoxBottom(out io.Writer) {
	fmt.Fprintf(out, "╰%s╯\n", strings.Repeat("─", progressWidth-2))
}

func writeBoxSection(out io.Writer, title string) {
	title = truncateDisplay(title, progressWidth-7)
	remaining := progressWidth - 5 - displayWidth(title)
	if remaining < 0 {
		remaining = 0
	}
	fmt.Fprintf(out, "├─ %s %s┤\n", title, strings.Repeat("─", remaining))
}

func writeBoxLine(out io.Writer, text string) {
	capacity := progressWidth - 4
	text = truncateDisplay(text, capacity)
	padding := capacity - displayWidth(text)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(out, "│ %s%s │\n", text, strings.Repeat(" ", padding))
}

func truncateDisplay(text string, limit int) string {
	var out strings.Builder
	width := 0
	for _, r := range text {
		runeWidth := displayRuneWidth(r)
		if width+runeWidth > limit {
			if width < limit {
				out.WriteRune('…')
			}
			return out.String()
		}
		out.WriteRune(r)
		width += runeWidth
	}
	return out.String()
}

func displayWidth(text string) int {
	width := 0
	for _, r := range text {
		width += displayRuneWidth(r)
	}
	return width
}

func displayRuneWidth(r rune) int {
	if unicode.Is(unicode.Mn, r) || r == '\u200d' || (r >= 0xfe00 && r <= 0xfe0f) {
		return 0
	}
	if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || (r >= 0xff01 && r <= 0xff60) || (r >= 0x1f300 && r <= 0x1faff) {
		return 2
	}
	return 1
}
