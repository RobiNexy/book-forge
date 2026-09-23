package outline

import "strings"
import "testing"

func TestParsePrefersLevelTwo(t *testing.T) {
	o, err := Parse(strings.NewReader("# Book\n## One\ntext\n## Two\n"))
	if err != nil || len(o.Chapters) != 2 || o.Chapters[0].Title != "One" {
		t.Fatalf("%#v %v", o, err)
	}
}
func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse(strings.NewReader("plain")); err == nil {
		t.Fatal("expected error")
	}
}
