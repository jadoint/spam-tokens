// Package spamtokens is a Unicode-aware tokenizer for spam classification
// shared across the jadoint spam stack (asianfanfics-spam, spam-robinson,
// spam-classifier).
//
// The tokenizer applies the following pipeline, in order:
//
//  1. HTML-unescape the input (e.g. &amp; -> &).
//  2. Detect non-Latin scripts (Han, Hiragana, Katakana, Hangul) and emit a
//     single "script_<name>" token per script present in the document. This
//     captures script-as-signal without paying the cost of per-character
//     tokenization for CJK content.
//  3. Extract HTML tags: emit "tag_<name>" tokens and, for tags with
//     href/src/action/formaction/data-url attributes, feed those values
//     through URL tokenization. Tags are then stripped from the working text.
//  4. Extract URLs (both http(s)://... and bare host.tld[/path] forms): emit
//     the bare domain as a single token and emit each path / query / fragment
//     segment as its own token after splitting on URL delimiters. URLs are
//     then stripped from the working text.
//  5. Replace every remaining rune that is not a letter, digit, or whitespace
//     with a space (Unicode-aware via unicode.IsLetter / unicode.IsDigit /
//     unicode.IsSpace). CJK characters are dropped here (treated as
//     punctuation) since they carry no word-level boundaries and would
//     otherwise explode the vocabulary at storage time.
//  6. Lowercase the working text.
//  7. Collapse runs of identical runes to at most two, except "www" which is
//     preserved.
//  8. Split on whitespace, drop pure-numeric tokens, drop tokens outside
//     [MinTokenRunes, MaxTokenRunes].
package spamtokens

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Options controls tokenizer behavior. Use DefaultOptions for the canonical
// configuration used across the jadoint spam stack.
type Options struct {
	// MinTokenRunes is the minimum rune count for a token (default 3).
	MinTokenRunes int
	// MaxTokenRunes is the maximum rune count for any token (default 40).
	MaxTokenRunes int
	// KeepHTMLTags emits "tag_<name>" tokens for HTML tags. Tags are always
	// stripped from the working text regardless of this flag.
	KeepHTMLTags bool
	// KeepURLs emits domain and path-segment tokens for URLs. URLs are
	// always stripped from the working text regardless of this flag.
	KeepURLs bool
	// KeepScriptFlags emits a single "script_<name>" token per non-Latin
	// script present in the document (script_han, script_hiragana,
	// script_katakana, script_hangul). CJK characters are always dropped
	// from word-level tokenization regardless of this flag.
	KeepScriptFlags bool
}

// DefaultOptions returns the canonical configuration used across the spam stack.
func DefaultOptions() Options {
	return Options{
		MinTokenRunes:   3,
		MaxTokenRunes:   40,
		KeepHTMLTags:    true,
		KeepURLs:        true,
		KeepScriptFlags: true,
	}
}

// Compiled once. Keep these regexes RE2-compatible so the Python port can
// share patterns through testdata fixtures.
var (
	reHTMLTag    = regexp.MustCompile(`</?([a-zA-Z][a-zA-Z0-9]*)([^>]*)>`)
	reURLAttr    = regexp.MustCompile(`(?i)(?:href|src|action|formaction|data-url)\s*=\s*["']([^"']+)["']`)
	reURLScheme  = regexp.MustCompile(`https?://[^\s<>"'\[\]{}|]+`)
	reBareDomain = regexp.MustCompile(`\b[A-Za-z0-9][A-Za-z0-9\-]*(?:\.[A-Za-z0-9][A-Za-z0-9\-]*)+(?:/[^\s<>"'\[\]{}|]*)?`)
	reURLSplit   = regexp.MustCompile(`[./?#&=_+%:\-]+`)
	reAllDigits  = regexp.MustCompile(`^[0-9]+$`)
)

// Tokenize returns a bag-of-words: token -> occurrence count. Empty input
// returns an empty map (not nil).
func Tokenize(text string, opts Options) map[string]int {
	stream := tokenStream(text, opts)
	out := make(map[string]int, len(stream))
	for _, t := range stream {
		out[t]++
	}
	return out
}

// CleanText returns a normalized whitespace-separated stream of tokens. It is
// the canonical preprocessing used to write training CSVs that downstream
// Python classifiers (TF-IDF, n-gram models) consume.
func CleanText(text string, opts Options) string {
	return strings.Join(tokenStream(text, opts), " ")
}

// tokenStream walks text once and emits tokens in the order they are
// encountered (HTML tag tokens first as tags are found, then URL tokens, then
// the remaining prose). The order is deterministic for a given input.
func tokenStream(text string, opts Options) []string {
	if text == "" {
		return nil
	}

	out := make([]string, 0, 32)
	processed := html.UnescapeString(text)

	if opts.KeepScriptFlags {
		emitScriptFlags(processed, &out)
	}

	processed = extractHTMLTags(processed, &out, opts)

	if opts.KeepURLs {
		processed = extractURLs(processed, &out)
	} else {
		processed = reURLScheme.ReplaceAllString(processed, " ")
		processed = reBareDomain.ReplaceAllString(processed, " ")
	}

	processed = sanitize(processed)
	processed = strings.ToLower(processed)
	processed = collapseRepeated(processed)

	for _, word := range strings.Fields(processed) {
		if !isValidToken(word, opts) {
			continue
		}
		out = append(out, word)
	}

	return out
}

// extractHTMLTags emits tag_<name> tokens and feeds <a href> values through
// URL tokenization. The returned string has all tags replaced with spaces.
func extractHTMLTags(text string, out *[]string, opts Options) string {
	matches := reHTMLTag.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	var sb strings.Builder
	sb.Grow(len(text))
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]
		nameStart, nameEnd := m[2], m[3]
		tagName := strings.ToLower(text[nameStart:nameEnd])

		if opts.KeepHTMLTags {
			*out = append(*out, "tag_"+tagName)
		}

		// Mine URL-bearing attributes (href, src, action, etc.) on every tag.
		// URL token emission is gated by KeepURLs, independently of KeepHTMLTags.
		if opts.KeepURLs && m[4] >= 0 && m[5] > m[4] {
			attrs := text[m[4]:m[5]]
			for _, am := range reURLAttr.FindAllStringSubmatch(attrs, -1) {
				if len(am) > 1 {
					tokenizeURL(am[1], out)
				}
			}
		}

		sb.WriteString(text[last:start])
		sb.WriteByte(' ')
		last = end
	}
	sb.WriteString(text[last:])
	return sb.String()
}

// extractURLs emits URL-derived tokens (domain + path/query segments) and
// returns text with the matched URLs replaced by spaces.
func extractURLs(text string, out *[]string) string {
	text = reURLScheme.ReplaceAllStringFunc(text, func(u string) string {
		tokenizeURL(u, out)
		return " "
	})
	text = reBareDomain.ReplaceAllStringFunc(text, func(u string) string {
		tokenizeURL(u, out)
		return " "
	})
	return text
}

// tokenizeURL strips scheme + leading www., emits the bare domain as one
// token, and emits each [./?#&=_+%:-]-split segment as a separate token.
// Pure-numeric and length-1 segments are dropped here so URL noise (ports,
// query timestamps) doesn't pollute the vocabulary.
func tokenizeURL(url string, out *[]string) {
	url = strings.ToLower(url)
	url = strings.TrimSpace(url)
	if idx := strings.Index(url, "://"); idx >= 0 {
		url = url[idx+3:]
	}
	url = strings.TrimPrefix(url, "www.")
	url = strings.Trim(url, "./")
	if url == "" {
		return
	}

	domain := url
	if d, _, ok := strings.Cut(url, "/"); ok {
		domain = d
	}
	if d, _, ok := strings.Cut(domain, ":"); ok {
		domain = d
	}
	if domain != "" && utf8.RuneCountInString(domain) >= 2 {
		*out = append(*out, domain)
	}

	for _, part := range reURLSplit.Split(url, -1) {
		if part == "" || reAllDigits.MatchString(part) {
			continue
		}
		if utf8.RuneCountInString(part) < 3 {
			continue
		}
		*out = append(*out, part)
	}
}

// emitScriptFlags appends a "script_<name>" token for each non-Latin script
// detected in text. At most one token per script is emitted per call, even if
// the script appears many times. CJK detection uses isCJK helpers below.
func emitScriptFlags(text string, out *[]string) {
	var hasHan, hasHiragana, hasKatakana, hasHangul bool
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			hasHan = true
		case unicode.Is(unicode.Hiragana, r):
			hasHiragana = true
		case unicode.Is(unicode.Katakana, r):
			hasKatakana = true
		case unicode.Is(unicode.Hangul, r):
			hasHangul = true
		}
		if hasHan && hasHiragana && hasKatakana && hasHangul {
			break
		}
	}
	if hasHan {
		*out = append(*out, "script_han")
	}
	if hasHiragana {
		*out = append(*out, "script_hiragana")
	}
	if hasKatakana {
		*out = append(*out, "script_katakana")
	}
	if hasHangul {
		*out = append(*out, "script_hangul")
	}
}

// sanitize replaces every rune that is not a letter, digit, or whitespace
// with a space. Latin/Cyrillic/Arabic/other word-bearing scripts are
// preserved; CJK runes are dropped because per-character CJK tokenization
// explodes vocabulary and slows training/classification — the script-presence
// signal is captured by emitScriptFlags instead.
func sanitize(text string) string {
	var sb strings.Builder
	sb.Grow(len(text))
	for _, r := range text {
		switch {
		case isCJK(r):
			sb.WriteByte(' ')
		case unicode.IsLetter(r), unicode.IsDigit(r):
			sb.WriteRune(r)
		case unicode.IsSpace(r):
			sb.WriteByte(' ')
		default:
			sb.WriteByte(' ')
		}
	}
	return sb.String()
}

// collapseRepeated reduces runs of identical runes to at most two, except for
// runs of 'w' which are allowed up to three (to preserve the "www" prefix in
// URLs that survived earlier extraction).
func collapseRepeated(text string) string {
	if text == "" {
		return ""
	}
	runes := []rune(text)
	out := make([]rune, 0, len(runes))
	out = append(out, runes[0])
	count := 1
	for i := 1; i < len(runes); i++ {
		r := runes[i]
		if r == runes[i-1] {
			count++
			switch {
			case count <= 2:
				out = append(out, r)
			case r == 'w' && count == 3:
				out = append(out, r)
			}
		} else {
			out = append(out, r)
			count = 1
		}
	}
	return string(out)
}

// isValidToken applies the size and content filters from the package docs.
func isValidToken(s string, opts Options) bool {
	if s == "" {
		return false
	}
	if reAllDigits.MatchString(s) {
		return false
	}
	n := utf8.RuneCountInString(s)
	return n >= opts.MinTokenRunes && n <= opts.MaxTokenRunes
}

// isCJK reports whether r belongs to one of the four major CJK script blocks.
// CJK runes are dropped during sanitize; their presence is captured by
// emitScriptFlags as a single "script_<name>" token per script per document.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}
