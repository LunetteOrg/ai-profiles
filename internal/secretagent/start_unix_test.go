//go:build !windows

package secretagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The agent outlives whoever starts it, often a worktree that is removed
// later: it works in its own folder, not in the caller's.
func TestStartRunsTheAgentInItsOwnFolder(t *testing.T) {
	home := t.TempDir()
	p := PathsFor(home)
	out := filepath.Join(t.TempDir(), "cwd")
	bin := filepath.Join(t.TempDir(), "fake-agent")
	script := "#!/bin/sh\npwd -P > " + out + ".tmp && mv " + out + ".tmp " + out + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	caller := filepath.Join(t.TempDir(), "worktree")
	if err := os.Mkdir(caller, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(caller)
	if err := os.Remove(caller); err != nil {
		t.Fatal(err)
	}

	if err := Start(bin, p); err != nil {
		t.Fatal(err)
	}

	var got []byte
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(out); err == nil {
			got = b
			break
		}
	}
	want, err := filepath.EvalSymlinks(filepath.Dir(p.Socket))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("agent folder = %q, want %q", strings.TrimSpace(string(got)), want)
	}
}
