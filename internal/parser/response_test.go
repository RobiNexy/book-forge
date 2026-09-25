package parser

import "testing"

func TestSplitPreservesOpaqueHookText(t *testing.T) {
	raw := "  Chapter prose.  \n<<<BOOKFORGE_HOOKS>>>\n# Arbitrary hooks\n- Item one\n{{not JSON}} \n"
	got, err := Split(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Chapter != "  Chapter prose.  \n" || got.Hooks != "\n# Arbitrary hooks\n- Item one\n{{not JSON}} \n" || got.EndOfBook {
		t.Fatalf("split result = %#v", got)
	}
}

func TestSplitRecognizesExclusiveEndMarker(t *testing.T) {
	got, err := Split("last chapter\n<<<END_OF_BOOK>>>\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Chapter != "last chapter\n" || got.Hooks != "" || !got.EndOfBook {
		t.Fatalf("end result = %#v", got)
	}
}

func TestSplitRejectsMissingRepeatedOrConflictingMarkers(t *testing.T) {
	tests := []string{
		"prose without delimiter",
		"a<<<BOOKFORGE_HOOKS>>>hooks<<<BOOKFORGE_HOOKS>>>again",
		"a<<<END_OF_BOOK>>>b<<<END_OF_BOOK>>>",
		"body<<<BOOKFORGE_HOOKS>>>hooks<<<END_OF_BOOK>>>",
		"body<<<END_OF_BOOK>>>hooks<<<BOOKFORGE_HOOKS>>>",
		"<<<BOOKFORGE_HOOKS>>>hooks",
		"prose<<<BOOKFORGE_HOOKS>>>",
		"<<<END_OF_BOOK>>>",
	}
	for _, raw := range tests {
		if _, err := Split(raw); err == nil {
			t.Errorf("Split(%q) unexpectedly succeeded", raw)
		}
	}
}
