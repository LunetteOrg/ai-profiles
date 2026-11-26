//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The real command, with fake op and bw: the code from 1Password reaches bw
// on its standard input, never on its command line, and the session key bw
// prints is not shown.
func TestBitwardenLogin(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "ai-secrets")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	fake := filepath.Join(root, "fake")
	os.MkdirAll(fake, 0o755)
	log := filepath.Join(root, "bw.log")
	os.WriteFile(filepath.Join(fake, "op"), []byte(`#!/bin/sh
case "$*" in
*attribute=otp*) printf 123456 ;;
*) printf principale ;;
esac
`), 0o755)
	os.WriteFile(filepath.Join(fake, "bw"), []byte(`#!/bin/sh
echo "args:$* dir:$BITWARDENCLI_APPDATA_DIR" >> `+log+`
if [ "$1" = login ]; then
	echo "password:$BW_PASSWORD" >> `+log+`
	printf '? Two-step login code: ' >&2
	read code
	printf '\r? Two-step login code: %s\n' "$code" >&2
	echo "code:$code" >> `+log+`
	echo sessione
fi
`), 0o755)
	home := filepath.Join(root, "home")
	os.MkdirAll(filepath.Join(home, ".config", "ai-secrets"), 0o700)
	os.WriteFile(filepath.Join(home, ".config", "ai-secrets", "config.toml"), []byte(`[stores.bitwarden.client-e]
server = "https://vault.bitwarden.eu"
password = "op://Private/bw/password"
email = "me@client-e.example"
otp = "op://Private/bw/one-time password?attribute=otp"
`), 0o600)

	cmd := exec.Command(bin, "bitwarden", "login", "client-e")
	cmd.Env = []string{"HOME=" + home, "PATH=" + fake + ":/usr/bin:/bin"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if strings.Contains(string(out), "sessione") || strings.Contains(string(out), "123456") || !strings.Contains(string(out), "logged in, locked") {
		t.Errorf("output: %s", out)
	}
	b, _ := os.ReadFile(log)
	got := string(b)
	dir := filepath.Join(home, ".config", "ai-secrets", "bitwarden", "client-e")
	for _, want := range []string{
		"args:logout dir:" + dir,
		"args:config server https://vault.bitwarden.eu",
		"args:login me@client-e.example --passwordenv BW_PASSWORD --method 0 --raw dir:" + dir,
		"password:principale",
		"code:123456",
		"args:lock",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("bw log lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "args:login") && strings.Contains(strings.SplitN(got[strings.Index(got, "args:login"):], "\n", 2)[0], "123456") {
		t.Errorf("the code is on bw's command line:\n%s", got)
	}

	cmd = exec.Command(bin, "bitwarden")
	cmd.Env = []string{"HOME=" + home, "PATH=" + fake + ":/usr/bin:/bin"}
	if out, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(out), "usage: ai-secrets bitwarden login") {
		t.Errorf("usage: %v %s", err, out)
	}
}
