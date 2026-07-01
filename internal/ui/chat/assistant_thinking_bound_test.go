package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBoundThinkingInput_ShortNotWindowed: content within the budget
// is returned verbatim so short blocks and the non-streaming steady
// state are untouched.
func TestBoundThinkingInput_ShortNotWindowed(t *testing.T) {
	t.Parallel()
	in := "a short thought\n\nwith two paragraphs"
	out, dropped := boundThinkingInput(in, thinkingCollapsed, 80)
	require.Equal(t, in, out)
	require.Zero(t, dropped)
}

// TestBoundThinkingInput_FullExpandedNeverWindowed: full expansion is
// an explicit request to see everything, so it is never bounded even
// for a huge trace.
func TestBoundThinkingInput_FullExpandedNeverWindowed(t *testing.T) {
	t.Parallel()
	in := strings.Repeat("wall of text with no breaks ", 100000)
	out, dropped := boundThinkingInput(in, thinkingFullExpanded, 80)
	require.Equal(t, in, out)
	require.Zero(t, dropped)
}

// TestBoundThinkingInput_BoundariedNotWindowed: a large trace whose
// tail has a recent safe boundary is NOT windowed — the incremental
// cache renders only the small trailing segment, so windowing (which
// would reset that cache every flush) must be skipped.
func TestBoundThinkingInput_BoundariedNotWindowed(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	for i := 0; i < 5000; i++ {
		b.WriteString("paragraph with a few words\n\n")
	}
	in := b.String()
	require.Greater(t, len(in), thinkingWindowBytes(thinkingCollapsed, 80),
		"fixture must exceed the budget so only the boundary gate can spare it")
	out, dropped := boundThinkingInput(in, thinkingCollapsed, 80)
	require.Equal(t, in, out, "boundaried content must not be windowed")
	require.Zero(t, dropped)
}

// TestBoundThinkingInput_WallWindowed: a long unbroken wall (no
// paragraph break the incremental cache can use) IS windowed to a
// bounded tail.
func TestBoundThinkingInput_WallWindowed(t *testing.T) {
	t.Parallel()
	budget := thinkingWindowBytes(thinkingCollapsed, 80)
	in := strings.Repeat("word ", budget) // one giant line, no '\n'
	out, _ := boundThinkingInput(in, thinkingCollapsed, 80)
	require.Less(t, len(out), len(in), "a wall past budget must be shortened")
	require.LessOrEqual(t, len(out), budget+8, "window must respect the budget (+fence slack)")
	require.True(t, strings.HasSuffix(in, out), "window must be a tail of the source")
}

// TestBoundThinkingInput_SnapsToLineBoundary: when a newline exists
// inside the trailing budget the window begins at the start of a
// line, never mid-line.
func TestBoundThinkingInput_SnapsToLineBoundary(t *testing.T) {
	t.Parallel()
	budget := thinkingWindowBytes(thinkingCollapsed, 80)
	// A long single line (forces windowing) followed by clean lines
	// within the budget window.
	head := strings.Repeat("x", budget)
	tail := "\nclean line one\nclean line two\n"
	out, dropped := boundThinkingInput(head+tail, thinkingCollapsed, 80)
	require.Greater(t, dropped, 0)
	require.False(t, strings.HasPrefix(out, "x"),
		"window must snap past the truncated head line")
	require.True(t, strings.HasPrefix(out, "clean line"))
}

// TestBoundThinkingInput_ReopensDanglingFence: when the dropped
// prefix leaves a code fence open, the window is prefixed with a
// fence opener so the tail renders as code (matching reality) instead
// of flipping to prose.
func TestBoundThinkingInput_ReopensDanglingFence(t *testing.T) {
	t.Parallel()
	budget := thinkingWindowBytes(thinkingCollapsed, 80)
	// Open a fence, then emit a long body (no closing fence) that
	// pushes the opener out of the window.
	in := "```go\n" + strings.Repeat("code line here\n", budget/15+500)
	out, dropped := boundThinkingInput(in, thinkingCollapsed, 80)
	require.Greater(t, dropped, 0, "fixture must actually be windowed")
	require.True(t, strings.HasPrefix(out, "```\n"),
		"a dangling open fence must be re-opened at the window head")
}

// TestFenceLineCount checks parity accounting used to detect a
// dangling open fence.
func TestFenceLineCount(t *testing.T) {
	t.Parallel()
	require.Equal(t, 0, fenceLineCount("no fences here\njust prose\n"))
	require.Equal(t, 2, fenceLineCount("```go\ncode\n```\n"))
	require.Equal(t, 1, fenceLineCount("```\nopen but never closed\n"))
	// A final line with no trailing newline still counts.
	require.Equal(t, 1, fenceLineCount("prose\n```"))
}
