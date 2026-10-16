package spamtokens

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unicode"
)

var update = flag.Bool("update", false, "rewrite testdata/scripts.json from Go's unicode tables")

// scriptsFileOrder is the key order of testdata/scripts.json.
var scriptsFileOrder = []struct {
	key   string
	class scriptClass
}{
	{"han", classHan},
	{"kana", classKana},
	{"hangul", classHangul},
	{"thai", classThai},
}

// classRanges walks every code point once and returns each class as sorted,
// inclusive [lo, hi] ranges, read through classOf so the file records exactly
// what the tokenizer does.
func classRanges() map[string][][2]rune {
	out := make(map[string][][2]rune, len(scriptsFileOrder))
	for _, s := range scriptsFileOrder {
		var ranges [][2]rune
		for r := rune(0); r <= unicode.MaxRune; r++ {
			if classOf(r) != s.class {
				continue
			}
			if n := len(ranges); n > 0 && ranges[n-1][1] == r-1 {
				ranges[n-1][1] = r
			} else {
				ranges = append(ranges, [2]rune{r, r})
			}
		}
		out[s.key] = ranges
	}
	return out
}

// encodeScripts writes the file one range per line, keys in scriptsFileOrder.
func encodeScripts(ranges map[string][][2]rune) []byte {
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, s := range scriptsFileOrder {
		fmt.Fprintf(&b, "  %q: [\n", s.key)
		rs := ranges[s.key]
		for j, r := range rs {
			sep := ","
			if j == len(rs)-1 {
				sep = ""
			}
			fmt.Fprintf(&b, "    [%d, %d]%s\n", r[0], r[1], sep)
		}
		sep := ","
		if i == len(scriptsFileOrder)-1 {
			sep = ""
		}
		fmt.Fprintf(&b, "  ]%s\n", sep)
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// TestScriptsFile holds testdata/scripts.json equal to the script classes this
// build of Go sees. A Go release with a newer Unicode table fails here until the
// file is regenerated with `go test -run TestScriptsFile -update`, and the ports
// that embed the file then follow it.
func TestScriptsFile(t *testing.T) {
	path := filepath.Join("testdata", "scripts.json")
	want := classRanges()
	if *update {
		if err := os.WriteFile(path, encodeScripts(want), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var got map[string][][2]rune
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s has %d classes, want %d (unicode %s); regenerate with -update", path, len(got), len(want), unicode.Version)
	}
	for _, s := range scriptsFileOrder {
		if !slices.Equal(got[s.key], want[s.key]) {
			t.Errorf("%s %q differs from Go's unicode %s tables; regenerate with -update", path, s.key, unicode.Version)
		}
	}
}

// TestClassOf pins the class edges the tokenizer depends on.
func TestClassOf(t *testing.T) {
	cases := []struct {
		r    rune
		want scriptClass
	}{
		{'a', classNone},
		{'я', classNone},
		{'我', classHan},
		{'々', classHan}, // U+3005 iteration mark is Han
		{'の', classKana},
		{'カ', classKana},
		{prolongedSoundMark, classKana},
		{'・', classNone}, // U+30FB middle dot is Common, a separator
		{'한', classHangul},
		{'ㄱ', classHangul}, // compatibility jamo
		{'ส', classThai},
		{'\u0E31', classThai}, // combining mai han-akat
		{'\u0E48', classThai}, // combining tone mark mai ek
		{'฿', classNone},      // U+0E3F baht sign is Common
		{'。', classNone},
		{'🔥', classNone},
	}
	for _, c := range cases {
		if got := classOf(c.r); got != c.want {
			t.Errorf("classOf(%U) = %d, want %d", c.r, got, c.want)
		}
	}
}
