package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func mkdirs(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

const sample = `
default = "personal"

[profiles.personal]
account = "me@example.com"

[profiles.client-d]
account = "me@client-d.example"
dirs = ["~/Developer/client-d", "~/Developer/client-e"]
agents = ["claude", "codex"]

[profiles.client-b]
dirs = ["~/Developer/client-b"]
handoff_to = ["personal"]
`

func TestLoadExpandsHomeAndDefaultsAgents(t *testing.T) {
	home := t.TempDir()
	cfg, err := Load(writeConfig(t, sample), home)
	if err != nil {
		t.Fatal(err)
	}
	clientD := cfg.Profiles["client-d"]
	if got, want := clientD.Dirs[0], filepath.Join(home, "Developer", "client-d"); got != want {
		t.Errorf("dir = %q, want %q", got, want)
	}
	if got := cfg.Profiles["client-b"].Agents; len(got) != 1 || got[0] != "claude" {
		t.Errorf("default agents = %v, want [claude]", got)
	}
	if cfg.Profiles["personal"].Name != "personal" {
		t.Errorf("profile name not set")
	}
}

func TestLoadDesktop(t *testing.T) {
	cfg, err := Load(writeConfig(t, sample+"\n[profiles.client-d.desktop]\nhue = 330\n"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if d := cfg.Profiles["client-d"].Desktop; d == nil || d.Hue != 330 {
		t.Errorf("desktop = %+v", d)
	}
	if cfg.Profiles["client-b"].Desktop != nil {
		t.Errorf("a profile without desktop has none")
	}
}

const sampleFolders = sample + `
[[folders]]
dir = "~/Developer/shared"
profiles = ["personal", "client-d", "client-b"]
`

func TestResolveFolderWithSeveralProfiles(t *testing.T) {
	home := t.TempDir()
	playbook := filepath.Join(home, "Developer", "shared")
	mkdirs(t, filepath.Join(playbook, "docs"))
	cfg, err := Load(writeConfig(t, sampleFolders), home)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Resolve(filepath.Join(playbook, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Profile.Name != "personal" || m.Dir != playbook || m.Last || len(m.Allowed) != 3 {
		t.Errorf("without a record the first listed applies: %+v", m)
	}
	if !m.Allows("client-b") || m.Allows("altro") {
		t.Errorf("Allows follows the folder's list")
	}
	cfg.LastUsed = func(dir string) string {
		if dir == playbook {
			return "client-d"
		}
		return ""
	}
	if m, _ := cfg.Resolve(playbook); m.Profile.Name != "client-d" || !m.Last {
		t.Errorf("the last used applies: %+v", m)
	}
	if got := m.Admitted(); len(got) != 3 || got[0] != "personal" {
		t.Errorf("Admitted = %v", got)
	}
	cfg.LastUsed = func(string) string { return "client-b" }
	if m, _ := cfg.Resolve(playbook); !slices.Equal(m.Admitted(), []string{"client-b", "personal", "client-d"}) {
		t.Errorf("Admitted puts the chosen profile first: %v", m.Admitted())
	}
	cfg.LastUsed = func(string) string { return "rimosso" }
	if m, _ := cfg.Resolve(playbook); m.Profile.Name != "personal" {
		t.Errorf("a last used no longer listed is ignored: %s", m.Profile.Name)
	}
}

func TestAllowsWithoutFolders(t *testing.T) {
	home := t.TempDir()
	mkdirs(t, filepath.Join(home, "Developer", "client-b"))
	cfg, err := Load(writeConfig(t, sample), home)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := cfg.Resolve(filepath.Join(home, "Developer", "client-b"))
	if !m.Allows("client-b") || !m.Allows("personal") || m.Allows("client-d") {
		t.Errorf("client-b admits itself and its handoff_to")
	}
	if got := m.Admitted(); len(got) != 2 || got[0] != "client-b" || got[1] != "personal" {
		t.Errorf("Admitted = %v", got)
	}
	m, _ = cfg.Resolve(filepath.Join(home, "altrove"))
	if !m.Allows("client-d") {
		t.Errorf("outside the configured folders every profile is admitted")
	}
}

func TestLoadRejectsBadConfig(t *testing.T) {
	cases := map[string]string{
		"default mancante":       `default = "nessuno"` + "\n[profiles.a]\n",
		"agente sconosciuto":     `default = "a"` + "\n[profiles.a]\nagents = [\"vim\"]\n",
		"nome riservato":         `default = "doctor"` + "\n[profiles.doctor]\n",
		"handoff sconosciuto":    `default = "a"` + "\n[profiles.a]\nhandoff_to = [\"b\"]\n",
		"desktop senza claude":   `default = "a"` + "\n[profiles.a]\nagents = [\"codex\"]\ndesktop = { hue = 10 }\n",
		"tinta fuori scala":      `default = "a"` + "\n[profiles.a]\ndesktop = { hue = 400 }\n",
		"cartella senza dir":     `default = "a"` + "\n[profiles.a]\n[[folders]]\nprofiles = [\"a\"]\n",
		"cartella senza profili": `default = "a"` + "\n[profiles.a]\n[[folders]]\ndir = \"/x\"\n",
		"profilo sconosciuto":    `default = "a"` + "\n[profiles.a]\n[[folders]]\ndir = \"/x\"\nprofiles = [\"a\", \"b\"]\n",
		"cartella anche in dirs": `default = "a"` + "\n[profiles.a]\ndirs = [\"/x\"]\n[profiles.b]\n[[folders]]\ndir = \"/x\"\nprofiles = [\"a\", \"b\"]\n",
		"cartella due volte":     `default = "a"` + "\n[profiles.a]\n[profiles.b]\n[[folders]]\ndir = \"/x\"\nprofiles = [\"a\", \"b\"]\n[[folders]]\ndir = \"/x/\"\nprofiles = [\"b\"]\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, body), t.TempDir()); err == nil {
				t.Errorf("expected an error")
			}
		})
	}
}

func TestResolve(t *testing.T) {
	home := t.TempDir()
	mkdirs(t,
		filepath.Join(home, "Developer", "client-d", "playbook", "src"),
		filepath.Join(home, "Developer", "client-e"),
		filepath.Join(home, "Developer", "client-d-other"),
		filepath.Join(home, "Developer", "ilmeskio"),
	)
	cfg, err := Load(writeConfig(t, sample), home)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		dir, profile string
		byDir        bool
	}{
		{filepath.Join(home, "Developer", "client-d", "playbook", "src"), "client-d", true},
		{filepath.Join(home, "Developer", "client-d"), "client-d", true},
		{filepath.Join(home, "Developer", "client-e"), "client-d", true},
		{filepath.Join(home, "Developer", "client-d-other"), "personal", false},
		{filepath.Join(home, "Developer", "ilmeskio"), "personal", false},
		{home, "personal", false},
	}
	for _, c := range cases {
		m, err := cfg.Resolve(c.dir)
		if err != nil {
			t.Fatalf("%s: %v", c.dir, err)
		}
		if m.Profile.Name != c.profile || (m.Dir != "") != c.byDir {
			t.Errorf("%s: got %s (dir %q), want %s (byDir %v)", c.dir, m.Profile.Name, m.Dir, c.profile, c.byDir)
		}
	}
}

func TestResolveDeepestDirWins(t *testing.T) {
	home := t.TempDir()
	mkdirs(t, filepath.Join(home, "Developer", "client-c", "app", "app"))
	body := `default = "personal"
[profiles.personal]
dirs = ["~/Developer/client-c"]
[profiles.client-c]
dirs = ["~/Developer/client-c/app"]
`
	cfg, err := Load(writeConfig(t, body), home)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Resolve(filepath.Join(home, "Developer", "client-c", "app", "app"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Profile.Name != "client-c" {
		t.Errorf("got %s, want client-c", m.Profile.Name)
	}
}

func TestResolveSameDirInTwoProfiles(t *testing.T) {
	home := t.TempDir()
	mkdirs(t, filepath.Join(home, "Developer", "x"))
	body := `default = "a"
[profiles.a]
dirs = ["~/Developer/x"]
[profiles.b]
dirs = ["~/Developer/x"]
`
	cfg, err := Load(writeConfig(t, body), home)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.Resolve(filepath.Join(home, "Developer", "x"))
	if err == nil || !strings.Contains(err.Error(), "two profiles") {
		t.Errorf("err = %v, want an error about two profiles", err)
	}
}

func TestResolveThroughSymlink(t *testing.T) {
	home := t.TempDir()
	real := filepath.Join(home, "Developer", "client-d")
	mkdirs(t, real)
	link := filepath.Join(t.TempDir(), "scorciatoia")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(writeConfig(t, sample), home)
	if err != nil {
		t.Fatal(err)
	}
	m, err := cfg.Resolve(link)
	if err != nil {
		t.Fatal(err)
	}
	if m.Profile.Name != "client-d" {
		t.Errorf("got %s, want client-d", m.Profile.Name)
	}
}

const mcpSample = `
default = "personal"

[mcp.google-workspace]
command = "~/.local/bin/google-workspace-mcp"

[mcp.notion]
url = "https://mcp.notion.com/mcp"

[mcp.esempio]
command = "uvx"
args = ["esempio-mcp", "--account", "{email}"]
env.ESEMPIO_TOKEN_DIR = "~/.esempio/{profilo}"
dirs = [{ path = "~/.esempio/{profilo}", only = ["{email}.json"] }]

[profiles.personal]
mcp = ["google-workspace"]

[profiles.client-d]
dirs = ["~/Developer/client-d"]
mcp = ["google-workspace", "esempio"]
vars.email = "me@client-d.example"

[[profiles.client-d.repos]]
dir = "~/Developer/client-d/playbook"
mcp = ["notion"]
plugins = ["posthog"]
vars.email = "altro@client-d.example"
`

func TestLoadMCP(t *testing.T) {
	home := t.TempDir()
	cfg, err := Load(writeConfig(t, mcpSample), home)
	if err != nil {
		t.Fatal(err)
	}
	clientD := cfg.Profiles["client-d"]
	if got, want := clientD.Repos[0].Dir, filepath.Join(home, "Developer", "client-d", "playbook"); got != want {
		t.Errorf("repo dir = %q, want %q", got, want)
	}

	s, err := cfg.MCP["esempio"].Render(clientD.VarsFor(nil), home)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Args[2]; got != "me@client-d.example" {
		t.Errorf("{email} = %q, want the profile's", got)
	}
	if got, want := s.Env["ESEMPIO_TOKEN_DIR"], filepath.Join(home, ".esempio", "client-d"); got != want {
		t.Errorf("env = %q, want %q", got, want)
	}
	if got, want := s.Dirs[0].Path, filepath.Join(home, ".esempio", "client-d"); got != want {
		t.Errorf("dir = %q, want %q", got, want)
	}
	if s.Dirs[0].Only[0] != "me@client-d.example.json" {
		t.Errorf("only = %v", s.Dirs[0].Only)
	}

	s, _ = cfg.MCP["esempio"].Render(clientD.VarsFor(&clientD.Repos[0]), home)
	if got := s.Args[2]; got != "altro@client-d.example" {
		t.Errorf("a repo's vars win over the profile's, got %q", got)
	}

	g, _ := cfg.MCP["google-workspace"].Render(clientD.VarsFor(nil), home)
	if got, want := g.Command, filepath.Join(home, ".local", "bin", "google-workspace-mcp"); got != want {
		t.Errorf("command = %q, want %q", got, want)
	}

	if _, err := cfg.MCP["esempio"].Render(cfg.Profiles["personal"].VarsFor(nil), home); err == nil ||
		!strings.Contains(err.Error(), "{email}") {
		t.Errorf("an unknown placeholder must be an error, got %v", err)
	}
}

func TestLoadRejectsBadMCP(t *testing.T) {
	cases := map[string]string{
		"server non definito":         "default = \"p\"\n[profiles.p]\nmcp = [\"nessuno\"]\n",
		"server di repo non definito": "default = \"p\"\n[profiles.p]\n[[profiles.p.repos]]\ndir = \"~/x\"\nmcp = [\"nessuno\"]\n",
		"né command né url":           "default = \"p\"\n[mcp.x]\nargs = [\"a\"]\n[profiles.p]\n",
		"command e url insieme":       "default = \"p\"\n[mcp.x]\ncommand = \"a\"\nurl = \"http://b\"\n[profiles.p]\n",
		"repo senza dir":              "default = \"p\"\n[profiles.p]\n[[profiles.p.repos]]\nmcp = []\n",
	}
	for name, body := range cases {
		if _, err := Load(writeConfig(t, body), t.TempDir()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

const secretsSample = `
default = "personal"

[mcp.budget]
command = "uv"
args = ["tool", "run", "mcp-budget"]
env.BUDGET_API_KEY = "op://Personal/BUDGET/credential"
env.LOG_LEVEL = "info"

[profiles.personal]
mcp = ["budget"]

[profiles.personal.secrets]
OPENAI_API_KEY = "env:PERSONAL_OPENAI_API_KEY"

[profiles.client-d]
dirs = ["~/Developer/client-d"]

[[profiles.client-d.repos]]
dir = "~/Developer/client-d/playbook"
mcp = ["budget"]
`

func TestSecretsFor(t *testing.T) {
	home := t.TempDir()
	cfg, err := Load(writeConfig(t, secretsSample), home)
	if err != nil {
		t.Fatal(err)
	}
	got := cfg.SecretsFor(cfg.Profiles["personal"])
	if len(got) != 1 || got["OPENAI_API_KEY"] != "env:PERSONAL_OPENAI_API_KEY" {
		t.Errorf("only the profile's provider keys are resolved at launch, got %v", got)
	}
}

func TestBitwardenAccountNames(t *testing.T) {
	home := t.TempDir()
	ok := map[string]string{
		"account del profilo":     "default = \"p\"\n[profiles.p]\nbitwarden = \"client-e\"\nsecrets.K = \"bw://Mistral\"\n",
		"account nel riferimento": "default = \"p\"\n[profiles.p]\nsecrets.K = \"bw://client-e/Mistral\"\n",
		"server MCP con bw://":    "default = \"p\"\n[mcp.x]\ncommand = \"x\"\nenv.K = \"bw://client-e/x\"\n[profiles.p]\n",
	}
	for name, body := range ok {
		if _, err := Load(writeConfig(t, body), home); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, bad := range map[string]string{
		"bw:// senza account":                  "default = \"p\"\n[profiles.p]\nsecrets.K = \"bw://x\"\n",
		"bw:// senza account in un server MCP": "default = \"p\"\n[mcp.x]\ncommand = \"x\"\nenv.K = \"bw://x\"\n[profiles.p]\n",
	} {
		if _, err := Load(writeConfig(t, bad), home); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadRejectsBadSecrets(t *testing.T) {
	cases := map[string]string{
		"valore in chiaro":  "default = \"p\"\n[profiles.p.secrets]\nK = \"sk-123\"\n",
		"nome non valido":   "default = \"p\"\n[profiles.p.secrets]\n\"1-K\" = \"env:X\"\n",
		"segreto in header": "default = \"p\"\n[mcp.x]\nurl = \"https://x\"\nheaders.Authorization = \"op://a/b/c\"\n[profiles.p]\n",
	}
	for name, body := range cases {
		if _, err := Load(writeConfig(t, body), t.TempDir()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadTools(t *testing.T) {
	home := t.TempDir()
	cfg, err := Load(writeConfig(t, `default = "p"
[profiles.p]
[profiles.client-a]
[profiles.client-a.tools.scw]
env.SCW_SECRET_KEY = "op://Client-a/Scaleway/secret key"
reason = "Scaleway CLI"
`), home)
	if err != nil {
		t.Fatal(err)
	}
	tool := cfg.Profiles["client-a"].Tools["scw"]
	if tool == nil || tool.Reason != "Scaleway CLI" || tool.Env["SCW_SECRET_KEY"] != "op://Client-a/Scaleway/secret key" {
		t.Errorf("tools.scw read as %+v", tool)
	}
	if len(cfg.Profiles["p"].Tools) != 0 {
		t.Errorf("a profile without tools has %v", cfg.Profiles["p"].Tools)
	}
}

func TestLoadRejectsBadTools(t *testing.T) {
	cases := map[string]string{
		"valore in chiaro":     "default = \"p\"\n[profiles.p.tools.scw]\nenv.SCW_SECRET_KEY = \"abc\"\n",
		"riferimento env:":     "default = \"p\"\n[profiles.p.tools.scw]\nenv.SCW_SECRET_KEY = \"env:X\"\n",
		"nome non valido":      "default = \"p\"\n[profiles.p.tools.scw]\nenv.\"1-K\" = \"op://a/b/c\"\n",
		"bw:// senza account":  "default = \"p\"\n[profiles.p.tools.scw]\nenv.K = \"bw://x\"\n",
		"nome con cartella":    "default = \"p\"\n[profiles.p.tools.\"a/scw\"]\nenv.K = \"op://a/b/c\"\n",
		"nome di un agente":    "default = \"p\"\n[profiles.p.tools.claude]\nenv.K = \"op://a/b/c\"\n",
		"nome di ai-secrets":   "default = \"p\"\n[profiles.p.tools.ai-secrets]\nenv.K = \"op://a/b/c\"\n",
		"tool senza variabili": "default = \"p\"\n[profiles.p.tools.scw]\nreason = \"x\"\n",
	}
	for name, body := range cases {
		if _, err := Load(writeConfig(t, body), t.TempDir()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadToolDenyArgs(t *testing.T) {
	cfg, err := Load(writeConfig(t, `default = "p"
[profiles.p.tools.scw]
env.SCW_SECRET_KEY = "op://a/b/c"
deny_args = [["config", "info"], ["config", "get", "secret-key"]]
`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if d := cfg.Profiles["p"].Tools["scw"].DenyArgs; len(d) != 2 || strings.Join(d[1], " ") != "config get secret-key" {
		t.Errorf("deny_args read as %v", d)
	}
	for name, body := range map[string]string{
		"prefisso vuoto":     "default = \"p\"\n[profiles.p.tools.scw]\nenv.K = \"op://a/b/c\"\ndeny_args = [[]]\n",
		"argomento vuoto":    "default = \"p\"\n[profiles.p.tools.scw]\nenv.K = \"op://a/b/c\"\ndeny_args = [[\"config\", \"\"]]\n",
		"argomento con flag": "default = \"p\"\n[profiles.p.tools.scw]\nenv.K = \"op://a/b/c\"\ndeny_args = [[\"-p\", \"x\"]]\n",
	} {
		if _, err := Load(writeConfig(t, body), t.TempDir()); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
