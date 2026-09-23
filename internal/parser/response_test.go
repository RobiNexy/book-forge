package parser

import "testing"

func TestParseStrict(t *testing.T) {
	r, err := Parse("<<<CHAPTER>>>\nhello\n<<<HOOKS>>>\n{\"added\":[{\"type\":\"fact\",\"content\":\"x\"}]}\n<<<END>>>", 2)
	if err != nil || r.Ops.Added[0].ID != "h_002_01" {
		t.Fatalf("%#v %v", r, err)
	}
}
func TestParseUnknownField(t *testing.T) {
	_, err := Parse("<<<CHAPTER>>>x<<<HOOKS>>>{\"bad\":1}<<<END>>>", 1)
	if err == nil {
		t.Fatal("expected strict error")
	}
}
