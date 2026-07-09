// Package difftastic provides a syntax-aware structural diff renderer by
// shelling out to the difftastic binary (https://github.com/Wilfred/difftastic).
package difftastic

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// RenderDiff runs difftastic on before/after content and returns ANSI-colored
// output. Returns empty string and a nil error if difft is not available so
// callers can fall back to the builtin renderer.
func RenderDiff(filePath, before, after string, width int) (string, error) {
	if _, err := exec.LookPath("difft"); err != nil {
		return "", nil
	}

	dir, err := os.MkdirTemp("", "crush-difftastic-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(dir)

	ext := filepath.Ext(filePath)
	oldPath := filepath.Join(dir, "old"+ext)
	newPath := filepath.Join(dir, "new"+ext)

	if err := os.WriteFile(oldPath, []byte(before), 0o644); err != nil {
		return "", fmt.Errorf("failed to write before content: %w", err)
	}
	if err := os.WriteFile(newPath, []byte(after), 0o644); err != nil {
		return "", fmt.Errorf("failed to write after content: %w", err)
	}

	args := []string{
		"--color=always",
		"--display=inline",
		"--width=" + strconv.Itoa(width),
		oldPath,
		newPath,
	}

	cmd := exec.CommandContext(context.Background(), "difft", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// difftastic returns non-zero exit code when differences are found.
		// The output is still valid, so return it rather than the error.
		if len(output) > 0 {
			return string(output), nil
		}
		return "", fmt.Errorf("failed to run difftastic: %w", err)
	}

	return strings.TrimRight(string(output), "\n"), nil
}
