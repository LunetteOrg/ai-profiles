//go:build windows

package secrets

// ExecIsolated is Exec on Windows, which has no process sessions to separate.
func ExecIsolated(extraEnv []string, name string, args ...string) (string, error) {
	return Exec(extraEnv, name, args...)
}
