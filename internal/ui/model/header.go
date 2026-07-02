package model

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/fsext"
	"github.com/charmbracelet/crush/internal/session"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/styles"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

const (
	headerDiag           = "╱"
	minHeaderDiags       = 3
	leftPadding          = 1
	rightPadding         = 1
	diagToDetailsSpacing = 1 // space between diagonal pattern and details section
)

type header struct {
	// cached logo and compact logo
	logo        string
	compactLogo string

	com     *common.Common
	width   int
	compact bool

	// compact-header render cache. drawHeader otherwise rebuilds the whole
	// compact header (LSP loop, config/model lookups, several
	// lipgloss.Render calls, PrettyPath+DirTrim, ansi.Truncate) every
	// frame; memoize the rendered string keyed by a cheap fingerprint of
	// everything it depends on.
	compactCacheKey  uint64
	compactCacheView string
	hasCompactCache  bool
	// compactFPBuf is a scratch buffer reused across frames to build the
	// compact-header fingerprint without allocating (see compactFingerprint).
	compactFPBuf []byte
}

// newHeader creates a new header model.
func newHeader(com *common.Common) *header {
	h := &header{
		com: com,
	}
	h.refresh()
	return h
}

// refresh rebuilds cached logo strings using the current styles. Call
// after the theme changes.
func (h *header) refresh() {
	t := h.com.Styles
	isHyper := h.com.IsHyper()
	charm := "Charm™"
	if !isHyper {
		charm = " " + charm
	}
	name := "CRUSH"
	if isHyper {
		name = "HYPERCRUSH"
	}
	h.compactLogo = t.Header.Charm.Render(charm) + " " +
		styles.ApplyBoldForegroundGrad(t.Header.LogoGradCanvas, name, t.Header.LogoGradFromColor, t.Header.LogoGradToColor) + " "
	// Force drawHeader to re-render the wide logo on the next frame.
	h.width = 0
	h.logo = ""
	// The compact-header cache embeds the old theme's styles; drop it.
	h.hasCompactCache = false
}

// drawHeader draws the header for the given session. lspErrorCount is the
// total diagnostic count summed by the caller from local LSP state, so
// this render path never issues an LSPGetStates RPC.
func (h *header) drawHeader(
	scr uv.Screen,
	area uv.Rectangle,
	session *session.Session,
	compact bool,
	detailsOpen bool,
	width int,
	hyperCredits *int,
	lspErrorCount int,
) {
	if width != h.width || compact != h.compact {
		h.logo = renderLogo(h.com.Styles, compact, h.com.IsHyper(), width)
	}

	h.width = width
	h.compact = compact

	if !compact || session == nil {
		uv.NewStyledString(h.logo).Draw(scr, area)
		return
	}

	if session.ID == "" {
		return
	}

	key := h.compactFingerprint(session, detailsOpen, width, hyperCredits, lspErrorCount)
	if !h.hasCompactCache || key != h.compactCacheKey {
		h.compactCacheView = h.buildCompactHeader(session, detailsOpen, width, hyperCredits, lspErrorCount)
		h.compactCacheKey = key
		h.hasCompactCache = true
	}

	uv.NewStyledString(h.compactCacheView).Draw(scr, area)
}

// compactFingerprint hashes every input that affects the compact header
// render, so drawHeader can reuse the cached string when nothing changed.
func (h *header) compactFingerprint(
	session *session.Session,
	detailsOpen bool,
	width int,
	hyperCredits *int,
	lspErrorCount int,
) uint64 {
	var contextWindow int64
	agentCfg := h.com.Config().Agents[config.AgentCoder]
	if model := h.com.Config().GetModelByType(agentCfg.Model); model != nil {
		contextWindow = model.ContextWindow
	}
	credits := 0
	hasCredits := hyperCredits != nil
	if hasCredits {
		credits = *hyperCredits
	}
	// Build into a reused buffer with strconv appends and hash once, rather
	// than fmt.Sprintf: this runs every frame in compact mode, so the arg
	// boxing and result-string allocation Sprintf performed were pure churn.
	b := h.compactFPBuf[:0]
	putInt := func(k string, v int64) { b = append(b, k...); b = strconv.AppendInt(b, v, 10) }
	putInt("w=", int64(width))
	b = append(b, ";sid="...)
	b = append(b, session.ID...)
	putInt(";ct=", session.CompletionTokens)
	putInt(";pt=", session.PromptTokens)
	b = append(b, ";eu="...)
	b = strconv.AppendBool(b, session.EstimatedUsage)
	putInt(";lsp=", int64(lspErrorCount))
	b = append(b, ";det="...)
	b = strconv.AppendBool(b, detailsOpen)
	b = append(b, ";hyper="...)
	b = strconv.AppendBool(b, h.com.IsHyper())
	b = append(b, ";hc="...)
	b = strconv.AppendBool(b, hasCredits)
	putInt("/", int64(credits))
	putInt(";cw=", contextWindow)
	b = append(b, ";cwd="...)
	b = append(b, h.com.Workspace.WorkingDir()...)
	h.compactFPBuf = b // retain the grown buffer for reuse next frame

	hash := fnv.New64a()
	hash.Write(b)
	return hash.Sum64()
}

// buildCompactHeader renders the compact header string.
func (h *header) buildCompactHeader(
	session *session.Session,
	detailsOpen bool,
	width int,
	hyperCredits *int,
	lspErrorCount int,
) string {
	t := h.com.Styles

	var b strings.Builder
	b.WriteString(h.compactLogo)

	availDetailWidth := width - leftPadding - rightPadding - lipgloss.Width(b.String()) - minHeaderDiags - diagToDetailsSpacing
	details := renderHeaderDetails(
		h.com,
		session,
		lspErrorCount,
		detailsOpen,
		availDetailWidth,
		hyperCredits,
	)

	remainingWidth := width -
		lipgloss.Width(b.String()) -
		lipgloss.Width(details) -
		leftPadding -
		rightPadding -
		diagToDetailsSpacing

	if remainingWidth > 0 {
		b.WriteString(t.Header.Diagonals.Render(
			strings.Repeat(headerDiag, max(minHeaderDiags, remainingWidth)),
		))
		b.WriteString(" ")
	}

	b.WriteString(details)

	return t.Header.Wrapper.Padding(0, rightPadding, 0, leftPadding).Render(b.String())
}

// renderHeaderDetails renders the details section of the header.
func renderHeaderDetails(
	com *common.Common,
	session *session.Session,
	lspErrorCount int,
	detailsOpen bool,
	availWidth int,
	hyperCredits *int,
) string {
	t := com.Styles

	var parts []string

	if lspErrorCount > 0 {
		parts = append(parts, t.LSP.ErrorDiagnostic.Render(fmt.Sprintf("%s%d", styles.LSPErrorIcon, lspErrorCount)))
	}

	agentCfg := com.Config().Agents[config.AgentCoder]
	model := com.Config().GetModelByType(agentCfg.Model)
	if model != nil && model.ContextWindow > 0 {
		percentage := (float64(session.CompletionTokens+session.PromptTokens) / float64(model.ContextWindow)) * 100
		percentageText := fmt.Sprintf("%d%%", int(percentage))
		if session.EstimatedUsage {
			percentageText = "~" + percentageText
		}
		formattedPercentage := t.Header.Percentage.Render(percentageText)
		parts = append(parts, formattedPercentage)
	}

	if com.IsHyper() && hyperCredits != nil {
		hc := t.Header.HypercreditIcon.Render(styles.HypercreditIcon) + " " + t.Header.Percentage.Render(common.FormatCredits(*hyperCredits))
		parts = append(parts, hc)
	}

	const keystroke = "ctrl+d"
	if detailsOpen {
		parts = append(parts, t.Header.Keystroke.Render(keystroke)+t.Header.KeystrokeTip.Render(" close"))
	} else {
		parts = append(parts, t.Header.Keystroke.Render(keystroke)+t.Header.KeystrokeTip.Render(" open "))
	}

	dot := t.Header.Separator.Render(" • ")
	metadata := strings.Join(parts, dot)
	metadata = dot + metadata

	const dirTrimLimit = 4
	cwd := fsext.DirTrim(fsext.PrettyPath(com.Workspace.WorkingDir()), dirTrimLimit)
	cwd = t.Header.WorkingDir.Render(cwd)

	result := cwd + metadata
	return ansi.Truncate(result, max(0, availWidth), "…")
}
