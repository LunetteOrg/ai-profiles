package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSplitOpRef(t *testing.T) {
	for ref, want := range map[string][2]string{
		"op://V/I/f":                            {"", "op://V/I/f"},
		"op://V/I/f?account=work":               {"work", "op://V/I/f"},
		"op://V/I/f?attribute=otp&account=work": {"work", "op://V/I/f?attribute=otp"},
		"op://V/I/f?account=work&attribute=otp": {"work", "op://V/I/f?attribute=otp"},
		"op://V/I/secret key?account=work":      {"work", "op://V/I/secret key"},
		"op://V/I/f?attribute=otp":              {"", "op://V/I/f?attribute=otp"},
	} {
		acc, plain := SplitOpRef(ref)
		if acc != want[0] || plain != want[1] {
			t.Errorf("SplitOpRef(%q) = %q, %q; want %q, %q", ref, acc, plain, want[0], want[1])
		}
	}
}

func TestWithOpAccount(t *testing.T) {
	for _, c := range []struct{ ref, account, want string }{
		{"op://V/I/f", "work", "op://V/I/f?account=work"},
		{"op://V/I/f?attribute=otp", "work", "op://V/I/f?attribute=otp&account=work"},
		{"op://V/I/f?account=personal", "work", "op://V/I/f?account=personal"},
		{"op://V/I/f", "", "op://V/I/f"},
		{"bw://Item", "work", "bw://Item"},
		{"env:X", "work", "env:X"},
	} {
		if got := WithOpAccount(c.ref, c.account); got != c.want {
			t.Errorf("WithOpAccount(%q, %q) = %q, want %q", c.ref, c.account, got, c.want)
		}
	}
}

func TestLoadStoresOnePasswordAccounts(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".config", "ai-secrets", "config.toml")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte(`[stores.1password]
read = "shared"
[stores.1password.personal]
account = "my.1password.com"
[stores.1password.work]
account = "work.1password.eu"
`), 0o600)
	s, err := LoadStores(home)
	if err != nil {
		t.Fatal(err)
	}
	if s.OpIsolated || len(s.OnePassword) != 2 || s.OnePassword["work"].Account != "work.1password.eu" || s.OnePassword["work"].Name != "work" {
		t.Errorf("stores = %+v, accounts %v", s, s.OnePassword)
	}
	for name, body := range map[string]string{
		"account vuoto":      "[stores.1password.work]\naccount = \"\"\n",
		"chiave sconosciuta": "[stores.1password]\nreed = \"shared\"\n",
		"read non stringa":   "[stores.1password]\nread = 1\n",
	} {
		os.WriteFile(p, []byte(body), 0o600)
		if _, err := LoadStores(home); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadStoresOnePasswordPrivate(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, ".config", "ai-secrets", "config.toml")
	priv := filepath.Join(home, ".config", "ai-secrets", "private.toml")
	os.MkdirAll(filepath.Dir(p), 0o700)
	os.WriteFile(p, []byte("[private]\nfile = \"~/.config/ai-secrets/private.toml\"\n[stores.1password]\nread = \"isolated\"\n[stores.1password.personal]\naccount = \"my.1password.com\"\n"), 0o600)
	os.WriteFile(priv, []byte("[stores.1password.work]\naccount = \"work.1password.eu\"\n"), 0o600)
	s, err := LoadStores(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.OnePassword) != 2 {
		t.Errorf("accounts from both files: %v", s.OnePassword)
	}
	os.WriteFile(priv, []byte("[stores.1password.personal]\naccount = \"x\"\n"), 0o600)
	if _, err := LoadStores(home); err == nil {
		t.Error("the same account in both files: expected an error")
	}
}

func TestResolveOpAccount(t *testing.T) {
	var calls []call
	r := &Resolver{
		Getenv: func(string) string { return "" },
		Run: func(env []string, name string, args ...string) (string, error) {
			calls = append(calls, call{env, name, args})
			return "v", nil
		},
		Stores: &Stores{OnePassword: map[string]*OnePassword{
			"work": {Name: "work", Account: "work.1password.eu"},
		}},
	}
	if _, err := r.Resolve("op://V/I/secret key?account=work"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Resolve("op://V/I/f"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(calls[0].args, "|"); got != "read|--account|work.1password.eu|--no-newline|op://V/I/secret key" {
		t.Errorf("with an account: op %s", got)
	}
	if got := strings.Join(calls[1].args, "|"); got != "read|--no-newline|op://V/I/f" {
		t.Errorf("without an account op picks: op %s", got)
	}
	if _, err := r.Resolve("op://V/I/f?account=nessuno"); err == nil || !strings.Contains(err.Error(), "nessuno") {
		t.Errorf("an undeclared account should be an error naming it, got %v", err)
	}
}

func TestBitwardenPasswordFromOpAccount(t *testing.T) {
	f := &fakeBW{}
	r := newResolver(f, true)
	r.Stores.OnePassword = map[string]*OnePassword{"personal": {Name: "personal", Account: "my.1password.com"}}
	r.Stores.Bitwarden["client-e"].Password = "op://Private/bw/password?account=personal"
	if _, err := r.Resolve("bw://Kimi"); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.isolated {
		if c.name == "op" && strings.Join(c.args, "|") != "read|--account|my.1password.com|--no-newline|op://Private/bw/password" {
			t.Errorf("master password read: op %v", c.args)
		}
	}
}

func TestOpReadWithoutAccountAmongSeveral(t *testing.T) {
	r := &Resolver{
		Getenv: func(string) string { return "" },
		Run: func(env []string, name string, args ...string) (string, error) {
			return "", errors.New("exit status 1")
		},
		Stores: &Stores{OnePassword: map[string]*OnePassword{
			"personal": {Name: "personal", Account: "my.1password.com"},
			"work":     {Name: "work", Account: "work.1password.eu"},
		}},
	}
	_, err := r.Resolve("op://V/I/f")
	if err == nil || !strings.Contains(err.Error(), "no 1Password account") || !strings.Contains(err.Error(), "?account=") || !strings.Contains(err.Error(), "personal, work") {
		t.Errorf("a failed read without an account among several should say so, got %v", err)
	}
	if _, err := r.Resolve("op://V/I/f?account=work"); err == nil || strings.Contains(err.Error(), "no 1Password account") {
		t.Errorf("with an account, the usual message: %v", err)
	}
}
