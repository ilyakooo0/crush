package styles

import (
	"github.com/charmbracelet/x/exp/charmtone"
)

// ThemeKeyForProvider returns a stable identifier for the theme
// associated with the given provider ID. Providers that share a theme
// yield the same key, so callers can cheaply detect when switching
// providers would not actually change the active theme and skip the
// expensive style rebuild. This is the single source of truth for the
// provider-to-theme mapping; [ThemeForProvider] builds on it.
func ThemeKeyForProvider(providerID string) string {
	switch providerID {
	case "hyper":
		return "hyper"
	default:
		return "default"
	}
}

// ThemeForProvider returns the dark Styles associated with the given
// provider ID. Unknown or empty provider IDs yield the default Charmtone
// Pantera theme. Retained for callers that always want the dark theme
// (e.g. the non-interactive CLI); the interactive TUI uses the
// background-aware [ThemeForProviderMode].
func ThemeForProvider(providerID string) Styles {
	return ThemeForProviderMode(providerID, true)
}

// ThemeKeyForProviderMode returns a stable identifier for the theme
// associated with the given provider ID and background mode (dark/light).
// The key incorporates the mode so callers can detect when a background
// change would actually alter the active theme and skip the rebuild
// otherwise.
func ThemeKeyForProviderMode(providerID string, dark bool) string {
	base := ThemeKeyForProvider(providerID)
	if dark {
		return base + "-dark"
	}
	return base + "-light"
}

// ThemeForProviderMode returns the Styles for the given provider ID in the
// requested background mode. When dark is false a light-background palette
// is returned so the UI stays legible on light terminals.
func ThemeForProviderMode(providerID string, dark bool) Styles {
	switch ThemeKeyForProvider(providerID) {
	case "hyper":
		if dark {
			return HypercrushObsidiana()
		}
		return HypercrushLatte()
	default:
		if dark {
			return CharmtonePantera()
		}
		return CharmtoneLatte()
	}
}

// CharmtonePantera returns the Charmtone dark theme. It's the default style
// for the UI.
func CharmtonePantera() Styles {
	return buildCharmtone(charmtoneDarkOpts())
}

// CharmtoneLatte returns the Charmtone light theme, used on light-background
// terminals. It mirrors the dark theme's structure but inverts the neutral
// ramp and swaps the neon accent/semantic colors for darker shades that stay
// legible on a light background.
//
// NOTE: this palette is a first pass tuned by hand; the exact accent/semantic
// color choices are worth a visual review on real light terminals.
func CharmtoneLatte() Styles {
	return buildCharmtone(charmtoneLightOpts())
}

// HypercrushObsidiana returns the Hypercrush dark theme.
func HypercrushObsidiana() Styles {
	return CharmtonePantera()
}

// HypercrushLatte returns the Hypercrush light theme.
func HypercrushLatte() Styles {
	return CharmtoneLatte()
}

// charmtoneDarkOpts returns the color options for the Charmtone dark theme.
func charmtoneDarkOpts() quickStyleOpts {
	return quickStyleOpts{
		primary:   charmtone.Charple,
		secondary: charmtone.Dolly,
		accent:    charmtone.Bok,
		keyword:   charmtone.Blush,

		fgBase:       charmtone.Sash,
		fgMoreSubtle: charmtone.Squid,
		fgSubtle:     charmtone.Smoke,
		fgMostSubtle: charmtone.Oyster,

		onPrimary: charmtone.Butter,

		bgBase:         charmtone.Pepper,
		bgLeastVisible: charmtone.BBQ,
		bgLessVisible:  charmtone.Char,
		bgMostVisible:  charmtone.Iron,

		separator: charmtone.Char,

		destructive:       charmtone.Coral,
		error:             charmtone.Sriracha,
		warningSubtle:     charmtone.Zest,
		warning:           charmtone.Mustard,
		denied:            charmtone.Tang,
		busy:              charmtone.Citron,
		info:              charmtone.Malibu,
		infoMoreSubtle:    charmtone.Sardine,
		infoMostSubtle:    charmtone.Damson,
		success:           charmtone.Julep,
		successMoreSubtle: charmtone.Bok,
		successMostSubtle: charmtone.Guac,

		// ANSI 16-color palette for remapping raw terminal output
		// (e.g. bang-mode shell commands) onto legible Charmtone colors.
		ansiBlack:   charmtone.BBQ,
		ansiRed:     charmtone.Coral,
		ansiGreen:   charmtone.Guac,
		ansiYellow:  charmtone.Mustard,
		ansiBlue:    charmtone.Charple,
		ansiMagenta: charmtone.Dolly,
		ansiCyan:    charmtone.Malibu,
		ansiWhite:   charmtone.Smoke,

		ansiBrightBlack:   charmtone.Iron,
		ansiBrightRed:     charmtone.Tuna,
		ansiBrightGreen:   charmtone.Julep,
		ansiBrightYellow:  charmtone.Zest,
		ansiBrightBlue:    charmtone.Guppy,
		ansiBrightMagenta: charmtone.Blush,
		ansiBrightCyan:    charmtone.Sardine,
		ansiBrightWhite:   charmtone.Salt,
	}
}

// charmtoneLightOpts returns the color options for the Charmtone light theme.
// The neutral ramp is inverted (light backgrounds, dark foregrounds) and the
// neon accents/semantics from the dark theme are replaced with darker,
// higher-contrast shades so text stays readable on a light background.
func charmtoneLightOpts() quickStyleOpts {
	return quickStyleOpts{
		primary:   charmtone.Charple, // saturated purple reads on white
		secondary: charmtone.Grape,
		accent:    charmtone.Zinc,
		keyword:   charmtone.Prince,

		// Foregrounds: dark on light, decreasing visibility.
		fgBase:       charmtone.Pepper,
		fgSubtle:     charmtone.Iron,
		fgMoreSubtle: charmtone.Oyster,
		fgMostSubtle: charmtone.Steam,

		onPrimary: charmtone.Butter, // light text on the purple primary

		// Backgrounds: light base, increasingly visible (darker) greys.
		bgBase:         charmtone.Salt,
		bgLeastVisible: charmtone.Sash,
		bgLessVisible:  charmtone.Steep,
		bgMostVisible:  charmtone.Smoke,

		separator: charmtone.Steep,

		destructive:       charmtone.Coral,
		error:             charmtone.Sriracha,
		warningSubtle:     charmtone.Yam,
		warning:           charmtone.Tang,
		denied:            charmtone.Paprika,
		busy:              charmtone.Cumin,
		info:              charmtone.Damson,
		infoMoreSubtle:    charmtone.Malibu,
		infoMostSubtle:    charmtone.Anchovy,
		success:           charmtone.Pickle,
		successMoreSubtle: charmtone.Guac,
		successMostSubtle: charmtone.Zinc,

		// ANSI 16-color palette tuned for legibility on a light background:
		// normal colors are darker/saturated, brights slightly lighter.
		ansiBlack:   charmtone.Pepper,
		ansiRed:     charmtone.Pom,
		ansiGreen:   charmtone.Pickle,
		ansiYellow:  charmtone.Cumin,
		ansiBlue:    charmtone.Oceania,
		ansiMagenta: charmtone.Grape,
		ansiCyan:    charmtone.Damson,
		ansiWhite:   charmtone.Oyster,

		ansiBrightBlack:   charmtone.Steam,
		ansiBrightRed:     charmtone.Sriracha,
		ansiBrightGreen:   charmtone.Guac,
		ansiBrightYellow:  charmtone.Tang,
		ansiBrightBlue:    charmtone.Sapphire,
		ansiBrightMagenta: charmtone.Urchin,
		ansiBrightCyan:    charmtone.Zinc,
		ansiBrightWhite:   charmtone.Iron,
	}
}

// buildCharmtone builds a Styles value from the given color options and
// applies the shared bang-prompt and shell-bar accent overrides. These
// overrides use saturated purple accents that read on both light and dark
// backgrounds.
func buildCharmtone(opts quickStyleOpts) Styles {
	s := quickStyle(opts)

	// Bang ! prompt overrides - use Salt/Hazy/Larple colors.
	s.Editor.PromptBangIconFocused = s.Editor.PromptBangIconFocused.
		Foreground(charmtone.Salt).
		Background(charmtone.Hazy)
	s.Editor.PromptBangDotsFocused = s.Editor.PromptBangDotsFocused.
		Foreground(charmtone.Hazy)
	s.Editor.PromptBangDotsBlurred = s.Editor.PromptBangDotsBlurred.
		Foreground(charmtone.Larple)

	// Shell bar/prompt overrides - use Charple/Iron/Hazy colors.
	s.Messages.ShellBarFocused = s.Messages.ShellBarFocused.
		BorderForeground(charmtone.Charple)
	s.Messages.ShellBarBlurred = s.Messages.ShellBarBlurred.
		BorderForeground(charmtone.Iron)
	s.Messages.ShellPrompt = s.Messages.ShellPrompt.
		Foreground(charmtone.Hazy)
	s.Messages.ShellPromptBlurred = s.Messages.ShellPromptBlurred.
		Foreground(charmtone.Hazy)

	return s
}
