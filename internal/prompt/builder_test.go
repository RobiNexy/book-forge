package prompt

import (
	"strings"
	"testing"
)

func TestBuildComposesThreeSystemSectionsAndOneOpaqueUserPrompt(t *testing.T) {
	style := "STYLE RULES"
	longRules := "LONG BOOK RULES"
	outline := "## arbitrary outline\n"
	hooks := "unstructured hook text\n"
	messages := Build(style, longRules, outline, hooks)
	if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("messages = %#v", messages)
	}
	system := messages[0].Content
	styleAt := strings.Index(system, style)
	rulesAt := strings.Index(system, longRules)
	protocolAt := strings.Index(system, "<<<BOOKFORGE_HOOKS>>>")
	endAt := strings.Index(system, "<<<END_OF_BOOK>>>")
	if styleAt < 0 || rulesAt <= styleAt || protocolAt <= rulesAt || endAt <= protocolAt {
		t.Fatalf("system sections are missing or out of order: %q", system)
	}
	user := messages[1].Content
	outlineAt := strings.Index(user, outline)
	hooksAt := strings.Index(user, hooks)
	if outlineAt < 0 || hooksAt <= outlineAt || strings.Contains(user, "generation number") {
		t.Fatalf("user prompt has wrong order or contains sequence: %q", user)
	}
}
