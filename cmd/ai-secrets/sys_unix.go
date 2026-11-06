//go:build !windows

package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
)

// execCommand replaces ai-secrets with the command, so the command owns
// stdin and stdout (the MCP protocol).
func execCommand(args []string, env []string) error {
	bin, err := exec.LookPath(args[0])
	if err != nil {
		return err
	}
	return syscall.Exec(bin, args, env)
}

// notify shows a macOS notification before a password manager asks.
func notify(msg string) {
	if runtime.GOOS != "darwin" {
		os.Stderr.WriteString("ai-secrets: " + msg + "\n")
		return
	}
	quote := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	exec.Command("osascript", "-e", "display notification "+quote(msg)+" with title \"ai-secrets\" subtitle \"Approve the 1Password request\"").Run()
}
