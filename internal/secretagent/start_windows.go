//go:build windows

package secretagent

import "errors"

var errWindows = errors.New("the ai-secrets agent is not available on Windows yet")

func Start(bin string, p Paths) error { return errWindows }

func Lock(path string) (func(), bool, error) { return nil, false, errWindows }
