package usage

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/launch"
)

func load(t *testing.T, home, body string) *config.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(body), 0o600)
	cfg, err := config.Load(p, home)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestKeychainService(t *testing.T) {
	// Same derivation as Claude Code: sha256 of the config folder, 8 hex chars.
	if got := launch.ClaudeKeychainService("/Users/io/.claude-personal"); got != "Claude Code-credentials-6094754a" {
		t.Errorf("got %q", got)
	}
}

func TestClaudeUsage(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude-personal"), 0o700)
	cfg := load(t, home, "default = \"personal\"\n[profiles.personal]\n")
	future := time.Now().Add(time.Hour).UnixMilli()
	var askedToken string
	src := Sources{
		Home: home,
		Keychain: func(service string) (string, error) {
			if service != launch.ClaudeKeychainService(filepath.Join(home, ".claude-personal")) {
				return "", errors.New("voce sbagliata")
			}
			return `{"claudeAiOauth":{"accessToken":"tok","expiresAt":` + strconv.FormatInt(future, 10) + `}}`, nil
		},
		Fetch: func(url, token string) ([]byte, error) {
			askedToken = token
			return []byte(`{"five_hour":{"utilization":12.5,"resets_at":"2026-10-21T15:00:00Z"},"seven_day":{"utilization":40,"resets_at":"2026-10-27T10:00:00Z"}}`), nil
		},
	}
	rows := Collect(cfg, src)
	if len(rows) != 1 || rows[0].Err != nil {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	if askedToken != "tok" || len(r.Windows) != 2 || r.Windows[0].Name != "5h" || r.Windows[0].Used != 12.5 || r.Windows[1].Used != 40 {
		t.Errorf("row = %+v", r)
	}
	if want, _ := time.Parse(time.RFC3339, "2026-10-21T15:00:00Z"); !r.Windows[0].Resets.Equal(want) {
		t.Errorf("reset = %v", r.Windows[0].Resets)
	}

	src.Keychain = func(string) (string, error) {
		return `{"claudeAiOauth":{"accessToken":"tok","expiresAt":1000}}`, nil
	}
	if r := Collect(cfg, src)[0]; r.Err == nil || !strings.Contains(r.Err.Error(), "expired") {
		t.Errorf("an expired token should say so, got %v", r.Err)
	}
}

func TestCodexUsageFromSessions(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".codex-personal", "sessions", "2026", "10", "21")
	os.MkdirAll(dir, 0o700)
	lines := `{"timestamp":"2026-10-21T10:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":3,"window_minutes":300,"resets_in_seconds":60}}}}
{"timestamp":"2026-10-21T11:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":20,"window_minutes":300,"resets_in_seconds":3600},"secondary":{"used_percent":7,"window_minutes":10080,"resets_at":1793000000}}}}
{"timestamp":"2026-10-21T11:05:00.000Z","type":"response_item","payload":{}}
`
	os.WriteFile(filepath.Join(dir, "rollout-2026-10-21T10-00-00-x.jsonl"), []byte(lines), 0o600)
	cfg := load(t, home, "default = \"personal\"\n[profiles.personal]\nagents = [\"codex\"]\n")
	rows := Collect(cfg, Sources{Home: home})
	if len(rows) != 1 || rows[0].Err != nil {
		t.Fatalf("rows = %+v", rows)
	}
	r := rows[0]
	if len(r.Windows) != 2 || r.Windows[0].Used != 20 || r.Windows[0].Name != "5h" || r.Windows[1].Name != "7d" || r.Windows[1].Used != 7 {
		t.Errorf("windows = %+v", r.Windows)
	}
	at, _ := time.Parse(time.RFC3339, "2026-10-21T11:00:00Z")
	if !r.Windows[0].Resets.Equal(at.Add(time.Hour)) || r.Windows[1].Resets.Unix() != 1793000000 {
		t.Errorf("resets = %v, %v", r.Windows[0].Resets, r.Windows[1].Resets)
	}
	if !r.AsOf.Equal(at) {
		t.Errorf("AsOf = %v, want the event's time", r.AsOf)
	}

	empty := load(t, t.TempDir(), "default = \"p\"\n[profiles.p]\nagents = [\"codex\"]\n")
	if r := Collect(empty, Sources{Home: t.TempDir()})[0]; r.Err == nil {
		t.Errorf("no sessions should be reported")
	}
}

func TestOpenCodeUsage(t *testing.T) {
	home := t.TempDir()
	cfg := load(t, home, "default = \"p\"\n[profiles.p]\nagents = [\"opencode\"]\n")
	var gotEnv []string
	src := Sources{Home: home, OpenCodeStats: func(env []string) ([]byte, error) {
		gotEnv = env
		return []byte(`{"sessions":4,"tokens":{"input":1000,"output":500,"reasoning":0,"cache":{"read":0,"write":0}},"cost":1.25}`), nil
	}}
	r := Collect(cfg, src)[0]
	if r.Err != nil || !strings.Contains(r.Detail, "1.25") || !strings.Contains(r.Detail, "4 sessions") {
		t.Errorf("row = %+v", r)
	}
	if !strings.Contains(strings.Join(gotEnv, " "), "OPENCODE_CONFIG_DIR="+filepath.Join(home, ".opencode-p", "config")) {
		t.Errorf("stats must run with the profile's environment: %v", gotEnv)
	}
}

func TestParseClaudeUsageIgnoresOtherKeys(t *testing.T) {
	body := `{"five_hour":{"utilization":5,"resets_at":"2026-10-21T15:00:00Z"},"seven_day":null,"limits":[{"x":1}],"extra_usage":{"is_enabled":false}}`
	w, err := parseClaudeUsage([]byte(body))
	if err != nil || len(w) != 1 || w[0].Used != 5 {
		t.Errorf("got %v, %v", w, err)
	}
	if _, err := parseClaudeUsage([]byte(`{"limits":[]}`)); err == nil || !strings.Contains(err.Error(), "--raw") {
		t.Errorf("without windows the error should point to --raw, got %v", err)
	}
}

const realResponse = `{"five_hour":{"utilization":72.0,"resets_at":"2026-10-21T15:30:00.287846+00:00"},"seven_day":{"utilization":92.0,"resets_at":"2026-10-22T00:00:00.287867+00:00"},"iguana_necktie":{"utilization":10.17},"extra_usage":{"is_enabled":false},"limits":[{"kind":"session","group":"session","percent":72,"severity":"normal","resets_at":"2026-10-21T15:30:00.287846+00:00","scope":null,"is_active":false},{"kind":"weekly_all","group":"weekly","percent":92,"severity":"critical","resets_at":"2026-10-22T00:00:00.287867+00:00","scope":null,"is_active":true},{"kind":"weekly_scoped","group":"weekly","percent":0,"severity":"normal","resets_at":"2026-10-22T00:00:00+00:00","scope":{"model":{"id":null,"display_name":"Fable"},"surface":null},"is_active":false}]}`

func TestParseClaudeLimits(t *testing.T) {
	w, err := parseClaudeUsage([]byte(realResponse))
	if err != nil {
		t.Fatal(err)
	}
	if len(w) != 3 {
		t.Fatalf("windows = %+v", w)
	}
	want := []struct {
		name     string
		used     float64
		severity string
	}{{"5h", 72, "normal"}, {"7d", 92, "critical"}, {"7d Fable", 0, "normal"}}
	for i, x := range want {
		if w[i].Name != x.name || w[i].Used != x.used || w[i].Severity != x.severity {
			t.Errorf("window %d = %+v, want %+v", i, w[i], x)
		}
	}
	if w[1].Resets.IsZero() {
		t.Errorf("resets parsed")
	}
}

func TestCollectSavedAccounts(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude-personal")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, ".claude.json"), []byte(`{"oauthAccount":{"emailAddress":"io@client-d.example"}}`), 0o600)
	cfg := load(t, home, "default = \"personal\"\n[profiles.personal]\n")
	future := strconv.FormatInt(time.Now().Add(time.Hour).UnixMilli(), 10)
	cred := func(tok string) string {
		return `{"claudeAiOauth":{"accessToken":"` + tok + `","expiresAt":` + future + `}}`
	}
	src := Sources{
		Home:     home,
		Keychain: func(string) (string, error) { return cred("tok-client-d"), nil },
		Fetch: func(url, token string) ([]byte, error) {
			pct := map[string]string{"tok-client-d": "7", "tok-personal": "89"}[token]
			return []byte(`{"five_hour":{"utilization":` + pct + `,"resets_at":"2026-10-22T16:20:00Z"},"seven_day":{"utilization":20,"resets_at":"2026-10-28T06:00:00Z"}}`), nil
		},
		Saved: func(profile string) ([]Saved, error) {
			return []Saved{{Email: "io@example.com", Credentials: cred("tok-personal")}, {Email: "io@client-d.example", Credentials: cred("vecchio")}}, nil
		},
	}
	rows := Collect(cfg, src)
	if len(rows) != 2 {
		t.Fatalf("one row per account, got %+v", rows)
	}
	if rows[0].Account != "io@client-d.example" || !rows[0].Active || rows[0].Windows[0].Used != 7 {
		t.Errorf("first the active account, read from the profile's entry: %+v", rows[0])
	}
	if rows[1].Account != "io@example.com" || rows[1].Active || rows[1].Windows[0].Used != 89 {
		t.Errorf("then the saved one: %+v", rows[1])
	}
}
