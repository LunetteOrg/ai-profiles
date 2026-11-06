package launch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

const cfgBody = `
default = "personal"

[profiles.personal]

[profiles.client-d]
dirs = ["~/Developer/client-d"]
handoff_to = ["personal"]

[mcp.budget]
command = "uv"
env.BUDGET_API_KEY = "op://Client-b/BUDGET/credential"

[profiles.client-b]
mcp = ["budget"]
bitwarden = "client-b"
secrets.MISTRAL_API_KEY = "bw://Client-b Mistral"
secrets.KIMI_API_KEY = "op://Client-b/Kimi/credential"
dirs = ["~/Developer/client-b"]
agents = ["claude", "codex", "opencode", "pi"]
`

type fixture struct {
	home string
	cfg  *config.Config
	env  map[string]string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	for _, d := range []string{"Developer/client-d/playbook", "Developer/client-b", "Developer/ilmeskio"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(cfgBody), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p, home)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{home: home, cfg: cfg, env: map[string]string{}}
}

func (f *fixture) plan(cwd string, req Request) (*Plan, error) {
	return Prepare(f.cfg, f.home, filepath.Join(f.home, cwd), func(k string) string { return f.env[k] }, req)
}

func TestPrepareUsesFolderProfile(t *testing.T) {
	f := setup(t)
	p, err := f.plan("Developer/client-d/playbook", Request{Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Profile.Name != "client-d" {
		t.Errorf("profile = %s, want client-d", p.Profile.Name)
	}
	if got, want := p.Env["CLAUDE_CONFIG_DIR"], filepath.Join(f.home, ".claude-client-d"); got != want {
		t.Errorf("CLAUDE_CONFIG_DIR = %q, want %q", got, want)
	}
}

func TestPrepareDefaultOutsideClientFolders(t *testing.T) {
	f := setup(t)
	p, err := f.plan("Developer/ilmeskio", Request{Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Profile.Name != "personal" || p.Match.Dir != "" {
		t.Errorf("got %s (dir %q), want default personal", p.Profile.Name, p.Match.Dir)
	}
}

func TestPrepareExplicitProfileFollowsHandoff(t *testing.T) {
	f := setup(t)
	if _, err := f.plan("Developer/client-d", Request{Agent: "claude", Profile: "personal"}); err != nil {
		t.Errorf("client-d → personal is in handoff_to, got %v", err)
	}
	_, err := f.plan("Developer/client-d", Request{Agent: "claude", Profile: "client-b"})
	if err == nil || !strings.Contains(err.Error(), "handoff_to") {
		t.Errorf("client-d → client-b should be refused, got %v", err)
	}
	if _, err := f.plan("Developer/ilmeskio", Request{Agent: "claude", Profile: "client-b"}); err != nil {
		t.Errorf("outside client folders any profile is allowed, got %v", err)
	}
	if _, err := f.plan("Developer/ilmeskio", Request{Agent: "claude", Profile: "nessuno"}); err == nil {
		t.Errorf("unknown profile should be refused")
	}
}

func TestPrepareRefusesAgentNotInProfile(t *testing.T) {
	f := setup(t)
	_, err := f.plan("Developer/client-d", Request{Agent: "codex"})
	if err == nil || !strings.Contains(err.Error(), "client-d") {
		t.Errorf("codex is not in client-d's agents, got %v", err)
	}
}

func TestPrepareAgentWithoutAdapter(t *testing.T) {
	f := setup(t)
	_, err := f.plan("Developer/client-b", Request{Agent: "pi"})
	if err == nil || !strings.Contains(err.Error(), "is not available yet") {
		t.Errorf("pi has no adapter yet, got %v", err)
	}
}

func TestPrepareRespectsExplicitEnv(t *testing.T) {
	f := setup(t)
	f.env["CLAUDE_CONFIG_DIR"] = "/altrove/.claude-x"
	p, err := f.plan("Developer/client-d", Request{Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, set := p.Env["CLAUDE_CONFIG_DIR"]; set {
		t.Errorf("CLAUDE_CONFIG_DIR already set: it must not be overridden")
	}
	if len(p.Notes) == 0 {
		t.Errorf("a differing CLAUDE_CONFIG_DIR should produce a note")
	}

	f.env["CLAUDE_CONFIG_DIR"] = filepath.Join(f.home, ".claude-client-d")
	p, _ = f.plan("Developer/client-d", Request{Agent: "claude"})
	if len(p.Notes) != 0 {
		t.Errorf("a matching CLAUDE_CONFIG_DIR needs no note, got %v", p.Notes)
	}

	f.env["CLAUDE_CONFIG_DIR"] = "/altrove/.claude-x"
	p, _ = f.plan("Developer/ilmeskio", Request{Agent: "claude", Profile: "client-b"})
	if got := p.Env["CLAUDE_CONFIG_DIR"]; got != filepath.Join(f.home, ".claude-client-b") {
		t.Errorf("an explicit profile wins over the environment, got %q", got)
	}
}

func TestPreparePlanShowsInheritedProfile(t *testing.T) {
	f := setup(t)
	f.env["CLAUDE_CONFIG_DIR"] = filepath.Join(f.home, ".claude-personal")
	p, err := f.plan("Developer/client-d", Request{Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Profile == nil || p.Profile.Name != "personal" {
		t.Errorf("the inherited CLAUDE_CONFIG_DIR is personal's, got %v", p.Profile)
	}
	if p.Match.Profile.Name != "client-d" {
		t.Errorf("the folder's profile is still client-d, got %s", p.Match.Profile.Name)
	}
	if p.Inherited != f.env["CLAUDE_CONFIG_DIR"] {
		t.Errorf("Inherited = %q, want %q", p.Inherited, f.env["CLAUDE_CONFIG_DIR"])
	}

	f.env["CLAUDE_CONFIG_DIR"] = "/altrove/.claude-x"
	p, _ = f.plan("Developer/client-d", Request{Agent: "claude"})
	if p.Profile != nil {
		t.Errorf("a folder that is no profile's has no profile, got %s", p.Profile.Name)
	}
}

func TestFindAgentSkipsSelf(t *testing.T) {
	dir := t.TempDir()
	self := filepath.Join(dir, "bin", "ai-profile")
	shims := filepath.Join(dir, "shims")
	real := filepath.Join(dir, "real")
	for _, d := range []string{filepath.Dir(self), shims, real} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(self, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(shims, "claude")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := strings.Join([]string{shims, filepath.Join(dir, "vuota"), real}, string(os.PathListSeparator))
	got, err := FindAgent("claude", path, self)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(real, "claude") {
		t.Errorf("got %s, want the real claude", got)
	}
	if _, err := FindAgent("claude", shims, self); err == nil {
		t.Errorf("only the shim in PATH: expected an error")
	}
}

func TestPrepareCodex(t *testing.T) {
	f := setup(t)
	p, err := f.plan("Developer/client-b", Request{Agent: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := p.Env["CODEX_HOME"], filepath.Join(f.home, ".codex-client-b"); got != want {
		t.Errorf("CODEX_HOME = %q, want %q", got, want)
	}
}

func TestPrepareOpenCode(t *testing.T) {
	f := setup(t)
	p, err := f.plan("Developer/client-b", Request{Agent: "opencode"})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(f.home, ".opencode-client-b")
	want := map[string]string{
		"OPENCODE_CONFIG_DIR": filepath.Join(base, "config"),
		"XDG_DATA_HOME":       filepath.Join(base, "data"),
		"XDG_STATE_HOME":      filepath.Join(base, "state"),
		// Tools started by OpenCode keep their own data where it was.
		"MISE_DATA_DIR":         filepath.Join(f.home, ".local", "share", "mise"),
		"MISE_STATE_DIR":        filepath.Join(f.home, ".local", "state", "mise"),
		"UV_PYTHON_INSTALL_DIR": filepath.Join(f.home, ".local", "share", "uv", "python"),
		"UV_TOOL_DIR":           filepath.Join(f.home, ".local", "share", "uv", "tools"),
	}
	for k, v := range want {
		if p.Env[k] != v {
			t.Errorf("%s = %q, want %q", k, p.Env[k], v)
		}
	}

	f.env["XDG_DATA_HOME"] = "/dati"
	f.env["MISE_DATA_DIR"] = "/mise"
	p, _ = f.plan("Developer/client-b", Request{Agent: "opencode"})
	if _, set := p.Env["MISE_DATA_DIR"]; set {
		t.Errorf("a MISE_DATA_DIR already set must be left alone")
	}
	if got := p.Env["UV_TOOL_DIR"]; got != "/dati/uv/tools" {
		t.Errorf("UV_TOOL_DIR follows the original XDG_DATA_HOME, got %q", got)
	}

	f.env["OPENCODE_CONFIG_DIR"] = filepath.Join(f.home, ".opencode-personal", "config")
	p, _ = f.plan("Developer/client-b", Request{Agent: "opencode"})
	if p.Profile == nil || p.Profile.Name != "personal" {
		t.Errorf("the inherited OPENCODE_CONFIG_DIR is personal's, got %v", p.Profile)
	}

	f.env["OPENCODE_CONFIG_DIR"] = "/altrove/config"
	p, _ = f.plan("Developer/client-b", Request{Agent: "opencode"})
	if len(p.Env) != 0 {
		t.Errorf("an inherited OPENCODE_CONFIG_DIR leaves every variable alone, got %v", p.Env)
	}
}

func TestPrepareListsSecrets(t *testing.T) {
	f := setup(t)
	p, err := f.plan("Developer/client-b", Request{Agent: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Secrets) != 2 || p.Secrets["MISTRAL_API_KEY"] != "bw://Client-b Mistral" || p.Secrets["KIMI_API_KEY"] != "op://Client-b/Kimi/credential" {
		t.Errorf("only provider keys, got %v", p.Secrets)
	}
	if _, set := p.Secrets["BUDGET_API_KEY"]; set {
		t.Errorf("MCP secrets are resolved by the server, not at launch")
	}
	p, _ = f.plan("Developer/client-d", Request{Agent: "claude"})
	if len(p.Secrets) != 0 {
		t.Errorf("client-d has no secrets, got %v", p.Secrets)
	}
}

type fakeSource struct {
	reason  string
	refs    map[string]string
	account string
}

func (f *fakeSource) Get(reason string, refs map[string]string, account string) (map[string]string, error) {
	f.reason, f.refs, f.account = reason, refs, account
	out := map[string]string{}
	for k, v := range refs {
		out[k] = "valore:" + v
	}
	return out, nil
}

func TestResolveSecrets(t *testing.T) {
	f := setup(t)
	f.env["PRESENTE"] = "dall-ambiente"
	f.cfg.Profiles["client-b"].Secrets["DA_ENV"] = "env:PRESENTE"
	p, _ := f.plan("Developer/client-b", Request{Agent: "claude"})
	src := &fakeSource{}
	r := &secrets.Resolver{Getenv: func(k string) string { return f.env[k] }}
	if err := p.ResolveSecrets(src, r, f.cfg, f.home); err != nil {
		t.Fatal(err)
	}
	if p.Env["MISTRAL_API_KEY"] != "valore:bw://Client-b Mistral" || p.Env["KIMI_API_KEY"] != "valore:op://Client-b/Kimi/credential" {
		t.Errorf("op:// and bw:// come from ai-secrets: %v", p.Env)
	}
	if p.Env["DA_ENV"] != "dall-ambiente" {
		t.Errorf("env: is read directly: %v", p.Env)
	}
	if src.account != "client-b" || !strings.Contains(src.reason, "client-b") {
		t.Errorf("the request carries the account and a reason: %+v", src)
	}
	if _, asked := src.refs["DA_ENV"]; asked {
		t.Errorf("env: references are not sent to the agent")
	}
	if p.Env["CLAUDE_CONFIG_DIR"] == "" {
		t.Errorf("the profile's variables stay")
	}
}

func TestPrepareFolderWithSeveralProfiles(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, "Developer", "shared"), 0o755)
	p := filepath.Join(t.TempDir(), "config.toml")
	os.WriteFile(p, []byte(cfgBody+`
[[folders]]
dir = "~/Developer/shared"
profiles = ["personal", "client-d"]
`), 0o600)
	cfg, err := config.Load(p, home)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{home: home, cfg: cfg, env: map[string]string{}}
	plan, err := f.plan("Developer/shared", Request{Agent: "claude"})
	if err != nil || plan.Profile.Name != "personal" {
		t.Fatalf("first listed: %v, %v", plan, err)
	}
	if why := Why(plan.Match, cfg); !strings.Contains(why, "first") || !strings.Contains(why, "personal, client-d") {
		t.Errorf("why = %q", why)
	}
	if _, err := f.plan("Developer/shared", Request{Agent: "claude", Profile: "client-d"}); err != nil {
		t.Errorf("client-d is listed: %v", err)
	}
	_, err = f.plan("Developer/shared", Request{Agent: "claude", Profile: "client-b"})
	if err == nil || !strings.Contains(err.Error(), "personal, client-d") {
		t.Errorf("client-b is not listed: %v", err)
	}
	cfg.LastUsed = func(string) string { return "client-d" }
	plan, err = f.plan("Developer/shared", Request{Agent: "claude"})
	if err != nil || plan.Profile.Name != "client-d" || !strings.Contains(Why(plan.Match, cfg), "last") {
		t.Errorf("last used: %v, %v", plan, err)
	}
}
