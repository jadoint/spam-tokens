// Package spamtokens is a Unicode-aware tokenizer for spam classification
// shared across the jadoint spam stack (gorobinson and spam-robinson, the
// spam-service exporter, and the Python port in spam-classifier).
//
// The tokenizer applies the following pipeline, in order:
//
//  1. HTML-unescape the input (e.g. &amp; -> &).
//  2. NFKC-normalise it, the normalisation half of Fold: full-width letters,
//     digits and punctuation become their ASCII twins, half-width Katakana
//     becomes full-width. Case is left alone until step 7.
//  3. Detect scripts written without spaces (Han, Hiragana, Katakana, Hangul,
//     Thai) and emit a single "script_<name>" token per script present, in
//     that order.
//  4. Extract HTML tags: emit "tag_<name>" tokens and, for tags with
//     href/src/action/formaction/data-url attributes, feed those values
//     through URL tokenization. Tags are then stripped from the working text.
//  5. Extract URLs (both http(s)://... and bare host.tld[/path] forms): emit
//     the bare domain as a single token and emit each path / query / fragment
//     segment as its own token after splitting on URL delimiters. URLs are
//     then stripped from the working text.
//  6. Sanitize and segment. A run is a maximal sequence of runes of one script
//     class: Han; Kana (Hiragana, Katakana and U+30FC); Hangul; Thai. With
//     ScriptBigrams, each run of n runes emits its n-1 overlapping character
//     bigrams, and a Hangul run of 2..MaxTokenRunes runes also emits itself
//     as a word, first. The run is then blanked out. Without ScriptBigrams,
//     Han, Kana and Hangul runes are blanked and Thai is sanitized like any
//     other script, as in v0.1.2. Every remaining rune that is not a letter,
//     digit or whitespace becomes a space.
//  7. Lowercase the working text.
//  8. Collapse runs of identical runes to at most two, except "www" which is
//     preserved.
//  9. Split on whitespace, drop pure-numeric tokens, drop tokens outside
//     [MinTokenRunes, MaxTokenRunes].
//
// Script tokens therefore follow the flag, tag and URL tokens in the stream
// and precede every word. Min and Max runes bound the words and URL tokens;
// bigrams are always two runes.
package spamtokens

import (
	"html"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
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
	// KeepScriptFlags emits a single "script_<name>" token per unspaced
	// script present in the document (script_han, script_hiragana,
	// script_katakana, script_hangul, script_thai).
	KeepScriptFlags bool
	// ScriptBigrams cuts Han, Kana, Hangul and Thai runs into character
	// bigrams (and Hangul words). False reproduces v0.1.2, which dropped every
	// Han, Kana and Hangul rune and split Thai at its vowel and tone marks; it
	// exists for an A/B against models trained on v0.1.2 tokens.
	ScriptBigrams bool
}

// DefaultOptions returns the canonical configuration used across the spam stack.
func DefaultOptions() Options {
	return Options{
		MinTokenRunes:   3,
		MaxTokenRunes:   40,
		KeepHTMLTags:    true,
		KeepURLs:        true,
		KeepScriptFlags: true,
		ScriptBigrams:   true,
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

// tokenStream runs the package pipeline and emits tokens in pipeline order:
// script flags, then HTML tag tokens as tags are found, then URL tokens, then
// script-run tokens, then the remaining prose. The order is deterministic for
// a given input.
func tokenStream(text string, opts Options) []string {
	if text == "" {
		return nil
	}

	out := make([]string, 0, 32)
	processed := html.UnescapeString(text)
	processed = norm.NFKC.String(processed)

	if opts.KeepScriptFlags {
		emitScriptFlags(processed, &out)
	}

	processed = extractHTMLTags(processed, &out, opts)

	if opts.KeepURLs {
		processed = extractURLs(processed, &out, opts)
	} else {
		processed = reURLScheme.ReplaceAllString(processed, " ")
		processed = reBareDomain.ReplaceAllString(processed, " ")
	}

	processed = sanitize(processed, &out, opts)
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
					tokenizeURL(am[1], out, opts)
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
func extractURLs(text string, out *[]string, opts Options) string {
	text = reURLScheme.ReplaceAllStringFunc(text, func(u string) string {
		tokenizeURL(u, out, opts)
		return " "
	})
	text = reBareDomain.ReplaceAllStringFunc(text, func(u string) string {
		tokenizeURL(u, out, opts)
		return " "
	})
	return text
}

// tokenizeURL strips scheme + leading www., emits the bare domain as one
// token, and emits each [./?#&=_+%:-]-split segment as a separate token.
// Pure-numeric and out-of-range segments are dropped here so URL noise
// (ports, query timestamps, long encoded blobs) doesn't pollute the vocabulary.
func tokenizeURL(url string, out *[]string, opts Options) {
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
	if isValidToken(domain, opts) {
		*out = append(*out, domain)
	}

	for _, part := range reURLSplit.Split(url, -1) {
		if !isValidToken(part, opts) {
			continue
		}
		*out = append(*out, part)
	}
}

// sanitize is pipeline step 6. It emits the tokens of every script run (see
// emitRun) and blanks the run out, then replaces every other rune that is not
// a letter, digit, or whitespace with a space, so Latin, Cyrillic, Arabic and
// the other spaced scripts reach the word split. Without ScriptBigrams the
// Han, Kana and Hangul runes are blanked unread and Thai is left to the letter
// test, as in v0.1.2.
func sanitize(text string, out *[]string, opts Options) string {
	var sb strings.Builder
	sb.Grow(len(text))
	var run []rune
	runClass := classNone
	for _, r := range text {
		c := classOf(r)
		if c == classThai && !opts.ScriptBigrams {
			c = classNone
		}
		if c != runClass {
			if len(run) > 0 {
				emitRun(run, runClass, out, opts)
				run = run[:0]
			}
			runClass = c
		}
		switch {
		case c != classNone:
			if opts.ScriptBigrams {
				run = append(run, r)
			}
			sb.WriteByte(' ')
		case unicode.IsLetter(r), unicode.IsDigit(r):
			sb.WriteRune(r)
		default:
			sb.WriteByte(' ')
		}
	}
	if len(run) > 0 {
		emitRun(run, runClass, out, opts)
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
