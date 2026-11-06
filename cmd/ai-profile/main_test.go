package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/config"
)

func TestParseLaunch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	body := "default = \"personal\"\n[profiles.personal]\n[profiles.client-d]\nagents = [\"claude\", \"codex\"]\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		args           []string
		profile, agent string
		rest           []string
		bad            bool
	}{
		{args: nil, agent: "claude"},
		{args: []string{"client-d"}, profile: "client-d", agent: "claude"},
		{args: []string{"codex"}, agent: "codex"},
		{args: []string{"client-d", "codex", "--", "-p", "ciao"}, profile: "client-d", agent: "codex", rest: []string{"-p", "ciao"}},
		{args: []string{"--", "--resume"}, agent: "claude", rest: []string{"--resume"}},
		{args: []string{"codex", "client-d"}, bad: true},
		{args: []string{"nessuno"}, bad: true},
		{args: []string{"client-d", "codex", "pi"}, bad: true},
	}
	for _, c := range cases {
		req, err := parseLaunch(cfg, c.args)
		if c.bad {
			if err == nil {
				t.Errorf("%v: expected an error", c.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v: %v", c.args, err)
			continue
		}
		if req.Profile != c.profile || req.Agent != c.agent || !slices.Equal(req.Args, c.rest) {
			t.Errorf("%v: got %+v", c.args, req)
		}
	}
}

func TestVersionString(t *testing.T) {
	old := version
	defer func() { version = old }()

	version = "1.2.3"
	if got := versionString(); got != "1.2.3" {
		t.Errorf("a version set at build time wins, got %q", got)
	}
	version = ""
	if got := versionString(); got == "" {
		t.Errorf("without a build version it falls back to the module's, got %q", got)
	}
}

func TestParseNew(t *testing.T) {
	home := t.TempDir()
	a, b := filepath.Join(home, "a"), filepath.Join(home, "b")
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)

	p, err := parseNew([]string{"acme", "--dir", a, b, "--agents", "claude,codex", "--account", "io@acme.it"}, home)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "acme" || !slices.Equal(p.Dirs, []string{a, b}) || !slices.Equal(p.Agents, []string{"claude", "codex"}) || p.Account != "io@acme.it" {
		t.Errorf("got %+v", p)
	}
	p, err = parseNew([]string{"solo"}, home)
	if err != nil || !slices.Equal(p.Agents, []string{"claude"}) {
		t.Errorf("defaults: %+v, %v", p, err)
	}
	for _, bad := range [][]string{
		nil,
		{"acme", "--dir"},
		{"acme", "--agents", "nessuno"},
		{"acme", "--desktop"},
		{"acme", "--boh"},
		{"acme", "--dir", filepath.Join(home, "manca")},
	} {
		if _, err := parseNew(bad, home); err == nil {
			t.Errorf("%v: expected an error", bad)
		}
	}
}

func TestFormatReset(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.Local)
	cases := map[time.Time]string{
		time.Date(2026, 10, 21, 15, 30, 0, 0, time.Local): "15:30",
		time.Date(2026, 10, 27, 10, 0, 0, 0, time.Local):  "Tue 27 10:00",
		{}: "?",
	}
	for in, want := range cases {
		if got := formatReset(in, now); got != want {
			t.Errorf("formatReset(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestStatusline(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude-client-d")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"io@client-d.example"}}`), 0o600)
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte("default = \"client-d\"\n[profiles.client-d]\n"), 0o600)
	cfg, err := config.Load(p, home)
	if err != nil {
		t.Fatal(err)
	}
	getenv := func(k string) string { return map[string]string{"CLAUDE_CONFIG_DIR": dir}[k] }

	var out strings.Builder
	if err := statusline(cfg, home, getenv, strings.NewReader(`{"model":{}}`), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "@client-d") || !strings.Contains(out.String(), "io@client-d.example") {
		t.Errorf("default line: %q", out.String())
	}

	script := filepath.Join(t.TempDir(), "mia.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nprintf 'mia %s %s ' \"$AI_PROFILE\" \"$AI_PROFILE_ACCOUNT\"; cat\n"), 0o755)
	cfg.Statusline.Claude = "sh " + script
	out.Reset()
	statusline(cfg, home, getenv, strings.NewReader(`{"input":1}`), &out)
	if out.String() != `mia client-d io@client-d.example {"input":1}` {
		t.Errorf("the user's script gets stdin and the two variables: %q", out.String())
	}
}

func TestRunFolder(t *testing.T) {
	home := t.TempDir()
	shared := filepath.Join(home, "Developer", "shared")
	if err := os.MkdirAll(shared, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "config.toml")
	priv := filepath.Join(home, "private.toml")
	os.WriteFile(cfgPath, []byte("default = \"personal\"\n\n[private]\nfile = \"~/private.toml\"\n\n[profiles.personal]\n"), 0o644)
	os.WriteFile(priv, []byte("[profiles.client-d]\ndirs = [\"~/Developer/shared\"]\n\n[profiles.client-b]\n"), 0o600)
	load := func() *config.Config {
		t.Helper()
		cfg, err := config.Load(cfgPath, home)
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	t.Chdir(shared)

	if err := runFolder(load(), home, []string{"add", "personal"}); err != nil {
		t.Fatal(err)
	}
	cfg := load()
	if len(cfg.Folders) != 1 || !slices.Equal(cfg.Folders[0].Profiles, []string{"client-d", "personal"}) || cfg.Folders[0].Path != priv {
		t.Fatalf("the folder moves from the dirs of client-d, which stays first, in its file: %+v", cfg.Folders)
	}
	if len(cfg.Profiles["client-d"].Dirs) != 0 {
		t.Errorf("client-d keeps the folder in dirs: %v", cfg.Profiles["client-d"].Dirs)
	}
	if info, _ := os.Stat(priv); info.Mode().Perm() != 0o600 {
		t.Errorf("the private file keeps its mode: %v", info.Mode())
	}

	if err := runFolder(load(), home, []string{"add", "~/Developer/shared", "client-b", "client-d"}); err != nil {
		t.Fatal(err)
	}
	if got := load().Folders[0].Profiles; !slices.Equal(got, []string{"client-d", "personal", "client-b"}) {
		t.Errorf("profiles = %v", got)
	}

	if err := runFolder(load(), home, []string{"rm", "client-d"}); err != nil {
		t.Fatal(err)
	}
	if got := load().Folders[0].Profiles; !slices.Equal(got, []string{"personal", "client-b"}) {
		t.Errorf("profiles after rm = %v", got)
	}
	if err := runFolder(load(), home, []string{"rm"}); err != nil {
		t.Fatal(err)
	}
	if got := load().Folders; len(got) != 0 {
		t.Errorf("rm without profiles removes the folder: %+v", got)
	}

	if err := runFolder(load(), home, []string{"add", "nessuno"}); err == nil {
		t.Errorf("an unknown profile, or a missing folder, is an error")
	}
	if err := runFolder(load(), home, []string{"rm", "personal"}); err == nil {
		t.Errorf("rm of a folder not in [[folders]] is an error")
	}
}
