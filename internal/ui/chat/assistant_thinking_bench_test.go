package chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/crush/internal/message"
	"github.com/charmbracelet/crush/internal/ui/styles"
)

// benchThinkingStream simulates a reasoning stream: it grows `body`
// to full length in `steps` increments and drives the real
// AssistantMessageItem render path (SetMessage + RawRender) on every
// increment, exactly as the live TUI does on each ~33ms flush while
// the block sits collapsed. It guards against a regression of the
// O(n²) full-trace re-render that made long thinking lag the UI:
// with boundThinkingInput the per-flush cost is bounded, so the wall
// (no-blank-line) case stays close to the blank-line case instead of
// blowing up quadratically.
func benchThinkingStream(b *testing.B, body string, steps int) {
	sty := styles.CharmtonePantera()
	const width = 80
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		item := NewAssistantMessageItem(&sty, thinkingMsg("")).(*AssistantMessageItem)
		for i := 1; i <= steps; i++ {
			cut := len(body) * i / steps
			item.SetMessage(thinkingMsg(body[:cut]))
			_ = item.RawRender(width)
		}
	}
}

func thinkingMsg(thinking string) *message.Message {
	return &message.Message{
		ID:   "bench",
		Role: message.Assistant,
		Parts: []message.ContentPart{
			message.ReasoningContent{Thinking: thinking, StartedAt: testStartedAt},
		},
	}
}

func benchParagraphs(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "Reasoning paragraph %d with a few sentences of prose that a model might emit while working through a problem step by step.\n\n", i)
	}
	return sb.String()
}

// benchWall is one giant paragraph with NO blank line — the case
// where no safe markdown boundary ever exists and, before
// boundThinkingInput, every flush re-rendered the whole growing
// trace.
func benchWall(n int) string {
	var sb strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "sentence %d that keeps going with no paragraph break at all. ", i)
	}
	return sb.String()
}

func BenchmarkThinkingStream_BlankLines(b *testing.B) {
	benchThinkingStream(b, benchParagraphs(200), 100)
}
func BenchmarkThinkingStream_Wall(b *testing.B) { benchThinkingStream(b, benchWall(1500), 100) }
