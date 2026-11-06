// Package usage reads, for every profile and agent, how much of the
// subscription windows is used, with each profile's own credentials and
// without ever writing them.
package usage

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/accounts"
	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/launch"
)

type Window struct {
	Name     string // "5h", "7d", "7d Fable"
	Used     float64
	Resets   time.Time
	Severity string // as Anthropic reports it: normal, warning, critical
}

type Row struct {
	Profile string
	Agent   string
	Account string // Claude: the account's email
	Active  bool   // Claude: the account the profile uses now
	Windows []Window
	Detail  string    // for agents without windows (OpenCode: cost)
	AsOf    time.Time // when the data was recorded, zero when live
	Err     error
}

// Sources are the ways to read usage; nil fields use the real ones.
type Sources struct {
	Home          string
	Keychain      func(service string) (string, error)
	Fetch         func(url, token string) ([]byte, error)
	OpenCodeStats func(env []string) ([]byte, error)
	// Only, when set, limits Collect to the combinations it accepts.
	Only func(profile, agent string) bool
	// Saved lists the Claude accounts saved for a profile (ai-profile
	// account save); nil for none.
	Saved func(profile string) ([]Saved, error)
}

// Saved is a Claude account saved for a profile: its email and the
// credentials as Claude Code stores them.
type Saved struct {
	Email       string
	Credentials string
}

func (s *Sources) defaults() {
	// Claude Code keeps the login in the Keychain on macOS, in
	// .credentials.json elsewhere. Saved accounts are read from the real
	// Keychain only when the login is too, never next to a fake one.
	realKeychain := s.Keychain == nil && runtime.GOOS == "darwin"
	if realKeychain {
		s.Keychain = keychain
	}
	if s.Fetch == nil {
		s.Fetch = fetch
	}
	if s.OpenCodeStats == nil {
		s.OpenCodeStats = openCodeStats
	}
	if s.Saved == nil && realKeychain {
		home := s.Home
		s.Saved = func(profile string) ([]Saved, error) {
			m := &accounts.Manager{Home: home, Profile: profile, Store: accounts.Keychain{}}
			list, err := m.SavedCredentials()
			var out []Saved
			for _, a := range list {
				out = append(out, Saved{Email: a.Email, Credentials: a.Credentials})
			}
			return out, err
		}
	}
}

// Collect reads usage for every profile and every agent it uses.
func Collect(cfg *config.Config, src Sources) []Row {
	src.defaults()
	var rows []Row
	for _, name := range cfg.Names() {
		for _, agent := range cfg.Profiles[name].Agents {
			if src.Only != nil && !src.Only(name, agent) {
				continue
			}
			row := Row{Profile: name, Agent: agent}
			switch agent {
			case "claude":
				rows = append(rows, claudeRows(src, name)...)
				continue
			case "codex":
				row.Windows, row.AsOf, row.Err = codex(launch.ProfileDir(src.Home, "codex", name))
			case "opencode":
				row.Detail, row.Err = openCode(src, name)
			default:
				row.Err = errors.New("no usage source")
			}
			rows = append(rows, row)
		}
	}
	return rows
}

// claudeRows reads the account the profile uses now, from its own entry,
// then every other account saved for the profile.
func claudeRows(src Sources, profile string) []Row {
	dir := launch.ProfileDir(src.Home, "claude", profile)
	active := Row{Profile: profile, Agent: "claude", Account: activeEmail(dir), Active: true}
	raw, err := profileCredentials(src, dir)
	if err == nil {
		active.Windows, err = claudeWindows(src, raw)
	}
	active.Err = err
	rows := []Row{active}
	if src.Saved == nil {
		return rows
	}
	saved, _ := src.Saved(profile)
	for _, s := range saved {
		if strings.EqualFold(s.Email, active.Account) {
			continue
		}
		r := Row{Profile: profile, Agent: "claude", Account: s.Email}
		r.Windows, r.Err = claudeWindows(src, s.Credentials)
		if r.Err != nil && strings.Contains(r.Err.Error(), "expired") {
			r.Err = errors.New("token expired: it renews when the account is active again (ai-profile account use)")
		}
		rows = append(rows, r)
	}
	return rows
}

func activeEmail(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, ".claude.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		OAuthAccount struct {
			Email string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	json.Unmarshal(b, &doc)
	return doc.OAuthAccount.Email
}

func claudeWindows(src Sources, raw string) ([]Window, error) {
	token, err := accessToken(raw)
	if err != nil {
		return nil, err
	}
	body, err := src.Fetch(claudeUsageURL, token)
	if err != nil {
		return nil, fmt.Errorf("reading usage failed: %w", err)
	}
	return parseClaudeUsage(body)
}

// profileCredentials reads the login of a profile folder: the Keychain on
// macOS, .credentials.json elsewhere.
func profileCredentials(src Sources, dir string) (string, error) {
	if src.Keychain != nil {
		raw, err := src.Keychain(launch.ClaudeKeychainService(dir))
		if err != nil {
			return "", fmt.Errorf("no readable login for %s", dir)
		}
		return raw, nil
	}
	b, err := os.ReadFile(filepath.Join(dir, ".credentials.json"))
	if err != nil {
		return "", fmt.Errorf("no readable login for %s", dir)
	}
	return string(b), nil
}

func claudeToken(src Sources, dir string) (string, error) {
	raw, err := profileCredentials(src, dir)
	if err != nil {
		return "", err
	}
	return accessToken(raw)
}

func accessToken(raw string) (string, error) {
	var cred struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal([]byte(raw), &cred) != nil || cred.OAuth.AccessToken == "" {
		return "", errors.New("login without an OAuth token")
	}
	if cred.OAuth.ExpiresAt > 0 && time.UnixMilli(cred.OAuth.ExpiresAt).Before(time.Now()) {
		return "", errors.New("token expired: it renews when you open Claude in the profile")
	}
	return cred.OAuth.AccessToken, nil
}

const claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"

// claudeWindow is one window of the usage response.
type claudeWindow struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    string   `json:"resets_at"`
}

// parseClaudeUsage reads five_hour and seven_day, ignoring every other key
// of the response, whatever its shape.
func parseClaudeUsage(body []byte) ([]Window, error) {
	var resp map[string]json.RawMessage
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unreadable usage response: %w", err)
	}
	if out := parseLimits(resp["limits"]); len(out) > 0 {
		return out, nil
	}
	var out []Window
	for _, w := range []struct{ key, name string }{{"five_hour", "5h"}, {"seven_day", "7d"}} {
		var v claudeWindow
		if raw, ok := resp[w.key]; !ok || json.Unmarshal(raw, &v) != nil || v.Utilization == nil {
			continue
		}
		reset, _ := time.Parse(time.RFC3339, v.ResetsAt)
		out = append(out, Window{Name: w.name, Used: *v.Utilization, Resets: reset})
	}
	if len(out) == 0 {
		return nil, errors.New("the response has no 5-hour and 7-day windows (ai-profile usage --raw <profile> shows it)")
	}
	return out, nil
}

// parseLimits reads the limits list: every window with its severity,
// including weekly limits of a single model.
func parseLimits(raw json.RawMessage) []Window {
	var limits []struct {
		Kind     string   `json:"kind"`
		Percent  *float64 `json:"percent"`
		Severity string   `json:"severity"`
		ResetsAt string   `json:"resets_at"`
		Scope    *struct {
			Model *struct {
				DisplayName string `json:"display_name"`
			} `json:"model"`
		} `json:"scope"`
	}
	if json.Unmarshal(raw, &limits) != nil {
		return nil
	}
	var out []Window
	for _, l := range limits {
		if l.Percent == nil {
			continue
		}
		name := l.Kind
		switch l.Kind {
		case "session":
			name = "5h"
		case "weekly_all":
			name = "7d"
		case "weekly_scoped":
			name = "7d"
			if l.Scope != nil && l.Scope.Model != nil && l.Scope.Model.DisplayName != "" {
				name += " " + l.Scope.Model.DisplayName
			}
		}
		reset, _ := time.Parse(time.RFC3339, l.ResetsAt)
		out = append(out, Window{Name: name, Used: *l.Percent, Resets: reset, Severity: l.Severity})
	}
	return out
}

// ClaudeRaw returns the usage response of a profile as it is, for checking
// its format; the token is never part of it.
func ClaudeRaw(home, profile string, src Sources) ([]byte, error) {
	src.Home = home
	src.defaults()
	token, err := claudeToken(src, launch.ProfileDir(home, "claude", profile))
	if err != nil {
		return nil, err
	}
	return src.Fetch(claudeUsageURL, token)
}

func keychain(service string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-w").Output()
	return strings.TrimSpace(string(out)), err
}

func fetch(url, token string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

// codex reads the rate limits of the newest token_count event in the
// profile's session files: no network, no credentials, as recent as the last
// session.
func codex(dir string) ([]Window, time.Time, error) {
	var files []string
	filepath.WalkDir(filepath.Join(dir, "sessions"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasPrefix(d.Name(), "rollout-") && strings.HasSuffix(d.Name(), ".jsonl") {
			files = append(files, p)
		}
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return modTime(files[i]).After(modTime(files[j])) })
	for _, f := range files {
		if w, at, ok := lastRateLimits(f); ok {
			return w, at, nil
		}
	}
	return nil, time.Time{}, errors.New("no session with limits: they show up after the first session")
}

func modTime(p string) time.Time {
	info, err := os.Stat(p)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

type codexLimit struct {
	UsedPercent     float64 `json:"used_percent"`
	WindowMinutes   int     `json:"window_minutes"`
	ResetsInSeconds *int64  `json:"resets_in_seconds"`
	ResetsAt        *int64  `json:"resets_at"`
}

func lastRateLimits(path string) ([]Window, time.Time, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, time.Time{}, false
	}
	defer f.Close()
	var found []Window
	var at time.Time
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"rate_limits"`) {
			continue
		}
		var ev struct {
			Timestamp string `json:"timestamp"`
			Payload   struct {
				Type       string                 `json:"type"`
				RateLimits map[string]*codexLimit `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Payload.Type != "token_count" || len(ev.Payload.RateLimits) == 0 {
			continue
		}
		t, _ := time.Parse(time.RFC3339, ev.Timestamp)
		var ws []Window
		for _, key := range []string{"primary", "secondary"} {
			l := ev.Payload.RateLimits[key]
			if l == nil {
				continue
			}
			w := Window{Name: windowName(l.WindowMinutes), Used: l.UsedPercent}
			switch {
			case l.ResetsAt != nil:
				w.Resets = time.Unix(*l.ResetsAt, 0)
			case l.ResetsInSeconds != nil && !t.IsZero():
				w.Resets = t.Add(time.Duration(*l.ResetsInSeconds) * time.Second)
			}
			ws = append(ws, w)
		}
		found, at = ws, t
	}
	return found, at, len(found) > 0
}

func windowName(minutes int) string {
	switch {
	case minutes == 300:
		return "5h"
	case minutes == 10080:
		return "7d"
	case minutes%1440 == 0 && minutes > 0:
		return fmt.Sprintf("%dd", minutes/1440)
	case minutes%60 == 0 && minutes > 0:
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%d min", minutes)
}

func openCode(src Sources, profile string) (string, error) {
	plan := launch.Adapters["opencode"].Env(src.Home, profile, os.Getenv)
	out, err := src.OpenCodeStats(launch.Environ(os.Environ(), plan))
	if err != nil {
		return "", fmt.Errorf("opencode stats failed: %w", err)
	}
	var st struct {
		Sessions int     `json:"sessions"`
		Cost     float64 `json:"cost"`
		Tokens   struct {
			Input  int64 `json:"input"`
			Output int64 `json:"output"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return "", fmt.Errorf("unreadable opencode stats response: %w", err)
	}
	return fmt.Sprintf("last 7 days: %d sessions, %d input tokens and %d output tokens, cost $%.2f", st.Sessions, st.Tokens.Input, st.Tokens.Output, st.Cost), nil
}

func openCodeStats(env []string) ([]byte, error) {
	cmd := exec.Command("opencode", "stats", "--days", "7", "--cost", "--json")
	cmd.Env = env
	return cmd.Output()
}
