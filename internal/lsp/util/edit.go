package util

import (
	"bytes"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	powernap "github.com/charmbracelet/x/powernap/pkg/lsp"
	"github.com/charmbracelet/x/powernap/pkg/lsp/protocol"
)

func applyTextEdits(uri protocol.DocumentURI, edits []protocol.TextEdit, encoding powernap.OffsetEncoding) error {
	path, err := uri.Path()
	if err != nil {
		return fmt.Errorf("invalid URI: %w", err)
	}

	// Read the file content
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Detect line ending style
	var lineEnding string
	if bytes.Contains(content, []byte("\r\n")) {
		lineEnding = "\r\n"
	} else {
		lineEnding = "\n"
	}

	// Track if file ends with a newline
	endsWithNewline := len(content) > 0 && bytes.HasSuffix(content, []byte(lineEnding))

	// Split into lines without the endings
	lines := strings.Split(string(content), lineEnding)

	// Sort edits once by ascending start position (end position as a stable
	// tiebreaker). This lets us both detect overlaps in a single linear pass
	// and apply edits from the bottom of the file upward.
	sortedEdits := make([]protocol.TextEdit, len(edits))
	copy(sortedEdits, edits)
	sort.Slice(sortedEdits, func(i, j int) bool {
		a, b := sortedEdits[i].Range, sortedEdits[j].Range
		if a.Start.Line != b.Start.Line {
			return a.Start.Line < b.Start.Line
		}
		if a.Start.Character != b.Start.Character {
			return a.Start.Character < b.Start.Character
		}
		if a.End.Line != b.End.Line {
			return a.End.Line < b.End.Line
		}
		return a.End.Character < b.End.Character
	})

	// Detect overlapping edits in a single linear pass. Because the edits are
	// sorted by ascending start, any overlap manifests between two adjacent
	// edits, so checking neighbors is sufficient.
	for i := 1; i < len(sortedEdits); i++ {
		if rangesOverlap(sortedEdits[i-1].Range, sortedEdits[i].Range) {
			return fmt.Errorf("overlapping edits detected between edit %d and %d", i-1, i)
		}
	}

	// Apply edits from the bottom of the file upward so that earlier edits'
	// line/character offsets are not shifted by later ones. applyTextEdit
	// splices the affected range in place instead of rebuilding the whole
	// slice per edit.
	for i := len(sortedEdits) - 1; i >= 0; i-- {
		newLines, err := applyTextEdit(lines, sortedEdits[i], encoding)
		if err != nil {
			return fmt.Errorf("failed to apply edit: %w", err)
		}
		lines = newLines
	}

	// Join lines with proper line endings
	var newContent strings.Builder
	for i, line := range lines {
		if i > 0 {
			newContent.WriteString(lineEnding)
		}
		newContent.WriteString(line)
	}

	// Only add a newline if the original file had one and we haven't already added it
	if endsWithNewline && !strings.HasSuffix(newContent.String(), lineEnding) {
		newContent.WriteString(lineEnding)
	}

	if err := os.WriteFile(path, []byte(newContent.String()), 0o644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func applyTextEdit(lines []string, edit protocol.TextEdit, encoding powernap.OffsetEncoding) ([]string, error) {
	startLine := int(edit.Range.Start.Line)
	endLine := int(edit.Range.End.Line)

	// Validate positions before accessing lines.
	if startLine < 0 || startLine >= len(lines) {
		return nil, fmt.Errorf("invalid start line: %d", startLine)
	}
	if endLine < 0 || endLine >= len(lines) {
		endLine = len(lines) - 1
	}

	var startChar, endChar int
	switch encoding {
	case powernap.UTF8:
		// UTF-8: Character offset is already a byte offset
		startChar = int(edit.Range.Start.Character)
		endChar = int(edit.Range.End.Character)
	case powernap.UTF16:
		// UTF-16 (default): Convert to byte offset
		startLineContent := lines[startLine]
		endLineContent := lines[endLine]
		startChar = powernap.PositionToByteOffset(startLineContent, edit.Range.Start.Character)
		endChar = powernap.PositionToByteOffset(endLineContent, edit.Range.End.Character)
	default:
		// UTF-32: Character offset is codepoint count, convert to byte offset
		startLineContent := lines[startLine]
		endLineContent := lines[endLine]
		startChar = utf32ToByteOffset(startLineContent, edit.Range.Start.Character)
		endChar = utf32ToByteOffset(endLineContent, edit.Range.End.Character)
	}

	// Get the prefix of the start line
	startLineContent := lines[startLine]
	if startChar < 0 || startChar > len(startLineContent) {
		startChar = len(startLineContent)
	}
	prefix := startLineContent[:startChar]

	// Get the suffix of the end line
	endLineContent := lines[endLine]
	if endChar < 0 || endChar > len(endLineContent) {
		endChar = len(endLineContent)
	}
	suffix := endLineContent[endChar:]

	// Build the replacement lines for the edited range [startLine, endLine].
	var region []string
	if edit.NewText == "" {
		if prefix+suffix != "" {
			region = []string{prefix + suffix}
		}
	} else {
		// Split new text into lines, being careful not to add extra newlines
		// newLines := strings.Split(strings.TrimRight(edit.NewText, "\n"), "\n")
		newLines := strings.Split(edit.NewText, "\n")

		if len(newLines) == 1 {
			// Single line change
			region = []string{prefix + newLines[0] + suffix}
		} else {
			// Multi-line change
			region = make([]string, 0, len(newLines))
			region = append(region, prefix+newLines[0])
			region = append(region, newLines[1:len(newLines)-1]...)
			region = append(region, newLines[len(newLines)-1]+suffix)
		}
	}

	// Splice the replacement in place instead of rebuilding the whole slice.
	// Callers apply edits from the bottom of the file upward, so mutating the
	// shared slice does not disturb not-yet-applied (earlier) edits.
	return slices.Replace(lines, startLine, endLine+1, region...), nil
}

// applyDocumentChange applies a DocumentChange (create/rename/delete operations)
func applyDocumentChange(change protocol.DocumentChange, encoding powernap.OffsetEncoding) error {
	if change.CreateFile != nil {
		path, err := change.CreateFile.URI.Path()
		if err != nil {
			return fmt.Errorf("invalid URI: %w", err)
		}

		if change.CreateFile.Options != nil {
			if change.CreateFile.Options.Overwrite {
				// Proceed with overwrite
			} else if change.CreateFile.Options.IgnoreIfExists {
				if _, err := os.Stat(path); err == nil {
					return nil // File exists and we're ignoring it
				}
			}
		}
		if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
			return fmt.Errorf("failed to create file: %w", err)
		}
	}

	if change.DeleteFile != nil {
		path, err := change.DeleteFile.URI.Path()
		if err != nil {
			return fmt.Errorf("invalid URI: %w", err)
		}

		if change.DeleteFile.Options != nil && change.DeleteFile.Options.Recursive {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("failed to delete directory recursively: %w", err)
			}
		} else {
			if err := os.Remove(path); err != nil {
				return fmt.Errorf("failed to delete file: %w", err)
			}
		}
	}

	if change.RenameFile != nil {
		var newPath, oldPath string
		var err error

		oldPath, err = change.RenameFile.OldURI.Path()
		if err != nil {
			return err
		}

		newPath, err = change.RenameFile.NewURI.Path()
		if err != nil {
			return err
		}

		if change.RenameFile.Options != nil {
			if !change.RenameFile.Options.Overwrite {
				if _, err := os.Stat(newPath); err == nil {
					return fmt.Errorf("target file already exists and overwrite is not allowed: %s", newPath)
				}
			}
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return fmt.Errorf("failed to rename file: %w", err)
		}
	}

	if change.TextDocumentEdit != nil {
		textEdits := make([]protocol.TextEdit, len(change.TextDocumentEdit.Edits))
		for i, edit := range change.TextDocumentEdit.Edits {
			var err error
			textEdits[i], err = edit.AsTextEdit()
			if err != nil {
				return fmt.Errorf("invalid edit type: %w", err)
			}
		}
		return applyTextEdits(change.TextDocumentEdit.TextDocument.URI, textEdits, encoding)
	}

	return nil
}

// utf32ToByteOffset converts a UTF-32 codepoint offset to a byte offset.
func utf32ToByteOffset(lineText string, codepointOffset uint32) int {
	if codepointOffset == 0 {
		return 0
	}

	var codepointCount uint32
	for byteOffset := range lineText {
		if codepointCount >= codepointOffset {
			return byteOffset
		}
		codepointCount++
	}
	return len(lineText)
}

// ApplyWorkspaceEdit applies the given WorkspaceEdit to the filesystem.
// The encoding parameter specifies the position encoding used by the LSP server
// (UTF8, UTF16, or UTF32). This affects how character offsets are interpreted.
func ApplyWorkspaceEdit(edit protocol.WorkspaceEdit, encoding powernap.OffsetEncoding) error {
	// Handle Changes field
	for uri, textEdits := range edit.Changes {
		if err := applyTextEdits(uri, textEdits, encoding); err != nil {
			return fmt.Errorf("failed to apply text edits: %w", err)
		}
	}

	// Handle DocumentChanges field
	for _, change := range edit.DocumentChanges {
		if err := applyDocumentChange(change, encoding); err != nil {
			return fmt.Errorf("failed to apply document change: %w", err)
		}
	}

	return nil
}

// rangesOverlap checks if two LSP ranges overlap.
// Per the LSP specification, ranges are half-open intervals [start, end),
// so adjacent ranges where one's end equals another's start do NOT overlap.
// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#range
func rangesOverlap(r1, r2 protocol.Range) bool {
	if r1.Start.Line > r2.End.Line || r2.Start.Line > r1.End.Line {
		return false
	}
	if r1.Start.Line == r2.End.Line && r1.Start.Character >= r2.End.Character {
		return false
	}
	if r2.Start.Line == r1.End.Line && r2.Start.Character >= r1.End.Character {
		return false
	}
	return true
}
