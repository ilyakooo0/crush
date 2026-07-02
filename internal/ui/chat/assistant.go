package chat

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/anim"
	"github.com/charmbracelet/crush/internal/ui/common"
	"github.com/charmbracelet/crush/internal/ui/list"
	"github.com/charmbracelet/crush/internal/ui/styles"
	"github.com/charmbracelet/x/ansi"
)

// assistantMessageTruncateFormat is the text shown when an assistant message is
// truncated in the collapsed state.
const assistantMessageTruncateFormat = "… (%d lines hidden) [click or space to expand]"

// assistantMessageTailWindowFormat is shown above a tail-windowed thinking
// block to advertise that earlier lines exist and that the user can
// promote the view to a full expansion. The promotion is wired through
// the existing ToggleExpanded path (click / space) — F5 deliberately
// does not add a new keybinding.
const assistantMessageTailWindowFormat = "… (%d earlier lines hidden) [click or space to expand]"

// maxCollapsedThinkingHeight defines the maximum height of the thinking
const maxCollapsedThinkingHeight = 10

// maxExpandedThinkingTailLines is the F5 tail-window cap. When the user
// expands a thinking block whose post-glamour line count exceeds this
// threshold, only the last N lines are shown with an affordance line
// indicating how many earlier lines are hidden. Clicking / pressing
// space again promotes the view to a full expansion. The slice is
// taken AFTER glamour render (not before) so fenced code blocks,
// lists, and tables are not torn at arbitrary boundaries.
const maxExpandedThinkingTailLines = 200

// thinkingViewMode is the F5 three-state view machine for the thinking
// block. ToggleExpanded cycles
// collapsed → tail-window → full-expanded → collapsed, skipping the
// tail-window step when the rendered thinking fits within the cap so
// short blocks still toggle in two clicks.
type thinkingViewMode uint8

const (
	thinkingCollapsed thinkingViewMode = iota
	thinkingTailWindow
	thinkingFullExpanded
)

// assistantSection is a per-section render cache for AssistantMessageItem.
// Each section (thinking, content, error) carries its own keys so that
// streaming a section does not invalidate a different — often more
// expensive — section's cached render. srcHash is an FNV-64 of the
// section's source text; extra captures any other state that changes
// the rendered output (e.g. thinkingExpanded, the thinking footer
// inputs). valid disambiguates a real cache hit from the zero value
// when both source text and extras hash to zero. aux carries any
// per-section side data that the caller needs to recover on a hit
// (e.g. the thinking box height for click detection).
type assistantSection struct {
	width   int
	srcHash uint64
	extra   uint64
	out     string
	h       int
	aux     int
	valid   bool
}

// hit reports whether the cache entry matches the requested key.
func (s *assistantSection) hit(width int, srcHash, extra uint64) bool {
	return s.valid && s.width == width && s.srcHash == srcHash && s.extra == extra
}

// store records the rendered output under the given key.
func (s *assistantSection) store(width int, srcHash, extra uint64, out string, aux int) {
	s.width = width
	s.srcHash = srcHash
	s.extra = extra
	s.out = out
	s.h = lipgloss.Height(out)
	s.aux = aux
	s.valid = true
}

// reset drops the cached output.
func (s *assistantSection) reset() {
	*s = assistantSection{}
}

// FNV-64a constants. We fold bytes into a plain uint64 accumulator
// rather than allocating a fnv.New64a per call: the returned
// hash.Hash64 escapes to the heap, which is wasteful on the per-render
// hashing hot path. The algorithm (offset basis, prime, xor-then-multiply)
// is identical to hash/fnv, so hashes are byte-for-byte the same.
const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211
)

// fnvAddString folds s into an in-progress FNV-64a hash.
func fnvAddString(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

// fnvAddBytes folds b into an in-progress FNV-64a hash.
func fnvAddBytes(h uint64, b []byte) uint64 {
	for _, c := range b {
		h ^= uint64(c)
		h *= fnvPrime64
	}
	return h
}

// fnvAddU64 folds v's 8 little-endian bytes into an in-progress hash,
// matching binary.LittleEndian.PutUint64 followed by a Write.
func fnvAddU64(h, v uint64) uint64 {
	for i := 0; i < 8; i++ {
		h ^= uint64(byte(v >> (8 * i)))
		h *= fnvPrime64
	}
	return h
}

// fnv64 hashes a single string with FNV-64a.
func fnv64(s string) uint64 {
	return fnvAddString(fnvOffset64, s)
}

// fnvFields hashes a list of byte fields with length-prefix framing
// so that no concatenation collision can occur between distinct
// field tuples (a NUL inside one field cannot impersonate a
// boundary between two fields). Each field is preceded by its
// length encoded as 8 bytes little-endian.
func fnvFields(fields ...[]byte) uint64 {
	h := uint64(fnvOffset64)
	for _, f := range fields {
		h = fnvAddU64(h, uint64(len(f)))
		h = fnvAddBytes(h, f)
	}
	return h
}

// assistantRenderKeys bundles the per-render cache keys and tool-call
// count computed once at the top of Render/RawRender and threaded
// through the render path, so each section's source text is hashed only
// once and ToolCalls() is materialized only once per render.
type assistantRenderKeys struct {
	thinkSrc, thinkExtra     uint64
	contentSrc, contentExtra uint64
	errSrc, errExtra         uint64
	toolCallCount            int
}

// renderKeys computes the per-render keys once. It calls ToolCalls()
// exactly once and reuses its length everywhere the render path needs it.
func (a *AssistantMessageItem) renderKeys() assistantRenderKeys {
	toolCallCount := len(a.message.ToolCalls())
	thinkSrc, thinkExtra := a.thinkingKey(toolCallCount)
	contentSrc, contentExtra := a.contentKey()
	errSrc, errExtra := a.errorKey()
	return assistantRenderKeys{
		thinkSrc:      thinkSrc,
		thinkExtra:    thinkExtra,
		contentSrc:    contentSrc,
		contentExtra:  contentExtra,
		errSrc:        errSrc,
		errExtra:      errExtra,
		toolCallCount: toolCallCount,
	}
}

// AssistantMessageItem represents an assistant message in the chat UI.
//
// This item includes thinking, and the content but does not include the tool calls.
type AssistantMessageItem struct {
	*list.Versioned
	*highlightableMessageItem
	*cachedMessageItem
	*focusableMessageItem

	message           *message.Message
	sty               *styles.Styles
	anim              *anim.Anim
	thinkingViewMode  thinkingViewMode
	thinkingBoxHeight int // Tracks the rendered thinking box height for click detection.

	// Per-section render caches. Splitting these out means content
	// streaming does not invalidate the (often expensive) thinking
	// render, and vice versa.
	thinkingSec assistantSection
	contentSec  assistantSection
	errorSec    assistantSection

	// streamingContent caches a "stable prefix" glamour render of
	// the assistant content body so each streaming flush only
	// re-renders the trailing partial. F8 of
	// docs/notes/2026-05-12-chat-rendering-perf.md. See
	// streaming_markdown.go for the full algorithm.
	streamingContent streamingMarkdown

	// streamingThinking applies the same stable-prefix caching to
	// the thinking/reasoning section. Without this, every streaming
	// delta forces a full glamour re-render of the entire accumulated
	// thinking text, which burns CPU and starves the terminal emulator
	// during long reasoning traces.
	streamingThinking streamingMarkdown
}

var _ Expandable = (*AssistantMessageItem)(nil)

// NewAssistantMessageItem creates a new AssistantMessageItem.
func NewAssistantMessageItem(sty *styles.Styles, message *message.Message) MessageItem {
	v := list.NewVersioned()
	a := &AssistantMessageItem{
		Versioned:                v,
		highlightableMessageItem: defaultHighlighter(sty, v),
		cachedMessageItem:        &cachedMessageItem{},
		focusableMessageItem:     newFocusableMessageItem(v),
		message:                  message,
		sty:                      sty,
	}

	a.anim = anim.New(anim.Settings{
		ID:          a.ID(),
		Size:        15,
		GradColorA:  sty.WorkingGradFromColor,
		GradColorB:  sty.WorkingGradToColor,
		LabelColor:  sty.WorkingLabelColor,
		CycleColors: true,
	})
	return a
}

// StartAnimation starts the assistant message animation if it should be spinning.
func (a *AssistantMessageItem) StartAnimation() tea.Cmd {
	if !a.isSpinning() {
		return nil
	}
	return a.anim.Start()
}

// Animate progresses the assistant message animation if it should be spinning.
func (a *AssistantMessageItem) Animate(msg anim.StepMsg) tea.Cmd {
	if !a.isSpinning() {
		return nil
	}
	// Bump the F6 list-cache version so the next draw re-renders
	// this item: a spinner tick mutates anim's internal frame
	// counter, which changes the rendered output but is invisible
	// to the per-section content hashes. Without the bump the
	// list cache would serve the previously rendered frame
	// indefinitely and the spinner would appear frozen.
	a.Bump()
	return a.anim.Animate(msg)
}

// ID implements MessageItem.
func (a *AssistantMessageItem) ID() string {
	return a.message.ID
}

// RawRender implements [MessageItem].
func (a *AssistantMessageItem) RawRender(width int) string {
	return a.rawRender(width, a.renderKeys())
}

// rawRender renders the item using per-render keys computed once by the
// caller, so the hashing/tool-call work is shared with Render.
func (a *AssistantMessageItem) rawRender(width int, keys assistantRenderKeys) string {
	cappedWidth := cappedMessageWidth(width)

	var spinner string
	if a.isSpinningWithToolCalls(keys.toolCallCount) {
		spinner = a.renderSpinning()
	}

	content, height := a.renderMessageContent(cappedWidth, keys)
	highlightedContent := a.renderHighlighted(content, cappedWidth, height)
	if spinner != "" {
		if highlightedContent != "" {
			highlightedContent += "\n\n"
		}
		return highlightedContent + spinner
	}

	return highlightedContent
}

// Render implements MessageItem.
func (a *AssistantMessageItem) Render(width int) string {
	// XXX: Here, we're manually applying the focused/blurred styles because
	// using lipgloss.Render can degrade performance for long messages due to
	// it's wrapping logic.
	// We already know that the content is wrapped to the correct width in
	// RawRender, so we can just apply the styles directly to each line.
	//
	// The split + per-line prefix loop is O(L); cache the result keyed
	// by (width, focused, sectionsFingerprint) so steady-state Render
	// becomes a pointer return. The sectionsFingerprint folds in the
	// per-section srcHash/extra so that any sub-cache change
	// invalidates this prefix cache without requiring an explicit
	// drop. Bypass the cache while spinning (RawRender's spinner
	// suffix changes every animation frame) or while a highlight
	// range is active (selection drag).
	keys := a.renderKeys()
	useCache := !a.isSpinningWithToolCalls(keys.toolCallCount) && !a.isHighlighted()
	cappedWidth := cappedMessageWidth(width)
	key := a.prefixCacheKey(cappedWidth, keys)
	if useCache {
		if cached, ok := a.getCachedPrefixedRender(width, key); ok {
			return cached
		}
	}
	// Only render the style actually used; rendering both wastes work
	// (lipgloss.Render is not free) when one is discarded.
	var prefix string
	if a.focused {
		prefix = a.sty.Messages.AssistantFocused.Render()
	} else {
		prefix = a.sty.Messages.AssistantBlurred.Render()
	}
	rendered := a.rawRender(width, keys)
	lines := strings.Split(rendered, "\n")
	var sb strings.Builder
	for i, line := range lines {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(prefix)
		sb.WriteString(line)
	}
	out := sb.String()
	if useCache {
		a.setCachedPrefixedRender(out, width, key)
	}
	return out
}

// prefixCacheKey builds the F3 prefixed-render cache key. We pack the
// focus bit into bit 0 and a fingerprint of the section caches into
// the upper bits, so any change to a sub-section's source text or
// extras forces the prefix cache to miss without needing an explicit
// drop. cappedWidth is included so a cached prefix never survives a
// section-cache miss caused by a width change. The finish reason is
// folded in too because it controls the composition of
// renderMessageContent (e.g. appending the constant "Canceled"
// string) — that decision lives outside any section's own hash.
func (a *AssistantMessageItem) prefixCacheKey(cappedWidth int, keys assistantRenderKeys) uint64 {
	h := uint64(fnvOffset64)
	h = fnvAddU64(h, uint64(cappedWidth))
	h = fnvAddU64(h, keys.thinkSrc)
	h = fnvAddU64(h, keys.thinkExtra)
	h = fnvAddU64(h, keys.contentSrc)
	h = fnvAddU64(h, keys.contentExtra)
	h = fnvAddU64(h, keys.errSrc)
	h = fnvAddU64(h, keys.errExtra)
	h = fnvAddU64(h, a.compositionKey())
	fingerprint := h
	var focusBit uint64
	if a.focused {
		focusBit = 1
	}
	return (fingerprint &^ 1) | focusBit
}

// compositionKey hashes the inputs to renderMessageContent's structural
// decisions (which sections to include, whether to append the
// constant "Canceled" footer) so that flipping IsFinished or the
// finish reason invalidates the prefix cache even when no section's
// own source text changed.
func (a *AssistantMessageItem) compositionKey() uint64 {
	var finishedFlag byte
	var reason string
	if a.message.IsFinished() {
		finishedFlag = 1
		reason = string(a.message.FinishReason())
	}
	// Length-prefixed framing keeps the finished flag and the reason
	// string from blending into one another.
	return fnvFields([]byte{finishedFlag}, []byte(reason))
}

// renderMessageContent renders the message content including thinking, main
// content, and finish reason. Each section is served from its own cache;
// only the section whose source text or extras changed since the last
// render is recomputed.
func (a *AssistantMessageItem) renderMessageContent(width int, keys assistantRenderKeys) (string, int) {
	var messageParts []string
	thinking := strings.TrimSpace(a.message.ReasoningContent().Thinking)
	content := strings.TrimSpace(a.message.Content().Text)

	if thinking != "" {
		messageParts = append(messageParts, a.cachedThinking(width, keys))
	}

	if content != "" {
		if thinking != "" {
			messageParts = append(messageParts, "")
		}
		messageParts = append(messageParts, a.cachedContent(width, keys))
	}

	if a.message.IsFinished() {
		switch a.message.FinishReason() {
		case message.FinishReasonCanceled:
			messageParts = append(messageParts, a.sty.Messages.AssistantCanceled.Render("Canceled"))
		case message.FinishReasonError:
			messageParts = append(messageParts, a.cachedError(width, keys))
		}
	}

	out := strings.Join(messageParts, "\n")
	return out, lipgloss.Height(out)
}

// thinkingKey returns the (srcHash, extra) cache key components for the
// thinking section. extra folds in everything other than the raw
// thinking text that affects the rendered output: the view mode
// (collapsed / tail-window / full) and the footer state (which
// depends on IsThinking, ToolCalls, and ThinkingDuration).
func (a *AssistantMessageItem) thinkingKey(toolCallCount int) (uint64, uint64) {
	thinking := a.message.ReasoningContent().Thinking
	srcHash := fnv64(thinking)

	showFooter := !a.message.IsThinking() || toolCallCount > 0
	var durationStr string
	if showFooter {
		duration := a.message.ThinkingDuration()
		if duration.String() != "0s" {
			durationStr = duration.String()
		}
	}
	var footer byte
	if showFooter {
		footer = 1
	}
	// Length-prefixed framing avoids any delimiter collision between
	// the flag bytes and the duration string. The view mode is folded
	// in so that toggling collapsed ↔ tail-window ↔ full invalidates
	// only the thinking section, not content/error.
	extra := fnvFields([]byte{byte(a.thinkingViewMode), footer}, []byte(durationStr))
	return srcHash, extra
}

// contentKey returns the (srcHash, extra) cache key components for the
// main content section.
func (a *AssistantMessageItem) contentKey() (uint64, uint64) {
	return fnv64(a.message.Content().Text), 0
}

// errorKey returns the (srcHash, extra) cache key components for the
// error section. Returns (0, 0) when no error is present so the cache
// stays a no-op for non-error messages.
func (a *AssistantMessageItem) errorKey() (uint64, uint64) {
	if !a.message.IsFinished() || a.message.FinishReason() != message.FinishReasonError {
		return 0, 0
	}
	finishPart := a.message.FinishPart()
	if finishPart == nil {
		return 0, 0
	}
	// Length-prefixed framing prevents Message+Details collisions
	// between distinct (Message, Details) tuples that would
	// otherwise concatenate to the same byte sequence.
	return fnvFields([]byte(finishPart.Message), []byte(finishPart.Details)), 0
}

// cachedThinking returns the rendered thinking section, computing and
// caching it on miss. The thinking-box height (used for click target
// detection) is preserved across hits via assistantSection.aux so the
// cached path never desyncs click detection.
func (a *AssistantMessageItem) cachedThinking(width int, keys assistantRenderKeys) string {
	srcHash, extra := keys.thinkSrc, keys.thinkExtra
	if a.thinkingSec.hit(width, srcHash, extra) {
		a.thinkingBoxHeight = a.thinkingSec.aux
		return a.thinkingSec.out
	}
	out := a.renderThinking(a.message.ReasoningContent().Thinking, width, keys.toolCallCount)
	a.thinkingSec.store(width, srcHash, extra, out, a.thinkingBoxHeight)
	return out
}

// cachedContent returns the rendered content section.
func (a *AssistantMessageItem) cachedContent(width int, keys assistantRenderKeys) string {
	srcHash, extra := keys.contentSrc, keys.contentExtra
	if a.contentSec.hit(width, srcHash, extra) {
		return a.contentSec.out
	}
	out := a.renderMarkdown(a.message.Content().Text, width)
	a.contentSec.store(width, srcHash, extra, out, 0)
	return out
}

// cachedError returns the rendered error section.
func (a *AssistantMessageItem) cachedError(width int, keys assistantRenderKeys) string {
	srcHash, extra := keys.errSrc, keys.errExtra
	if a.errorSec.hit(width, srcHash, extra) {
		return a.errorSec.out
	}
	out := a.renderError(width)
	a.errorSec.store(width, srcHash, extra, out, 0)
	return out
}

// renderThinking renders the thinking/reasoning content with footer.
//
// Slicing happens AFTER glamour rendering so fenced code blocks, list
// continuations, and tables are not split mid-block — the same
// boundary problem §4.4 of the design note flags. The bordered
// ThinkingBox style is applied on top of the (already-windowed)
// lines so the visual box matches what the user sees today.
func (a *AssistantMessageItem) renderThinking(thinking string, width int, toolCallCount int) string {
	renderer := common.QuietMarkdownRenderer(a.sty, width)

	// Cap the raw text handed to glamour while the block is windowed.
	// The collapsed and tail-window views only ever DISPLAY the last
	// N rendered lines, so rendering the entire (unbounded) reasoning
	// trace on every ~33ms streaming flush is wasted work that grows
	// with the trace — O(n) per flush, O(n²) over the stream — and is
	// the source of the "long thinking makes the UI lag" problem.
	// Full expansion is an explicit request to see everything and is
	// never bounded. droppedLines keeps the "N lines hidden" count
	// roughly truthful and never affects what is displayed.
	windowed, droppedLines := boundThinkingInput(thinking, a.thinkingViewMode, width)
	rendered := a.streamingThinking.Render(windowed, width, renderer)
	rendered = strings.TrimSpace(rendered)

	lines := strings.Split(rendered, "\n")
	renderedLines := len(lines)
	// totalLines counts everything the trace would render to, so the
	// affordance's hidden-line count stays honest even though only
	// the windowed tail was actually rendered.
	totalLines := renderedLines + droppedLines

	switch a.thinkingViewMode {
	case thinkingCollapsed:
		if totalLines > maxCollapsedThinkingHeight {
			if renderedLines > maxCollapsedThinkingHeight {
				lines = lines[renderedLines-maxCollapsedThinkingHeight:]
			}
			hint := a.sty.Messages.ThinkingTruncationHint.Render(
				fmt.Sprintf(assistantMessageTruncateFormat, totalLines-maxCollapsedThinkingHeight),
			)
			lines = append([]string{hint, ""}, lines...)
		}
	case thinkingTailWindow:
		if totalLines > maxExpandedThinkingTailLines {
			if renderedLines > maxExpandedThinkingTailLines {
				lines = lines[renderedLines-maxExpandedThinkingTailLines:]
			}
			hint := a.sty.Messages.ThinkingTruncationHint.Render(
				fmt.Sprintf(assistantMessageTailWindowFormat, totalLines-maxExpandedThinkingTailLines),
			)
			lines = append([]string{hint, ""}, lines...)
		}
	}

	thinkingStyle := a.sty.Messages.ThinkingBox.Width(width)
	result := thinkingStyle.Render(strings.Join(lines, "\n"))
	a.thinkingBoxHeight = lipgloss.Height(result)

	var footer string
	// if thinking is done add the thought for footer
	if !a.message.IsThinking() || toolCallCount > 0 {
		duration := a.message.ThinkingDuration()
		if duration.String() != "0s" {
			footer = a.sty.Messages.ThinkingFooterTitle.Render("Thought for ") +
				a.sty.Messages.ThinkingFooterDuration.Render(duration.String())
		}
	}

	if footer != "" {
		result += "\n\n" + footer
	}

	return result
}

// boundThinkingInput caps the raw reasoning text fed to glamour so a
// streaming flush costs O(window) instead of O(entire trace). It
// returns the (possibly shortened) text to render plus the number of
// source lines dropped ahead of the window.
//
// A full render is preserved for [thinkingFullExpanded] (the user
// asked to see everything) and whenever the trace already fits the
// budget, so short blocks and the non-streaming steady state behave
// exactly as before.
//
// The window is snapped forward to a line boundary so a source line
// is never cut mid-way, and a code fence left open by the cut is
// re-opened at the window head so the tail keeps rendering as code
// (matching reality) instead of flipping to prose. Any other
// construct a raw cut might tear — a list, table, or blockquote at
// the very top of the window — only corrupts lines that sit above
// the displayed tail slice and are discarded before display.
func boundThinkingInput(thinking string, mode thinkingViewMode, width int) (windowed string, droppedLines int) {
	budget := thinkingWindowBytes(mode, width)
	if budget <= 0 || len(thinking) <= budget {
		return thinking, 0
	}
	// The streamingMarkdown incremental cache already keeps a flush
	// cheap when a safe boundary sits within `budget` of the end: it
	// re-renders only the small trailing segment. Windowing would
	// break that fast path (a sliding window is never a prefix
	// extension, forcing a cache reset every flush), so only window
	// when the tail since the last boundary is itself larger than the
	// budget — the "wall of text with no paragraph break" case the
	// cache cannot handle.
	if b := findSafeMarkdownBoundary(thinking); b >= 0 && len(thinking)-b <= budget {
		return thinking, 0
	}
	cut := len(thinking) - budget
	// Snap forward to the start of the next line so the window never
	// begins in the middle of a source line.
	if nl := strings.IndexByte(thinking[cut:], '\n'); nl >= 0 {
		cut += nl + 1
	}
	dropped := thinking[:cut]
	windowed = thinking[cut:]
	// Re-open a code fence left dangling by the cut so the window's
	// code renders as code rather than prose.
	if fenceLineCount(dropped)%2 != 0 {
		windowed = "```\n" + windowed
	}
	return windowed, strings.Count(dropped, "\n")
}

// thinkingWindowBytes returns the maximum bytes of raw reasoning text
// to render for a view mode, or -1 for "no bound". The budget scales
// with the wrap width so the window still comfortably overfills the
// visible line cap after wrapping, with a floor for narrow widths.
func thinkingWindowBytes(mode thinkingViewMode, width int) int {
	if width < 1 {
		width = 1
	}
	switch mode {
	case thinkingCollapsed:
		return max(4096, maxCollapsedThinkingHeight*width*8)
	case thinkingTailWindow:
		return max(32768, maxExpandedThinkingTailLines*width*4)
	default:
		return -1
	}
}

// fenceLineCount counts fenced-code-block delimiter lines in s. An
// odd count means a fence is still open at the end of s. Reuses the
// same [isFenceLine] rule as the streaming-markdown boundary scanner.
func fenceLineCount(s string) int {
	n := 0
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' {
			continue
		}
		if isFenceLine(s[start:i]) {
			n++
		}
		start = i + 1
	}
	if start < len(s) && isFenceLine(s[start:]) {
		n++
	}
	return n
}

// renderMarkdown renders content as markdown. F8 routes the call
// through streamingContent, which caches the glamour render of a
// "stable prefix" so each streaming flush only re-renders the
// trailing partial. The streaming cache invalidates itself on
// width change and on any content that is not a prefix-extension
// of the previously rendered content (e.g. user retried the
// turn), and falls back to a full render whenever boundary
// detection has the slightest doubt — see
// findSafeMarkdownBoundary.
func (a *AssistantMessageItem) renderMarkdown(content string, width int) string {
	renderer := common.MarkdownRenderer(a.sty, width)
	return a.streamingContent.Render(content, width, renderer)
}

func (a *AssistantMessageItem) renderSpinning() string {
	if a.message.IsThinking() {
		a.anim.SetLabel("Thinking")
	} else if a.message.IsSummaryMessage {
		a.anim.SetLabel("Summarizing")
	}
	return a.anim.Render()
}

// renderError renders an error message.
func (a *AssistantMessageItem) renderError(width int) string {
	finishPart := a.message.FinishPart()
	errTag := a.sty.Messages.ErrorTag.Render("ERROR")
	truncated := ansi.Truncate(finishPart.Message, width-2-lipgloss.Width(errTag), "...")
	title := fmt.Sprintf("%s %s", errTag, a.sty.Messages.ErrorTitle.Render(truncated))
	details := a.sty.Messages.ErrorDetails.Width(width - 2).Render(finishPart.Details)
	return fmt.Sprintf("%s\n\n%s", title, details)
}

// isSpinning returns true if the assistant message is still generating.
func (a *AssistantMessageItem) isSpinning() bool {
	return a.isSpinningWithToolCalls(len(a.message.ToolCalls()))
}

// isSpinningWithToolCalls is isSpinning given an already-computed
// tool-call count, so the render path avoids re-materializing ToolCalls().
func (a *AssistantMessageItem) isSpinningWithToolCalls(toolCallCount int) bool {
	isThinking := a.message.IsThinking()
	isFinished := a.message.IsFinished()
	hasContent := strings.TrimSpace(a.message.Content().Text) != ""
	return (isThinking || !isFinished) && !hasContent && toolCallCount == 0
}

// SetMessage is used to update the underlying message. Only the
// sub-section caches whose source text or extras changed are
// invalidated; the others survive and serve cache hits on the next
// RawRender.
func (a *AssistantMessageItem) SetMessage(msg *message.Message) tea.Cmd {
	wasSpinning := a.isSpinning()
	a.message = msg
	// Bump the F6 version even if the underlying *message.Message
	// pointer is identical: callers may have mutated the message in
	// place (delta append) and we cannot tell from here. The
	// per-section caches dedupe identical content via FNV-64 hashes,
	// so a redundant bump only costs one list-cache repopulation.
	a.Bump()
	// The prefix cache is keyed by a fingerprint that includes every
	// section's source hash, so an unchanged section keeps its prefix
	// cache valid while a changed section forces a miss naturally.
	// Section caches themselves are content-keyed, so they do not
	// need an explicit drop here either.
	if !wasSpinning && a.isSpinning() {
		return a.StartAnimation()
	}
	return nil
}

// Finished implements list.Item. The assistant message is freezable
// once the message reports IsFinished() and is no longer spinning
// (no animation tick remains pending). Streaming tail animation is
// caught by isSpinning, so freezing only kicks in once the turn is
// fully terminal. The list cache invalidates the entry on the next
// version bump if anything (focus, highlight, expansion) changes.
func (a *AssistantMessageItem) Finished() bool {
	return a.message.IsFinished() && !a.isSpinning()
}

// clearCache drops every cached render for this item, including the
// per-section caches. Shadows the embedded cachedMessageItem.clearCache
// so ClearItemCaches (style change) wipes the section caches too.
// F8: also drop the streaming-markdown stable-prefix cache because
// the cached glamour render embeds the OLD style's ANSI sequences
// and is no longer visually consistent with the new style.
func (a *AssistantMessageItem) clearCache() {
	a.cachedMessageItem.clearCache()
	a.thinkingSec.reset()
	a.contentSec.reset()
	a.errorSec.reset()
	a.streamingContent.Reset()
	a.streamingThinking.Reset()
}

// ToggleExpanded advances the F5 thinking view-mode cycle and returns
// whether the item is now in any expanded state (tail-window or full).
// The cycle is collapsed → tail-window → full → collapsed, with the
// tail-window step skipped when the rendered thinking fits within
// maxExpandedThinkingTailLines so short blocks remain a two-click
// toggle. Both the thinking section cache and the F3 prefix cache
// fold thinkingViewMode into their keys, so no explicit invalidation
// is required here.
//
// When the message carries no thinking text the toggle is a no-op:
// there is nothing to expand, and mutating the view mode would
// thrash the thinking-section cache key for no visible benefit.
func (a *AssistantMessageItem) ToggleExpanded() bool {
	if strings.TrimSpace(a.message.ReasoningContent().Thinking) == "" {
		return a.thinkingViewMode != thinkingCollapsed
	}
	switch a.thinkingViewMode {
	case thinkingCollapsed:
		if a.tailWindowWouldTruncate() {
			a.thinkingViewMode = thinkingTailWindow
		} else {
			a.thinkingViewMode = thinkingFullExpanded
		}
	case thinkingTailWindow:
		a.thinkingViewMode = thinkingFullExpanded
	case thinkingFullExpanded:
		a.thinkingViewMode = thinkingCollapsed
	}
	// View-mode changes alter the windowing slice applied after
	// glamour render. The streaming prefix cache may have been
	// seeded under a different slice regime, and glued renders are
	// not byte-identical to monolithic ones. Drop the prefix cache
	// so the next render is clean.
	a.streamingThinking.Reset()
	a.Bump()
	return a.thinkingViewMode != thinkingCollapsed
}

// tailWindowWouldTruncate reports whether the current thinking text
// is long enough that the tail-window step is worth inserting into
// the toggle cycle. We use a cheap source-text logical-line count
// as the heuristic rather than peeking into the cache: the cache
// may be populated in collapsed state (where its height is bounded
// by maxCollapsedThinkingHeight and tells us nothing about the
// underlying length), and re-running glamour just to count lines
// would defeat the cache. The heuristic can over-trigger (a source
// with many short lines may wrap to fewer than N lines), in which
// case the tail-window render is visually identical to full and
// the cycle costs the user one extra toggle — preferred over the
// alternative of failing to show the affordance on a genuinely
// long block.
//
// Logical line count is `1 + newlineCount` (a string with no
// newlines is one line). Comparing newline count alone introduced
// an off-by-one that let a source whose post-newline-split length
// equalled the cap skip the tail-window step.
func (a *AssistantMessageItem) tailWindowWouldTruncate() bool {
	lineCount := 1 + strings.Count(a.message.ReasoningContent().Thinking, "\n")
	return lineCount > maxExpandedThinkingTailLines
}

// HandleMouseClick implements MouseClickable. It signals (via a true return)
// that the click lies on the thinking box so the caller can invoke
// [AssistantMessageItem.ToggleExpanded] through the generic [Expandable]
// path. Toggling here directly would double-toggle because the caller always
// runs the generic path after a handled click.
func (a *AssistantMessageItem) HandleMouseClick(btn ansi.MouseButton, x, y int) bool {
	if btn != ansi.MouseLeft {
		return false
	}
	// Only the thinking box is clickable; other regions of the assistant
	// message should not trigger expansion.
	return a.thinkingBoxHeight > 0 && y < a.thinkingBoxHeight
}

// HandleKeyEvent implements KeyEventHandler.
func (a *AssistantMessageItem) HandleKeyEvent(key tea.KeyMsg) (bool, tea.Cmd) {
	if k := key.String(); k == "c" || k == "y" {
		text := a.message.Content().Text
		return true, common.CopyToClipboard(text, "Message copied to clipboard")
	}
	return false, nil
}
