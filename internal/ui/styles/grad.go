package styles

import (
	"fmt"
	"hash/fnv"
	"image/color"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/rivo/uniseg"
)

// gradCache memoizes the per-cluster rendered output of ForegroundGrad. The
// function walks grapheme clusters (uniseg), blends a color ramp and renders
// each cluster on every call, and it is invoked every frame by dialog titles
// and similar constant text. Results are keyed by a fingerprint of every input
// that affects the output.
var gradCache sync.Map // map[uint64][]string

// gradKey derives a stable cache key from all inputs that affect the rendered
// gradient output.
func gradKey(base lipgloss.Style, input string, bold bool, color1, color2 color.Color) uint64 {
	h := fnv.New64a()
	h.Write([]byte(input))
	h.Write([]byte{0})
	writeColor := func(c color.Color) {
		var buf [8]byte
		if c != nil {
			r, g, b, a := c.RGBA()
			buf[0] = byte(r >> 8)
			buf[1] = byte(g >> 8)
			buf[2] = byte(b >> 8)
			buf[3] = byte(a >> 8)
		}
		h.Write(buf[:4])
	}
	writeColor(color1)
	writeColor(color2)
	if bold {
		h.Write([]byte{1})
	} else {
		h.Write([]byte{0})
	}
	// base carries the remaining style attributes (e.g. background); include a
	// fingerprint so distinct bases never collide.
	fmt.Fprintf(h, "%v", base)
	return h.Sum64()
}

// ForegroundGrad returns a slice of strings representing the input string
// rendered with a horizontal gradient foreground from color1 to color2. Each
// string in the returned slice corresponds to a grapheme cluster in the input
// string. If bold is true, the rendered strings will be bolded.
func ForegroundGrad(base lipgloss.Style, input string, bold bool, color1, color2 color.Color) []string {
	if input == "" {
		return []string{""}
	}
	key := gradKey(base, input, bold, color1, color2)
	if cached, ok := gradCache.Load(key); ok {
		return cached.([]string)
	}
	result := foregroundGrad(base, input, bold, color1, color2)
	gradCache.Store(key, result)
	return result
}

func foregroundGrad(base lipgloss.Style, input string, bold bool, color1, color2 color.Color) []string {
	if len(input) == 1 {
		style := base.Foreground(color1)
		if bold {
			style = style.Bold(true)
		}
		return []string{style.Render(input)}
	}
	var clusters []string
	gr := uniseg.NewGraphemes(input)
	for gr.Next() {
		clusters = append(clusters, string(gr.Runes()))
	}

	ramp := lipgloss.Blend1D(len(clusters), color1, color2)
	for i, c := range ramp {
		style := base.Foreground(c)
		if bold {
			style = style.Bold(true)
		}
		clusters[i] = style.Render(clusters[i])
	}
	return clusters
}

// ApplyForegroundGrad renders a given string with a horizontal gradient
// foreground.
func ApplyForegroundGrad(base lipgloss.Style, input string, color1, color2 color.Color) string {
	if input == "" {
		return ""
	}
	var o strings.Builder
	clusters := ForegroundGrad(base, input, false, color1, color2)
	for _, c := range clusters {
		fmt.Fprint(&o, c)
	}
	return o.String()
}

// ApplyBoldForegroundGrad renders a given string with a horizontal gradient
// foreground.
func ApplyBoldForegroundGrad(base lipgloss.Style, input string, color1, color2 color.Color) string {
	if input == "" {
		return ""
	}
	var o strings.Builder
	clusters := ForegroundGrad(base, input, true, color1, color2)
	for _, c := range clusters {
		fmt.Fprint(&o, c)
	}
	return o.String()
}
