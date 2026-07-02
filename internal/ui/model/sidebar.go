package model

import (
	"cmp"
	"fmt"
	"hash/fnv"
	"image"
	"sort"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/logo"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/ultraviolet/layout"
)

// modelInfo renders the current model information including reasoning
// settings and context usage/cost for the sidebar.
func (m *UI) modelInfo(width int) string {
	model := m.selectedLargeModel()
	reasoningInfo := ""
	providerName := ""

	if model != nil {
		// Get provider name first
		providerConfig, ok := m.com.Config().Providers.Get(model.ModelCfg.Provider)
		if ok {
			providerName = providerConfig.Name

			// Only check reasoning if model can reason
			if model.CatwalkCfg.CanReason {
				if len(model.CatwalkCfg.ReasoningLevels) == 0 {
					if model.ModelCfg.Think {
						reasoningInfo = "Thinking On"
					} else {
						reasoningInfo = "Thinking Off"
					}
				} else {
					reasoningEffort := cmp.Or(model.ModelCfg.ReasoningEffort, model.CatwalkCfg.DefaultReasoningEffort)
					reasoningInfo = fmt.Sprintf("Reasoning %s", common.FormatReasoningEffort(reasoningEffort))
				}
			}
		}
	}

	var modelContext *common.ModelContextInfo
	if model != nil && m.session != nil {
		modelContext = &common.ModelContextInfo{
			ContextUsed:    m.session.CompletionTokens + m.session.PromptTokens,
			Cost:           m.session.Cost,
			ModelContext:   model.CatwalkCfg.ContextWindow,
			EstimatedUsage: m.session.EstimatedUsage,
		}
	}
	var modelName string
	if model != nil {
		modelName = model.CatwalkCfg.Name
	}
	return common.ModelInfo(m.com.Styles, modelName, providerName, reasoningInfo, modelContext, width, m.hyperCredits)
}

// getDynamicHeightLimits will give us the num of items to show in each section based on the height
// some items are more important than others.
func getDynamicHeightLimits(availableHeight, fileCount, lspCount, mcpCount, skillCount int) (maxFiles, maxLSPs, maxMCPs, maxSkills int) {
	const (
		minItemsPerSection = 2
		// Keep these high so dynamic layout uses available sidebar space
		// instead of hitting small hard limits.
		defaultMaxFilesShown    = 1000
		defaultMaxLSPsShown     = 1000
		defaultMaxMCPsShown     = 1000
		defaultMaxSkillsShown   = 1000
		minAvailableHeightLimit = 10
	)

	if availableHeight < minAvailableHeightLimit {
		return minItemsPerSection, minItemsPerSection, minItemsPerSection, minItemsPerSection
	}

	maxFiles = minItemsPerSection
	maxLSPs = minItemsPerSection
	maxMCPs = minItemsPerSection
	maxSkills = minItemsPerSection

	remainingHeight := max(0, availableHeight-(minItemsPerSection*4))

	sectionValues := []*int{&maxFiles, &maxLSPs, &maxMCPs, &maxSkills}
	sectionCaps := []int{defaultMaxFilesShown, defaultMaxLSPsShown, defaultMaxMCPsShown, defaultMaxSkillsShown}
	sectionNeeds := []int{max(0, fileCount-maxFiles), max(0, lspCount-maxLSPs), max(0, mcpCount-maxMCPs), max(0, skillCount-maxSkills)}

	for remainingHeight > 0 {
		allocated := false
		for i, section := range sectionValues {
			if remainingHeight == 0 {
				break
			}
			if sectionNeeds[i] == 0 || *section >= sectionCaps[i] {
				continue
			}
			*section = *section + 1
			sectionNeeds[i]--
			remainingHeight--
			allocated = true
		}
		if !allocated {
			break
		}
	}

	for remainingHeight > 0 {
		allocated := false
		for i, section := range sectionValues {
			if remainingHeight == 0 {
				break
			}
			if *section >= sectionCaps[i] {
				continue
			}
			*section = *section + 1
			remainingHeight--
			allocated = true
		}
		if !allocated {
			break
		}
	}

	return maxFiles, maxLSPs, maxMCPs, maxSkills
}

// sidebarFingerprint computes a hash of every input that affects the
// rendered sidebar, so drawSidebar can reuse a cached render when nothing
// changed.
func (m *UI) sidebarFingerprint(area uv.Rectangle) uint64 {
	h := fnv.New64a()
	fmt.Fprintf(h, "w=%d;h=%d;theme=%s;", area.Dx(), area.Dy(), m.themeKey)

	if m.session != nil {
		fmt.Fprintf(h, "sid=%s;title=%s;ct=%d;pt=%d;cost=%f;eu=%t;",
			m.session.ID, m.session.Title,
			m.session.CompletionTokens, m.session.PromptTokens,
			m.session.Cost, m.session.EstimatedUsage)
	}

	fmt.Fprintf(h, "cwd=%s;hyper=%t;", m.com.Workspace.WorkingDir(), m.com.IsHyper())
	if m.hyperCredits != nil {
		fmt.Fprintf(h, "hc=%d;", *m.hyperCredits)
	}
	// The logo string is already cached; fold it in so any change to it (or
	// the small-render breakpoint) invalidates the sidebar.
	h.Write([]byte("logo="))
	h.Write([]byte(m.sidebarLogo))
	h.Write([]byte{';'})

	if model := m.selectedLargeModel(); model != nil {
		fmt.Fprintf(h, "model=%s|%s|reason=%t|lvls=%d|def=%s|think=%t|eff=%s|ctx=%d;",
			model.ModelCfg.Provider, model.CatwalkCfg.Name,
			model.CatwalkCfg.CanReason, len(model.CatwalkCfg.ReasoningLevels),
			model.CatwalkCfg.DefaultReasoningEffort, model.ModelCfg.Think,
			model.ModelCfg.ReasoningEffort, model.CatwalkCfg.ContextWindow)
	} else {
		h.Write([]byte("model=nil;"))
	}

	// Files.
	for i := range m.sessionFiles {
		f := &m.sessionFiles[i]
		fmt.Fprintf(h, "f%d=%s|%d|%d|%d;", i,
			f.LatestVersion.Path, f.Additions, f.Deletions, f.LatestVersion.UpdatedAt)
	}

	// LSPs (sorted by name for a stable fingerprint), with diagnostic counts.
	lspNames := make([]string, 0, len(m.lspStates))
	for name := range m.lspStates {
		lspNames = append(lspNames, name)
	}
	sort.Strings(lspNames)
	for _, name := range lspNames {
		st := m.lspStates[name]
		counts := m.com.Workspace.LSPGetDiagnosticCounts(name)
		// Hash the error text, not just its presence: the render prints
		// the message, so a changed message within the same state must
		// invalidate the cache.
		fmt.Fprintf(h, "l=%s|%v|err=%s|%d/%d/%d/%d;",
			st.Name, st.State, errText(st.Error),
			counts.Error, counts.Warning, counts.Hint, counts.Information)
	}

	// MCPs (config order, matching the render).
	for _, mcpCfg := range m.com.Config().MCP.Sorted() {
		state, ok := m.mcpStates[mcpCfg.Name]
		if !ok {
			continue
		}
		fmt.Fprintf(h, "m=%s|%v|err=%s|%d/%d/%d;",
			state.Name, state.State, errText(state.Error),
			state.Counts.Tools, state.Counts.Prompts, state.Counts.Resources)
	}

	// Skills.
	for _, it := range m.skillStatusItems() {
		fmt.Fprintf(h, "s=%s|%s|%s;", it.icon, it.name, it.title)
	}

	return h.Sum64()
}

// errText returns err's message, or "" when nil. Used to fold error text
// into cache fingerprints.
func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// sidebar renders the chat sidebar containing session title, working
// directory, model info, file list, LSP status, and MCP status.
func (m *UI) drawSidebar(scr uv.Screen, area uv.Rectangle) {
	if m.session == nil {
		return
	}

	key := m.sidebarFingerprint(area)
	if !m.hasSidebarCache || key != m.sidebarCacheKey {
		m.sidebarView = m.buildSidebar(area)
		m.sidebarCacheKey = key
		m.hasSidebarCache = true
	}

	uv.NewStyledString(m.sidebarView).Draw(scr, area)
}

// buildSidebar renders the sidebar content block as a string.
func (m *UI) buildSidebar(area uv.Rectangle) string {
	const logoHeightBreakpoint = 30

	t := m.com.Styles
	width := area.Dx()
	height := area.Dy()

	title := t.Sidebar.SessionTitle.Width(width).MaxHeight(2).Render(m.session.Title)
	cwd := common.PrettyPath(t, m.com.Workspace.WorkingDir(), width)
	sidebarLogo := m.sidebarLogo
	if height < logoHeightBreakpoint {
		sidebarLogo = logo.SmallRender(m.com.Styles, width, logo.Opts{
			Hyper: m.com.IsHyper(),
		})
	}
	blocks := []string{
		sidebarLogo,
		title,
		"",
		cwd,
		"",
		m.modelInfo(width),
		"",
	}

	sidebarHeader := lipgloss.JoinVertical(
		lipgloss.Left,
		blocks...,
	)

	var remainingHeightArea image.Rectangle
	layout.Vertical(
		layout.Len(lipgloss.Height(sidebarHeader)),
		layout.Fill(1),
	).Split(m.layout.sidebar).Assign(new(image.Rectangle), &remainingHeightArea)
	remainingHeight := remainingHeightArea.Dy() - 6
	filesCount := 0
	for _, f := range m.sessionFiles {
		if f.Additions == 0 && f.Deletions == 0 {
			continue
		}
		filesCount++
	}

	lspsCount := len(m.lspStates)

	mcpsCount := 0
	for _, mcpCfg := range m.com.Config().MCP.Sorted() {
		if _, ok := m.mcpStates[mcpCfg.Name]; ok {
			mcpsCount++
		}
	}

	skillsCount := len(m.skillStatusItems())

	maxFiles, maxLSPs, maxMCPs, maxSkills := getDynamicHeightLimits(remainingHeight, filesCount, lspsCount, mcpsCount, skillsCount)

	lspSection := m.lspInfo(width, maxLSPs, true)
	mcpSection := m.mcpInfo(width, maxMCPs, true)
	skillsSection := m.skillsInfo(width, maxSkills, true)
	filesSection := m.filesInfo(m.com.Workspace.WorkingDir(), width, maxFiles, true)

	return lipgloss.NewStyle().
		MaxWidth(width).
		MaxHeight(height).
		Render(
			lipgloss.JoinVertical(
				lipgloss.Left,
				sidebarHeader,
				filesSection,
				"",
				lspSection,
				"",
				mcpSection,
				"",
				skillsSection,
			),
		)
}
