package accounts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// memStore is a Keychain in memory.
type memStore map[string]string

func (m memStore) Read(service string) (string, error) {
	v, ok := m[service]
	if !ok {
		return "", errNotFound
	}
	return v, nil
}

func (m memStore) Write(service, value string) error { m[service] = value; return nil }

func cred(token string) string {
	return `{"claudeAiOauth":{"accessToken":"` + token + `","refreshToken":"r-` + token + `"}}`
}

func setup(t *testing.T) (*Manager, memStore, string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".claude-personal")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"numStartups": 5, "oauthAccount": {"emailAddress": "io@example.com", "accountUuid": "u-personal"}}`), 0o600)
	store := memStore{}
	m := &Manager{Home: home, Profile: "personal", Store: store}
	store[m.entry()] = cred("personal-1")
	return m, store, dir
}

func TestSaveAndUse(t *testing.T) {
	m, store, dir := setup(t)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	// The user logs in with the other account (Claude rewrites both places)
	// and saves it too.
	store[m.entry()] = cred("client-d-1")
	os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"numStartups": 5, "oauthAccount": {"emailAddress": "io@client-d.example", "accountUuid": "u-client-d"}}`), 0o600)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	list, err := m.List()
	if err != nil || len(list) != 2 || !list[0].Active || list[0].Email != "io@client-d.example" {
		t.Fatalf("list = %+v, %v", list, err)
	}

	// While client-d is active a session refreshes its token.
	store[m.entry()] = cred("client-d-2")
	if err := m.Use("io@example.com"); err != nil {
		t.Fatal(err)
	}
	if store[m.entry()] != cred("personal-1") {
		t.Errorf("the profile entry should hold personal's credentials")
	}
	var slot struct{ Credentials string }
	json.Unmarshal([]byte(store[slotService("personal", "io@client-d.example")]), &slot)
	if slot.Credentials != cred("client-d-2") {
		t.Errorf("the refreshed client-d token must be saved before switching, got %q", slot.Credentials)
	}
	b, _ := os.ReadFile(filepath.Join(dir, ".claude.json"))
	if !strings.Contains(string(b), `"io@example.com"`) || !strings.Contains(string(b), `"numStartups": 5`) {
		t.Errorf(".claude.json should show the active account and keep the rest: %s", b)
	}

	if err := m.Use("io@client-d.example"); err != nil {
		t.Fatal(err)
	}
	if store[m.entry()] != cred("client-d-2") {
		t.Errorf("back to client-d with its refreshed token")
	}
	if err := m.Use("io@client-d.example"); err != nil {
		t.Errorf("using the active account again is fine: %v", err)
	}
	if err := m.Use("nessuno@x.it"); err == nil || !strings.Contains(err.Error(), "account save") {
		t.Errorf("an unsaved account should point to account save, got %v", err)
	}
}

func TestEntryService(t *testing.T) {
	m := &Manager{Home: "/Users/io", Profile: "personal"}
	if got := m.entry(); got != "Claude Code-credentials-6094754a" {
		t.Errorf("entry = %q", got)
	}
}

func (m memStore) Keys() []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestMatch(t *testing.T) {
	m, store, dir := setup(t)
	m.Save()
	store[m.entry()] = cred("client-d-1")
	os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount": {"emailAddress": "io@client-d.example"}}`), 0o600)
	m.Save()
	for in, want := range map[string]string{
		"io@client-d.example": "io@client-d.example",
		"client-d":            "io@client-d.example",
		"EXAMPLE.COM":         "io@example.com",
	} {
		if got, err := m.Match(in); err != nil || got != want {
			t.Errorf("Match(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := m.Match("io@"); err == nil || !strings.Contains(err.Error(), "io@example.com") {
		t.Errorf("an ambiguous part lists the candidates: %v", err)
	}
	if _, err := m.Match("nessuno"); err == nil {
		t.Errorf("no match is an error")
	}
}
