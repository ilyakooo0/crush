package diffview

import (
	"image/color"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/exp/charmtone"
)

// Diff insert/delete backgrounds are derived by washing a semantic accent
// (green for inserts, red for deletes) over the base background. Keeping the
// derivation here gives both DefaultDarkStyle/DefaultLightStyle and the styles
// package's palette-driven theme a single source of truth, rather than several
// copies of the same hardcoded hex.
const (
	diffCodeTint    = 0.10 // symbol/code background wash strength.
	diffLineNumTint = 0.07 // line-number background wash (slightly darker).
)

// BlendBg washes accent over base by ratio in [0,1], returning a subtle tint.
// It is the shared basis for diff insert/delete backgrounds.
func BlendBg(base, accent color.Color, ratio float64) color.Color {
	br, bg, bb, _ := base.RGBA()
	ar, ag, ab, _ := accent.RGBA()
	lerp := func(a, b uint32) uint8 {
		return uint8(float64(a>>8)*(1-ratio) + float64(b>>8)*ratio + 0.5)
	}
	return color.RGBA{R: lerp(br, ar), G: lerp(bg, ag), B: lerp(bb, ab), A: 0xff}
}

// ChangeLineStyle builds an insert- or delete-line style from semantic colors:
// accent drives the symbol/line-number foreground and the background wash over
// base, while codeFg colors the code text (pass nil to leave it unset).
func ChangeLineStyle(accent, base, codeFg color.Color) LineStyle {
	code := lipgloss.NewStyle().Background(BlendBg(base, accent, diffCodeTint))
	if codeFg != nil {
		code = code.Foreground(codeFg)
	}
	return LineStyle{
		LineNumber: lipgloss.NewStyle().
			Foreground(accent).
			Background(BlendBg(base, accent, diffLineNumTint)),
		Symbol: lipgloss.NewStyle().
			Foreground(accent).
			Background(BlendBg(base, accent, diffCodeTint)),
		Code: code,
	}
}

// LineStyle defines the styles for a given line type in the diff view.
type LineStyle struct {
	LineNumber lipgloss.Style
	Symbol     lipgloss.Style
	Code       lipgloss.Style
}

// Style defines the overall style for the diff view, including styles for
// different line types such as divider, missing, equal, insert, and delete
// lines.
type Style struct {
	DividerLine LineStyle
	MissingLine LineStyle
	EqualLine   LineStyle
	InsertLine  LineStyle
	DeleteLine  LineStyle
	Filename    LineStyle
}

// DefaultLightStyle provides a default light theme style for the diff view.
func DefaultLightStyle() Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(charmtone.Iron).
				Background(charmtone.Thunder),
			Code: lipgloss.NewStyle().
				Foreground(charmtone.Oyster).
				Background(charmtone.Anchovy),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(charmtone.Sash),
			Code: lipgloss.NewStyle().
				Background(charmtone.Sash),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(charmtone.Char).
				Background(charmtone.Sash),
			Code: lipgloss.NewStyle().
				Foreground(charmtone.Pepper).
				Background(charmtone.Salt),
		},
		InsertLine: ChangeLineStyle(charmtone.Turtle, charmtone.Salt, charmtone.Pepper),
		DeleteLine: ChangeLineStyle(charmtone.Cherry, charmtone.Salt, charmtone.Pepper),
		Filename: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(charmtone.Iron).
				Background(charmtone.Thunder),
			Code: lipgloss.NewStyle().
				Foreground(charmtone.Iron).
				Background(charmtone.Thunder),
		},
	}
}

// DefaultDarkStyle provides a default dark theme style for the diff view.
func DefaultDarkStyle() Style {
	return Style{
		DividerLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(charmtone.Smoke).
				Background(charmtone.Sapphire),
			Code: lipgloss.NewStyle().
				Foreground(charmtone.Smoke).
				Background(charmtone.Ox),
		},
		MissingLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Background(charmtone.Char),
			Code: lipgloss.NewStyle().
				Background(charmtone.Char),
		},
		EqualLine: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(charmtone.Sash).
				Background(charmtone.Char),
			Code: lipgloss.NewStyle().
				Foreground(charmtone.Salt).
				Background(charmtone.Pepper),
		},
		InsertLine: ChangeLineStyle(charmtone.Turtle, charmtone.Pepper, charmtone.Salt),
		DeleteLine: ChangeLineStyle(charmtone.Cherry, charmtone.Pepper, charmtone.Salt),
		Filename: LineStyle{
			LineNumber: lipgloss.NewStyle().
				Foreground(charmtone.Smoke).
				Background(charmtone.Sapphire),
			Code: lipgloss.NewStyle().
				Foreground(charmtone.Smoke).
				Background(charmtone.Sapphire),
		},
	}
}
