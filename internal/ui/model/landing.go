package model

import (
	"fmt"
	"image"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/config"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/workspace"
	"github.com/charmbracelet/ultraviolet/layout"
)

// selectedLargeModel returns the currently selected large language model from
// the agent coordinator, if one exists.
//
// Resolving the model hits the agent coordinator, which is an RPC
// (AgentIsReady + AgentModel) in client mode. Because this runs on the
// per-frame sidebar fingerprint path, the result is cached against a
// cheap, locally-cached config fingerprint (largeModelConfigKey) and
// only re-fetched when the selection changes. A nil result is never
// cached, so the not-ready → ready transition is picked up on the next
// frame.
func (m *UI) selectedLargeModel() *workspace.AgentModel {
	key := m.largeModelConfigKey()
	if m.largeModel != nil && key == m.largeModelCacheKey {
		return m.largeModel
	}
	m.largeModelCacheKey = key
	if m.com.Workspace.AgentIsReady() {
		model := m.com.Workspace.AgentModel()
		m.largeModel = &model
		return m.largeModel
	}
	m.largeModel = nil
	return nil
}

// largeModelConfigKey builds a cheap fingerprint of the locally-cached
// config that determines the selected large (coder) model. The resolved
// model is cached against this key so selectedLargeModel only re-fetches
// when the selection actually changes. The cached workspace config is
// refreshed on every server-side config change (ConfigChanged), so
// remote model switches flip this key too.
func (m *UI) largeModelConfigKey() string {
	cfg := m.com.Config()
	if cfg == nil {
		return ""
	}
	agentCfg, ok := cfg.Agents[config.AgentCoder]
	if !ok {
		return "no-agent"
	}
	sm, ok := cfg.Models[agentCfg.Model]
	if !ok {
		return "no-model:" + string(agentCfg.Model)
	}
	// Fold in the catwalk-derived fields that the sidebar renders (looked
	// up from the same cached config, no RPC), so a provider-config
	// change that alters e.g. the context window or reasoning support for
	// the same provider+model still flips the key and forces a refresh.
	var cwName, provName string
	var contextWindow int64
	var canReason bool
	if cm := cfg.GetModelByType(agentCfg.Model); cm != nil {
		cwName = cm.Name
		contextWindow = cm.ContextWindow
		canReason = cm.CanReason
	}
	if pc, ok := cfg.Providers.Get(sm.Provider); ok {
		provName = pc.Name
	}
	return fmt.Sprintf("%s|%s|%s|%t|%s|%s|%s|%d|%t",
		agentCfg.Model, sm.Provider, sm.Model, sm.Think, sm.ReasoningEffort,
		provName, cwName, contextWindow, canReason)
}

// landingView renders the landing page view showing the current working
// directory, model information, and LSP/MCP status in a two-column layout.
func (m *UI) landingView() string {
	t := m.com.Styles
	width := m.layout.main.Dx()
	cwd := common.PrettyPath(t, m.com.Workspace.WorkingDir(), width)

	parts := []string{
		cwd,
	}

	cta := t.Resource.AdditionalText.Render("Type a message to begin · / for commands · ctrl+s for sessions")
	parts = append(parts, "", m.modelInfo(width), "", cta)
	infoSection := lipgloss.JoinVertical(lipgloss.Left, parts...)

	var remainingHeightArea image.Rectangle
	layout.Vertical(
		layout.Len(lipgloss.Height(infoSection)+1),
		layout.Fill(1),
	).Split(m.layout.main).Assign(new(image.Rectangle), &remainingHeightArea)

	// On narrow terminals the three side-by-side sections get squeezed to a
	// few columns each, so stack them vertically instead. narrowLandingWidth
	// is the point below which a 3-column split would leave each column under
	// ~20 cells wide ((width-2)/3 < 20).
	const narrowLandingWidth = 60
	var content string
	if width < narrowLandingWidth {
		perSection := max(1, remainingHeightArea.Dy()/3)
		lspSection := m.lspInfo(width, perSection, false)
		mcpSection := m.mcpInfo(width, perSection, false)
		skillsSection := m.skillsInfo(width, perSection, false)
		content = lipgloss.JoinVertical(lipgloss.Left, lspSection, "", mcpSection, "", skillsSection)
	} else {
		mcpLspSectionWidth := min(30, (width-2)/3)
		lspSection := m.lspInfo(mcpLspSectionWidth, max(1, remainingHeightArea.Dy()), false)
		mcpSection := m.mcpInfo(mcpLspSectionWidth, max(1, remainingHeightArea.Dy()), false)
		skillsSection := m.skillsInfo(mcpLspSectionWidth, max(1, remainingHeightArea.Dy()), false)
		content = lipgloss.JoinHorizontal(lipgloss.Left, lspSection, " ", mcpSection, " ", skillsSection)
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(m.layout.main.Dy() - 1).
		PaddingTop(1).
		Render(
			lipgloss.JoinVertical(lipgloss.Left, infoSection, "", content),
		)
}
