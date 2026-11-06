//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

// execAgent runs the agent as a child (Windows has no exec) and returns its
// exit code.
func execAgent(bin string, argv, env []string) error {
	cmd := exec.Command(bin, argv[1:]...)
	cmd.Env, cmd.Stdin, cmd.Stdout, cmd.Stderr = env, os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exitError(exit.ExitCode())
	}
	return err
}
