package apply

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"os/exec"

	"github.com/BurntSushi/toml"

	"github.com/LunetteOrg/ai-profiles/internal/config"
)

const cfgBody = `
default = "personal"

[mcp.google]
command = "~/.local/bin/google-workspace-mcp"

[mcp.notion]
url = "https://mcp.notion.com/mcp"

[mcp.esempio]
command = "uvx"
args = ["esempio-mcp"]
env.TOKEN_DIR = "~/.esempio/{profilo}"
env.ESEMPIO_KEY = "op://Client-d/Esempio/credential"
dirs = [{ path = "~/.esempio/{profilo}" }]

[profiles.personal]
mcp = ["google"]

[profiles.client-d]
dirs = ["~/Developer/client-d"]
agents = ["claude", "codex", "opencode"]
mcp = ["google", "notion", "esempio"]

[[profiles.client-d.repos]]
dir = "~/Developer/client-d/playbook"
mcp = ["notion"]
plugins = ["posthog"]
`

type fixture struct {
	home string
	cfg  *config.Config
}

func setup(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	for _, d := range []string{".claude-personal", ".claude-client-d", ".agents", "Developer/client-d/playbook", ".claude/plugins"} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(home, ".claude-personal", ".claude.json"), `{
  "oauthAccount": {"emailAddress": "a@b.c"},
  "numStartups": 12,
  "mcpServers": {
    "budget": {"type": "stdio", "command": "uv", "args": ["tool", "run", "mcp-budget"], "env": {"BUDGET_API_KEY": "segreto"}},
    "google": {"type": "stdio", "command": "/vecchio/google", "args": [], "env": {}}
  }
}`)
	write(t, filepath.Join(home, ".claude", "plugins", "installed_plugins.json"),
		`{"plugins": {"posthog@claude-plugins-official": [], "cloudflare@claude-plugins-official": []}}`)
	p := filepath.Join(t.TempDir(), "config.toml")
	write(t, p, cfgBody)
	cfg, err := config.Load(p, home)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{home: home, cfg: cfg}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readJSONT(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (f *fixture) sync(t *testing.T, dry bool) *Report {
	t.Helper()
	rep, err := Sync(Options{Home: f.home, Cfg: f.cfg, DryRun: dry})
	if err != nil {
		t.Fatal(err)
	}
	return rep
}

func TestSyncClaude(t *testing.T) {
	f := setup(t)
	rep := f.sync(t, false)

	m := readJSONT(t, filepath.Join(f.home, ".claude-personal", ".claude.json"))
	servers := m["mcpServers"].(map[string]any)
	google := servers["google"].(map[string]any)
	if got, want := google["command"], filepath.Join(f.home, ".local", "bin", "google-workspace-mcp"); got != want {
		t.Errorf("google command = %v, want %v", got, want)
	}
	if servers["budget"].(map[string]any)["env"].(map[string]any)["BUDGET_API_KEY"] != "segreto" {
		t.Errorf("an undeclared server must be left as it was")
	}
	if m["numStartups"] != float64(12) || m["oauthAccount"] == nil {
		t.Errorf("other keys must be kept, got %v", m)
	}
	if !hasNote(rep, "budget") {
		t.Errorf("an undeclared server should be reported, notes: %v", rep.Notes)
	}

	tm := readJSONT(t, filepath.Join(f.home, ".claude-client-d", ".claude.json"))
	notion := tm["mcpServers"].(map[string]any)["notion"].(map[string]any)
	if notion["type"] != "http" || notion["url"] != "https://mcp.notion.com/mcp" {
		t.Errorf("notion = %v", notion)
	}
	repo := filepath.Join(f.home, "Developer", "client-d", "playbook")
	local := tm["projects"].(map[string]any)[repo].(map[string]any)["mcpServers"].(map[string]any)
	if local["notion"] == nil {
		t.Errorf("the repo's server goes in local scope, got %v", local)
	}
	plugins := readJSONT(t, filepath.Join(repo, ".claude", "settings.local.json"))
	if plugins["enabledPlugins"].(map[string]any)["posthog@claude-plugins-official"] != true {
		t.Errorf("posthog should be enabled in the repo, got %v", plugins)
	}
	if info, err := os.Stat(filepath.Join(f.home, ".esempio", "client-d")); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("credential folder should exist with mode 700: %v", err)
	}

	if again := f.sync(t, false); len(again.Actions) != 0 {
		t.Errorf("a second sync should change nothing, got %v", again.Actions)
	}
}

func TestSyncDryRunWritesNothing(t *testing.T) {
	f := setup(t)
	before, _ := os.ReadFile(filepath.Join(f.home, ".claude-personal", ".claude.json"))
	rep := f.sync(t, true)
	if len(rep.Actions) == 0 {
		t.Fatalf("dry run should list the pending changes")
	}
	after, _ := os.ReadFile(filepath.Join(f.home, ".claude-personal", ".claude.json"))
	if string(before) != string(after) {
		t.Errorf("dry run changed .claude.json")
	}
	if _, err := os.Stat(filepath.Join(f.home, ".codex-client-d")); err == nil {
		t.Errorf("dry run created the Codex profile")
	}
}

func TestSyncCodex(t *testing.T) {
	f := setup(t)
	f.sync(t, false)
	dir := filepath.Join(f.home, ".codex-client-d")
	if target, _ := os.Readlink(filepath.Join(dir, "AGENTS.md")); target != filepath.Join(f.home, ".agents", "AGENTS.md") {
		t.Errorf("AGENTS.md → %q", target)
	}
	var m map[string]any
	if _, err := toml.DecodeFile(filepath.Join(dir, "config.toml"), &m); err != nil {
		t.Fatal(err)
	}
	if m["mcp_oauth_credentials_store"] != "file" {
		t.Errorf("mcp_oauth_credentials_store = %v", m["mcp_oauth_credentials_store"])
	}
	servers := m["mcp_servers"].(map[string]any)
	if servers["notion"].(map[string]any)["url"] != "https://mcp.notion.com/mcp" {
		t.Errorf("notion = %v", servers["notion"])
	}
	esempio := servers["esempio"].(map[string]any)
	if esempio["env"].(map[string]any)["TOKEN_DIR"] != filepath.Join(f.home, ".esempio", "client-d") {
		t.Errorf("esempio = %v", esempio)
	}
	if _, err := os.Stat(filepath.Join(f.home, "Developer", "client-d", agentsRoot)); err != nil {
		t.Errorf("the client folder should hold %s: %v", agentsRoot, err)
	}

	// Settings of the user survive a sync.
	cfg := filepath.Join(dir, "config.toml")
	data, _ := os.ReadFile(cfg)
	write(t, cfg, "model = \"gpt-5\"\n"+string(data))
	if rep := f.sync(t, false); len(rep.Actions) != 0 {
		t.Errorf("nothing to change, got %v", rep.Actions)
	}
	m = map[string]any{}
	toml.DecodeFile(cfg, &m)
	if m["model"] != "gpt-5" {
		t.Errorf("model lost: %v", m)
	}
}

func TestSyncOpenCode(t *testing.T) {
	f := setup(t)
	f.sync(t, false)
	conf := filepath.Join(f.home, ".opencode-client-d", "config")
	m := readJSONT(t, filepath.Join(conf, "opencode.json"))
	mcp := m["mcp"].(map[string]any)["servers"].(map[string]any)
	google := mcp["google"].(map[string]any)
	cmd := google["command"].([]any)
	if google["type"] != "local" || cmd[0] != filepath.Join(f.home, ".local", "bin", "google-workspace-mcp") {
		t.Errorf("google = %v", google)
	}
	if mcp["notion"].(map[string]any)["type"] != "remote" {
		t.Errorf("notion = %v", mcp["notion"])
	}
	port := readJSONT(t, filepath.Join(conf, "service.json"))["port"].(float64)
	if port < portBase || port >= portBase+portRange {
		t.Errorf("port %v outside the range", port)
	}
	for _, d := range []string{"data", "state"} {
		if _, err := os.Stat(filepath.Join(f.home, ".opencode-client-d", d)); err != nil {
			t.Errorf("%s missing: %v", d, err)
		}
	}

	write(t, filepath.Join(conf, "service.json"), `{"port": 50123}`)
	f.sync(t, false)
	if got := readJSONT(t, filepath.Join(conf, "service.json"))["port"]; got != float64(50123) {
		t.Errorf("a port already set must be kept, got %v", got)
	}
}

func TestSyncUnknownPlugin(t *testing.T) {
	f := setup(t)
	f.cfg.Profiles["client-d"].Repos[0].Plugins = []string{"nessuno"}
	if _, err := Sync(Options{Home: f.home, Cfg: f.cfg}); err == nil || !strings.Contains(err.Error(), "nessuno") {
		t.Errorf("an unknown plugin should be an error, got %v", err)
	}
}

func TestResolvePlugin(t *testing.T) {
	installed := []string{"a@x", "b@x", "b@y"}
	if id, err := resolvePlugin("a", installed); err != nil || id != "a@x" {
		t.Errorf("a → %q, %v", id, err)
	}
	if _, err := resolvePlugin("b", installed); err == nil {
		t.Errorf("b is in two marketplaces: expected an error")
	}
	if id, err := resolvePlugin("b@y", installed); err != nil || id != "b@y" {
		t.Errorf("b@y → %q, %v", id, err)
	}
}

func hasNote(r *Report, s string) bool {
	for _, n := range r.Notes {
		if strings.Contains(n, s) {
			return true
		}
	}
	return false
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// The user's global gitignore must not decide what the test sees.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // default core.excludesFile
	cmd := exec.Command("git", "-C", dir, "init", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

func TestSyncRepoServersForEveryAgent(t *testing.T) {
	f := setup(t)
	repo := filepath.Join(f.home, "Developer", "client-d", "playbook")
	gitInit(t, repo)
	f.sync(t, false)

	var codex map[string]any
	if _, err := toml.DecodeFile(filepath.Join(repo, ".codex", "config.toml"), &codex); err != nil {
		t.Fatal(err)
	}
	if codex["mcp_servers"].(map[string]any)["notion"] == nil {
		t.Errorf("Codex repo config = %v", codex)
	}
	var prof map[string]any
	toml.DecodeFile(filepath.Join(f.home, ".codex-client-d", "config.toml"), &prof)
	real, _ := filepath.EvalSymlinks(repo)
	trust, _ := prof["projects"].(map[string]any)[real].(map[string]any)
	if trust["trust_level"] != "trusted" {
		t.Errorf("Codex reads a repo's config only when the project is trusted, got %v", prof["projects"])
	}

	oc := readJSONT(t, filepath.Join(repo, "opencode.json"))
	if oc["mcp"].(map[string]any)["servers"].(map[string]any)["notion"] == nil {
		t.Errorf("OpenCode repo config = %v", oc)
	}

	exclude, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	for _, want := range []string{"/.codex/config.toml", "/opencode.json", "/.claude/settings.local.json"} {
		if !strings.Contains(string(exclude), want+"\n") {
			t.Errorf("%s should be in .git/info/exclude:\n%s", want, exclude)
		}
	}
	if rep := f.sync(t, false); len(rep.Actions) != 0 {
		t.Errorf("a second sync should change nothing, got %v", rep.Actions)
	}
}

func TestSyncLeavesTrackedRepoFilesAlone(t *testing.T) {
	f := setup(t)
	repo := filepath.Join(f.home, "Developer", "client-d", "playbook")
	gitInit(t, repo)
	write(t, filepath.Join(repo, "opencode.json"), `{"del": "team"}`)
	cmd := exec.Command("git", "-C", repo, "add", "opencode.json")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	rep := f.sync(t, false)
	if b, _ := os.ReadFile(filepath.Join(repo, "opencode.json")); string(b) != `{"del": "team"}` {
		t.Errorf("a tracked opencode.json must not be touched, got %s", b)
	}
	if !hasNote(rep, "opencode.json") {
		t.Errorf("a tracked file left alone should be reported: %v", rep.Notes)
	}
}

func TestSyncDisabledPlugins(t *testing.T) {
	f := setup(t)
	r := &f.cfg.Profiles["client-d"].Repos[0]
	r.DisabledPlugins = []string{"cloudflare"}
	f.sync(t, false)
	m := readJSONT(t, filepath.Join(r.Dir, ".claude", "settings.local.json"))
	if m["enabledPlugins"].(map[string]any)["cloudflare@claude-plugins-official"] != false {
		t.Errorf("cloudflare should be disabled in the repo, got %v", m)
	}
}

func TestSyncWritesSecretsAsReferences(t *testing.T) {
	f := setup(t)
	f.cfg.MCP["esempio"].Env["DA_AMBIENTE"] = "env:VARIABILE"
	f.sync(t, false)

	isOp := func(cmd any) bool { c, _ := cmd.(string); return filepath.Base(c) == "ai-secrets" }
	wantArgs := []any{"run", "--reason", "server esempio, profile client-d", "--", "uvx", "esempio-mcp"}
	sameArgs := func(got any) bool {
		a, _ := got.([]any)
		if len(a) != len(wantArgs) {
			return false
		}
		for i := range a {
			if a[i] != wantArgs[i] {
				return false
			}
		}
		return true
	}

	tm := readJSONT(t, filepath.Join(f.home, ".claude-client-d", ".claude.json"))
	es := tm["mcpServers"].(map[string]any)["esempio"].(map[string]any)
	env := es["env"].(map[string]any)
	if !isOp(es["command"]) || !sameArgs(es["args"]) {
		t.Errorf("Claude: the server starts through ai-secrets, got %v %v", es["command"], es["args"])
	}
	if env["ESEMPIO_KEY"] != "op://Client-d/Esempio/credential" || env["DA_AMBIENTE"] != "${VARIABILE}" {
		t.Errorf("Claude env = %v", env)
	}

	var codex map[string]any
	toml.DecodeFile(filepath.Join(f.home, ".codex-client-d", "config.toml"), &codex)
	ce := codex["mcp_servers"].(map[string]any)["esempio"].(map[string]any)
	if !isOp(ce["command"]) || !sameArgs(ce["args"]) {
		t.Errorf("Codex: ai-secrets, got %v", ce)
	}
	if ce["env"].(map[string]any)["ESEMPIO_KEY"] != "op://Client-d/Esempio/credential" {
		t.Errorf("Codex passes the reference in env for op run: %v", ce)
	}
	if ce["startup_timeout_sec"] != int64(60) {
		t.Errorf("Codex waits for the 1Password approval: %v", ce["startup_timeout_sec"])
	}
	if vars, _ := ce["env_vars"].([]any); len(vars) != 1 || vars[0] != "VARIABILE" {
		t.Errorf("Codex passes env: variables through env_vars: %v", ce)
	}

	oc := readJSONT(t, filepath.Join(f.home, ".opencode-client-d", "config", "opencode.json"))
	oe := oc["mcp"].(map[string]any)["servers"].(map[string]any)["esempio"].(map[string]any)
	cmd := oe["command"].([]any)
	if !isOp(cmd[0]) || cmd[1] != "run" || cmd[5] != "uvx" {
		t.Errorf("OpenCode: ai-secrets, got %v", cmd)
	}
	oenv := oe["environment"].(map[string]any)
	if oenv["ESEMPIO_KEY"] != "op://Client-d/Esempio/credential" || oenv["DA_AMBIENTE"] != "{env:VARIABILE}" {
		t.Errorf("OpenCode environment = %v", oenv)
	}
}

func TestCreateClaudeProfile(t *testing.T) {
	f := setup(t)
	write(t, filepath.Join(f.home, ".agents", "AGENTS.md"), "# comuni\n")
	write(t, filepath.Join(f.home, ".claude", "settings.json"), "{}")
	os.MkdirAll(filepath.Join(f.home, ".claude", "skills"), 0o755)

	rep, err := CreateClaudeProfile(f.home, "acme", false)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(f.home, ".claude-acme")
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("profile folder: %v", err)
	}
	links := map[string]string{
		"CLAUDE.md":     filepath.Join(f.home, ".agents", "AGENTS.md"),
		"settings.json": filepath.Join(f.home, ".claude", "settings.json"),
		"skills":        filepath.Join(f.home, ".claude", "skills"),
		"plugins":       filepath.Join(f.home, ".claude", "plugins"),
	}
	for name, want := range links {
		if got, _ := os.Readlink(filepath.Join(dir, name)); got != want {
			t.Errorf("%s → %q, want %q", name, got, want)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "hooks")); err == nil {
		t.Errorf("hooks does not exist in ~/.claude: no link to nothing")
	}
	if !hasNote(rep, "hooks") {
		t.Errorf("a missing shared part should be reported: %v", rep.Notes)
	}
	if _, err := CreateClaudeProfile(f.home, "acme", false); err == nil {
		t.Errorf("an existing profile folder is an error")
	}
}

func TestSyncInstallsSkillsAndCommands(t *testing.T) {
	f := setup(t)
	os.MkdirAll(filepath.Join(f.home, ".claude", "skills"), 0o755)
	f.sync(t, false)

	skill := filepath.Join(f.home, ".agents", "skills", "handoff", "SKILL.md")
	b, err := os.ReadFile(skill)
	if err != nil || !strings.Contains(string(b), "name: handoff") || !strings.Contains(string(b), "Installed by ai-profile sync") {
		t.Fatalf("handoff skill in ~/.agents/skills: %v", err)
	}
	if target, _ := os.Readlink(filepath.Join(f.home, ".claude", "skills", "handoff")); target != filepath.Dir(skill) {
		t.Errorf("Claude Code sees it through a link, got %q", target)
	}
	cmd := filepath.Join(f.home, ".config", "opencode", "commands", "handoff.md")
	if b, err := os.ReadFile(cmd); err != nil || !strings.Contains(string(b), "ai-profile handoff $ARGUMENTS") {
		t.Errorf("OpenCode /handoff command: %v", err)
	}
	if again := f.sync(t, false); len(again.Actions) != 0 {
		t.Errorf("installed files are not rewritten: %v", again.Actions)
	}

	// A file of the user's, without the marker, is left alone.
	write(t, cmd, "mio\n")
	rep := f.sync(t, false)
	if b, _ := os.ReadFile(cmd); string(b) != "mio\n" || !hasNote(rep, "handoff.md") {
		t.Errorf("a file without the marker is kept and reported: %s, %v", b, rep.Notes)
	}
	// An older installed copy is updated.
	write(t, skill, "---\nname: handoff\n---\n\n<!-- Installed by ai-profile sync from the ai-profiles repository; changes here are overwritten. -->\nvecchia\n")
	f.sync(t, false)
	if b, _ := os.ReadFile(skill); strings.Contains(string(b), "vecchia") {
		t.Errorf("an installed copy that differs is updated")
	}
}

func TestSyncSetsClaudeStatusLine(t *testing.T) {
	f := setup(t)
	settings := filepath.Join(f.home, ".claude", "settings.json")
	write(t, settings, `{"model":"opus","statusLine":{"type":"command","command":"sh ~/.claude/mia.sh"}}`)

	// Without [statusline] claude a custom command is kept and reported.
	rep, err := Sync(Options{Home: f.home, Cfg: f.cfg, Self: "/bin/ai-profile"})
	if err != nil {
		t.Fatal(err)
	}
	if m := readJSONT(t, settings); m["statusLine"].(map[string]any)["command"] != "sh ~/.claude/mia.sh" || !hasNote(rep, "statusline") {
		t.Errorf("an unknown statusLine is left and reported: %v %v", m["statusLine"], rep.Notes)
	}

	f.cfg.Statusline.Claude = "sh ~/.claude/mia.sh"
	if _, err := Sync(Options{Home: f.home, Cfg: f.cfg, Self: "/bin/ai-profile"}); err != nil {
		t.Fatal(err)
	}
	m := readJSONT(t, settings)
	if got := m["statusLine"].(map[string]any)["command"]; got != "/bin/ai-profile statusline" {
		t.Errorf("statusLine = %v", got)
	}
	if m["model"] != "opus" {
		t.Errorf("other settings are kept")
	}
	rep, _ = Sync(Options{Home: f.home, Cfg: f.cfg, Self: "/bin/ai-profile"})
	if len(rep.Actions) != 0 {
		t.Errorf("nothing to change the second time: %v", rep.Actions)
	}

	// An ai-profile installed by mise runs from a versioned folder: the
	// statusLine goes through the mise shim, which survives upgrades.
	data := t.TempDir()
	t.Setenv("MISE_DATA_DIR", data)
	installed := filepath.Join(data, "installs", "github-lunetteorg-ai-profiles", "0.1.1", "ai-profile")
	shim := filepath.Join(data, "shims", "ai-profile")
	for _, p := range []string{installed, shim} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		write(t, p, "")
	}
	if _, err := Sync(Options{Home: f.home, Cfg: f.cfg, Self: installed}); err != nil {
		t.Fatal(err)
	}
	if got := readJSONT(t, settings)["statusLine"].(map[string]any)["command"]; got != shim+" statusline" {
		t.Errorf("statusLine = %v, want the mise shim", got)
	}
}
