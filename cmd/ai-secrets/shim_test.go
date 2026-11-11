//go:build !windows

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shimEnv builds ai-secrets, links it as fakecli in a tool-shims folder
// before a real fakecli, and returns the shim's path and its environment.
func shimEnv(t *testing.T, config string) (shim string, env []string, marker string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin", "ai-secrets")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	shims := filepath.Join(root, "tool-shims")
	os.MkdirAll(shims, 0o755)
	shim = filepath.Join(shims, "fakecli")
	if err := os.Symlink(bin, shim); err != nil {
		t.Fatal(err)
	}
	marker = filepath.Join(root, "ran")
	realDir := filepath.Join(root, "real")
	os.MkdirAll(realDir, 0o755)
	script := "#!/bin/sh\ntouch " + marker + "\necho \"args:$*\"\nread line\necho \"stdin:$line\"\nexit 3\n"
	if err := os.WriteFile(filepath.Join(realDir, "fakecli"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	os.MkdirAll(home, 0o755)
	cfgPath := filepath.Join(root, "missing.toml")
	if config != "" {
		cfgPath = filepath.Join(root, "config.toml")
		os.WriteFile(cfgPath, []byte(config), 0o600)
	}
	env = []string{
		"HOME=" + home,
		"PATH=" + shims + string(os.PathListSeparator) + realDir + string(os.PathListSeparator) + "/usr/bin:/bin",
		"AI_PROFILES_CONFIG=" + cfgPath,
	}
	return shim, env, marker
}

func TestShimRunsRealToolUnchanged(t *testing.T) {
	_, env, marker := shimEnv(t, "")
	// Called by name, as a shell finds it in PATH.
	cmd := exec.Command("/bin/sh", "-c", "fakecli instance list")
	cmd.Env, cmd.Dir = env, t.TempDir()
	cmd.Stdin = strings.NewReader("hello\n")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 3 {
		t.Fatalf("exit code: %v, want 3 from the real tool", err)
	}
	if out.String() != "args:instance list\nstdin:hello\n" {
		t.Errorf("output %q", out.String())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Error("the real tool did not run")
	}
}

func TestShimRefusesDeniedArgs(t *testing.T) {
	dir := t.TempDir()
	shim, env, marker := shimEnv(t, `default = "p"
[profiles.p]
[profiles.client-a]
dirs = [`+"\""+dir+"\""+`]
[profiles.client-a.tools.fakecli]
env.FAKE_SECRET = "op://Vault/Item/field"
deny_args = [["config", "info"]]
`)
	cmd := exec.Command(shim, "-p", "prod", "config", "info")
	cmd.Env, cmd.Dir = env, dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() == 0 {
		t.Fatalf("a denied subcommand should exit non-zero, got %v", err)
	}
	if !strings.Contains(stderr.String(), "fakecli config info is denied") {
		t.Errorf("stderr %q should say why", stderr.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the real tool ran")
	}
}
