package agent

import (
	"hash/fnv"
	"io"

	"charm.land/fantasy"
)

const (
	loopDetectionWindowSize = 10
	loopDetectionMaxRepeats = 5
)

// hasRepeatedToolCalls checks whether the agent is stuck in a loop by looking
// at recent steps. It examines the last windowSize steps and returns true if
// any tool-call signature appears more than maxRepeats times.
func hasRepeatedToolCalls(steps []fantasy.StepResult, windowSize, maxRepeats int) bool {
	if len(steps) < windowSize {
		return false
	}

	window := steps[len(steps)-windowSize:]
	counts := make(map[uint64]int, windowSize)

	for _, step := range window {
		sig := getToolInteractionSignature(step.Content)
		if sig == 0 {
			continue
		}
		counts[sig]++
		if counts[sig] > maxRepeats {
			return true
		}
	}

	return false
}

// getToolInteractionSignature computes a hash signature for the tool
// interactions in a single step's content. It pairs tool calls with their
// results (matched by ToolCallID) and returns an FNV-1a hash. FNV is ~10x
// faster than SHA-256 and collision risk is negligible for loop detection
// over a 10-step window. If the step contains no tool calls, it returns 0.
func getToolInteractionSignature(content fantasy.ResponseContent) uint64 {
	toolCalls := content.ToolCalls()
	if len(toolCalls) == 0 {
		return 0
	}

	// Index tool results by their ToolCallID for fast lookup.
	resultsByID := make(map[string]fantasy.ToolResultContent, len(toolCalls))
	for _, tr := range content.ToolResults() {
		resultsByID[tr.ToolCallID] = tr
	}

	h := fnv.New64a()
	for _, tc := range toolCalls {
		output := ""
		if tr, ok := resultsByID[tc.ToolCallID]; ok {
			output = toolResultOutputString(tr.Result)
		}
		_, _ = io.WriteString(h, tc.ToolName)
		h.Write([]byte{0})
		_, _ = io.WriteString(h, tc.Input)
		h.Write([]byte{0})
		_, _ = io.WriteString(h, output)
		h.Write([]byte{0})
	}
	return h.Sum64()
}

// toolResultOutputString converts a ToolResultOutputContent to a stable string
// representation for signature comparison.
func toolResultOutputString(result fantasy.ToolResultOutputContent) string {
	if result == nil {
		return ""
	}
	if text, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentText](result); ok {
		return text.Text
	}
	if errResult, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](result); ok {
		if errResult.Error != nil {
			return errResult.Error.Error()
		}
		return ""
	}
	if media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result); ok {
		return media.Data
	}
	return ""
}
