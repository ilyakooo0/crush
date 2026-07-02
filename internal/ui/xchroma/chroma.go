package xchroma

import (
	"fmt"
	"image/color"
	"io"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
)

// Formatter is func that returns a custom formatter for Chroma that uses
// Lip Gloss for foreground styling, while keeping a forced background color.
func Formatter(bgColor color.Color, processValue func(string) string) chroma.Formatter {
	return chroma.FormatterFunc(func(w io.Writer, style *chroma.Style, it chroma.Iterator) error {
		// Precompute the base background style once, then memoize the
		// resolved lipgloss.Style per token type so the per-token work is
		// just a map lookup + one Render.
		baseStyle := lipgloss.NewStyle().Background(bgColor)

		type tokenEntry struct {
			style lipgloss.Style
			zero  bool
		}
		palette := make(map[chroma.TokenType]tokenEntry)

		for token := it(); token != chroma.EOF; token = it() {
			value := token.Value
			if processValue != nil {
				value = processValue(value)
			}

			te, ok := palette[token.Type]
			if !ok {
				entry := style.Get(token.Type)
				if entry.IsZero() {
					te = tokenEntry{zero: true}
				} else {
					s := baseStyle
					if entry.Bold == chroma.Yes {
						s = s.Bold(true)
					}
					if entry.Underline == chroma.Yes {
						s = s.Underline(true)
					}
					if entry.Italic == chroma.Yes {
						s = s.Italic(true)
					}
					if entry.Colour.IsSet() {
						s = s.Foreground(lipgloss.Color(entry.Colour.String()))
					}
					te = tokenEntry{style: s}
				}
				palette[token.Type] = te
			}

			if te.zero {
				if _, err := fmt.Fprint(w, value); err != nil {
					return err
				}
				continue
			}

			if _, err := fmt.Fprint(w, te.style.Render(value)); err != nil {
				return err
			}
		}
		return nil
	})
}
