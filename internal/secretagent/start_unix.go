//go:build !windows

package secretagent

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// Start launches "bin agent" detached from the caller, which may be an MCP
// server about to exec another program.
func Start(bin string, p Paths) error {
	if err := os.MkdirAll(filepath.Dir(p.Socket), 0o700); err != nil {
		return err
	}
	log, err := os.OpenFile(p.Log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()
	cmd := exec.Command(bin, "agent")
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// Lock takes the agent lock; ok is false when another agent holds it.
func Lock(path string) (release func(), ok bool, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, false, nil
	}
	return func() { f.Close() }, true, nil
}
