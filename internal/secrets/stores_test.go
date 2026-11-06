package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/private"
)

func TestLoadStoresDefaults(t *testing.T) {
	home := t.TempDir()
	s, err := LoadStores(home)
	if err != nil {
		t.Fatal(err)
	}
	if s.TTL != 30*time.Minute || !s.OpIsolated || len(s.Bitwarden) != 0 {
		t.Errorf("defaults without a file: %+v", s)
	}
}

func TestLoadStores(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".config", "ai-secrets", "config.toml")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte(`ttl = "45m"
[stores.1password]
read = "shared"
[stores.bitwarden.client-e]
server = "https://vault.bitwarden.eu"
password = "op://Private/x/password"
[stores.bitwarden.aperto]
password = "op://Private/y/password"
lock_after_read = false
`), 0o600)
	s, err := LoadStores(home)
	if err != nil {
		t.Fatal(err)
	}
	if s.TTL != 45*time.Minute || s.OpIsolated {
		t.Errorf("ttl and read: %+v", s)
	}
	w := s.Bitwarden["client-e"]
	if w == nil || w.Unlock != "password" || !w.LockAfterRead || w.DataDir != filepath.Join(home, ".config", "ai-secrets", "bitwarden", "client-e") || w.Name != "client-e" {
		t.Errorf("client-e = %+v", w)
	}
	if s.Bitwarden["aperto"].LockAfterRead {
		t.Errorf("lock_after_read = false is kept")
	}

	for name, body := range map[string]string{
		"read sconosciuto":   "[stores.1password]\nread = \"boh\"\n",
		"unlock sconosciuto": "[stores.bitwarden.x]\nunlock = \"boh\"\npassword = \"op://a/b/c\"\n",
		"password non op":    "[stores.bitwarden.x]\npassword = \"segreta\"\n",
		"password mancante":  "[stores.bitwarden.x]\nserver = \"https://x\"\n",
		"ttl non valido":     "ttl = \"presto\"\n",
	} {
		os.WriteFile(p, []byte(body), 0o600)
		if _, err := LoadStores(home); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	os.WriteFile(p, []byte("[stores.bitwarden.bio]\nunlock = \"biometric\"\n"), 0o600)
	if _, err := LoadStores(home); err != nil {
		t.Errorf("biometric needs no password: %v", err)
	}
}

func TestSplitBitwardenRef(t *testing.T) {
	accounts := map[string]*Bitwarden{"client-e": {Name: "client-e"}}
	for ref, want := range map[string][2]string{
		"bw://client-e/VPN - Firewall (openvpn)": {"client-e", "VPN - Firewall (openvpn)"},
		"bw://VPN":                               {"", "VPN"},
		"bw://non-account/elemento":              {"", "non-account/elemento"},
	} {
		acc, item := SplitBitwardenRef(ref, accounts)
		if acc != want[0] || item != want[1] {
			t.Errorf("%s → %q, %q; want %q", ref, acc, item, want)
		}
	}
	if !strings.HasPrefix("bw://client-e/x", "bw://") {
		t.Fatal()
	}
}

func TestLoadStoresPrivateFile(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".config", "ai-secrets", "config.toml")
	priv := filepath.Join(home, ".config", "ai-secrets", "private.toml")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte(`ttl = "45m"
[private]
file = "~/.config/ai-secrets/private.toml"
restore = "cat archive"
backup = "cat > archive"
[stores.1password]
read = "isolated"
`), 0o600)

	_, err := LoadStores(home)
	var missing *private.MissingError
	if !errors.As(err, &missing) || missing.File != priv {
		t.Fatalf("a missing private file must be reported as such, got %v", err)
	}

	os.WriteFile(priv, []byte(`[stores.bitwarden.client-e]
server = "https://vault.bitwarden.eu"
password = "op://Private/x/password"
`), 0o600)
	s, err := LoadStores(home)
	if err != nil {
		t.Fatal(err)
	}
	if s.TTL != 45*time.Minute || s.Bitwarden["client-e"] == nil || s.Private.File != priv {
		t.Errorf("stores = %+v", s)
	}

	for name, body := range map[string]string{
		"ttl in both":     "ttl = \"1h\"\n",
		"read in both":    "[stores.1password]\nread = \"shared\"\n",
		"nested private":  "[private]\nfile = \"x\"\n",
		"invalid account": "[stores.bitwarden.x]\npassword = \"segreta\"\n",
	} {
		os.WriteFile(priv, []byte(body), 0o600)
		if _, err := LoadStores(home); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
