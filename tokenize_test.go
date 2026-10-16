package spamtokens

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fixture is one case of testdata/fixtures.json. Expected is the bag under
// DefaultOptions; ExpectedLegacy, present only where the two differ, is the bag
// with ScriptBigrams false; ExpectedStream, where present, is CleanText under
// DefaultOptions split on spaces. Since names the first version whose rules
// produce Expected, on a case v0.1.2 answered differently.
type fixture struct {
	Name           string         `json:"name"`
	Since          string         `json:"since"`
	Input          string         `json:"input"`
	Expected       map[string]int `json:"expected"`
	ExpectedLegacy map[string]int `json:"expected_legacy"`
	ExpectedStream []string       `json:"expected_stream"`
}

func legacyOptions() Options {
	opts := DefaultOptions()
	opts.ScriptBigrams = false
	return opts
}

// bag drops zero counts and never returns nil, so maps compare cleanly.
func bag(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		if v != 0 {
			out[k] = v
		}
	}
	return out
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
	for _, fx := range loadFixtures(t) {
		t.Run(fx.Name, func(t *testing.T) {
			if fx.ExpectedLegacy != nil && maps.Equal(bag(fx.ExpectedLegacy), bag(fx.Expected)) {
				t.Fatalf("expected_legacy equals expected; drop it")
			}
			legacy := fx.ExpectedLegacy
			if legacy == nil {
				legacy = fx.Expected
			}
			for _, setting := range []struct {
				name string
				opts Options
				want map[string]int
			}{
				{"default", DefaultOptions(), fx.Expected},
				{"legacy", legacyOptions(), legacy},
			} {
				got := bag(Tokenize(fx.Input, setting.opts))
				if want := bag(setting.want); !maps.Equal(got, want) {
					t.Errorf("%s: Tokenize(%q)\n got:  %v\n want: %v", setting.name, fx.Input, got, want)
				}
			}
			if fx.ExpectedStream != nil {
				got := strings.Fields(CleanText(fx.Input, DefaultOptions()))
				if !slices.Equal(got, fx.ExpectedStream) {
					t.Errorf("CleanText(%q)\n got:  %q\n want: %q", fx.Input, got, fx.ExpectedStream)
				}
			}
		})
	}
}

// TestFixturesDeterministic runs every fixture 50 times under both settings and
// requires the same bag and the same stream each time (INV-TOK-8).
func TestFixturesDeterministic(t *testing.T) {
	for _, fx := range loadFixtures(t) {
		for _, opts := range []Options{DefaultOptions(), legacyOptions()} {
			firstBag := Tokenize(fx.Input, opts)
			firstStream := CleanText(fx.Input, opts)
			for range 50 {
				if got := Tokenize(fx.Input, opts); !maps.Equal(got, firstBag) {
					t.Fatalf("%s: Tokenize not deterministic: %v vs %v", fx.Name, got, firstBag)
				}
				if got := CleanText(fx.Input, opts); got != firstStream {
					t.Fatalf("%s: CleanText not deterministic: %q vs %q", fx.Name, got, firstStream)
				}
			}
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
	if !o.KeepHTMLTags || !o.KeepURLs || !o.KeepScriptFlags || !o.ScriptBigrams {
		t.Errorf("expected KeepHTMLTags, KeepURLs, KeepScriptFlags, ScriptBigrams to default true: %+v", o)
	}
}

func TestKeepScriptFlagsFalse(t *testing.T) {
	opts := DefaultOptions()
	opts.KeepScriptFlags = false
	got := Tokenize("我喜欢这个 hello world สวัสดี", opts)
	for _, banned := range []string{"script_han", "script_hiragana", "script_katakana", "script_hangul", "script_thai"} {
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

func TestURLTokensRespectMaxTokenRunes(t *testing.T) {
	opts := DefaultOptions()
	longSegment := strings.Repeat("a", opts.MaxTokenRunes+1)
	got := Tokenize("visit https://example.com/"+longSegment+"?ok=short", opts)

	if _, ok := got[longSegment]; ok {
		t.Fatalf("URL segment over MaxTokenRunes should not be emitted: %v", got)
	}
	if got["example.com"] != 1 || got["example"] != 1 || got["com"] != 1 || got["short"] != 1 {
		t.Fatalf("valid URL tokens should still be emitted: %v", got)
	}
	for tok := range got {
		if len(tok) > 255 {
			t.Fatalf("token %q has %d bytes, exceeds spam_tokens.token storage limit", tok, len(tok))
		}
	}
}
