package config

import (
	"strings"
	"testing"
)

const editSample = `default = "personal"

# server condivisi
[mcp.google]
command = "google"

[mcp.notion]
url = "https://mcp.notion.com/mcp"

[profiles.personal]
account = "a@b.it" # commento
mcp = ["google"]   # tenuto

[profiles.client-d]
dirs = ["~/Developer/client-d"]

[[profiles.client-d.repos]]
dir = "~/Developer/client-d/playbook"
plugins = ["posthog"]
`

func TestAddToProfile(t *testing.T) {
	out, changed, err := AddToProfile([]byte(editSample), "personal", "mcp", "notion")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(out), `mcp = ["google", "notion"]   # tenuto`) {
		t.Errorf("the list should grow in place, keeping the comment:\n%s", out)
	}
	if !strings.Contains(string(out), "# server condivisi") {
		t.Errorf("comments elsewhere must stay")
	}
	if _, changed, _ := AddToProfile(out, "personal", "mcp", "notion"); changed {
		t.Errorf("adding twice changes nothing")
	}

	out, _, err = AddToProfile([]byte(editSample), "client-d", "mcp", "google")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "dirs = [\"~/Developer/client-d\"]\nmcp = [\"google\"]\n") {
		t.Errorf("a missing key goes at the end of the profile's table:\n%s", out)
	}
	if _, _, err := AddToProfile([]byte(editSample), "nessuno", "mcp", "google"); err == nil {
		t.Errorf("an unknown profile is an error")
	}
}

func TestEditRepoLists(t *testing.T) {
	home := "/Users/io"
	out, _, err := AddToRepo([]byte(editSample), home, "client-d", "/Users/io/Developer/client-d/playbook", "plugins", "cloudflare")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `plugins = ["posthog", "cloudflare"]`) {
		t.Errorf("existing repo:\n%s", out)
	}

	out, _, err = AddToRepo(out, home, "client-d", "/Users/io/Developer/client-d/nuovo", "mcp", "notion")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "\n[[profiles.client-d.repos]]\ndir = \"~/Developer/client-d/nuovo\"\nmcp = [\"notion\"]\n") {
		t.Errorf("a new repo is appended:\n%s", out)
	}

	out, changed, err := RemoveFromRepo(out, home, "client-d", "/Users/io/Developer/client-d/playbook", "plugins", "posthog")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(out), `plugins = ["cloudflare"]`) {
		t.Errorf("remove:\n%s", out)
	}
	if _, changed, _ := RemoveFromRepo(out, home, "client-d", "/Users/io/Developer/client-d/assente", "plugins", "x"); changed {
		t.Errorf("removing from a missing repo changes nothing")
	}
}

func TestEditRefusesMultilineLists(t *testing.T) {
	src := "default = \"p\"\n[profiles.p]\nmcp = [\n  \"a\",\n]\n"
	if _, _, err := AddToProfile([]byte(src), "p", "mcp", "b"); err == nil || !strings.Contains(err.Error(), "by hand") {
		t.Errorf("a list on several lines should ask for a manual edit, got %v", err)
	}
}

func TestEditFileValidates(t *testing.T) {
	home := t.TempDir()
	p := writeConfig(t, editSample)
	err := EditFile(p, p, home, func(b []byte) ([]byte, bool, error) {
		return AddToProfile(b, "personal", "mcp", "inesistente")
	})
	if err == nil || !strings.Contains(err.Error(), "inesistente") {
		t.Errorf("an edit that breaks the config must be refused, got %v", err)
	}
	if err := EditFile(p, p, home, func(b []byte) ([]byte, bool, error) {
		return AddToProfile(b, "personal", "mcp", "notion")
	}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p, home)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Profiles["personal"].MCP; len(got) != 2 {
		t.Errorf("mcp = %v", got)
	}
}

func TestAppendProfile(t *testing.T) {
	home := "/Users/io"
	out, err := AppendProfile([]byte(editSample), home, NewProfile{
		Name:    "acme",
		Account: "io@acme.it",
		Dirs:    []string{"/Users/io/Developer/acme", "/altrove/acme"},
		Agents:  []string{"claude", "codex"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\n[profiles.acme]\naccount = \"io@acme.it\"\ndirs = [\"~/Developer/acme\", \"/altrove/acme\"]\nagents = [\"claude\", \"codex\"]\n"
	if !strings.HasSuffix(string(out), want) {
		t.Errorf("got:\n%s", out)
	}
	out, _ = AppendProfile([]byte(editSample), home, NewProfile{Name: "solo", Agents: []string{"claude"}})
	if !strings.HasSuffix(string(out), "\n[profiles.solo]\n") {
		t.Errorf("defaults are not written:\n%s", out)
	}
	if _, err := AppendProfile([]byte(editSample), home, NewProfile{Name: "client-d"}); err == nil {
		t.Errorf("an existing profile is an error")
	}
	for _, bad := range []string{"new", "claude", "Con Spazi", ""} {
		if _, err := AppendProfile([]byte(editSample), home, NewProfile{Name: bad}); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

func TestSetDefault(t *testing.T) {
	out, changed, err := SetDefault([]byte(editSample), "client-d")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !strings.HasPrefix(string(out), "default = \"client-d\"\n") || !strings.Contains(string(out), "# server condivisi") {
		t.Errorf("got:\n%s", out)
	}
	if _, changed, _ := SetDefault(out, "client-d"); changed {
		t.Errorf("same default: nothing to change")
	}
	src := "# commento\ndefault = 'personal' # scelto\n[profiles.personal]\n[profiles.client-b]\n"
	out, _, err = SetDefault([]byte(src), "client-b")
	if err != nil || !strings.Contains(string(out), "default = \"client-b\" # scelto\n") {
		t.Errorf("comment after the value should stay: %q, %v", out, err)
	}
	if _, _, err := SetDefault([]byte("[profiles.p]\n"), "p"); err == nil {
		t.Errorf("a config without a default line is an error")
	}
}

func TestAddToFolder(t *testing.T) {
	home := "/Users/io"
	src := editSample + `
[[folders]]
dir = "~/Developer/shared" # condivisa
profiles = ["personal"]
`
	out, changed, err := AddToFolder([]byte(src), home, "/Users/io/Developer/shared", []string{"client-d", "personal"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(out), `profiles = ["personal", "client-d"]`) {
		t.Errorf("the missing profiles are appended in place:\n%s", out)
	}
	if _, changed, _ := AddToFolder(out, home, "/Users/io/Developer/shared", []string{"client-d"}); changed {
		t.Errorf("adding a listed profile changes nothing")
	}

	out, _, err = AddToFolder([]byte(editSample), home, "/Users/io/Developer/shared", []string{"personal", "client-d"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "plugins = [\"posthog\"]\n\n[[folders]]\ndir = \"~/Developer/shared\"\nprofiles = [\"personal\", \"client-d\"]\n") {
		t.Errorf("a new folder is appended at the end:\n%s", out)
	}
}

func TestRemoveFromFolder(t *testing.T) {
	home := "/Users/io"
	src := editSample + `
[[folders]]
dir = "~/Developer/shared"
profiles = ["personal", "client-d"]

[[folders]]
dir = "~/Developer/other"
profiles = ["client-d"]
`
	out, changed, err := RemoveFromFolder([]byte(src), home, "/Users/io/Developer/shared", []string{"client-d"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if !strings.Contains(string(out), "dir = \"~/Developer/shared\"\nprofiles = [\"personal\"]\n") {
		t.Errorf("the profile is removed from the list:\n%s", out)
	}
	if _, changed, _ := RemoveFromFolder(out, home, "/Users/io/Developer/shared", []string{"client-d"}); changed {
		t.Errorf("removing an unlisted profile changes nothing")
	}

	out, _, err = RemoveFromFolder(out, home, "/Users/io/Developer/shared", []string{"personal"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "shared") || !strings.Contains(string(out), "plugins = [\"posthog\"]\n\n[[folders]]\ndir = \"~/Developer/other\"") {
		t.Errorf("a folder left without profiles is removed whole:\n%s", out)
	}

	out, _, err = RemoveFromFolder(out, home, "/Users/io/Developer/other", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "folders") || !strings.HasSuffix(string(out), "plugins = [\"posthog\"]\n") {
		t.Errorf("without profiles the whole folder goes:\n%s", out)
	}
	if _, changed, _ := RemoveFromFolder(out, home, "/Users/io/Developer/other", nil); changed {
		t.Errorf("removing a missing folder changes nothing")
	}
}

func TestRemoveDirFromProfile(t *testing.T) {
	home := "/Users/io"
	out, changed, err := RemoveDirFromProfile([]byte(editSample), home, "client-d", "/Users/io/Developer/client-d")
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if strings.Contains(string(out), "dirs") {
		t.Errorf("a dirs list left empty goes away:\n%s", out)
	}
	if _, changed, _ := RemoveDirFromProfile(out, home, "client-d", "/Users/io/Developer/client-d"); changed {
		t.Errorf("removing a missing dir changes nothing")
	}
}
