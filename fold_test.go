package spamtokens

import "testing"

func TestFold(t *testing.T) {
	// The first case is asianfanfics-goth's internal/spamjudge TestFold, copied
	// so the two Fold functions are held to the same answer.
	cases := []struct{ in, want string }{
		{"  ＩＧＮＯＲＥ\t\n  Previous  ", "ignore previous"},
		{"", ""},
		{" \t\n ", ""},
		{"Ｈｅｌｌｏ　ＷＯＲＬＤ", "hello world"}, // U+3000 ideographic space folds to U+0020
		{"ｈｔｔｐｓ：／／ｓｈａｄｙ．ｂｉｚ", "https://shady.biz"},
		{"ｶﾀｶﾅ", "カタカナ"}, // half-width Katakana folds to full-width
		{"今天 天气", "今天 天气"},
		{"Привет МИР", "привет мир"},
	}
	for _, c := range cases {
		if got := Fold(c.in); got != c.want {
			t.Errorf("Fold(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
