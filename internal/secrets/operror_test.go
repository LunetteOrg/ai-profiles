package secrets

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func failingOp(stderr string) *Resolver {
	return &Resolver{
		Getenv: func(string) string { return "" },
		Run: func(env []string, name string, args ...string) (string, error) {
			return "", &RunError{Err: errors.New("exit status 1"), Stderr: stderr}
		},
		Stores: &Stores{OnePassword: map[string]*OnePassword{
			"personal": {Name: "personal", Account: "my.1password.com"},
			"work":     {Name: "work", Account: "work.1password.eu"},
		}},
	}
}

func opLine(msg string) string {
	return "[ERROR] 2026/11/20 21:00:58 could not read secret 'op://V/I/f': " + msg + "\n"
}

func TestOpReadFailureNamesTheCause(t *testing.T) {
	cases := []struct {
		name, stderr string
		want         []string
	}{
		{"timeout", opLine("error initializing client: authorization timeout"),
			[]string{"op://V/I/f?account=work", "approval not given in time", "approve the 1Password notification"}},
		{"dismissed", opLine("error initializing client: authorization prompt dismissed"),
			[]string{"approval refused"}},
		{"item", opLine(`"I" isn't an item in the "V" vault`),
			[]string{"not found", `"I" isn't an item in the "V" vault`, "vault, item and field"}},
		{"field", opLine(`"f" isn't a field in the "I" item`),
			[]string{"not found", `"f" isn't a field in the "I" item`}},
		{"other", opLine("error initializing client: something new"),
			[]string{"op read failed: error initializing client: something new"}},
	}
	for _, c := range cases {
		_, err := failingOp(c.stderr).Resolve("op://V/I/f?account=work")
		if err == nil {
			t.Fatalf("%s: expected an error", c.name)
		}
		for _, w := range c.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%s: %q lacks %q", c.name, err, w)
			}
		}
		if strings.Contains(err.Error(), "[ERROR]") || strings.Contains(err.Error(), "could not read secret") {
			t.Errorf("%s: op's prefix should go: %q", c.name, err)
		}
	}
}

func TestOpReadMultipleAccounts(t *testing.T) {
	_, err := failingOp(opLine("error initializing client: multiple accounts found. Use the --account flag or set the OP_ACCOUNT environment variable to select an account.")).Resolve("op://V/I/f")
	if err == nil || !strings.Contains(err.Error(), "no 1Password account for this reference") || !strings.Contains(err.Error(), "personal, work") {
		t.Errorf("got %v", err)
	}
}

func TestExecKeepsStderr(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	for name, run := range map[string]Runner{"Exec": Exec, "ExecIsolated": ExecIsolated} {
		_, err := run(nil, "sh", "-c", "echo first >&2; echo boom >&2; exit 1")
		var re *RunError
		if !errors.As(err, &re) || !strings.Contains(re.Stderr, "boom") {
			t.Errorf("%s: stderr not kept: %#v", name, err)
		}
	}
}
