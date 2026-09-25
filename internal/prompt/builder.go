// Package prompt combines project writing instructions, long-book rules, the fixed protocol,
// and a single user prompt containing the two opaque text inputs.
package prompt

import (
	"strings"

	"github.com/RobiNexy/book-forge/internal/llm"
)

const protocol = `固定输出协议：每次只能输出一章。非最后一章输出正文、换行、<<<BOOKFORGE_HOOKS>>>、换行、下一轮完整钩子文本。最后一章输出正文、换行、<<<END_OF_BOOK>>>。两个标记互斥，禁止同时出现。不要在结束标记后输出内容。`

// Build emits three system instruction sections and one user message. Outline and hook bytes
// remain unchanged within their labeled sections; generation sequence is deliberately omitted.
func Build(style, longBookRules, outline, currentHooks string) []llm.Message {
	system := strings.Join([]string{style, longBookRules, protocol}, "\n\n")
	user := "以下是一份大纲：\n" + outline + "\n\n以下是前次输出章节的钩子：\n" + currentHooks + "\n\n按要求输出并只输出一章新章节。"
	return []llm.Message{{Role: "system", Content: system}, {Role: "user", Content: user}}
}
