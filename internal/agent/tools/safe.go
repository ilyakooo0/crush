package tools

import (
	"runtime"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// safeCommands are read-only commands that can run without a permission
// prompt. Entries may be multi-word (e.g. "git log") to scope a
// subcommand. Do NOT add wrapper commands that execute an arbitrary
// argument (env, time, timeout, nohup, nice, xargs, sh, bash, find -exec,
// ...): those would let an unapproved command ride in on a safe prefix.
var safeCommands = []string{
	// Bash builtins and core utils
	"cal",
	"date",
	"df",
	"du",
	"echo",
	"free",
	"groups",
	"hostname",
	"id",
	"kill",
	"killall",
	"ls",
	"printenv",
	"ps",
	"pwd",
	"set",
	"top",
	"type",
	"uname",
	"unset",
	"uptime",
	"whatis",
	"whereis",
	"which",
	"whoami",

	// Git
	"git blame",
	"git branch",
	"git config --get",
	"git config --list",
	"git describe",
	"git diff",
	"git grep",
	"git log",
	"git ls-files",
	"git ls-remote",
	"git remote",
	"git rev-parse",
	"git shortlog",
	"git show",
	"git status",
	"git tag",
}

func init() {
	if runtime.GOOS == "windows" {
		safeCommands = append(
			safeCommands,
			// Windows-specific commands
			"ipconfig",
			"nslookup",
			"ping",
			"systeminfo",
			"tasklist",
			"where",
		)
	}
}

// isSafeReadOnly reports whether command is a single, simple, read-only
// command that can run without a permission prompt.
//
// It parses the command and only returns true when the whole input is
// exactly one simple command (no pipes, chaining, subshells, blocks, or
// negation), with no redirections, no backgrounding, no inline variable
// assignments (which could inject e.g. LD_PRELOAD), and no command or
// process substitution anywhere. The command name (and any subcommand
// tokens) must be plain literals matching an entry in safeCommands.
//
// This replaces raw-string prefix matching, which let wrapper commands
// and redirects/chaining smuggle unapproved commands past the gate
// (e.g. "time rm -rf x", "echo x > file", "echo hi & rm -rf x").
func isSafeReadOnly(command string) bool {
	parser := syntax.NewParser()
	file, err := parser.Parse(strings.NewReader(command), "")
	if err != nil {
		return false
	}

	// Exactly one statement, and it must be a plain foreground command.
	if len(file.Stmts) != 1 {
		return false
	}
	stmt := file.Stmts[0]
	if stmt.Background || stmt.Negated || len(stmt.Redirs) > 0 {
		return false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Args) == 0 || len(call.Assigns) > 0 {
		return false
	}

	// Reject any command/process substitution anywhere in the statement:
	// those execute arbitrary commands the allowlist never sees.
	safe := true
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node.(type) {
		case *syntax.CmdSubst, *syntax.ProcSubst:
			safe = false
			return false
		}
		return true
	})
	if !safe {
		return false
	}

	// Extract literal argv. Non-literal words (quoting, parameter
	// expansion, globbing) yield "" via Word.Lit(); the command name and
	// any matched subcommand tokens must be plain literals.
	argv := make([]string, len(call.Args))
	for i, w := range call.Args {
		argv[i] = w.Lit()
	}
	if argv[0] == "" {
		return false
	}

	for _, safeCmd := range safeCommands {
		tokens := strings.Fields(safeCmd)
		if len(argv) < len(tokens) {
			continue
		}
		match := true
		for i, tok := range tokens {
			if argv[i] != tok {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
