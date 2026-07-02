package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSafeReadOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Plain safe commands.
		{"plain ls", "ls -la", true},
		{"plain echo", "echo hello world", true},
		{"plain pwd", "pwd", true},
		{"git status", "git status", true},
		{"git log with flags", "git log --oneline", true},
		{"git config --get", "git config --get user.name", true},
		{"echo with param expansion", "echo $HOME", true},
		{"simple kill", "kill 1234", true},

		// Not on the allowlist.
		{"rm", "rm -rf /tmp/x", false},
		{"unknown", "frobnicate", false},
		{"git push not allowed", "git push", false},
		{"prefix is not a match (lsof)", "lsof", false},

		// Chaining / multiple statements.
		{"pipe", "ls | grep foo", false},
		{"and", "ls && echo done", false},
		{"or", "ls || echo fail", false},
		{"semicolon", "ls; echo done", false},
		{"newline", "echo hi\nrm -rf x", false},
		{"background then rm", "echo hi & rm -rf x", false},
		{"rm first with and", "rm -rf / && ls", false},

		// Substitution.
		{"command subst", "ls $(rm -rf x)", false},
		{"backticks", "ls `rm -rf x`", false},
		{"param default with subst", "echo ${x:-$(rm -rf x)}", false},

		// Redirection.
		{"redirect out", "echo x > /tmp/out", false},
		{"redirect append", "echo x >> ~/.bashrc", false},
		{"redirect in", "cat < /etc/passwd", false},

		// Wrapper commands must not smuggle an unapproved command through.
		{"time wrapper", "time rm -rf x", false},
		{"env wrapper", "env FOO=bar rm -rf x", false},
		{"timeout wrapper", "timeout 10 rm -rf x", false},
		{"nohup wrapper", "nohup rm -rf x", false},
		{"nice wrapper", "nice rm -rf x", false},

		// Inline assignment (e.g. LD_PRELOAD) is not safe.
		{"inline assignment", "LD_PRELOAD=/evil.so ls", false},

		// Malformed / empty.
		{"empty", "", false},
		{"unbalanced quote", "echo \"unterminated", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isSafeReadOnly(tt.input)
			assert.Equal(t, tt.expected, got, "isSafeReadOnly(%q)", tt.input)
		})
	}
}
