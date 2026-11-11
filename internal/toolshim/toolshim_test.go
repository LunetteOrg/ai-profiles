package toolshim

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LunetteOrg/ai-profiles/internal/config"
)

func write(t *testing.T, p, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func loadConfig(t *testing.T, home, body string) *config.Config {
	t.Helper()
	p := filepath.Join(home, "config.toml")
	write(t, p, body, 0o600)
	cfg, err := config.Load(p, home)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

const twoProfiles = `default = "personal"
[profiles.personal]
[profiles.client-a]
dirs = ["~/Developer/client-a"]
[profiles.client-a.tools.scw]
env.SCW_SECRET_KEY = "op://Client-a/Scaleway/secret key"
reason = "Scaleway CLI"
`

func TestLookupFolderProfile(t *testing.T) {
	home := t.TempDir()
	cfg := loadConfig(t, home, twoProfiles)
	dir := filepath.Join(home, "Developer", "client-a", "infra")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s, err := Lookup(cfg, dir, "scw")
	if err != nil {
		t.Fatal(err)
	}
	if s == nil || s.Profile != "client-a" {
		t.Fatalf("in client-a's folder the section of client-a applies, got %+v", s)
	}
	if s.Refs["SCW_SECRET_KEY"] != "op://Client-a/Scaleway/secret key" {
		t.Errorf("refs %v", s.Refs)
	}
	if s.Reason != "Scaleway CLI (profile client-a)" {
		t.Errorf("reason %q", s.Reason)
	}
}

func TestLookupWithoutSection(t *testing.T) {
	home := t.TempDir()
	cfg := loadConfig(t, home, twoProfiles)
	// Outside client-a's folders the default profile applies, which has no
	// tools: the shim runs the real tool unchanged.
	s, err := Lookup(cfg, home, "scw")
	if err != nil || s != nil {
		t.Errorf("outside client-a: section %+v, error %v; want none", s, err)
	}
	dir := filepath.Join(home, "Developer", "client-a")
	os.MkdirAll(dir, 0o755)
	if s, err := Lookup(cfg, dir, "aws"); err != nil || s != nil {
		t.Errorf("a tool the profile does not declare: section %+v, error %v; want none", s, err)
	}
}

func TestLookupReasonDefaultsToTool(t *testing.T) {
	home := t.TempDir()
	cfg := loadConfig(t, home, "default = \"p\"\n[profiles.p]\nbitwarden = \"work\"\n[profiles.p.tools.scw]\nenv.K = \"bw://Scaleway\"\n")
	s, err := Lookup(cfg, home, "scw")
	if err != nil || s == nil {
		t.Fatalf("section %+v, error %v", s, err)
	}
	if s.Reason != "scw (profile p)" || s.Account != "work" {
		t.Errorf("reason %q account %q", s.Reason, s.Account)
	}
}

// fakeBins makes an ai-secrets executable and returns its path.
func fakeSelf(t *testing.T) string {
	t.Helper()
	self := filepath.Join(t.TempDir(), "bin", "ai-secrets")
	write(t, self, "#!/bin/sh\n", 0o755)
	return self
}

func shimLink(t *testing.T, self, p string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.Symlink(self, p); err != nil {
		t.Fatal(err)
	}
}

func TestFindRealSkipsOwnFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links")
	}
	self := fakeSelf(t)
	root := t.TempDir()
	shims := filepath.Join(root, "tool-shims")
	shimLink(t, self, filepath.Join(shims, "scw"))
	// A real file in the shim folder is skipped too: the whole folder is
	// the shim's.
	write(t, filepath.Join(shims, "other"), "#!/bin/sh\n", 0o755)
	// Another link to ai-secrets later in PATH would loop: skipped.
	shimLink(t, self, filepath.Join(root, "more-shims", "scw"))
	notExec := filepath.Join(root, "noexec", "scw")
	write(t, notExec, "x", 0o644)
	real := filepath.Join(root, "real", "scw")
	write(t, real, "#!/bin/sh\n", 0o755)
	pathList := strings.Join([]string{shims, filepath.Join(root, "more-shims"), filepath.Join(root, "noexec"), filepath.Join(root, "real")}, string(os.PathListSeparator))

	got, err := FindReal("scw", pathList, shims, self)
	if err != nil || got != real {
		t.Errorf("FindReal = %q, %v; want %q", got, err, real)
	}

	// The shim folder skipped even when it holds a real file of that name.
	write(t, filepath.Join(root, "own", "scw"), "#!/bin/sh\n", 0o755)
	pathList = strings.Join([]string{filepath.Join(root, "own"), filepath.Join(root, "real")}, string(os.PathListSeparator))
	if got, err := FindReal("scw", pathList, filepath.Join(root, "own"), self); err != nil || got != real {
		t.Errorf("own folder not skipped: %q, %v", got, err)
	}

	if _, err := FindReal("scw", shims, shims, self); err == nil {
		t.Error("only the shim in PATH: expected an error")
	}
}

func TestShimDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links")
	}
	self := fakeSelf(t)
	root := t.TempDir()
	shims := filepath.Join(root, "tool-shims")
	shimLink(t, self, filepath.Join(shims, "scw"))
	write(t, filepath.Join(root, "real", "scw"), "#!/bin/sh\n", 0o755)
	pathList := strings.Join([]string{filepath.Join(root, "empty"), shims, filepath.Join(root, "real")}, string(os.PathListSeparator))
	// Called by name, the shell found it in PATH.
	if got := ShimDir("scw", pathList); got != shims {
		t.Errorf("ShimDir(scw) = %q, want %q", got, shims)
	}
	// Called by path.
	if got := ShimDir(filepath.Join(shims, "scw"), pathList); got != shims {
		t.Errorf("ShimDir(path) = %q, want %q", got, shims)
	}
}

func TestScwConfigSecrets(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	write(t, p, `access_key: SCWAAAAAAAAAAAAAAAAA
secret_key: 11111111-1111-1111-1111-111111111111
default_organization_id: x
profiles:
  prod:
    access_key: SCWBBBBBBBBBBBBBBBBB
    secret_key: "22222222-2222-2222-2222-222222222222"
  dev:
    access_key: SCWCCCCCCCCCCCCCCCCC
    default_zone: fr-par-1
  "staging":
    secret_key: '33333333-3333-3333-3333-333333333333'
`, 0o600)
	got, err := ScwSecretProfiles(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "default,prod,staging" {
		t.Errorf("profiles with secret_key = %v, want default,prod,staging", got)
	}

	write(t, p, "access_key: SCWAAAAAAAAAAAAAAAAA\nsecret_key: \"\"\nprofiles:\n  dev:\n    default_zone: fr-par-1\n", 0o600)
	if got, err := ScwSecretProfiles(p); err != nil || len(got) != 0 {
		t.Errorf("no secret: %v, %v", got, err)
	}
	if got, err := ScwSecretProfiles(filepath.Join(dir, "missing.yaml")); err != nil || len(got) != 0 {
		t.Errorf("missing file: %v, %v", got, err)
	}
}

func TestScwConfigPath(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }
	if got := ScwConfigPath("/h", getenv); got != filepath.Join("/h", ".config", "scw", "config.yaml") {
		t.Errorf("default %q", got)
	}
	env["XDG_CONFIG_HOME"] = "/x"
	if got := ScwConfigPath("/h", getenv); got != filepath.Join("/x", "scw", "config.yaml") {
		t.Errorf("XDG %q", got)
	}
	env["SCW_CONFIG_PATH"] = "/c/scw.yaml"
	if got := ScwConfigPath("/h", getenv); got != "/c/scw.yaml" {
		t.Errorf("SCW_CONFIG_PATH %q", got)
	}
}

var scwDeny = [][]string{{"config", "info"}, {"config", "dump"}, {"config", "get", "secret-key"}, {"config", "get", "secret_key"}}

func TestDenied(t *testing.T) {
	denied := [][]string{
		{"config", "info"},
		{"config", "dump", "--extra"},
		{"-p", "prod", "config", "info"},
		{"--profile=prod", "config", "info"},
		{"--debug", "config", "info"},
		{"config", "-o", "json", "get", "secret-key"},
		{"config", "get", "secret_key"},
		{"-p", "prod", "--debug", "config", "dump"},
	}
	for _, a := range denied {
		if Denied(a, scwDeny) == nil {
			t.Errorf("%q should be denied", a)
		}
	}
	allowed := [][]string{
		{},
		{"config", "get", "default-region"},
		{"-p", "prod", "config", "get", "default-region"},
		{"config"},
		{"instance", "server", "list"},
		{"info"},
		{"config", "set", "default-region=fr-par"},
		{"x", "config", "info"},
	}
	for _, a := range allowed {
		if p := Denied(a, scwDeny); p != nil {
			t.Errorf("%q denied by %q", a, p)
		}
	}
}

// fakeShim runs Shim with a recorded Exec and a Get that fails the test
// when ask is false.
func fakeShim(t *testing.T, cfg *config.Config, dir string, ask bool) (*Shim, *[]string, *[]string) {
	t.Helper()
	var gotArgs, gotEnv []string
	s := &Shim{
		Name:    "scw",
		Real:    "/real/scw",
		Dir:     dir,
		Cfg:     cfg,
		Environ: []string{"PATH=/x", "HOME=/h"},
		Get: func(reason string, refs map[string]string, account string) (map[string]string, error) {
			if !ask {
				t.Errorf("ai-secrets asked (reason %q) when it must not be", reason)
			}
			out := map[string]string{}
			for k := range refs {
				out[k] = "fake-value"
			}
			return out, nil
		},
		Exec: func(bin string, args, env []string) error {
			gotArgs, gotEnv = append([]string{bin}, args...), env
			return nil
		},
	}
	return s, &gotArgs, &gotEnv
}

func hasVar(env []string, k string) bool {
	for _, kv := range env {
		if strings.HasPrefix(kv, k+"=") {
			return true
		}
	}
	return false
}

const scwProfile = `default = "client-a"
[profiles.client-a]
[profiles.client-a.tools.scw]
env.SCW_SECRET_KEY = "op://Client-a/Scaleway/secret key"
reason = "Scaleway CLI"
deny_args = [["config", "info"], ["config", "dump"], ["config", "get", "secret-key"], ["config", "get", "secret_key"]]
`

func TestShimResolvesAndExecs(t *testing.T) {
	home := t.TempDir()
	s, args, env := fakeShim(t, loadConfig(t, home, scwProfile), home, true)
	if err := s.Run([]string{"instance", "server", "list"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*args, " ") != "/real/scw instance server list" {
		t.Errorf("exec args %q", *args)
	}
	if !hasVar(*env, "SCW_SECRET_KEY") || !hasVar(*env, "PATH") {
		t.Error("the tool's environment lacks SCW_SECRET_KEY or the inherited variables")
	}
}

func TestShimWithoutSectionRunsUnchanged(t *testing.T) {
	home := t.TempDir()
	for name, cfg := range map[string]*config.Config{
		"nessuna configurazione": nil,
		"profilo senza tools":    loadConfig(t, home, "default = \"p\"\n[profiles.p]\n"),
	} {
		s, args, env := fakeShim(t, cfg, home, false)
		// Without a section deny_args does not apply either.
		if err := s.Run([]string{"config", "info"}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if strings.Join(*args, " ") != "/real/scw config info" || strings.Join(*env, " ") != "PATH=/x HOME=/h" {
			t.Errorf("%s: exec %q with env %q, want the real tool unchanged", name, *args, *env)
		}
	}
}

func TestShimDenies(t *testing.T) {
	home := t.TempDir()
	s, args, _ := fakeShim(t, loadConfig(t, home, scwProfile), home, false)
	err := s.Run([]string{"-p", "prod", "config", "info"})
	if err == nil || !strings.Contains(err.Error(), "config info") {
		t.Fatalf("expected a refusal naming config info, got %v", err)
	}
	if *args != nil {
		t.Errorf("the tool was launched: %q", *args)
	}

	s, args, env := fakeShim(t, loadConfig(t, home, scwProfile), home, true)
	if err := s.Run([]string{"config", "get", "default-region"}); err != nil {
		t.Fatal(err)
	}
	if *args == nil || !hasVar(*env, "SCW_SECRET_KEY") {
		t.Error("config get default-region should run with the secret")
	}
}
