# spam-tokens

A Unicode-aware tokenizer for spam classification. Produces a bag-of-words
suitable for Bayesian classifiers (Gary Robinson, Naive Bayes), TF-IDF
vectorizers, and similar bag-of-words models.

The tokenizer is designed to be portable: a shared JSON fixture file
(`testdata/fixtures.json`) defines the expected `input -> {token: count}`
mapping for every supported case, so ports to other languages can be
validated for byte-for-byte parity against the Go implementation.

## Install

```go
import spamtokens "github.com/jadoint/spam-tokens"
```

```go
tokens := spamtokens.Tokenize("안녕 친구 http://shady.biz/buy/viagra", spamtokens.DefaultOptions())
// map[string]int{
//   "script_hangul": 1,
//   "shady.biz": 1, "shady": 1, "biz": 1,
//   "buy": 1, "viagra": 1,
//   "안녕": 2, "친구": 2,  // a two-syllable word is its own bigram: word + bigram
// }

stream := spamtokens.CleanText("Hello WORLD 你好世界", spamtokens.DefaultOptions())
// "script_han 你好 好世 世界 hello world"

folded := spamtokens.Fold("  ＩＧＮＯＲＥ\t Previous ")
// "ignore previous"
```

`Tokenize` returns a `map[string]int` bag-of-words. `CleanText` returns the
same tokens joined by spaces, in pipeline order — useful when feeding training
rows to a downstream classifier that expects a single text field per document.
`Fold` is the canonical folded form of a text for hashing and matching (below).

## v0.2.0

v0.2.0 stops dropping the scripts written without spaces. v0.1.2 replaced every
Han, Kana and Hangul rune with a space and emitted only one `script_<name>`
flag per script, so a classifier never saw a Chinese, Japanese or Korean word;
Thai, whose vowel and tone marks are not letters, was cut into arbitrary
fragments at every mark. v0.2.0 NFKC-normalises first and cuts those scripts
into character bigrams.

Everything that shares tokens with this module must move with it:

- **gorobinson** bumps to this version (its `v0.3.0`), and a Robinson token
  table trained on v0.1.2 must be retrained before a v0.2.0 Robinson serves:
  the bigram tokens never appear in the old vocabulary.
- **The Python port**, `spam-classifier/spam_detector_ai/spam_tokenizer.py`,
  implements the same pipeline and is validated against
  `testdata/fixtures.json` and `testdata/scripts.json`.

v0.2.0 adds `golang.org/x/text` (v0.32.0, the module's only dependency, for
NFKC) and raises the `go` directive from 1.22 to **1.24.0**, the minimum that
x/text release requires; a module that requires v0.2.0 is raised to it too.
`ScriptBigrams: false` reproduces v0.1.2 for an A/B (Options, below).

## Pipeline

Applied in order:

1. **HTML-unescape** — `&amp;` → `&`, etc.
2. **NFKC-normalise** — the normalisation half of `Fold`: full-width letters,
   digits and punctuation become their ASCII twins (`ＦＲＥＥ` → `FREE`,
   `ｈｔｔｐｓ：／／` → `https://`), half-width Katakana becomes full-width.
   Case is left alone until step 7, so tag and URL extraction see the same
   case classes as before. NFKC also decomposes Thai SARA AM (U+0E33) into
   NIKHAHIT + SARA AA, two runes.
3. **Script flags** — emit one `script_<name>` token per script present, in
   the fixed order `script_han`, `script_hiragana`, `script_katakana`,
   `script_hangul`, `script_thai`, before any other token. The flags follow
   Unicode's script tables (U+30FC on its own raises none).
4. **Extract HTML tags** — emit `tag_<name>` tokens (e.g. `tag_a`, `tag_img`,
   `tag_iframe`, `tag_script`). On every tag, scan `href`, `src`, `action`,
   `formaction`, and `data-url` attributes; feed each value through URL
   tokenization. Strip tags from the working text (text inside an attribute
   goes with the tag: it can raise a flag at step 3 but emits no bigrams).
5. **Extract URLs** — match both `https?://...` and bare `host.tld[/path]`.
   For each URL:
   - Strip scheme (`http://` / `https://`) and leading `www.`.
   - Emit the bare domain (e.g. `shady.biz`) as a token.
   - Split on `[./?#&=_+%:\-]+` and emit each segment as a token (catches
     spammy path/query components like `buy`, `viagra`, `cheap`).
   - Drop pure-digit segments (ports, timestamps) and segments outside
     `[MinTokenRunes, MaxTokenRunes]`.
   - Strip the URL from the working text.
6. **Sanitize and segment** — emit the script-run tokens (below) and blank
   each run out, then replace every other rune that is not a letter, digit or
   whitespace with a space. Latin, Cyrillic, Arabic and the other spaced
   scripts are preserved for the word split.
7. **Lowercase** — Unicode-aware.
8. **Collapse repeated runes** — runs of identical runes reduced to ≤ 2,
   except `www` which is preserved.
9. **Whitespace-split + filter** — drop pure-numeric tokens, drop tokens
   outside `[MinTokenRunes, MaxTokenRunes]` (defaults 3 and 40).

v0.1.2 was the same list without step 2, with no `script_thai`, and with step 6
blanking every Han, Kana and Hangul rune instead of segmenting it.

The stream order is therefore: flags, then tag and attribute-URL tokens as
tags are found, then bare-text URL tokens, then script-run tokens in position
order, then words.

### Script runs and bigrams

A **run** is a maximal sequence of runes of one script class, uninterrupted by
any rune outside it. The four classes:

| Class    | Runes                                   |
|----------|-----------------------------------------|
| `han`    | Unicode script Han                      |
| `kana`   | Hiragana ∪ Katakana ∪ U+30FC (ー)        |
| `hangul` | Unicode script Hangul                   |
| `thai`   | Unicode script Thai, combining vowel and tone marks included |

Runs are found at step 6, on the text with tags and URLs already removed. With
`ScriptBigrams` (the default):

- A Han, Kana or Thai run of *n* runes emits exactly its *n*−1 overlapping
  bigrams, `rune[i]+rune[i+1]`, in position order. A one-rune run emits
  nothing. `今天天气很好` → `今天 天天 天气 气很 很好`.
- A Hangul run (Korean spaces its words) emits the whole run as one token
  first, when it is 2..`MaxTokenRunes` runes long, then its *n*−1 bigrams.
  `학생입니다` → `학생입니다 학생 생입 입니 니다`. A two-rune word is its own
  single bigram, so it counts twice (`친구: 2`).
- Hiragana and Katakana are one class, so `これはテスト` is one run; Han next
  to Kana is two (`日本語のテスト` → `日本 本語` and `のテ テス スト`).
- Bigrams are not lower-cased, collapsed or size-filtered: `MinTokenRunes`
  and `MaxTokenRunes` bound words and URL tokens, and `MaxTokenRunes` the
  Hangul whole-word token.

`testdata/scripts.json` records the four classes as inclusive code-point
ranges, written from Go's `unicode` tables (plus U+30FC in kana) by
`go test -run TestScriptsFile -update` and checked on every normal test run.
A Go release with a newer Unicode table fails that test until the file is
regenerated; a port embeds the same ranges, so the two never disagree on which
rune is Han.

## Fold

```go
func Fold(s string) string
```

NFKC-normalise, lower-case, collapse every whitespace run to one space and trim
the ends. It is the same function as asianfanfics-goth's
`internal/spamjudge.Fold`, so a text folded on either side compares equal; the
spam corpus hashes `Fold(text)` to find the same text across sites.

## Options

| Field              | Default | Notes                                                   |
|--------------------|---------|---------------------------------------------------------|
| `MinTokenRunes`    | `3`     | Minimum runes for a word or URL token                   |
| `MaxTokenRunes`    | `40`    | Maximum runes for a word, URL token or Hangul word      |
| `KeepHTMLTags`     | `true`  | Emit `tag_<name>` tokens (tags are always stripped)     |
| `KeepURLs`         | `true`  | Emit URL-derived tokens (URLs are always stripped)      |
| `KeepScriptFlags`  | `true`  | Emit `script_<name>` per unspaced script present        |
| `ScriptBigrams`    | `true`  | Segment Han, Kana, Hangul and Thai runs; `false` is v0.1.2 (Han, Kana, Hangul blanked, Thai split at its marks) |

Build options from `DefaultOptions()`: a zero `Options{}` has every flag false,
`ScriptBigrams` included.

## Cross-language parity

`testdata/fixtures.json` is the canonical specification. Each fixture has
the form:

```json
{
  "name": "human-readable description",
  "since": "0.2.0",
  "input": "raw input string",
  "expected": { "token1": 1, "token2": 3 },
  "expected_legacy": { "token1": 1 },
  "expected_stream": ["token1", "token2", "token2", "token2"]
}
```

- `expected` is the bag under `DefaultOptions()`.
- `expected_legacy`, present only where the two differ, is the bag with
  `ScriptBigrams: false`; where it is absent, `expected` holds under both.
- `expected_stream`, where present, is `CleanText` split on spaces, so a
  port's token order is checked as well as its counts.
- `since` marks a case whose `expected` follows rules v0.1.2 did not have
  (re-recorded CJK cases keep their v0.1.2 bag as `expected_legacy`).

Every expectation is written by hand from the rules above, never generated by
running the code. A port is correct if it produces `expected` (and
`expected_legacy` with bigrams off, and `expected_stream`) for every fixture,
and embeds the ranges in `testdata/scripts.json`. Pull requests adding
fixtures are welcome.

## Testing

```sh
go test ./...
go test -run TestScriptsFile -update   # after a Go release moves the Unicode tables
golangci-lint run
```

## License

MIT
