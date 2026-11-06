package handoff

import (
	"strings"
	"testing"
)

func TestShellCommand(t *testing.T) {
	got := ShellCommand("/Users/io/Developer/my repo", []string{"ai-profile", "client-d", "claude", "--", "--resume", "abc"})
	want := `cd '/Users/io/Developer/my repo' && 'ai-profile' 'client-d' 'claude' '--' '--resume' 'abc'`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if got := ShellCommand("/x", []string{"echo", "it's"}); !strings.Contains(got, `'it'\''s'`) {
		t.Errorf("single quotes must be escaped: %s", got)
	}
}

func TestChooseTerminal(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	has := func(names ...string) func(string) bool {
		return func(n string) bool {
			for _, x := range names {
				if x == n {
					return true
				}
			}
			return false
		}
	}
	if got := ChooseTerminal(env(map[string]string{"TERM_PROGRAM": "Orca"}), has("orca", "osascript"), "darwin"); got != Orca {
		t.Errorf("inside Orca: %v", got)
	}
	if got := ChooseTerminal(env(nil), has("orca", "osascript"), "darwin"); got != TerminalApp {
		t.Errorf("outside Orca on macOS: %v", got)
	}
	if got := ChooseTerminal(env(nil), has(), "linux"); got != PrintOnly {
		t.Errorf("elsewhere: %v", got)
	}
}
