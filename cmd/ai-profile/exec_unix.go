//go:build !windows

package main

import "syscall"

// execAgent replaces ai-profile with the agent, so the agent owns the
// terminal, signals and exit code.
func execAgent(bin string, argv, env []string) error {
	return syscall.Exec(bin, argv, env)
}
