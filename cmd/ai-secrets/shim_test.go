//go:build !windows

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// Called through its link, the shim starts the agent as ai-secrets, not as
// the tool: the request reaches the agent, which fails here only because op
// is not in PATH.
func TestShimStartsAgent(t *testing.T) {
	dir := t.TempDir()
	shim, env, marker := shimEnv(t, `default = "p"
[profiles.p]
dirs = [`+"\""+dir+"\""+`]
[profiles.p.tools.fakecli]
env.FAKE_SECRET = "op://Vault/Item/field"
`)
	// A short home: the agent's socket path must fit in sun_path.
	home, err := os.MkdirTemp("", "as")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	env[0] = "HOME=" + home
	bin := filepath.Join(filepath.Dir(filepath.Dir(shim)), "bin", "ai-secrets")
	t.Cleanup(func() {
		stop := exec.Command(bin, "stop")
		stop.Env = env
		stop.Run()
	})

	cmd := exec.Command("/bin/sh", "-c", "fakecli list")
	cmd.Env, cmd.Dir = env, dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("without op the secret cannot be read: expected an error")
	}
	if strings.Contains(stderr.String(), "does not answer") || !strings.Contains(stderr.String(), "op") {
		t.Errorf("the agent should start and fail on op, got: %s", stderr.String())
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the real tool ran without its secret")
	}
}

// ai-secrets run takes the 1Password account of the folder's profile for
// references that name none: the reference that reaches the agent carries
// it (the read then fails here only because op is not in PATH).
func TestRunTakesFolderAccount(t *testing.T) {
	dir := t.TempDir()
	_, env, _ := shimEnv(t, `default = "p"
[profiles.p]
[profiles.client]
dirs = [`+"\""+dir+"\""+`]
onepassword = "work"
`)
	home, err := os.MkdirTemp("", "as")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(home) })
	os.MkdirAll(filepath.Join(home, ".config", "ai-secrets"), 0o700)
	os.WriteFile(filepath.Join(home, ".config", "ai-secrets", "config.toml"), []byte("[stores.1password.work]\naccount = \"work.1password.eu\"\n[stores.1password.personal]\naccount = \"my.1password.com\"\n"), 0o600)
	env[0] = "HOME=" + home
	bin := filepath.Join(filepath.Dir(strings.TrimPrefix(env[2], "AI_PROFILES_CONFIG=")), "bin", "ai-secrets")
	t.Cleanup(func() {
		stop := exec.Command(bin, "stop")
		stop.Env = env
		stop.Run()
	})
	run := func(extra string, args ...string) string {
		cmd := exec.Command(bin, append([]string{"run"}, append(args, "--", "/usr/bin/true")...)...)
		cmd.Env, cmd.Dir = append(slices.Clone(env), "FAKE="+extra), dir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		cmd.Run()
		return stderr.String()
	}
	if out := run("op://V/I/f"); !strings.Contains(out, "op://V/I/f?account=work") {
		t.Errorf("the folder's account should reach the agent: %s", out)
	}
	if out := run("op://V/I/f", "--account", "personal"); !strings.Contains(out, "op://V/I/f?account=personal") {
		t.Errorf("--account comes before the folder's: %s", out)
	}
	if out := run("op://V/I/f?account=personal"); !strings.Contains(out, "op://V/I/f?account=personal") || strings.Contains(out, "account=work") {
		t.Errorf("the reference's account comes first: %s", out)
	}
}
