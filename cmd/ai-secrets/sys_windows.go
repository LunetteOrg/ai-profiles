//go:build windows

package main

import "errors"

var errWindows = errors.New("the ai-secrets agent is not available on Windows yet")

func execCommand(args []string, env []string) error { return errWindows }

func notify(msg string) {}
