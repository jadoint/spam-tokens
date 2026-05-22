# spam-tokens

Unicode-aware tokenizer for spam classification, shared across the jadoint
spam stack (`asianfanfics-spam`, `spam-robinson`, `spam-classifier`).

This module replaces three diverged preprocessing pipelines (`CleanTextAggressive`
and `cleanText` in asianfanfics-spam, `clean_text` in spam-classifier) with a
single source of truth used at both training time and inference time, in Go
and Python.

## Install (Go)

```go
import "github.com/jadoint/spam-tokens"
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

## Install (Python)

Ships inside `spam-classifier` as `spam_detector_ai.spam_tokenizer`:

```python
from spam_detector_ai.spam_tokenizer import tokenize, clean_text, default_options

tokens = tokenize("안녕 친구 http://shady.biz/buy/viagra")
# {"script_hangul": 1,
#  "shady.biz": 1, "shady": 1, "biz": 1,
#  "buy": 1, "viagra": 1}
```

## Pipeline

Applied in order:

1. **HTML-unescape** — `&amp;` → `&`, etc.
2. **Detect non-Latin scripts** — emit a single `script_<name>` token per
   script present (`script_han`, `script_hiragana`, `script_katakana`,
   `script_hangul`). This captures the script-as-signal pattern (e.g.
   pure-hangul + spammy URL → spam) without paying the cost of per-character
   CJK tokenization.
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

The full rule set is exercised by `testdata/fixtures.json`, which is
consumed by both the Go and Python test suites to guarantee cross-language
parity.

## Options

| Field              | Default | Notes                                                   |
|--------------------|---------|---------------------------------------------------------|
| `MinTokenRunes`    | `3`     | Minimum runes for any token                             |
| `MaxTokenRunes`    | `40`    | Maximum runes for any token                             |
| `KeepHTMLTags`     | `true`  | Emit `tag_<name>` tokens (tags are always stripped)     |
| `KeepURLs`         | `true`  | Emit URL-derived tokens (URLs are always stripped)      |
| `KeepScriptFlags`  | `true`  | Emit `script_<name>` per non-Latin script present       |

## Testing

```bash
# Go
go test ./...

# Python (from spam-classifier/)
./venv/bin/python -m pytest spam_detector_ai/tests/test_spam_tokenizer.py
```

The Python tests load `testdata/fixtures.json` from this repo via a relative
path resolution (`../spam-tokens/testdata/fixtures.json`). Override with the
`SPAM_TOKENS_FIXTURES` environment variable when running outside the
monorepo layout.

## License

Internal jadoint tooling. No license declared.
