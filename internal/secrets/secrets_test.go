package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type call struct {
	env  []string
	name string
	args []string
}

func TestIsRef(t *testing.T) {
	for v, want := range map[string]bool{
		"op://Vault/Item/field": true,
		"bw://Client-d Mistral": true,
		"env:CLIENT_D_KEY":      true,
		"bws://1234":            false,
		"uvx":                   false,
		"https://x":             false,
	} {
		if IsRef(v) != want {
			t.Errorf("IsRef(%q) = %v", v, !want)
		}
	}
}

func TestResolveOpAndEnv(t *testing.T) {
	home := t.TempDir()
	os.WriteFile(filepath.Join(home, ".env"), []byte("# chiavi\nexport DA_FILE=\"dal file\"\nALTRA='singola'\n"), 0o600)
	var calls []call
	r := &Resolver{
		Home:   home,
		Getenv: func(k string) string { return map[string]string{"NEL_AMBIENTE": "x"}[k] },
		Run: func(env []string, name string, args ...string) (string, error) {
			calls = append(calls, call{env, name, args})
			return "dal-1password", nil
		},
	}
	for ref, want := range map[string]string{
		"op://Client-d/Kimi/credential": "dal-1password",
		"env:NEL_AMBIENTE":              "x",
		"env:DA_FILE":                   "dal file",
		"env:ALTRA":                     "singola",
	} {
		if got, err := r.Resolve(ref); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", ref, got, err, want)
		}
	}
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != "read --no-newline op://Client-d/Kimi/credential" {
		t.Errorf("calls = %v", calls)
	}
	if _, err := r.Resolve("env:MANCA"); err == nil {
		t.Errorf("a missing variable is an error")
	}
}

// fakeBW records the calls of a Bitwarden flow; the master password is
// "principale", the unlocked session "sessione".
type fakeBW struct {
	shared, isolated []call
	failGet          bool
}

func (f *fakeBW) run(env []string, name string, args ...string) (string, error) {
	f.shared = append(f.shared, call{env, name, args})
	if name == "op" {
		return "valore-op", nil
	}
	if name == "bw" && args[0] == "get" {
		if f.failGet {
			return "", errors.New("elemento non trovato")
		}
		if !contains(env, "BW_SESSION=sessione") {
			return "", errors.New("bloccato")
		}
		return "chiave-" + args[2] + "\n", nil
	}
	return "", nil
}

func (f *fakeBW) runIsolated(env []string, name string, args ...string) (string, error) {
	f.isolated = append(f.isolated, call{env, name, args})
	switch {
	case name == "op":
		return "principale", nil
	case name == "bw" && args[0] == "unlock":
		if !contains(env, "BW_PASSWORD=principale") {
			return "", errors.New("password sbagliata")
		}
		return "sessione\n", nil
	}
	return "", nil
}

func (f *fakeBW) count(calls []call, name, first string) int {
	n := 0
	for _, c := range calls {
		if c.name == name && (first == "" || (len(c.args) > 0 && c.args[0] == first)) {
			n++
		}
	}
	return n
}

func newResolver(f *fakeBW, lockAfterRead bool) *Resolver {
	return &Resolver{
		Getenv:      func(string) string { return "" },
		Run:         f.run,
		RunIsolated: f.runIsolated,
		Account:     "client-e",
		Stores: &Stores{TTL: DefaultTTL, OpIsolated: true, Bitwarden: map[string]*Bitwarden{
			"client-e": {Name: "client-e", DataDir: "/dati/client-e", Unlock: "password", Password: "op://Private/bw/password", LockAfterRead: lockAfterRead},
		}},
	}
}

func TestBitwardenLockedAfterEveryRead(t *testing.T) {
	f := &fakeBW{}
	r := newResolver(f, true)
	for _, ref := range []string{"bw://Kimi", "bw://client-e/Mistral"} {
		if _, err := r.Resolve(ref); err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
	}
	if f.count(f.isolated, "op", "") != 2 || f.count(f.isolated, "bw", "unlock") != 2 {
		t.Errorf("every read logs in again, in isolation: %v", f.isolated)
	}
	if f.count(f.shared, "bw", "lock") != 2 {
		t.Errorf("bw lock after every read: %v", f.shared)
	}
	if r.Sessions() != 0 {
		t.Errorf("no session is kept")
	}
}

func TestBitwardenUnlockedForTTL(t *testing.T) {
	f := &fakeBW{}
	r := newResolver(f, false)
	now := time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)
	r.Now = func() time.Time { return now }
	r.Resolve("bw://Kimi")
	r.Resolve("bw://Mistral")
	if f.count(f.isolated, "bw", "unlock") != 1 || f.count(f.shared, "bw", "lock") != 0 {
		t.Errorf("one login for the ttl: isolated %v", f.isolated)
	}
	now = now.Add(31 * time.Minute)
	r.Resolve("bw://Kimi")
	if f.count(f.isolated, "bw", "unlock") != 2 || f.count(f.shared, "bw", "lock") != 1 {
		t.Errorf("after the ttl the old session is locked and a new login made: %v / %v", f.isolated, f.shared)
	}
	r.LockAll()
	if f.count(f.shared, "bw", "lock") != 2 || r.Sessions() != 0 {
		t.Errorf("LockAll locks the open sessions")
	}
}

func TestSecretsNeverInClear(t *testing.T) {
	f := &fakeBW{failGet: true}
	r := newResolver(f, true)
	_, err := r.Resolve("bw://Kimi")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, secret := range []string{"principale", "sessione"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("an error must not show %q: %v", secret, err)
		}
	}
	for _, c := range f.shared {
		if c.name == "op" {
			t.Errorf("the master password is read only in isolation")
		}
		for _, e := range c.env {
			if strings.HasPrefix(e, "BW_PASSWORD=") {
				t.Errorf("the master password must not reach other commands")
			}
		}
	}
}

func TestBitwardenErrors(t *testing.T) {
	f := &fakeBW{}
	r := newResolver(f, true)
	r.Account = ""
	if _, err := r.Resolve("bw://Kimi"); err == nil || !strings.Contains(err.Error(), "account") {
		t.Errorf("no account named or given: %v", err)
	}
	r.Stores.Bitwarden["bio"] = &Bitwarden{Name: "bio", Unlock: "biometric", LockAfterRead: true}
	if _, err := r.Resolve("bw://bio/Kimi"); err == nil || !strings.Contains(err.Error(), "biometric") {
		t.Errorf("biometric is not available yet: %v", err)
	}
}

func TestOpIsolatedOrShared(t *testing.T) {
	f := &fakeBW{}
	r := newResolver(f, true)
	r.Resolve("op://a/b/c")
	if f.count(f.isolated, "op", "") != 1 || f.count(f.shared, "op", "") != 0 {
		t.Errorf("op read isolated by default")
	}
	r.Stores.OpIsolated = false
	r.Resolve("op://a/b/c")
	if f.count(f.shared, "op", "") != 1 {
		t.Errorf("read = shared uses the agent's session")
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func TestBitwardenFields(t *testing.T) {
	var gets [][]string
	item := `{"id":"id-123","name":"VPN","login":{"username":"admin"},"fields":[{"name":"OTP seed","value":"semeotp"}]}`
	run := func(env []string, name string, args ...string) (string, error) {
		if name == "bw" && args[0] == "get" {
			gets = append(gets, args)
			switch args[1] {
			case "password":
				return "pw\n", nil
			case "username":
				return "admin\n", nil
			case "item":
				return item, nil
			case "attachment":
				return "client\nremote vpn.example 1194\n", nil
			}
		}
		return "", nil
	}
	f := &fakeBW{}
	r := newResolver(f, true)
	r.Run = run
	cases := map[string]string{
		"bw://client-e/VPN":                        "pw",
		"bw://client-e/VPN#username":               "admin",
		"bw://client-e/VPN#field=OTP seed":         "semeotp",
		"bw://client-e/VPN#attachment=client.ovpn": "client\nremote vpn.example 1194\n",
	}
	for ref, want := range cases {
		got, err := r.Resolve(ref)
		if err != nil || got != want {
			t.Errorf("%s = %q, %v; want %q", ref, got, err, want)
		}
	}
	var att []string
	for _, g := range gets {
		if g[1] == "attachment" {
			att = g
		}
	}
	if strings.Join(att, " ") != "get attachment client.ovpn --itemid id-123 --raw" {
		t.Errorf("attachment read by item id, to stdout: %v", att)
	}
	if _, err := r.Resolve("bw://client-e/VPN#field=manca"); err == nil || strings.Contains(err.Error(), "semeotp") {
		t.Errorf("a missing field is an error that shows no value: %v", err)
	}
	if _, err := r.Resolve("bw://client-e/VPN#boh"); err == nil {
		t.Errorf("an unknown part is an error")
	}
}

func TestBitwardenOneLoginPerRequest(t *testing.T) {
	f := &fakeBW{}
	r := newResolver(f, true)
	r.Hold()
	for _, ref := range []string{"bw://client-e/VPN", "bw://client-e/VPN#username", "bw://client-e/Altro"} {
		if _, err := r.Resolve(ref); err != nil {
			t.Fatal(err)
		}
	}
	if f.count(f.isolated, "bw", "unlock") != 1 || f.count(f.shared, "bw", "lock") != 0 {
		t.Errorf("reads asked together share one login: %v", f.isolated)
	}
	r.Release()
	if f.count(f.shared, "bw", "lock") != 1 || r.Sessions() != 0 {
		t.Errorf("Release locks: %v", f.shared)
	}
	r.Resolve("bw://client-e/VPN")
	if f.count(f.isolated, "bw", "unlock") != 2 || f.count(f.shared, "bw", "lock") != 2 {
		t.Errorf("outside a request every read locks again")
	}
}
