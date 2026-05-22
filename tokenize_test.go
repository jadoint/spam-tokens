package spamtokens

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	Name     string         `json:"name"`
	Input    string         `json:"input"`
	Expected map[string]int `json:"expected"`
}

func loadFixtures(t *testing.T) []fixture {
	t.Helper()
	path := filepath.Join("testdata", "fixtures.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var fx []fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	return fx
}

func TestTokenizeFixtures(t *testing.T) {
	opts := DefaultOptions()
	for _, fx := range loadFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			got := Tokenize(fx.Input, opts)
			// Drop zero-count entries from got to compare cleanly.
			for k, v := range got {
				if v == 0 {
					delete(got, k)
				}
			}
			want := fx.Expected
			if want == nil {
				want = map[string]int{}
			}
			if !maps.Equal(got, want) {
				t.Fatalf("Tokenize(%q)\n got:  %v\n want: %v", fx.Input, got, want)
			}
		})
	}
}

func TestCleanTextDeterministic(t *testing.T) {
	opts := DefaultOptions()
	in := "Hello WORLD hello world"
	a := CleanText(in, opts)
	b := CleanText(in, opts)
	if a != b {
		t.Fatalf("CleanText is not deterministic: %q vs %q", a, b)
	}
	// Should contain the lowercased tokens at minimum.
	for _, want := range []string{"hello", "world"} {
		if !strings.Contains(a, want) {
			t.Errorf("CleanText output %q missing token %q", a, want)
		}
	}
}

func TestCleanTextRoundTripsCounts(t *testing.T) {
	opts := DefaultOptions()
	in := "spam spam spam wonderful spam"
	stream := CleanText(in, opts)
	// Re-tokenize the stream and confirm counts are preserved.
	got := Tokenize(stream, opts)
	if got["spam"] != 4 || got["wonderful"] != 1 {
		t.Fatalf("counts not preserved through CleanText: %v", got)
	}
}

func TestEmptyInput(t *testing.T) {
	out := Tokenize("", DefaultOptions())
	if out == nil {
		t.Fatal("Tokenize must return a non-nil map for empty input")
	}
	if len(out) != 0 {
		t.Fatalf("Tokenize(\"\") = %v, want empty", out)
	}
}

func TestDefaultOptions(t *testing.T) {
	o := DefaultOptions()
	if o.MinTokenRunes != 3 || o.MaxTokenRunes != 40 {
		t.Errorf("unexpected size defaults: %+v", o)
	}
	if !o.KeepHTMLTags || !o.KeepURLs || !o.KeepScriptFlags {
		t.Errorf("expected KeepHTMLTags, KeepURLs, KeepScriptFlags to default true: %+v", o)
	}
}

func TestKeepScriptFlagsFalse(t *testing.T) {
	opts := DefaultOptions()
	opts.KeepScriptFlags = false
	got := Tokenize("我喜欢这个 hello world", opts)
	for _, banned := range []string{"script_han", "script_hiragana", "script_katakana", "script_hangul"} {
		if _, ok := got[banned]; ok {
			t.Errorf("token %q should not be emitted when KeepScriptFlags=false: %v", banned, got)
		}
	}
	if got["hello"] != 1 || got["world"] != 1 {
		t.Errorf("Latin words should still tokenize: %v", got)
	}
}

func TestKeepHTMLTagsFalse(t *testing.T) {
	opts := DefaultOptions()
	opts.KeepHTMLTags = false
	got := Tokenize("<b>hello world</b>", opts)
	if _, ok := got["tag_b"]; ok {
		t.Errorf("tag_b should not be emitted when KeepHTMLTags=false: %v", got)
	}
	if got["hello"] != 1 || got["world"] != 1 {
		t.Errorf("body words should still be tokenized: %v", got)
	}
}

func TestKeepURLsFalse(t *testing.T) {
	opts := DefaultOptions()
	opts.KeepURLs = false
	got := Tokenize("visit http://shady.biz/buy now", opts)
	for _, banned := range []string{"shady.biz", "shady", "biz", "buy"} {
		if _, ok := got[banned]; ok {
			t.Errorf("token %q should not be emitted when KeepURLs=false: %v", banned, got)
		}
	}
	if got["visit"] != 1 || got["now"] != 1 {
		t.Errorf("non-URL words should still tokenize: %v", got)
	}
}
