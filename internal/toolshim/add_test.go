package toolshim

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

type addFixture struct {
	home, dir, cfgPath, scwPath, self, shims string
	created                                  []map[string]any // templates op received
	createdAccount                           string
	asked                                    map[string]string
	wrongValue                               bool
	out                                      bytes.Buffer
}

func newAddFixture(t *testing.T) *addFixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("links")
	}
	f := &addFixture{home: t.TempDir()}
	f.dir = filepath.Join(f.home, "Developer", "client")
	os.MkdirAll(f.dir, 0o755)
	f.cfgPath = filepath.Join(f.home, "config.toml")
	write(t, f.cfgPath, `default = "personal"
[profiles.personal]
[profiles.client]
dirs = ["~/Developer/client"]
onepassword = "work"
`, 0o600)
	f.scwPath = filepath.Join(f.home, ".config", "scw", "config.yaml")
	write(t, f.scwPath, scwFile, 0o600)
	f.self = filepath.Join(f.home, "bin", "ai-secrets")
	write(t, f.self, "#!/bin/sh\n", 0o755)
	f.shims = filepath.Join(f.home, ".local", "share", "ai-profiles", "tool-shims")
	return f
}

func (f *addFixture) adder(t *testing.T) *Adder {
	cfg, err := config.Load(f.cfgPath, f.home)
	if err != nil {
		t.Fatal(err)
	}
	return &Adder{
		Home: f.home, Dir: f.dir, Getenv: func(string) string { return "" },
		Cfg: cfg,
		Stores: &secrets.Stores{OnePassword: map[string]*secrets.OnePassword{
			"work":     {Name: "work", Account: "work.1password.eu"},
			"personal": {Name: "personal", Account: "my.1password.com"},
		}},
		ShimsDir: f.shims, Self: f.self, PathList: "",
		CreateItem: func(account, vault string, item []byte) (string, error) {
			var m map[string]any
			if err := json.Unmarshal(item, &m); err != nil {
				return "", err
			}
			f.created, f.createdAccount = append(f.created, m), account
			return "newitemid", nil
		},
		// Get returns what the item holds, by the label in the reference.
		Get: func(reason string, refs map[string]string, account string) (map[string]string, error) {
			f.asked = refs
			byLabel := map[string]string{}
			for _, fl := range f.created[len(f.created)-1]["fields"].([]any) {
				m := fl.(map[string]any)
				byLabel[m["label"].(string)], _ = m["value"].(string)
			}
			out := map[string]string{}
			for k, ref := range refs {
				_, plain := secrets.SplitOpRef(ref)
				out[k] = byLabel[plain[strings.LastIndex(plain, "/")+1:]]
				if f.wrongValue && k == "SCW_SECRET_KEY" {
					out[k] = "other"
				}
			}
			return out, nil
		},
		Out: &f.out,
	}
}

func TestAddTool(t *testing.T) {
	f := newAddFixture(t)
	if err := f.adder(t).Add(AddOptions{Tool: "scw", Vault: "Client"}); err != nil {
		t.Fatal(err)
	}
	if len(f.created) != 1 || f.created[0]["title"] != "Scaleway - scw CLI indagine" || f.createdAccount != "work.1password.eu" {
		t.Errorf("item %v in account %q", f.created, f.createdAccount)
	}
	if f.asked["SCW_SECRET_KEY"] != "op://Client/newitemid/secret key?account=work" {
		t.Errorf("verified refs %v", f.asked)
	}
	cfg, err := config.Load(f.cfgPath, f.home)
	if err != nil {
		t.Fatal(err)
	}
	tool := cfg.Profiles["client"].Tools["scw"]
	if tool == nil || tool.Env["SCW_SECRET_KEY"] != "op://Client/newitemid/secret key" || tool.Env["SCW_ACCESS_KEY"] != "op://Client/newitemid/access key" || len(tool.DenyArgs) != 4 || tool.Reason != "Scaleway CLI" {
		t.Errorf("section %+v", tool)
	}
	if real, err := filepath.EvalSymlinks(filepath.Join(f.shims, "scw")); err != nil || real != canonical(f.self) {
		t.Errorf("link: %q, %v", real, err)
	}
	b, _ := os.ReadFile(f.scwPath)
	if strings.Contains(string(b), "indagine-secret-value") || strings.Contains(string(b), "old-secret-value") || !strings.Contains(string(b), "SCWINDAGINEAAAAAAAAA") {
		t.Errorf("scw config after:\n%s", b)
	}
	for _, v := range []string{"indagine-secret-value", "SCWINDAGINEAAAAAAAAA"} {
		if strings.Contains(f.out.String(), v) {
			t.Fatalf("the command printed a value:\n%s", f.out.String())
		}
	}
}

func TestAddToolOtherAccount(t *testing.T) {
	f := newAddFixture(t)
	if err := f.adder(t).Add(AddOptions{Tool: "scw", Vault: "Private", Account: "personal", From: "altro", Item: "Scaleway altro"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(f.cfgPath, f.home)
	if got := cfg.Profiles["client"].Tools["scw"].Env["SCW_SECRET_KEY"]; got != "op://Private/newitemid/secret key?account=personal" {
		t.Errorf("a reference outside the profile's account names it: %q", got)
	}
	if f.createdAccount != "my.1password.com" || f.created[0]["title"] != "Scaleway altro" {
		t.Errorf("item in %q titled %v", f.createdAccount, f.created[0]["title"])
	}
}

func TestAddToolMismatchLeavesFiles(t *testing.T) {
	f := newAddFixture(t)
	f.wrongValue = true
	cfgBefore, _ := os.ReadFile(f.cfgPath)
	err := f.adder(t).Add(AddOptions{Tool: "scw", Vault: "Client"})
	if err == nil || !strings.Contains(err.Error(), "SCW_SECRET_KEY") || !strings.Contains(err.Error(), "newitemid") {
		t.Fatalf("expected a mismatch naming the variable and the item, got %v", err)
	}
	if strings.Contains(err.Error(), "indagine-secret-value") || strings.Contains(err.Error(), "other") {
		t.Fatal("the error carries a value")
	}
	cfgAfter, _ := os.ReadFile(f.cfgPath)
	b, _ := os.ReadFile(f.scwPath)
	if !bytes.Equal(cfgBefore, cfgAfter) || string(b) != scwFile {
		t.Error("files changed after a mismatch")
	}
	if _, err := os.Lstat(filepath.Join(f.shims, "scw")); !errors.Is(err, os.ErrNotExist) {
		t.Error("link created after a mismatch")
	}
}

func TestAddToolRefusesBeforeCreating(t *testing.T) {
	cases := map[string]func(f *addFixture) AddOptions{
		"sezione già presente": func(f *addFixture) AddOptions {
			os.WriteFile(f.cfgPath, []byte(`default = "personal"
[profiles.personal]
[profiles.client]
dirs = ["~/Developer/client"]
onepassword = "work"
[profiles.client.tools.scw]
env.SCW_SECRET_KEY = "op://V/I/f"
`), 0o600)
			return AddOptions{Tool: "scw", Vault: "Client"}
		},
		"tool senza preset":   func(f *addFixture) AddOptions { return AddOptions{Tool: "aws", Vault: "Client"} },
		"account sconosciuto": func(f *addFixture) AddOptions { return AddOptions{Tool: "scw", Vault: "Client", Account: "nessuno"} },
		"profilo scw assente": func(f *addFixture) AddOptions { return AddOptions{Tool: "scw", Vault: "Client", From: "manca"} },
		"vault mancante":      func(f *addFixture) AddOptions { return AddOptions{Tool: "scw"} },
		"link di un altro file": func(f *addFixture) AddOptions {
			write(t, filepath.Join(f.shims, "scw"), "#!/bin/sh\n", 0o755)
			return AddOptions{Tool: "scw", Vault: "Client"}
		},
	}
	for name, setup := range cases {
		f := newAddFixture(t)
		o := setup(f)
		if err := f.adder(t).Add(o); err == nil {
			t.Errorf("%s: expected an error", name)
		}
		if len(f.created) != 0 {
			t.Errorf("%s: the item was created before the refusal", name)
		}
	}
}

func TestAddToolMiseNote(t *testing.T) {
	f := newAddFixture(t)
	miseDir := filepath.Join(f.home, ".local", "share", "mise", "installs", "scaleway", "2.65.2")
	write(t, filepath.Join(miseDir, "scw"), "#!/bin/sh\n", 0o755)
	a := f.adder(t)
	a.PathList = f.shims + string(os.PathListSeparator) + miseDir
	if err := a.Add(AddOptions{Tool: "scw", Vault: "Client"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.out.String(), "_.path") {
		t.Errorf("a tool from mise needs the _.path note:\n%s", f.out.String())
	}
}
