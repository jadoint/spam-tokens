package spamtokens

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Fold is the canonical folded form of a text, for hashing and matching:
// NFKC-normalised (so full-width letters and punctuation read as their ASCII
// twins), lower-cased, with every whitespace run collapsed to one space and the
// ends trimmed.
//
// It is the same function as asianfanfics-goth's internal/spamjudge.Fold, so a
// text folded on either side compares equal. The tokenizer applies only the
// NFKC half of it, at pipeline step 2; lower-casing stays at step 7.
func Fold(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(norm.NFKC.String(s))), " ")
}
