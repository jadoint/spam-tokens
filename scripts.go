package spamtokens

import "unicode"

// scriptClass is one of the scripts written without spaces between words, which
// the tokenizer cuts into character bigrams instead of splitting on whitespace.
// testdata/scripts.json records each class as code-point ranges, for the ports.
type scriptClass uint8

const (
	classNone scriptClass = iota
	classHan
	classKana // Hiragana ∪ Katakana ∪ U+30FC
	classHangul
	classThai
)

// prolongedSoundMark is U+30FC (ー). Unicode files it under the Common script
// because Hiragana and Katakana share it, so neither table holds it, but it sits
// mid-word in Kana (コーヒー); the Kana class adds it.
const prolongedSoundMark = 'ー'

// classOf names the script class of r, or classNone.
func classOf(r rune) scriptClass {
	if r < 'ก' { // no class has a rune below Thai's first
		return classNone
	}
	switch {
	case unicode.Is(unicode.Thai, r):
		return classThai
	case unicode.Is(unicode.Hangul, r):
		return classHangul
	case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), r == prolongedSoundMark:
		return classKana
	case unicode.Is(unicode.Han, r):
		return classHan
	}
	return classNone
}

// emitScriptFlags appends one "script_<name>" token per script present in
// text, in the fixed order han, hiragana, katakana, hangul, thai. The flags
// follow Unicode's script tables, so U+30FC on its own raises none.
func emitScriptFlags(text string, out *[]string) {
	var hasHan, hasHiragana, hasKatakana, hasHangul, hasThai bool
	for _, r := range text {
		if r < 'ก' {
			continue
		}
		switch {
		case unicode.Is(unicode.Han, r):
			hasHan = true
		case unicode.Is(unicode.Hiragana, r):
			hasHiragana = true
		case unicode.Is(unicode.Katakana, r):
			hasKatakana = true
		case unicode.Is(unicode.Hangul, r):
			hasHangul = true
		case unicode.Is(unicode.Thai, r):
			hasThai = true
		}
	}
	for _, f := range []struct {
		has  bool
		name string
	}{
		{hasHan, "script_han"},
		{hasHiragana, "script_hiragana"},
		{hasKatakana, "script_katakana"},
		{hasHangul, "script_hangul"},
		{hasThai, "script_thai"},
	} {
		if f.has {
			*out = append(*out, f.name)
		}
	}
}

// emitRun appends the tokens of one run of class c: its n-1 overlapping
// bigrams in position order, preceded for Hangul, which does space its words,
// by the whole run when it is 2..MaxTokenRunes runes long. A run of one rune
// emits nothing. For a two-rune Hangul word the word and its one bigram are the
// same string, so it counts twice.
func emitRun(run []rune, c scriptClass, out *[]string, opts Options) {
	n := len(run)
	if c == classHangul && n >= 2 && n <= opts.MaxTokenRunes {
		*out = append(*out, string(run))
	}
	for i := range n - 1 {
		*out = append(*out, string(run[i:i+2]))
	}
}
