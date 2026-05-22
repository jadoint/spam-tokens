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
// }

stream := spamtokens.CleanText("Hello WORLD", spamtokens.DefaultOptions())
// "hello world"
```

`Tokenize` returns a `map[string]int` bag-of-words. `CleanText` returns the
same tokens joined by spaces — useful when feeding training rows to a
downstream classifier that expects a single text field per document.

## Pipeline

Applied in order:

1. **HTML-unescape** — `&amp;` → `&`, etc.
2. **Detect non-Latin scripts** — emit a single `script_<name>` token per
   script present (`script_han`, `script_hiragana`, `script_katakana`,
   `script_hangul`). This captures the script-as-signal pattern (e.g.
   pure-hangul + spammy URL → spam) without paying the cost of per-character
   CJK tokenization, which would otherwise explode vocabulary size and
   slow down classification.
3. **Extract HTML tags** — emit `tag_<name>` tokens (e.g. `tag_a`, `tag_img`,
   `tag_iframe`, `tag_script`). On every tag, scan `href`, `src`, `action`,
   `formaction`, and `data-url` attributes; feed each value through URL
   tokenization. Strip tags from the working text.
4. **Extract URLs** — match both `https?://...` and bare `host.tld[/path]`.
   For each URL:
   - Strip scheme (`http://` / `https://`) and leading `www.`.
   - Emit the bare domain (e.g. `shady.biz`) as a token.
   - Split on `[./?#&=_+%:\-]+` and emit each segment as a token (catches
     spammy path/query components like `buy`, `viagra`, `cheap`).
   - Drop pure-digit segments (ports, timestamps) and segments < 3 chars.
   - Strip the URL from the working text.
5. **Sanitize** — replace runes that are not letters / digits / whitespace
   with spaces. Latin / Cyrillic / Arabic / other word-bearing scripts are
   preserved. CJK runes are **dropped** (treated as punctuation) — the
   script-presence signal is already carried by step 2.
6. **Lowercase** — Unicode-aware.
7. **Collapse repeated runes** — runs of identical runes reduced to ≤ 2,
   except `www` which is preserved.
8. **Whitespace-split + filter** — drop pure-numeric tokens, drop tokens
   outside `[MinTokenRunes, MaxTokenRunes]` (defaults 3 and 40).

## Options

| Field              | Default | Notes                                                   |
|--------------------|---------|---------------------------------------------------------|
| `MinTokenRunes`    | `3`     | Minimum runes for any token                             |
| `MaxTokenRunes`    | `40`    | Maximum runes for any token                             |
| `KeepHTMLTags`     | `true`  | Emit `tag_<name>` tokens (tags are always stripped)     |
| `KeepURLs`         | `true`  | Emit URL-derived tokens (URLs are always stripped)      |
| `KeepScriptFlags`  | `true`  | Emit `script_<name>` per non-Latin script present       |

## Cross-language parity

`testdata/fixtures.json` is the canonical specification. Each fixture has
the form:

```json
{
  "name": "human-readable description",
  "input": "raw input string",
  "expected": { "token1": 1, "token2": 3 }
}
```

A port in another language is correct if it produces the `expected` map for
every fixture. Pull requests adding fixtures are welcome.

## Testing

```sh
go test ./...
```

## License

MIT
