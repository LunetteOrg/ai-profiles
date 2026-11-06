package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LunetteOrg/ai-profiles/internal/apply"
	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/desktop"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func link(t *testing.T, target, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
}

// fakeHome builds a home with a healthy "personal" profile and a "client-d"
// profile that has every problem doctor should find.
func fakeHome(t *testing.T) (string, *config.Config) {
	t.Helper()
	home := t.TempDir()
	write(t, filepath.Join(home, ".agents", "AGENTS.md"), "# comuni\n")
	for _, f := range []string{"settings.json", "statusline-command.sh", "statusline-helpers.sh"} {
		write(t, filepath.Join(home, ".claude", f), "{}\n")
	}
	for _, d := range []string{"skills", "plugins", "hooks"} {
		if err := os.MkdirAll(filepath.Join(home, ".claude", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{"personal", "client-d"} {
		dir := filepath.Join(home, ".claude-"+p)
		link(t, filepath.Join(home, ".agents", "AGENTS.md"), filepath.Join(dir, "CLAUDE.md"))
		for _, f := range []string{"skills", "plugins", "hooks", "statusline-command.sh", "statusline-helpers.sh"} {
			link(t, filepath.Join(home, ".claude", f), filepath.Join(dir, f))
		}
	}
	link(t, filepath.Join(home, ".claude", "settings.json"), filepath.Join(home, ".claude-personal", "settings.json"))
	// client-d: settings.json is a real file, statusline link is broken.
	write(t, filepath.Join(home, ".claude-client-d", "settings.json"), "{}\n")
	os.Remove(filepath.Join(home, ".claude-client-d", "statusline-helpers.sh"))
	link(t, filepath.Join(home, ".claude", "manca.sh"), filepath.Join(home, ".claude-client-d", "statusline-helpers.sh"))

	write(t, filepath.Join(home, ".claude-personal", ".claude.json"),
		`{"oauthAccount":{"emailAddress":"io@personal.example","accountUuid":"u1"}}`)
	write(t, filepath.Join(home, ".claude-client-d", ".claude.json"),
		`{"oauthAccount":{"emailAddress":"io@personal.example","accountUuid":"u1"}}`)

	if err := os.MkdirAll(filepath.Join(home, "Developer", "client-d"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, `default = "personal"
[profiles.personal]
account = "io@personal.example"
[profiles.client-d]
account = "io@client-d.example"
dirs = ["~/Developer/client-d", "~/Developer/client-e"]
`)
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	return home, cfg
}

func find(results []Result, status Status, substr string) bool {
	for _, r := range results {
		if r.Status == status && strings.Contains(r.Area+" "+r.Msg, substr) {
			return true
		}
	}
	return false
}

func dump(results []Result) string {
	var b strings.Builder
	for _, r := range results {
		b.WriteString(r.String() + "\n")
	}
	return b.String()
}

func TestDoctorFindsProblems(t *testing.T) {
	home, cfg := fakeHome(t)
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "vecchio\n")
	write(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{"budget":{}}}`)
	write(t, filepath.Join(home, ".zshrc"), "export CLAUDE_CONFIG_DIR=~/.claude-personal\n")
	write(t, filepath.Join(home, "Developer", "client-d", "mise.toml"), "[env]\nCLAUDE_CONFIG_DIR = \"x\"\n")

	res := Run(Env{Home: home, Cfg: cfg, PathList: "", Self: "/nessuno"})
	out := dump(res)
	for _, want := range []struct {
		status Status
		substr string
	}{
		{Fail, "settings.json is a real file"},
		{Fail, "statusline-helpers.sh points to"},
		{Fail, "io@client-d.example"},
		{Fail, "same Claude account"},
		{Fail, ".claude/CLAUDE.md"},
		{Fail, "budget"},
		{Warn, "Developer/client-e"},
		{Warn, ".zshrc"},
		{Warn, "mise.toml"},
		{Fail, "claude not found"},
	} {
		if !find(res, want.status, want.substr) {
			t.Errorf("missing %v containing %q in:\n%s", want.status, want.substr, out)
		}
	}
	if find(res, Fail, ".claude-personal/settings.json") {
		t.Errorf("personal is healthy, got:\n%s", out)
	}
}

func TestDoctorHealthyHome(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, ".agents", "AGENTS.md"), "# comuni\n")
	for _, f := range []string{"settings.json", "statusline-command.sh", "statusline-helpers.sh"} {
		write(t, filepath.Join(home, ".claude", f), "{}\n")
	}
	for _, d := range []string{"skills", "plugins", "hooks"} {
		os.MkdirAll(filepath.Join(home, ".claude", d), 0o755)
	}
	dir := filepath.Join(home, ".claude-solo")
	link(t, filepath.Join(home, ".agents", "AGENTS.md"), filepath.Join(dir, "CLAUDE.md"))
	for _, f := range apply.ClaudeSharedLinks[1:] {
		link(t, filepath.Join(home, ".claude", f), filepath.Join(dir, f))
	}
	write(t, filepath.Join(dir, ".claude.json"), `{"oauthAccount":{"emailAddress":"a@b.it","accountUuid":"u"}}`)
	write(t, filepath.Join(home, ".claude.json"), `{"mcpServers":{}}`)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, "default = \"solo\"\n[profiles.solo]\naccount = \"a@b.it\"\n")
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	self := filepath.Join(home, "ai-profile")
	write(t, self, "#!/bin/sh\n")
	os.Chmod(self, 0o755)
	link(t, self, filepath.Join(bin, "shim", "claude"))
	write(t, filepath.Join(bin, "real", "claude"), "#!/bin/sh\n")
	os.Chmod(filepath.Join(bin, "real", "claude"), 0o755)

	pathList := filepath.Join(bin, "shim") + string(os.PathListSeparator) + filepath.Join(bin, "real")
	// A healthy home is one that has been synced (skills and commands installed).
	if _, err := apply.Sync(apply.Options{Home: home, Cfg: cfg}); err != nil {
		t.Fatal(err)
	}
	res := Run(Env{Home: home, Cfg: cfg, PathList: pathList, Self: self})
	for _, r := range res {
		if r.Status != OK {
			t.Errorf("unexpected: %s", r)
		}
	}
}

func TestDoctorPendingSync(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, `default = "personal"
[mcp.notion]
url = "https://mcp.notion.com/mcp"
[profiles.personal]
account = "io@personal.example"
mcp = ["notion"]
`)
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno"})
	if !find(res, Warn, "ai-profile sync") {
		t.Errorf("a declared server missing from the profile should ask for sync:\n%s", dump(res))
	}
}

func TestDoctorOnlyFiles(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, `default = "personal"
[mcp.esempio]
command = "esempio"
dirs = [{ path = "~/.esempio/{profilo}", only = ["{email}.json", "*.lock"] }]
[profiles.personal]
account = "io@personal.example"
mcp = ["esempio"]
vars.email = "io@personal.example"
`)
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".esempio", "personal")
	write(t, filepath.Join(dir, "io@personal.example.json"), "{}")
	write(t, filepath.Join(dir, "x.lock"), "")
	res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno"})
	if find(res, Warn, "not allowed") {
		t.Errorf("only allowed files are present:\n%s", dump(res))
	}
	write(t, filepath.Join(dir, "altro@cliente.it.json"), "{}")
	res = Run(Env{Home: home, Cfg: cfg, Self: "/nessuno"})
	if !find(res, Warn, "altro@cliente.it.json") {
		t.Errorf("a file outside only should be reported:\n%s", dump(res))
	}
}

func TestDoctorSecrets(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, `default = "personal"
[profiles.personal]
account = "io@personal.example"
[profiles.personal.secrets]
A = "op://V/I/f"
B = "env:PRESENTE"
C = "env:NEL_FILE"
D = "env:ASSENTE"
`)
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(home, ".env"), "NEL_FILE=x\n")
	bin := filepath.Join(home, "bin")
	for _, c := range []string{"op", "ai-secrets"} {
		write(t, filepath.Join(bin, c), "#!/bin/sh\n")
		os.Chmod(filepath.Join(bin, c), 0o755)
	}
	getenv := func(k string) string { return map[string]string{"PRESENTE": "1"}[k] }

	res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno", PathList: bin, Getenv: getenv})
	out := dump(res)
	if !find(res, Warn, "ASSENTE") {
		t.Errorf("a missing env: variable should be reported:\n%s", out)
	}
	if find(res, Warn, "PRESENTE") || find(res, Warn, "NEL_FILE") || find(res, Warn, "op, which is not") {
		t.Errorf("available secrets reported as missing:\n%s", out)
	}

	res = Run(Env{Home: home, Cfg: cfg, Self: "/nessuno", PathList: "", Getenv: getenv})
	if !find(res, Fail, "op") {
		t.Errorf("1Password references without op in PATH should fail:\n%s", dump(res))
	}
}

func TestDoctorBitwardenAndServerSecrets(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, `default = "personal"
[mcp.budget]
command = "uv"
env.BUDGET_API_KEY = "op://Private/x/credential"
[profiles.personal]
account = "io@personal.example"
mcp = ["budget"]
bitwarden = "client-e"
secrets.MISTRAL_API_KEY = "bw://Mistral"
`)
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(home, "bin")
	for _, c := range []string{"op", "bw", "ai-secrets"} {
		write(t, filepath.Join(bin, c), "#!/bin/sh\n")
		os.Chmod(filepath.Join(bin, c), 0o755)
	}
	stores := filepath.Join(home, ".config", "ai-secrets", "config.toml")
	write(t, stores, "[stores.bitwarden.altro]\npassword = \"op://Private/bw/password\"\n")
	if res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno", PathList: bin}); !find(res, Fail, "[stores.bitwarden.client-e]") {
		t.Errorf("an account missing from the ai-secrets configuration should fail:\n%s", dump(res))
	}
	write(t, stores, "[stores.bitwarden.client-e]\npassword = \"op://Private/bw/password\"\n")
	status := "unauthenticated"
	env := Env{Home: home, Cfg: cfg, Self: "/nessuno", PathList: bin,
		BitwardenStatus: func(*secrets.Bitwarden) (string, error) { return status, nil }}
	if res := Run(env); !find(res, Fail, "bw login") {
		t.Errorf("a Bitwarden account without login should fail:\n%s", dump(res))
	}
	status = "locked"
	if res := Run(env); !find(res, OK, "2 references") || find(res, Warn, "Node") {
		t.Errorf("a logged-in account and op in PATH are fine:\n%s", dump(res))
	}
	// The npm package of bw is a Node script: it runs with the node of the
	// folder it starts in.
	write(t, filepath.Join(bin, "bw"), "#!/usr/bin/env node\n")
	if res := Run(env); !find(res, Warn, "Node script") {
		t.Errorf("a bw that runs on Node should be reported:\n%s", dump(res))
	}
	// npm and mise put a shell shim in node_modules/.bin in front of it.
	shim := filepath.Join(home, "lib", "node_modules", ".bin", "bw")
	write(t, shim, "#!/bin/sh\n")
	os.Chmod(shim, 0o755)
	os.Remove(filepath.Join(bin, "bw"))
	if err := os.Symlink(shim, filepath.Join(bin, "bw")); err != nil {
		t.Fatal(err)
	}
	if res := Run(env); !find(res, Warn, "Node script") {
		t.Errorf("a bw from an npm package should be reported:\n%s", dump(res))
	}
	os.Remove(filepath.Join(bin, "bw"))
	write(t, filepath.Join(bin, "bw"), "#!/bin/sh\n")
	os.Chmod(filepath.Join(bin, "bw"), 0o755)
	env.PathList = filepath.Join(home, "vuota")
	if res := Run(env); !find(res, Fail, "op, which is not in PATH") {
		t.Errorf("servers with op:// need op:\n%s", dump(res))
	}
}

func TestDoctorFix(t *testing.T) {
	home, cfg := fakeHome(t)
	write(t, filepath.Join(home, ".claude", "CLAUDE.md"), "vecchio\n")
	write(t, filepath.Join(home, ".claude.json"), `{"numStartups": 3, "mcpServers": {"budget": {"command": "uv"}}}`)
	os.Remove(filepath.Join(home, ".claude-personal", "hooks"))

	env := Env{Home: home, Cfg: cfg, Self: "/nessuno"}
	res := Run(env)
	fixed := 0
	for _, r := range res {
		if r.Fix == nil {
			continue
		}
		if _, err := r.Fix(); err != nil {
			t.Fatalf("%s: %v", r, err)
		}
		fixed++
	}
	if fixed < 3 {
		t.Fatalf("expected at least 3 fixes, got %d:\n%s", fixed, dump(res))
	}

	if target, _ := os.Readlink(filepath.Join(home, ".claude-personal", "hooks")); target != filepath.Join(home, ".claude", "hooks") {
		t.Errorf("hooks link not restored: %q", target)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "CLAUDE.md")); err == nil {
		t.Errorf("~/.claude/CLAUDE.md should be moved away")
	}
	backups, _ := filepath.Glob(filepath.Join(home, ".claude", "CLAUDE.md.ai-profile-*"))
	if len(backups) != 1 {
		t.Errorf("CLAUDE.md backup: %v", backups)
	}
	b, _ := os.ReadFile(filepath.Join(home, ".claude.json"))
	if strings.Contains(string(b), "budget") || !strings.Contains(string(b), "numStartups") {
		t.Errorf("~/.claude.json should keep everything but mcpServers: %s", b)
	}
	if bk, _ := filepath.Glob(filepath.Join(home, ".claude.json.ai-profile-*")); len(bk) != 1 {
		t.Errorf(".claude.json backup: %v", bk)
	}

	again := Run(env)
	for _, bad := range []string{".claude/CLAUDE.md", "budget", "hooks is missing"} {
		if find(again, Fail, bad) {
			t.Errorf("%q still reported after the fix:\n%s", bad, dump(again))
		}
	}
	// A real file in place of a link is left for a manual fix.
	for _, r := range again {
		if strings.Contains(r.Msg, "settings.json is a real file") && r.Fix != nil {
			t.Errorf("a real settings.json must not get an automatic fix")
		}
	}
}

func TestDoctorCodexLogin(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, "default = \"personal\"\n[profiles.personal]\naccount = \"io@personal.example\"\nagents = [\"claude\", \"codex\"]\n")
	cfg, _ := config.Load(cfgPath, home)
	os.MkdirAll(filepath.Join(home, ".codex-personal"), 0o700)
	if res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno"}); !find(res, Warn, "login") || !find(res, Warn, "Codex personal") {
		t.Errorf("a Codex profile without auth.json should be reported:\n%s", dump(res))
	}
	write(t, filepath.Join(home, ".codex-personal", "auth.json"), "{}")
	if res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno"}); find(res, Warn, "Codex personal") {
		t.Errorf("with auth.json there is nothing to report:\n%s", dump(res))
	}
}

// desktopSystem stands in for macOS: versions come from a file in each app.
type desktopSystem struct{ procs []desktop.Process }

func (d *desktopSystem) Version(app string) string {
	b, _ := os.ReadFile(filepath.Join(app, "Contents", "version"))
	return string(b)
}
func (d *desktopSystem) Clone(src, dst string) error {
	if err := os.MkdirAll(filepath.Join(dst, "Contents"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dst, "Contents", "version"), []byte(d.Version(src)), 0o644)
}
func (d *desktopSystem) Tint(app, from string, hue float64) error {
	return os.WriteFile(filepath.Join(app, "Icon\r"), nil, 0o644)
}
func (d *desktopSystem) Processes() ([]desktop.Process, error) { return d.procs, nil }
func (d *desktopSystem) Quit([]int) bool                       { return false }
func (d *desktopSystem) Ask(_, _, _, _ string) bool            { return false }
func (d *desktopSystem) Open([]string) error                   { return nil }
func (d *desktopSystem) Register(string)                       {}
func (d *desktopSystem) Applet(app, source string, keys map[string]string) error {
	plist := "<plist><dict>\n"
	for k, v := range keys {
		plist += "<key>" + k + "</key><string>" + v + "</string>\n"
	}
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte(plist+"</dict></plist>\n"), 0o644)
}

func TestDoctorDesktop(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, "default = \"personal\"\n[profiles.personal]\ndesktop = { hue = 160 }\n[profiles.client-d]\ndesktop = { hue = 330 }\n[profiles.client-b]\n")
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(home, "Claude.app")
	write(t, filepath.Join(source, "Contents", "version"), "3.6556.2")
	sys := &desktopSystem{}
	env := Env{Home: home, Cfg: cfg, Self: "/x/ai-profile", Desktop: true, DesktopSystem: sys, DesktopSource: source}
	personal := desktop.Instance{Name: "personal", Home: home, Hue: 160, Source: source}
	clientD := desktop.Instance{Name: "client-d", Home: home, Hue: 330, Source: source}
	clientB := desktop.Instance{Name: "client-b", Home: home}

	// personal: no launcher, copy behind and without its icon, a leftover.
	write(t, filepath.Join(personal.Copy(), "Contents", "version"), "2.31226.0")
	os.MkdirAll(personal.Copy()+".refreshing", 0o755)
	// client-d: claude-multiprofile's launcher, copy open without the profile.
	write(t, filepath.Join(clientD.Launcher(), "Contents", "Resources", "Scripts", "main.scpt"), "applet")
	write(t, filepath.Join(clientD.Copy(), "Contents", "version"), "3.6556.2")
	sys.procs = []desktop.Process{{PID: 4, Command: clientD.Copy() + "/Contents/MacOS/Claude"}}
	// client-b: a launcher, but no desktop in config.toml.
	write(t, filepath.Join(clientB.Launcher(), "Contents", "Info.plist"), "x")
	write(t, filepath.Join(home, "Library", "Application Support", "claude-multiprofile", "bin", "launch.js"), "x")

	res := Run(env)
	for _, want := range []struct {
		s    Status
		text string
	}{
		{Warn, "Desktop personal no launcher"},
		{Warn, "2.31226.0"},
		{Warn, ".refreshing"},
		{Warn, "icon"},
		{Warn, "Desktop client-d " + "~/Applications/Claude Client-d.app was not written by ai-profile"},
		{Fail, "without its profile"},
		{Warn, "Desktop client-b"},
		{Warn, "claude-multiprofile"},
	} {
		if !find(res, want.s, want.text) {
			t.Errorf("missing %s %q:\n%s", want.s, want.text, dump(res))
		}
	}

	for _, r := range res {
		if r.Fix != nil {
			if _, err := r.Fix(); err != nil {
				t.Errorf("%s: fix: %v", r, err)
			}
		}
	}
	sys.procs = nil
	res = Run(env)
	if !find(res, OK, "Desktop personal") || !find(res, OK, "Desktop client-d") {
		t.Errorf("after --fix personal and client-d are in order:\n%s", dump(res))
	}
	if !desktop.Ours(personal) || !desktop.Ours(clientD) || sys.Version(personal.Copy()) != "3.6556.2" {
		t.Errorf("launchers %v %v, copy %q", desktop.Ours(personal), desktop.Ours(clientD), sys.Version(personal.Copy()))
	}

	env.Cfg, _ = config.Load(writeTemp(t, "default = \"personal\"\n[profiles.personal]\n"), t.TempDir())
	env.Home = t.TempDir()
	for _, r := range Run(env) {
		if strings.HasPrefix(r.Area, "Desktop") {
			t.Errorf("without desktop in config.toml nor launchers Desktop is not checked: %s", r)
		}
	}
}

func writeTemp(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "config.toml")
	write(t, p, body)
	return p
}

func TestDoctorAcceptsClaudeAccounts(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, "default = \"personal\"\n[profiles.personal]\naccount = \"altro@x.it\"\nclaude_accounts = [\"io@personal.example\"]\n[profiles.client-d]\naccount = \"io@personal.example\"\n")
	cfg, _ := config.Load(cfgPath, home)
	res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno"})
	if find(res, Fail, "expected") {
		t.Errorf("an account of claude_accounts is accepted:\n%s", dump(res))
	}
	if find(res, Fail, "same Claude account") {
		t.Errorf("an account in claude_accounts may also be another profile's:\n%s", dump(res))
	}
}

func TestDoctorPrivateFiles(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	store := filepath.Join(t.TempDir(), "store")
	write(t, cfgPath, `[private]
file = "~/.config/ai-profiles/private.toml"
restore = "cat `+store+`"
backup = "cat > `+store+`"
[profiles.personal]
account = "io@personal.example"
`)
	write(t, filepath.Join(home, ".config", "ai-profiles", "private.toml"), `default = "personal"
files = ["~/Developer/client-d/AGENTS.md"]
`)
	notes := filepath.Join(home, "Developer", "client-d", "AGENTS.md")
	write(t, notes, "# client-d\n")
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	env := Env{Home: home, Cfg: cfg, Self: "/nessuno", Getenv: func(string) string { return "" }}

	res := Run(env)
	if !find(res, Warn, "ai-profile backup") || !find(res, Warn, "Developer/client-d/AGENTS.md") {
		t.Errorf("files never backed up should be reported:\n%s", dump(res))
	}
	if err := cfg.Private.Save(home, cfg.Files); err != nil {
		t.Fatal(err)
	}
	res = Run(env)
	if find(res, Warn, "ai-profile backup") || !find(res, OK, "Private files") {
		t.Errorf("after a backup nothing should be reported:\n%s", dump(res))
	}
	os.Remove(notes)
	if res = Run(env); !find(res, Fail, "Developer/client-d/AGENTS.md") {
		t.Errorf("a listed file that is missing should fail:\n%s", dump(res))
	}
}

func TestDoctorSecretsPrivateFile(t *testing.T) {
	home, cfg := fakeHome(t)
	write(t, filepath.Join(home, ".config", "ai-secrets", "config.toml"), `[private]
file = "~/.config/ai-secrets/private.toml"
restore = "true"
backup = "true"
`)
	env := Env{Home: home, Cfg: cfg, Self: "/nessuno", Getenv: func(string) string { return "" }}
	if res := Run(env); !find(res, Fail, "ai-secrets restore") {
		t.Errorf("a missing ai-secrets private file should fail:\n%s", dump(res))
	}
	write(t, filepath.Join(home, ".config", "ai-secrets", "private.toml"), "ttl = \"1h\"\n")
	if res := Run(env); !find(res, Warn, "ai-secrets backup") {
		t.Errorf("an ai-secrets private file never backed up should be reported:\n%s", dump(res))
	}
}

func TestDoctorClaudian(t *testing.T) {
	home, cfg := fakeHome(t)
	claudian := func(vault, env string) {
		write(t, filepath.Join(vault, ".claudian", "claudian-settings.json"),
			`{"providerConfigs":{"claude":{"cliPath":"/shim/claude","environmentVariables":"`+env+`"}}}`)
	}
	notes := filepath.Join(home, "Notes")
	work := filepath.Join(home, "Developer", "client-d", "vault")
	bare := filepath.Join(home, "Bare")
	plain := filepath.Join(home, "Plain")
	claudian(notes, `FOO=1\nCLAUDE_CONFIG_DIR=`+filepath.Join(home, ".claude-personal"))
	claudian(work, `CLAUDE_CONFIG_DIR=`+filepath.Join(home, ".claude-personal"))
	claudian(bare, ``)
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	registry := filepath.Join(home, "obsidian.json")
	write(t, registry, `{"vaults":{"a":{"path":"`+notes+`"},"b":{"path":"`+work+`"},"c":{"path":"`+bare+`"},"d":{"path":"`+plain+`"}}}`)

	res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno", ObsidianConfig: registry})
	out := dump(res)
	for _, want := range []struct {
		status Status
		substr string
	}{
		{OK, "Claudian ~/Notes"},
		{Fail, "Claudian ~/Developer/client-d/vault"},
		{Fail, "~/.claude-client-d"},
		{Fail, "Claudian ~/Bare"},
	} {
		if !find(res, want.status, want.substr) {
			t.Errorf("missing %v containing %q in:\n%s", want.status, want.substr, out)
		}
	}
	if find(res, OK, "Claudian ~/Plain") || find(res, Fail, "Claudian ~/Plain") {
		t.Errorf("a vault without Claudian has nothing to check:\n%s", out)
	}
}

func TestDoctorClaudianNoObsidian(t *testing.T) {
	home, cfg := fakeHome(t)
	res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno", ObsidianConfig: filepath.Join(home, "missing.json")})
	for _, r := range res {
		if strings.HasPrefix(r.Area, "Claudian") {
			t.Errorf("no Obsidian, no Claudian check: %s", r)
		}
	}
}

func TestDoctorFolders(t *testing.T) {
	home, _ := fakeHome(t)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, `default = "personal"
[profiles.personal]
[profiles.client-d]
dirs = ["~/Developer/client-d"]
[[folders]]
dir = "~/Developer/shared"
profiles = ["personal", "client-d"]
[[folders]]
dir = "~/Developer/solo"
profiles = ["client-d"]
`)
	write(t, filepath.Join(home, "Developer", "shared", "mise.local.toml"), "[env]\nCLAUDE_CONFIG_DIR = \"{{env.HOME}}/.claude-client-d\"\n")
	write(t, filepath.Join(home, "Developer", "client-d", "app", ".envrc"), "export CODEX_HOME=~/.codex-client-d\n")
	write(t, filepath.Join(home, "Developer", "client-d", "altro", "mise.toml"), "[tools]\ngo = \"1\"\n")
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(Env{Home: home, Cfg: cfg, Self: "/nessuno"})
	for _, want := range []struct {
		s    Status
		text string
	}{
		{Warn, "~/Developer/solo lists only client-d"},
		{Warn, "~/Developer/solo does not exist"},
		{Fail, "~/Developer/shared/mise.local.toml sets CLAUDE_CONFIG_DIR"},
		{Fail, "~/Developer/client-d/app/.envrc sets CODEX_HOME"},
	} {
		if !find(res, want.s, want.text) {
			t.Errorf("missing %s %q:\n%s", want.s, want.text, dump(res))
		}
	}
	if find(res, Fail, "altro/mise.toml") {
		t.Errorf("a mise.toml without the variables is fine:\n%s", dump(res))
	}
}

// shellHome is a home whose shims (links to self) and real claude live in
// separate folders, for the startup-file checks.
func shellHome(t *testing.T) (home, self string, cfg *config.Config) {
	t.Helper()
	home = t.TempDir()
	self = filepath.Join(home, "ai-profile")
	write(t, self, "#!/bin/sh\n")
	os.Chmod(self, 0o755)
	link(t, self, filepath.Join(home, "shims", "claude"))
	write(t, filepath.Join(home, ".local", "bin", "claude"), "#!/bin/sh\n")
	os.Chmod(filepath.Join(home, ".local", "bin", "claude"), 0o755)
	cfgPath := filepath.Join(home, "config.toml")
	write(t, cfgPath, "default = \"solo\"\n[profiles.solo]\naccount = \"a@b.it\"\n")
	cfg, err := config.Load(cfgPath, home)
	if err != nil {
		t.Fatal(err)
	}
	return home, self, cfg
}

func TestDoctorStartupFilePutsAgentBeforeShims(t *testing.T) {
	home, self, cfg := shellHome(t)
	zprofile := filepath.Join(home, ".zprofile")
	head := "export PATH=\"$HOME/.local/bin:$PATH\"\nexport PATH=\"$HOME/shims:$PATH\"\n\n"
	write(t, zprofile, head+"# Added by Foo installer\nexport PATH=\"$HOME/.local/bin:$PATH\"\n")
	// A later file that adds a folder without agents, after the shims, is fine.
	write(t, filepath.Join(home, ".zshrc"), "export PATH=\"/opt/tools/bin:$PATH\"\npath=(~/.local/bin $path)\n")

	env := Env{Home: home, Cfg: cfg, Self: self}
	res := Run(env)
	var fixes []Result
	for _, r := range res {
		if r.Area == "PATH" && strings.Contains(r.Msg, "before the shims") {
			fixes = append(fixes, r)
		}
	}
	if len(fixes) != 2 {
		t.Fatalf("expected the .zprofile and .zshrc lines, got:\n%s", dump(res))
	}
	if !find(res, Warn, "~/.zprofile:5 puts ~/.local/bin, where claude is, before the shims") {
		t.Errorf("missing the .zprofile line:\n%s", dump(res))
	}
	if !find(res, Warn, "~/.zshrc:2") || find(res, Warn, "/opt/tools") {
		t.Errorf("only the zshrc line that adds claude counts:\n%s", dump(res))
	}
	for _, r := range fixes {
		if r.Fix == nil {
			t.Fatalf("~/.local/bin is added elsewhere too, so the line can go: %s", r)
		}
		if _, err := r.Fix(); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(zprofile); string(b) != head {
		t.Errorf("the line and its comment should be gone, got:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".zshrc")); string(b) != "export PATH=\"/opt/tools/bin:$PATH\"\n" {
		t.Errorf("zshrc: %q", b)
	}
	if bk, _ := filepath.Glob(zprofile + ".ai-profile-*"); len(bk) != 1 {
		t.Errorf(".zprofile backup: %v", bk)
	}
	if again := Run(env); find(again, Warn, "before the shims") {
		t.Errorf("still reported after the fix:\n%s", dump(again))
	}
}

func TestDoctorStartupFileOnlySource(t *testing.T) {
	home, self, cfg := shellHome(t)
	// The shims come first, then the only line that adds ~/.local/bin:
	// removing it would lose the folder, so the fix adds the shims again
	// right after it.
	zshrc := filepath.Join(home, ".zshrc")
	write(t, zshrc, "path=($HOME/shims $path)\nexport PATH=~/.local/bin:$PATH\nalias x=y\n")
	env := Env{Home: home, Cfg: cfg, Self: self}
	var fix func() (string, error)
	for _, r := range Run(env) {
		if strings.Contains(r.Msg, "before the shims") {
			if !strings.Contains(r.Msg, "~/.zshrc:2") || r.Fix == nil {
				t.Fatalf("want a fixable warning on ~/.zshrc:2, got %s", r)
			}
			fix = r.Fix
		}
	}
	if fix == nil {
		t.Fatalf("no warning:\n%s", dump(Run(env)))
	}
	if _, err := fix(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(zshrc)
	want := "path=($HOME/shims $path)\nexport PATH=~/.local/bin:$PATH\n"
	if !strings.HasPrefix(string(b), want) || !strings.HasSuffix(string(b), "export PATH=\"$HOME/shims:$PATH\"\nalias x=y\n") {
		t.Errorf("the shims line should follow the line, got:\n%s", b)
	}
	if again := Run(env); find(again, Warn, "before the shims") {
		t.Errorf("still reported after the fix:\n%s", dump(again))
	}
}

func TestDoctorStartupFileBashLogin(t *testing.T) {
	home, self, cfg := shellHome(t)
	// Orca starts agents with bash -lc, which reads only ~/.profile: the
	// shims come from the inherited PATH, and an installer's line goes
	// before them. .zprofile knows the shims line.
	write(t, filepath.Join(home, ".zprofile"), "export PATH=\"$HOME/shims:$PATH\"\n")
	profile := filepath.Join(home, ".profile")
	write(t, profile, "export PATH=\"$PATH:/opt/x\"\n\n# Added by Foo installer\nexport PATH=\"$HOME/.local/bin:$PATH\"\n")
	// .bashrc is not read by a login shell.
	write(t, filepath.Join(home, ".bashrc"), "export PATH=\"$HOME/.local/bin:$PATH\"\n")
	env := Env{Home: home, Cfg: cfg, Self: self}
	res := Run(env)
	if !find(res, Warn, "~/.profile:4 puts ~/.local/bin, where claude is, before the shims") || find(res, Warn, ".bashrc") {
		t.Fatalf("want the ~/.profile line only:\n%s", dump(res))
	}
	for _, r := range res {
		if r.Fix != nil && strings.Contains(r.Msg, "before the shims") {
			if _, err := r.Fix(); err != nil {
				t.Fatal(err)
			}
		}
	}
	b, _ := os.ReadFile(profile)
	if !strings.HasSuffix(string(b), "export PATH=\"$HOME/.local/bin:$PATH\"\n# ai-profile shims first (ai-profile doctor --fix)\nexport PATH=\"$HOME/shims:$PATH\"\n") {
		t.Errorf("the shims line should follow the installer's line, got:\n%s", b)
	}
	// A .bash_profile hides .profile from bash.
	write(t, filepath.Join(home, ".bash_profile"), "true\n")
	write(t, profile, "export PATH=\"$HOME/.local/bin:$PATH\"\n")
	if res := Run(env); find(res, Warn, ".profile") {
		t.Errorf("bash does not read .profile when .bash_profile exists:\n%s", dump(res))
	}
}

func TestDoctorStartupFileShimsAddedAgain(t *testing.T) {
	home, self, cfg := shellHome(t)
	// .zprofile adds the shims, .zshrc puts ~/.local/bin first and then the
	// shims again: every shell ends with the shims first, until an installer
	// appends its line to both files.
	zshrc := filepath.Join(home, ".zshrc")
	original := "export PATH=\"$HOME/.local/bin:$PATH\"\nexport PATH=\"$HOME/shims:$PATH\"\n\n# Added by Foo\nexport PATH=\"$HOME/.local/bin:$PATH\"\n# Added by Bar\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"
	write(t, filepath.Join(home, ".zprofile"), "export PATH=\"$HOME/shims:$PATH\"\n")
	write(t, zshrc, original)
	res := Run(Env{Home: home, Cfg: cfg, Self: self})
	if find(res, Warn, "~/.zshrc:1 ") {
		t.Errorf("~/.zshrc:1 comes before the shims of .zshrc:\n%s", dump(res))
	}
	fixed := 0
	for _, r := range res {
		if r.Fix != nil && strings.Contains(r.Msg, "before the shims") {
			if _, err := r.Fix(); err != nil {
				t.Fatal(err)
			}
			fixed++
		}
	}
	if fixed != 2 {
		t.Fatalf("want two fixes, got %d:\n%s", fixed, dump(res))
	}
	bk, _ := filepath.Glob(zshrc + ".ai-profile-*")
	if len(bk) != 1 {
		t.Fatalf("backups: %v", bk)
	}
	if b, _ := os.ReadFile(bk[0]); string(b) != original {
		t.Errorf("the backup must keep the file as it was before the first fix, got:\n%s", b)
	}
}
