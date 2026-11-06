package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LunetteOrg/ai-profiles/internal/private"
)

const publicSample = `
[private]
file = "~/.config/ai-profiles/private.toml"
restore = "cat archive"
backup = "cat > archive"

[mcp.google]
command = "google-mcp"

[profiles.personal]
mcp = ["google"]
`

const privateSample = `
default = "personal"
files = ["~/Developer/acme/AGENTS.md"]

[mcp.notion-acme]
url = "https://mcp.notion.com/mcp"

[profiles.acme]
dirs = ["~/Developer/acme"]
mcp = ["google", "notion-acme"]
`

func writePrivate(t *testing.T, home, body string) string {
	t.Helper()
	p := filepath.Join(home, ".config", "ai-profiles", "private.toml")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMergesPrivateFile(t *testing.T) {
	home := t.TempDir()
	pub := writeConfig(t, publicSample)
	priv := writePrivate(t, home, privateSample)

	cfg, err := Load(pub, home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "personal" || cfg.DefaultFile != priv {
		t.Errorf("default = %q from %s", cfg.Default, cfg.DefaultFile)
	}
	if cfg.Profiles["acme"] == nil || cfg.Profiles["acme"].Path != priv {
		t.Errorf("acme should come from the private file: %+v", cfg.Profiles["acme"])
	}
	if cfg.Profiles["personal"].Path != pub {
		t.Errorf("personal should come from the public file, got %s", cfg.Profiles["personal"].Path)
	}
	if cfg.MCP["notion-acme"] == nil {
		t.Error("servers of the private file should be merged")
	}
	if len(cfg.Files) != 1 || cfg.Files[0] != filepath.Join(home, "Developer", "acme", "AGENTS.md") {
		t.Errorf("files = %v", cfg.Files)
	}
	if cfg.Private.File != priv {
		t.Errorf("private file = %s", cfg.Private.File)
	}
}

func TestLoadWithoutPrivateFile(t *testing.T) {
	home := t.TempDir()
	pub := writeConfig(t, publicSample)
	_, err := Load(pub, home)
	var missing *private.MissingError
	if !errors.As(err, &missing) || missing.File != filepath.Join(home, ".config", "ai-profiles", "private.toml") {
		t.Errorf("a missing private file must be reported as such, got %v", err)
	}
}

func TestLoadPrivateConflicts(t *testing.T) {
	cases := map[string]struct{ public, private, want string }{
		"profile in both": {publicSample, privateSample + "\n[profiles.personal]\n", "personal"},
		"server in both":  {publicSample, privateSample + "\n[mcp.google]\ncommand = \"x\"\n", "google"},
		"default in both": {"default = \"personal\"\n" + publicSample, privateSample, "default"},
		"nested private":  {publicSample, privateSample + "\n[private]\nfile = \"x\"\n", "[private]"},
		"public files":    {"files = [\"~/x\"]\n" + publicSample, privateSample, "files"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			pub := writeConfig(t, c.public)
			writePrivate(t, home, c.private)
			if _, err := Load(pub, home); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want an error about %s, got %v", c.want, err)
			}
		})
	}
}

func TestLoadWithoutPrivateSection(t *testing.T) {
	home := t.TempDir()
	cfg, err := Load(writeConfig(t, sample), home)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Profiles {
		if p.Path != cfg.Path {
			t.Errorf("profile %s from %s, want %s", p.Name, p.Path, cfg.Path)
		}
	}
	if cfg.DefaultFile != cfg.Path {
		t.Errorf("default from %s", cfg.DefaultFile)
	}
}

func TestEditPrivateFile(t *testing.T) {
	home := t.TempDir()
	pub := writeConfig(t, publicSample)
	priv := writePrivate(t, home, privateSample)

	if err := EditFile(pub, priv, home, func(b []byte) ([]byte, bool, error) {
		return AddToProfile(b, "acme", "mcp", "nope")
	}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("an edit that breaks the merged config must be refused, got %v", err)
	}
	if err := EditFile(pub, priv, home, func(b []byte) ([]byte, bool, error) {
		return SetDefault(b, "acme")
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(pub, home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Default != "acme" {
		t.Errorf("default = %s", cfg.Default)
	}
}
