package common

import (
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/crush/internal/ui/diffview"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// diffStyleCache memoizes the *chroma.Style built for a given theme. Building
// the style (MustNewStyle) parses colors for every entry and was previously
// done on every DiffFormatter call. The Styles struct is mutated in place on
// theme change, so we key on the theme's content fingerprint rather than its
// pointer identity.
var diffStyleCache sync.Map // map[uint64]*chroma.Style

// DiffFormatter returns a diff formatter with the given styles that can be
// used to format diff outputs.
func DiffFormatter(s *styles.Styles) *diffview.DiffView {
	formatDiff := diffview.New()

	theme := s.ChromaTheme()
	key := chromaStyleKey(theme, nil)
	var style *chroma.Style
	if cached, ok := diffStyleCache.Load(key); ok {
		style = cached.(*chroma.Style)
	} else {
		style = chroma.MustNewStyle("crush", theme)
		diffStyleCache.Store(key, style)
	}

	diff := formatDiff.ChromaStyle(style).Style(s.Diff).TabWidth(4)
	return diff
}
