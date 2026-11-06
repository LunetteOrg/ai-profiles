package handoff

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/usage"
)

const cfgBody = `
default = "personal"
[profiles.personal]
agents = ["claude", "codex"]
handoff_to = ["client-d"]
[profiles.client-d]
dirs = ["~/Developer/client-d"]
agents = ["claude", "codex", "pi"]
[profiles.client-b]
dirs = ["~/Developer/client-b"]
`

func loadCfg(t *testing.T) (*config.Config, string) {
	t.Helper()
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "Developer", "client-d"), 0o755)
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(cfgBody), 0o600)
	cfg, err := config.Load(p, home)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, home
}

func TestAllowed(t *testing.T) {
	cfg, home := loadCfg(t)
	m, _ := cfg.Resolve(filepath.Join(home, "Developer", "client-d"))
	got := Allowed(cfg, m)
	want := []Combo{{"client-d", "claude"}, {"client-d", "codex"}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("in client-d: %v, want %v (no handoff_to, pi has no adapter)", got, want)
	}
	m, _ = cfg.Resolve(filepath.Join(home, "altrove"))
	got = Allowed(cfg, m)
	if len(got) != 4 || got[0] != (Combo{"personal", "claude"}) || got[2] != (Combo{"client-d", "claude"}) {
		t.Errorf("outside client folders: %v", got)
	}
}

func TestBest(t *testing.T) {
	combos := []Combo{{"personal", "claude"}, {"personal", "codex"}, {"client-d", "claude"}}
	rows := []usage.Row{
		{Profile: "personal", Agent: "claude", Windows: []usage.Window{{Used: 62}, {Used: 91}}},
		{Profile: "personal", Agent: "codex", Err: errors.New("nessuna sessione")},
		{Profile: "client-d", Agent: "claude", Windows: []usage.Window{{Used: 2}, {Used: 3}}},
	}
	c, why := Best(combos, rows)
	if c != (Combo{"client-d", "claude"}) || why == "" {
		t.Errorf("best = %v (%s)", c, why)
	}
	// Without any data the first allowed combination wins.
	c, _ = Best(combos, nil)
	if c != combos[0] {
		t.Errorf("no data: %v", c)
	}
	// Known margin beats unknown.
	c, _ = Best(combos[:2], rows)
	if c != (Combo{"personal", "claude"}) {
		t.Errorf("known margin should win over unknown: %v", c)
	}
}
