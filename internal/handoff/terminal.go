package handoff

import (
	"fmt"
	"os/exec"
	"strings"
)

// A session of an agent cannot start another interactive agent in its own
// shell, which has no terminal: the handoff opens a new terminal instead.
type Terminal int

const (
	PrintOnly   Terminal = iota // print the command and copy it to the clipboard
	Orca                        // a new terminal in the same Orca worktree
	TerminalApp                 // a new macOS Terminal window
)

// ChooseTerminal picks where the next agent starts.
func ChooseTerminal(getenv func(string) string, has func(string) bool, goos string) Terminal {
	if (getenv("TERM_PROGRAM") == "Orca" || getenv("ORCA_TAB_ID") != "") && has("orca") {
		return Orca
	}
	if goos == "darwin" && has("osascript") {
		return TerminalApp
	}
	return PrintOnly
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// ShellCommand is a shell line that runs args in dir.
func ShellCommand(dir string, args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = quote(a)
	}
	return "cd " + quote(dir) + " && " + strings.Join(q, " ")
}

// Open starts line in a new terminal of kind t, titled title, and says
// where it went.
func Open(t Terminal, title, line string) (string, error) {
	switch t {
	case Orca:
		out, err := exec.Command("orca", "terminal", "create", "--worktree", "active", "--title", title, "--command", line, "--json").CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("orca terminal create: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return "a new Orca terminal (" + title + ")", nil
	case TerminalApp:
		script := `tell application "Terminal"
	do script ` + appleString(line) + `
	activate
end tell`
		if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
			return "", fmt.Errorf("osascript: %v: %s", err, strings.TrimSpace(string(out)))
		}
		return "a new Terminal window", nil
	}
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(line)
	if cmd.Run() == nil {
		return "the clipboard: paste it in a terminal", nil
	}
	return "nowhere: run it in a terminal", nil
}

func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
