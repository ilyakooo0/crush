package common

import (
	"bytes"
	"hash/fnv"
	"image/color"
	"sort"
	"strconv"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// highlightStyleCache memoizes the *chroma.Style built for a given theme +
// background combination. Building the style (MustNewStyle + Builder.Transform.
// Build) parses colors for every entry and was previously done on every
// SyntaxHighlight call. The Styles struct is mutated in place on theme change,
// so we key on the theme's content fingerprint (plus bg) rather than its
// pointer identity.
var highlightStyleCache sync.Map // map[uint64]*chroma.Style

// chromaStyleKey derives a stable cache key from the theme entries and bg.
func chromaStyleKey(theme chroma.StyleEntries, bg color.Color) uint64 {
	h := fnv.New64a()
	// Sort token types for a deterministic fingerprint (map order is random).
	types := make([]int, 0, len(theme))
	for tt := range theme {
		types = append(types, int(tt))
	}
	sort.Ints(types)
	for _, tt := range types {
		h.Write([]byte(strconv.Itoa(tt)))
		h.Write([]byte{0})
		h.Write([]byte(theme[chroma.TokenType(tt)]))
		h.Write([]byte{0})
	}
	var buf [8]byte
	if bg != nil {
		r, g, b, a := bg.RGBA()
		buf[0] = byte(r >> 8)
		buf[1] = byte(g >> 8)
		buf[2] = byte(b >> 8)
		buf[3] = byte(a >> 8)
	}
	h.Write(buf[:4])
	return h.Sum64()
}

// SyntaxHighlight applies syntax highlighting to the given source code based
// on the file name and background color. It returns the highlighted code as a
// string.
func SyntaxHighlight(st *styles.Styles, source, fileName string, bg color.Color) (string, error) {
	// Determine the language lexer to use
	l := lexers.Match(fileName)
	if l == nil {
		l = lexers.Analyse(source)
	}
	if l == nil {
		l = lexers.Fallback
	}
	l = chroma.Coalesce(l)

	// Get the formatter
	f := formatters.Get("terminal16m")
	if f == nil {
		f = formatters.Fallback
	}

	// Build (or reuse a cached) chroma style for this theme + background.
	theme := st.ChromaTheme()
	key := chromaStyleKey(theme, bg)
	var s *chroma.Style
	if cached, ok := highlightStyleCache.Load(key); ok {
		s = cached.(*chroma.Style)
	} else {
		style := chroma.MustNewStyle("crush", theme)

		// Modify the style to use the provided background
		built, err := style.Builder().Transform(
			func(t chroma.StyleEntry) chroma.StyleEntry {
				r, g, b, _ := bg.RGBA()
				t.Background = chroma.NewColour(uint8(r>>8), uint8(g>>8), uint8(b>>8))
				return t
			},
		).Build()
		if err != nil {
			built = chromastyles.Fallback
		}
		s = built
		highlightStyleCache.Store(key, s)
	}

	// Tokenize and format
	it, err := l.Tokenise(nil, source)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = f.Format(&buf, s, it)
	return buf.String(), err
}
