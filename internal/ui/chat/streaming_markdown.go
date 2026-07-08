package chat

import (
	"strings"

	"charm.land/glamour/v2"
	"github.com/charmbracelet/crush/internal/ui/common"
)

// streamingMarkdown caches a "stable prefix" glamour render so each
// streaming flush only re-renders the trailing portion of the
// document. F8 of docs/notes/2026-05-12-chat-rendering-perf.md.
//
// The boundary between "stable" and "trailing" is detected by
// [findSafeMarkdownBoundary]: a position immediately after a blank
// line at which we can prove no markdown construct is open
// (fenced code block, list, table, block quote, setext header).
//
// Two renders concatenated are NOT generally equal to a single
// render of the whole document — glamour's wrap state is reset
// between calls. The boundary check is therefore deliberately
// conservative; whenever it has the slightest doubt the call
// falls back to a full render and the cache is left untouched.
//
// Invariants:
//
//   - stablePrefix is always a literal byte prefix of the most
//     recently rendered content. If a new content does not have
//     stablePrefix as its prefix the cache is dropped.
//   - stablePrefixRender is the glamour render of stablePrefix
//     alone, with surrounding whitespace trimmed for clean
//     concatenation.
//   - width is the glamour wrap width that produced
//     stablePrefixRender. A width change drops the cache.
type streamingMarkdown struct {
	width              int
	stablePrefix       string
	stablePrefixRender string
}

// Reset drops every cached field. After Reset the next Render call
// is guaranteed to be a full render.
func (s *streamingMarkdown) Reset() {
	s.width = 0
	s.stablePrefix = ""
	s.stablePrefixRender = ""
}

// Render returns the glamour render of content at the given width,
// reusing the cached stable-prefix render when it is safe to do so.
// On any uncertainty the call falls back to a full render via
// renderer and leaves the cache untouched (or drops it).
//
// The returned string has its trailing newline trimmed to match
// the existing renderMarkdown contract on AssistantMessageItem.
//
// Concurrency: glamour's Render is stateful and not safe for
// concurrent invocation on a shared renderer. Crush's TUI is
// single-threaded so production never contends, but parallel
// callers (most notably the test suite) must serialize. We hold
// [common.LockMarkdownRenderer] for the entire prefix +
// trailing render sequence so other goroutines cannot interleave
// their own Render calls and corrupt goldmark's BlockStack.
func (s *streamingMarkdown) Render(content string, width int, renderer *glamour.TermRenderer) string {
	mu := common.LockMarkdownRenderer(renderer)
	mu.Lock()
	defer mu.Unlock()
	full := func() string {
		out, err := renderer.Render(content)
		if err != nil {
			return content
		}
		return strings.TrimSuffix(out, "\n")
	}

	// Width change OR content not a prefix-extension: drop cache,
	// full render, optionally try to seed a fresh boundary on this
	// call (step "f" in the design note).
	if width != s.width || !strings.HasPrefix(content, s.stablePrefix) {
		s.Reset()
		s.width = width
		out := full()
		s.tryAdvanceFromEmpty(content, width, renderer)
		return out
	}

	boundary := findSafeMarkdownBoundary(content)
	if boundary < 0 {
		// No safe boundary anywhere yet. Full render; do not
		// modify the cache (a future flush may find one).
		return full()
	}

	if boundary <= len(s.stablePrefix) {
		// Cached prefix already covers an at-least-as-late
		// boundary. Render the trailing partial fresh and glue.
		trail := content[len(s.stablePrefix):]
		return glueRenders(s.stablePrefixRender, s.renderTrailing(trail, renderer))
	}

	// boundary > len(stablePrefix): we have a NEW chunk of safe
	// content. Render the new chunk, append to stablePrefixRender,
	// promote the boundary, then render the remaining trail.
	newChunk := content[len(s.stablePrefix):boundary]
	newChunkRender := s.renderTrailing(newChunk, renderer)
	s.stablePrefixRender = glueRenders(s.stablePrefixRender, newChunkRender)
	s.stablePrefix = content[:boundary]

	trail := content[boundary:]
	if trail == "" {
		// boundary == len(content): no trailing content. Returning
		// the cached prefix render directly is correct.
		return s.stablePrefixRender
	}
	return glueRenders(s.stablePrefixRender, s.renderTrailing(trail, renderer))
}

// tryAdvanceFromEmpty seeds the cache from a fresh state. We've
// already paid the cost of a full render of `content`; if there is
// a safe boundary inside it, render the prefix once more (cheap
// relative to the full render we just did) and cache it so the
// next flush can avoid the full work.
//
// This is the optional optimisation step "f" from the design
// note. We render the prefix separately rather than try to
// recover it from the full render output because two renders
// concatenated ≠ a single render of the whole, and we prefer the
// cached prefix render to be byte-for-byte what we'd produce on a
// future cached call.
func (s *streamingMarkdown) tryAdvanceFromEmpty(content string, width int, renderer *glamour.TermRenderer) {
	boundary := findSafeMarkdownBoundary(content)
	if boundary <= 0 {
		return
	}
	prefix := content[:boundary]
	out, err := renderer.Render(prefix)
	if err != nil {
		return
	}
	s.stablePrefix = prefix
	s.stablePrefixRender = trimGlamourMargins(out)
	s.width = width
}

// renderTrailing renders a trailing partial as a fresh glamour
// document and trims the surrounding whitespace so it can be
// concatenated to a cached prefix render without doubled blank
// lines.
func (s *streamingMarkdown) renderTrailing(text string, renderer *glamour.TermRenderer) string {
	if text == "" {
		return ""
	}
	out, err := renderer.Render(text)
	if err != nil {
		return text
	}
	return trimGlamourMargins(out)
}

// glueRenders concatenates two glamour-rendered fragments with a
// single blank line separator. Glamour outputs typically carry
// their own surrounding margins; trimming on both sides and
// gluing with "\n\n" prevents the visible double-margin seam.
//
// Empty fragments are tolerated so the same helper works for the
// "boundary == len(content)" path where there is no trailing
// segment.
func glueRenders(prefix, trail string) string {
	prefix = trimGlamourMargins(prefix)
	trail = trimGlamourMargins(trail)
	switch {
	case prefix == "" && trail == "":
		return ""
	case prefix == "":
		return trail
	case trail == "":
		return prefix
	default:
		return prefix + "\n\n" + trail
	}
}

// trimGlamourMargins strips leading and trailing whitespace
// (including newlines) from a glamour-rendered fragment.
// Glamour adds a leading blank line for documents that open with
// a heading or paragraph, plus a trailing newline; both must be
// removed before concatenation.
func trimGlamourMargins(s string) string {
	return strings.Trim(s, " \t\n")
}

// findSafeMarkdownBoundary returns the byte offset of the END of
// the latest safe boundary in content, i.e. the offset such that
// content[:boundary] is a valid stable-prefix candidate. The
// returned offset always points immediately after a blank-line
// separator, so concatenating a fresh render of content[boundary:]
// to a cached render of content[:boundary] does not require glamour
// to share state across the cut.
//
// Returns -1 when no safe boundary exists. SAFETY FIRST: any time
// we have the slightest doubt we return -1 and let the caller fall
// back to a full render.
//
// The implementation is a single O(n) forward scan
// ([scanSafeBoundaries]) that tracks block state across the whole
// document: fenced-code toggle, list open/close depth, HTML-block
// opener, and link-reference-definition presence. State is updated
// per line, so every blank-line candidate is evaluated against
// already-computed state in O(1) — no re-scanning of the prefix.
//
// Closure-aware list tracking (B1) replaces the prior "any list
// marker anywhere → reject" rule. A list is considered open from
// the first marker line until a blank line is followed by a
// non-continuation, non-marker line at the same or shallower
// indent. Once closed, prefixes ending after the close are
// accepted, so the incremental cache resumes working for the
// paragraphs that follow a list — the common case in LLM reasoning
// traces. On any ambiguity the list is kept open (conservative —
// falls back to -1).
//
// B2 (HTML block) and B3 (link reference definition) remain
// anywhere-in-prefix rejects: the typical assistant output
// contains neither, so the perf cost is zero in the common case.
//
// Returns the byte offset of the first character AFTER the blank
// line, i.e. the start of the trailing segment.
func findSafeMarkdownBoundary(content string) int {
	boundaries := scanSafeBoundaries(content)
	if len(boundaries) == 0 {
		return -1
	}
	return boundaries[len(boundaries)-1]
}

// scanSafeBoundaries walks content once, O(n), tracking block
// state, and returns the byte offsets of every safe stable-prefix
// boundary in increasing order. A boundary is the byte offset
// immediately after a blank-line separator at which content[:p]
// can be rendered independently of content[p:].
//
// State carried across the scan:
//
//   - inFence: toggled by each fence line. While true, no boundary
//     is safe (we're inside a fenced code block) and list/html/ref
//     detection is skipped so code-block contents don't false-
//     trigger the hazards.
//   - listDepth: current open-list nesting. Incremented on a
//     marker line, decremented on closure (see below). A boundary
//     is only safe when listDepth == 0 (no list open).
//   - sawHTMLBlock / sawLinkRef: anywhere-in-prefix flags for B2
//     and B3. Once set they never clear — the prefix is
//     permanently unsafe for splitting.
//   - prevNonBlank: the last non-blank line seen before the
//     current candidate, for the last-line check
//     ([lineOpensConstruct]).
//
// Each blank-line candidate is evaluated in O(1) against the
// already-computed state, so the full scan is O(lines) regardless
// of how many candidates exist. This replaces the prior O(n²)
// backward search that re-scanned the prefix for every candidate.
//
// List closure detection (B1 fix): a list opens on any marker line
// when no list is open. A list closes when, after a blank line, a
// non-marker line appears at indent < listBaseIndent+2 (not a
// continuation paragraph). On any ambiguity the list is kept open
// (conservative — falls back to -1). This replaces the prior "any
// list marker anywhere → reject" rule, so boundaries after a
// closed list followed by paragraphs are accepted — the common
// case in LLM reasoning traces.
//
// Crucially, boundary evaluation happens BEFORE the state update
// for the current line: the boundary at the start of line L has
// prefix = content[:L], so the state must reflect everything
// BEFORE L, not including L's effects. For example, the boundary
// at the start of a fence-opening line is safe (the fence hasn't
// opened in the prefix yet); the boundary at the start of a list-
// marker line is safe (the list hasn't opened in the prefix yet).
func scanSafeBoundaries(content string) []int {
	if len(content) == 0 {
		return nil
	}

	var boundaries []int
	var prevNonBlank string
	inFence := false
	listDepth := 0
	sawHTMLBlock := false
	sawLinkRef := false

	// listBaseIndent is the indent column of the innermost open
	// list's markers. Used for closure detection: a non-marker
	// line at indent < listBaseIndent+2 after a blank line closes
	// the list.
	listBaseIndent := 0

	// blankPending tracks whether the line immediately before the
	// current line was blank. A boundary candidate exists at the
	// start of the first non-blank line after a blank line.
	blankPending := false

	// processLine handles one line (without its newline
	// terminator) starting at byte offset off. It evaluates any
	// pending boundary candidate, then updates block state for
	// the line.
	//
	// Boundary evaluation happens BEFORE the state update for the
	// current line: the boundary at the start of line L has
	// prefix = content[:off], so the state must reflect everything
	// BEFORE L. However, when an open list CLOSES at line L (L is
	// a non-marker, dedented line after a blank line), the prefix
	// contains a COMPLETE list — the boundary is safe even though
	// listDepth is still > 0 in the pre-L state. We compute
	// listClosesHere before evaluation and pass it as an override.
	processLine := func(line string, off int) {
		blank := strings.TrimRight(line, " \t") == ""

		if blank {
			blankPending = true
			return
		}

		trimmed := strings.TrimLeft(line, " \t")
		indent := len(line) - len(trimmed)
		isMarker := isListItemMarker(trimmed)

		// Compute whether this line closes the open list.
		// A list closes when a blank line is followed by a
		// non-marker line at indent < listBaseIndent+2 (not
		// a continuation paragraph).
		listClosesHere := listDepth > 0 && blankPending && !isMarker && indent < listBaseIndent+2

		// Non-blank line. If a blank line preceded, this is a
		// boundary candidate at offset `off`. Evaluate BEFORE
		// updating state — the prefix is content[:off].
		if blankPending {
			if isSafeBoundaryState(inFence, listDepth, listClosesHere, sawHTMLBlock, sawLinkRef, prevNonBlank, trimmed) {
				boundaries = append(boundaries, off)
			}
			blankPending = false
		}

		// Now update state for this line.
		if isFenceLine(line) {
			inFence = !inFence
			prevNonBlank = line
			return
		}

		if inFence {
			prevNonBlank = line
			return
		}

		// Update list depth (closure-aware B1).
		switch {
		case listDepth == 0 && isMarker:
			listDepth = 1
			listBaseIndent = indent
		case listDepth > 0 && isMarker:
			switch {
			case indent > listBaseIndent:
				// Nested list opens.
				listDepth++
				listBaseIndent = indent
			case indent < listBaseIndent:
				// Marker at shallower indent: close
				// current list. If an outer list
				// existed we'd need its base indent;
				// we don't track the stack, so be
				// conservative and close everything,
				// then open a new list at this indent.
				listDepth = 1
				listBaseIndent = indent
			}
			// Same indent: another item, depth unchanged.
		case listDepth > 0 && !isMarker && blankPending:
			// Blank line then non-marker line: check for
			// closure. A continuation paragraph is indented
			// 2+ relative to the marker.
			if indent < listBaseIndent+2 {
				// Dedent: list closed. Be conservative
				// and close all nesting levels.
				listDepth = 0
			}
			// Otherwise: continuation, list stays open.
		}
		// listDepth > 0 && !isMarker && !blankPending:
		//   A non-marker line directly after another line
		//   (no blank line between). This is either a lazy
		//   continuation or part of a tight list's item.
		//   Keep the list open (conservative).

		// B2: HTML block opener (anywhere in prefix).
		if !sawHTMLBlock && isHTMLBlockOpener(line) {
			sawHTMLBlock = true
		}
		// B3: link reference definition (anywhere in prefix).
		if !sawLinkRef && isLinkRefDefinition(line) {
			sawLinkRef = true
		}

		prevNonBlank = line
	}

	start := 0
	for i := 0; i < len(content); i++ {
		if content[i] != '\n' {
			continue
		}
		processLine(content[start:i], start)
		start = i + 1
	}
	// Process the trailing segment (content after the last '\n').
	// A non-blank trailing line after a blank line is a valid
	// boundary candidate. A blank trailing line is irrelevant.
	if start < len(content) {
		processLine(content[start:], start)
	}

	return boundaries
}

// isSafeBoundaryState evaluates the safety of a boundary candidate
// given the block state computed by the forward scan (reflecting
// everything BEFORE the boundary line). All state is passed in
// explicitly so this helper stays pure and testable.
//
//	nextLineTrimmed is the first non-blank line of the suffix
//	(already left-trimmed), used for the setext-underline check
//	(rule 4). listClosesHere reports whether the next line closes
//	the currently-open list; when true, the prefix contains a
//	complete list and the boundary is safe even though listDepth
//	is non-zero in the pre-line state.
func isSafeBoundaryState(inFence bool, listDepth int, listClosesHere, sawHTMLBlock, sawLinkRef bool, prevNonBlank, nextLineTrimmed string) bool {
	// (2) Not inside a fence.
	if inFence {
		return false
	}
	// (B1) No open list, unless the next line closes it.
	if listDepth != 0 && !listClosesHere {
		return false
	}
	// (B2) No HTML block opener anywhere in the prefix.
	if sawHTMLBlock {
		return false
	}
	// (B3) No link reference definition anywhere in the prefix.
	if sawLinkRef {
		return false
	}
	// (3) The last non-blank line of the prefix must not keep a
	// construct open. When the list closes at the next line, the
	// prefix's last non-blank line is a list marker — that's
	// expected and safe because the list is complete, so skip
	// the check in that case.
	if !listClosesHere && prevNonBlank != "" && lineOpensConstruct(prevNonBlank) {
		return false
	}
	// (4) The first non-blank line of the suffix must not look
	// like a setext underline that would retroactively turn the
	// last paragraph of the prefix into a header.
	if isSetextUnderlineCandidate(nextLineTrimmed) {
		return false
	}
	return true
}

// isFenceLine reports whether line opens or closes a fenced code
// block.
func isFenceLine(line string) bool {
	// Strip up to 3 spaces of indentation.
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) {
		return false
	}
	c := line[i]
	if c != '`' && c != '~' {
		return false
	}
	run := 0
	for i < len(line) && line[i] == c {
		i++
		run++
	}
	return run >= 3
}

// lineOpensConstruct reports whether line keeps a markdown
// construct open across the boundary. We err conservatively —
// any case that smells like list/table/quote/setext/indented-code
// returns true.
func lineOpensConstruct(line string) bool {
	// Indented code: a tab, or 4+ leading spaces.
	if len(line) > 0 && line[0] == '\t' {
		return true
	}
	if strings.HasPrefix(line, "    ") {
		return true
	}

	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return false
	}

	// Block quote.
	if trimmed[0] == '>' {
		return true
	}

	// List item: "- " "* " "+ " or "<digits>. " or "<digits>) ".
	if isListItemMarker(trimmed) {
		return true
	}

	// Table: any pipe character anywhere in the line. Conservative:
	// pipe-in-prose is rare and the cost of bailing is one slow
	// frame.
	if strings.ContainsRune(line, '|') {
		return true
	}

	// Setext underline candidate as the LAST line of the prefix:
	// this would be a setext header for an even-earlier paragraph.
	// Refuse to split at all in this case — the boundary is right
	// in the middle of a header.
	if isSetextUnderlineCandidate(trimmed) {
		return true
	}

	return false
}

// isListItemMarker reports whether line (already left-trimmed)
// starts with a CommonMark list-item marker followed by a space
// or tab.
func isListItemMarker(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	if c == '-' || c == '*' || c == '+' {
		if len(line) >= 2 && (line[1] == ' ' || line[1] == '\t') {
			return true
		}
		return false
	}
	// Ordered list: digits followed by '.' or ')' and a space.
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i > 9 {
		return false
	}
	if i >= len(line) {
		return false
	}
	if line[i] != '.' && line[i] != ')' {
		return false
	}
	if i+1 >= len(line) {
		return false
	}
	return line[i+1] == ' ' || line[i+1] == '\t'
}

// isSetextUnderlineCandidate reports whether line (with optional
// leading whitespace) consists entirely of '=' or entirely of '-'
// characters with optional trailing whitespace. CommonMark
// requires no leading whitespace on the underline; we accept up
// to three spaces for safety so an indented underline still
// blocks a split.
func isSetextUnderlineCandidate(line string) bool {
	// Strip leading whitespace.
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i == len(line) {
		return false
	}
	c := line[i]
	if c != '=' && c != '-' {
		return false
	}
	j := i
	for j < len(line) && line[j] == c {
		j++
	}
	// Allow trailing whitespace.
	for j < len(line) {
		if line[j] != ' ' && line[j] != '\t' {
			return false
		}
		j++
	}
	// Need at least one underline character. "-" alone is also a
	// list marker without a trailing space; the listItem check
	// covers the marker case before we get here.
	return j-i >= 1
}

// isHTMLBlockOpener reports whether line begins one of the seven
// CommonMark HTML block patterns. We accept up to three spaces of
// leading indentation (CommonMark rule). Matching is intentionally
// loose — we only need to know the line "looks like an HTML
// block start", not parse the contained markup.
func isHTMLBlockOpener(line string) bool {
	// Strip up to 3 spaces of indentation.
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	rest := line[i:]
	if len(rest) < 2 || rest[0] != '<' {
		return false
	}

	// Type 2: HTML comment "<!--".
	if strings.HasPrefix(rest, "<!--") {
		return true
	}
	// Type 3: processing instruction "<?".
	if strings.HasPrefix(rest, "<?") {
		return true
	}
	// Type 5: CDATA "<![CDATA[".
	if strings.HasPrefix(rest, "<![CDATA[") {
		return true
	}
	// Type 4: declaration "<!" followed by an ASCII letter.
	if len(rest) >= 3 && rest[1] == '!' && isASCIILetter(rest[2]) {
		return true
	}

	// Type 1: <script | <pre | <style | <textarea (case-insensitive)
	// followed by whitespace, '>', end-of-line, or other non-name
	// terminators. Use a permissive HasPrefix check on lowercase.
	low := strings.ToLower(rest)
	for _, t := range []string{"<script", "<pre", "<style", "<textarea"} {
		if strings.HasPrefix(low, t) {
			next := byte(0)
			if len(low) > len(t) {
				next = low[len(t)]
			}
			if next == 0 || next == ' ' || next == '\t' || next == '>' {
				return true
			}
		}
	}

	// Types 6 & 7: open or close of a block-level tag.
	//
	// Type 6 matches a fixed CommonMark tag set; type 7 matches any
	// otherwise-valid open/close tag whose name is not in the
	// script/pre/style/textarea family. We collapse both into a
	// single check: the line must start with '<' or '</' followed
	// by an ASCII letter. This deliberately mirrors the other
	// hazards — when in doubt, forfeit the boundary. Lines like
	// "<3", "<-", "<<", or mid-line "<foo>" do NOT trigger because
	// we require the line to *start* (after up to 3 spaces) with
	// '<letter' or '</letter'.
	j := 1 // past '<'
	if j < len(rest) && rest[j] == '/' {
		j++
	}
	if j >= len(rest) || !isASCIILetter(rest[j]) {
		return false
	}
	return true
}

// isASCIILetter reports whether b is an ASCII letter.
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isLinkRefDefinition reports whether line matches a CommonMark
// link reference definition opener. The conservative pattern:
//
//	^[ ]{0,3}\[[^\]]+\]:\s*\S+
//
// i.e. up to 3 spaces, then a bracketed label (no nested ']'),
// then a colon, then whitespace, then at least one non-whitespace
// character of destination. We do not validate the destination —
// presence of a ref-def opener anywhere in the prefix is enough
// to forfeit the boundary.
func isLinkRefDefinition(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) || line[i] != '[' {
		return false
	}
	i++
	labelStart := i
	for i < len(line) && line[i] != ']' {
		i++
	}
	if i >= len(line) || i == labelStart {
		// No closing bracket, or empty label.
		return false
	}
	// i points at ']'.
	i++
	if i >= len(line) || line[i] != ':' {
		return false
	}
	i++
	// Skip required whitespace.
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	// At least one non-whitespace character of destination.
	return i < len(line)
}
