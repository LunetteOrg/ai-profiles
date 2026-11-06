// Package accounts switches the Claude account of a profile while its
// sessions keep running: Claude Code rereads its Keychain entry, so replacing
// the entry's credentials moves the open sessions to the other account.
// Each account of a profile is saved in its own Keychain entry.
package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/apply"
	"github.com/LunetteOrg/ai-profiles/internal/launch"
)

var errNotFound = errors.New("item not found")

// Store reads and writes Keychain entries.
type Store interface {
	Read(service string) (string, error)
	Write(service, value string) error
}

// Keychain is the macOS Keychain through the security CLI, the way Claude
// Code itself writes: an entry written by another program changes its access
// rules, and every read by Claude Code would then ask for the password.
type Keychain struct{ Account string }

func (k Keychain) Read(service string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-w").Output()
	if err != nil {
		return "", errNotFound
	}
	return strings.TrimSpace(string(out)), nil
}

func (k Keychain) Write(service, value string) error {
	exec.Command("security", "delete-generic-password", "-s", service).Run()
	// The value goes as hex on security's command line, as Claude Code does.
	cmd := exec.Command("security", "add-generic-password", "-U", "-a", k.Account, "-s", service, "-X", fmt.Sprintf("%x", value))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("writing %s failed: %w", service, err)
	}
	return nil
}

type Manager struct {
	Home    string
	Profile string
	Store   Store
}

// slot is a saved account: the Keychain credentials and the oauthAccount
// Claude Code shows in .claude.json.
type slot struct {
	Credentials  string          `json:"credentials"`
	OAuthAccount json.RawMessage `json:"oauthAccount"`
}

const slotPrefix = "ai-profile-account:"

func slotService(profile, email string) string {
	return slotPrefix + profile + ":" + strings.ToLower(email)
}

func (m *Manager) dir() string   { return launch.ProfileDir(m.Home, "claude", m.Profile) }
func (m *Manager) entry() string { return launch.ClaudeKeychainService(m.dir()) }

// active returns the email and oauthAccount of the account in use.
func (m *Manager) active() (string, json.RawMessage, error) {
	b, err := os.ReadFile(filepath.Join(m.dir(), ".claude.json"))
	if err != nil {
		return "", nil, err
	}
	var doc struct {
		OAuthAccount json.RawMessage `json:"oauthAccount"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return "", nil, err
	}
	var acc struct {
		Email string `json:"emailAddress"`
	}
	json.Unmarshal(doc.OAuthAccount, &acc)
	if acc.Email == "" {
		return "", nil, fmt.Errorf("no account logged in to profile %s: log in to Claude", m.Profile)
	}
	return acc.Email, doc.OAuthAccount, nil
}

// Save stores the active account's current credentials in its own entry.
func (m *Manager) Save() error {
	email, oauth, err := m.active()
	if err != nil {
		return err
	}
	creds, err := m.Store.Read(m.entry())
	if err != nil {
		return fmt.Errorf("credentials of profile %s are unreadable: %w", m.Profile, err)
	}
	b, _ := json.Marshal(slot{Credentials: creds, OAuthAccount: oauth})
	return m.Store.Write(slotService(m.Profile, email), string(b))
}

// Use makes email the profile's account: the current account is saved first,
// since a session may have refreshed its token, then the other account's
// credentials go into the profile's entry and .claude.json shows it.
func (m *Manager) Use(email string) error {
	current, _, err := m.active()
	if err != nil {
		return err
	}
	if strings.EqualFold(current, email) {
		return nil
	}
	raw, err := m.Store.Read(slotService(m.Profile, email))
	if err != nil {
		return fmt.Errorf("%s is not saved in profile %s: log in with that account, then run ai-profile account save", email, m.Profile)
	}
	var target slot
	if err := json.Unmarshal([]byte(raw), &target); err != nil || target.Credentials == "" {
		return fmt.Errorf("saved credentials of %s are unreadable", email)
	}
	if err := m.Save(); err != nil {
		return fmt.Errorf("saving %s before the switch: %w", current, err)
	}
	if err := m.Store.Write(m.entry(), target.Credentials); err != nil {
		return err
	}
	return apply.EditJSONFile(filepath.Join(m.dir(), ".claude.json"), func(doc map[string]any) bool {
		var acc any
		json.Unmarshal(target.OAuthAccount, &acc)
		doc["oauthAccount"] = acc
		return true
	})
}

type Account struct {
	Email  string
	Active bool
}

// List returns the accounts saved for the profile, the active one marked.
func (m *Manager) List() ([]Account, error) {
	current, _, _ := m.active()
	names, err := m.saved()
	if err != nil {
		return nil, err
	}
	var out []Account
	for _, e := range names {
		out = append(out, Account{Email: e, Active: strings.EqualFold(e, current)})
	}
	return out, nil
}

// saved finds the profile's slots: in a memory store by key, in the
// Keychain by its listing.
func (m *Manager) saved() ([]string, error) {
	prefix := slotPrefix + m.Profile + ":"
	var out []string
	switch s := m.Store.(type) {
	case interface{ Services() ([]string, error) }:
		all, err := s.Services()
		if err != nil {
			return nil, err
		}
		for _, svc := range all {
			if rest, ok := strings.CutPrefix(svc, prefix); ok {
				out = append(out, rest)
			}
		}
	default:
		if mem, ok := m.Store.(interface{ Keys() []string }); ok {
			for _, k := range mem.Keys() {
				if rest, ok := strings.CutPrefix(k, prefix); ok {
					out = append(out, rest)
				}
			}
		}
	}
	sort.Strings(out)
	return out, nil
}

// Services lists the generic-password services of the Keychain, names only.
func (k Keychain) Services() ([]string, error) {
	out, err := exec.Command("security", "dump-keychain").Output()
	if err != nil {
		return nil, err
	}
	var svcs []string
	for _, line := range strings.Split(string(out), "\n") {
		if i := strings.Index(line, `"svce"<blob>="`); i >= 0 {
			v := line[i+len(`"svce"<blob>="`):]
			svcs = append(svcs, strings.TrimSuffix(v, `"`))
		}
	}
	return svcs, nil
}

// SavedAccount is a saved account and its credentials.
type SavedAccount struct {
	Email       string
	Credentials string
}

// SavedCredentials returns the credentials of every saved account.
func (m *Manager) SavedCredentials() ([]SavedAccount, error) {
	names, err := m.saved()
	if err != nil {
		return nil, err
	}
	var out []SavedAccount
	for _, e := range names {
		raw, err := m.Store.Read(slotService(m.Profile, e))
		if err != nil {
			continue
		}
		var s slot
		if json.Unmarshal([]byte(raw), &s) == nil && s.Credentials != "" {
			out = append(out, SavedAccount{Email: e, Credentials: s.Credentials})
		}
	}
	return out, nil
}

// Match finds the saved account an argument means: the email itself, or a
// part of it that only one saved account contains.
func (m *Manager) Match(arg string) (string, error) {
	names, err := m.saved()
	if err != nil {
		return "", err
	}
	var found []string
	for _, e := range names {
		if strings.EqualFold(e, arg) {
			return e, nil
		}
		if strings.Contains(strings.ToLower(e), strings.ToLower(arg)) {
			found = append(found, e)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", fmt.Errorf("no saved account of profile %s matches %q (saved: %s)", m.Profile, arg, strings.Join(names, ", "))
	}
	return "", fmt.Errorf("%q matches several accounts: %s", arg, strings.Join(found, ", "))
}
