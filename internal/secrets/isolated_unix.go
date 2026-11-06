//go:build !windows

package secrets

import (
	"os"
	"os/exec"
	"syscall"
)

// ExecIsolated runs a CLI in a new session: 1Password scopes an approval to
// the session that asked, so this one covers only this command.
func ExecIsolated(extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	out, err := cmd.Output()
	return string(out), err
}
