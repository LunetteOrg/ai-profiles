//go:build windows

package main

import "errors"

var errWindows = errors.New("the ai-secrets agent is not available on Windows yet")

func execCommand(args []string, env []string) error { return errWindows }

// shimSupported: without exec and without the agent, a tool shim cannot
// hand the secret to the tool alone.
const shimSupported = false

func execPath(bin string, argv, env []string) error { return errWindows }

func notify(msg string) {}
