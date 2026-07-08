package chat

import (
	"strings"
	"testing"
)

// combineBodySpinner replicates rawRender's spinning combine: an empty body
// yields the spinner alone, otherwise the two are joined by a blank line.
func combineBodySpinner(body, spinner string) string {
	if body != "" {
		return body + "\n\n" + spinner
	}
	return spinner
}

// TestAssembleSpinningRender_ByteIdentical proves the decoupled Render path
// (cache the prefixed body, prefix the spinner separately, reassemble) is
// byte-for-byte identical to the old path of prefixing the full
// body+spinner string in one pass. If this ever diverges, the spinner
// decoupling would visibly corrupt assistant rendering.
func TestAssembleSpinningRender_ByteIdentical(t *testing.T) {
	t.Parallel()
	prefixes := []string{"", "P", "▏ ", "\x1b[38;5;1m│\x1b[0m "}
	bodies := []string{
		"",
		"single line",
		"line1\nline2\nline3",
		"has\n\ninternal blank",
		"trailing\n",
	}
	spinners := []string{
		"spin",
		"spin1\nspin2",
		"⠋ Thinking",
	}
	for _, prefix := range prefixes {
		for _, body := range bodies {
			for _, spinner := range spinners {
				want := prefixLines(prefix, combineBodySpinner(body, spinner))
				got := assembleSpinningRender(
					prefix,
					prefixLines(prefix, body),
					body == "",
					prefixLines(prefix, spinner),
				)
				if got != want {
					t.Errorf("prefix=%q body=%q spinner=%q\n got=%q\nwant=%q",
						prefix, body, spinner, got, want)
				}
			}
		}
	}
}

// TestPrefixLines matches the original inline per-line prefix loop.
func TestPrefixLines(t *testing.T) {
	t.Parallel()
	cases := []struct{ prefix, in, want string }{
		{"P", "", "P"},
		{"P", "a", "Pa"},
		{"P", "a\nb", "Pa\nPb"},
		{"P", "a\n\nb", "Pa\nP\nPb"},
		{"", "a\nb", "a\nb"},
		{"P", "trailing\n", "Ptrailing\nP"},
	}
	for _, c := range cases {
		if got := prefixLines(c.prefix, c.in); got != c.want {
			t.Errorf("prefixLines(%q,%q)=%q want %q", c.prefix, c.in, got, c.want)
		}
	}
	// Cross-check against strings.Split semantics for a random-ish sample.
	s := "x1\nx2\n\nx3"
	want := strings.ReplaceAll("Q"+strings.ReplaceAll(s, "\n", "\nQ"), "\r", "")
	if got := prefixLines("Q", s); got != want {
		t.Errorf("prefixLines cross-check: got %q want %q", got, want)
	}
}
