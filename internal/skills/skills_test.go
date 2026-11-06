package skills

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func mkskill(t *testing.T, dir string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o644)
}

func setup(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	shared := filepath.Join(home, ".claude", "skills")
	for _, s := range []string{"humanizer", "budget", "synced/org_acc"} {
		mkskill(t, filepath.Join(shared, s))
	}
	mkskill(t, filepath.Join(home, ".agents", "skills", "orca-cli"))
	mkskill(t, filepath.Join(home, ".agents", "skills", "orchestration"))
	os.Symlink(filepath.Join(home, ".agents", "skills", "orca-cli"), filepath.Join(shared, "orca-cli"))
	prof := filepath.Join(home, ".claude-client-d")
	os.MkdirAll(prof, 0o700)
	os.Symlink(shared, filepath.Join(prof, "skills"))
	return home
}

func TestSplitAndSync(t *testing.T) {
	home := setup(t)
	prof := filepath.Join(home, ".claude-client-d")
	if Mode(home, "client-d") != Shared {
		t.Fatalf("a symlink to the shared folder is shared mode")
	}
	if err := Split(home, "client-d", []string{"budget"}); err != nil {
		t.Fatal(err)
	}
	if Mode(home, "client-d") != PerProfile {
		t.Fatalf("after split the profile has its own folder")
	}
	link := func(name string) string {
		target, _ := os.Readlink(filepath.Join(prof, "skills", name))
		return target
	}
	if link("humanizer") != filepath.Join(home, ".claude", "skills", "humanizer") || link("orca-cli") == "" {
		t.Errorf("shared skills should be linked")
	}
	if _, err := os.Lstat(filepath.Join(prof, "skills", "budget")); err == nil {
		t.Errorf("an excluded skill must not be linked")
	}
	if _, err := os.Lstat(filepath.Join(prof, "skills", "synced")); err == nil {
		t.Errorf("synced skills belong to each account, not shared")
	}

	// A skill of the profile, a new shared skill, a shared one removed.
	mkskill(t, filepath.Join(prof, "skills", "processi-client-d"))
	mkskill(t, filepath.Join(home, ".claude", "skills", "nuova"))
	os.RemoveAll(filepath.Join(home, ".claude", "skills", "humanizer"))
	changes, err := Sync(home, "client-d", []string{"budget"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Errorf("expected link nuova and unlink humanizer, got %v", changes)
	}
	if link("nuova") == "" {
		t.Errorf("new shared skill not linked")
	}
	if _, err := os.Lstat(filepath.Join(prof, "skills", "humanizer")); err == nil {
		t.Errorf("link to a removed skill should go")
	}
	if _, err := os.Stat(filepath.Join(prof, "skills", "processi-client-d", "SKILL.md")); err != nil {
		t.Errorf("the profile's own skill must stay")
	}

	// Excluding later removes the link.
	changes, _ = Sync(home, "client-d", []string{"budget", "nuova"}, false)
	if len(changes) != 1 {
		t.Errorf("excluding nuova: %v", changes)
	}
	if err := Split(home, "client-d", nil); err == nil {
		t.Errorf("splitting twice is an error")
	}
}

func TestShareAndLink(t *testing.T) {
	home := setup(t)
	prof := filepath.Join(home, ".claude-client-d")
	Split(home, "client-d", nil)
	mkskill(t, filepath.Join(prof, "skills", "mia"))
	if err := Share(home, "client-d", "mia"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "skills", "mia", "SKILL.md")); err != nil {
		t.Errorf("shared skill should move to ~/.claude/skills: %v", err)
	}
	if target, _ := os.Readlink(filepath.Join(prof, "skills", "mia")); target != filepath.Join(home, ".claude", "skills", "mia") {
		t.Errorf("the profile should link the shared copy, got %q", target)
	}

	if got := Unlinked(home); !slices.Equal(got, []string{"orchestration"}) {
		t.Errorf("Unlinked = %v", got)
	}
	if err := Link(home, "orchestration"); err != nil {
		t.Fatal(err)
	}
	if len(Unlinked(home)) != 0 {
		t.Errorf("orchestration should now be linked")
	}
	if err := Link(home, "manca"); err == nil {
		t.Errorf("linking a missing skill is an error")
	}
}

func TestList(t *testing.T) {
	home := setup(t)
	prof := filepath.Join(home, ".claude-client-d")
	Split(home, "client-d", []string{"budget"})
	mkskill(t, filepath.Join(prof, "skills", "processi-client-d"))
	got := map[string]string{}
	for _, s := range List(home, "client-d", []string{"budget"}, Origins{CLI: []string{"orca-cli"}}) {
		got[s.Name] = s.Origin
	}
	want := map[string]string{
		"humanizer":         "shared",
		"orca-cli":          "shared, from the skills CLI",
		"processi-client-d": "profile's own",
		"budget":            "excluded",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}
