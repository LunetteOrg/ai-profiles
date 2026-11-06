package private

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestPackUnpack(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, ".config", "tool", "private.toml")
	notes := filepath.Join(home, "Developer", "acme", "AGENTS.md")
	write(t, cfg, "secret = 1\n")
	write(t, notes, "# Acme\n")

	data, err := Pack(home, []string{cfg, "~/Developer/acme/AGENTS.md"})
	if err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	res, err := Unpack(other, data, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(other, ".config", "tool", "private.toml")); got != "secret = 1\n" {
		t.Errorf("private.toml = %q", got)
	}
	if got := read(t, filepath.Join(other, "Developer", "acme", "AGENTS.md")); got != "# Acme\n" {
		t.Errorf("AGENTS.md = %q", got)
	}
	if len(res.Written) != 2 {
		t.Errorf("written = %v", res.Written)
	}
	info, _ := os.Stat(filepath.Join(other, ".config", "tool", "private.toml"))
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode().Perm())
	}
}

func TestPackRefusesOutsideHome(t *testing.T) {
	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x")
	write(t, outside, "x")
	if _, err := Pack(home, []string{outside}); err == nil {
		t.Error("a file outside home must be refused")
	}
}

func TestPackRefusesMissingFile(t *testing.T) {
	home := t.TempDir()
	if _, err := Pack(home, []string{"~/nope"}); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("a missing file must be named, got %v", err)
	}
}

func TestUnpackKeepsDifferentFiles(t *testing.T) {
	home := t.TempDir()
	f := filepath.Join(home, "a.toml")
	write(t, f, "archived\n")
	same := filepath.Join(home, "b.toml")
	write(t, same, "same\n")
	data, err := Pack(home, []string{f, same})
	if err != nil {
		t.Fatal(err)
	}
	write(t, f, "local\n")

	res, err := Unpack(home, data, false)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, f) != "local\n" {
		t.Error("a file that differs must not be overwritten without force")
	}
	if !slices.Equal(res.Kept, []string{f}) || !slices.Equal(res.Same, []string{same}) || len(res.Written) != 0 {
		t.Errorf("result = %+v", res)
	}

	res, err = Unpack(home, data, true)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, f) != "archived\n" || !slices.Equal(res.Written, []string{f}) {
		t.Errorf("force must overwrite: %q, %+v", read(t, f), res)
	}
}

func TestUnpackRefusesEscapingNames(t *testing.T) {
	home := t.TempDir()
	data, err := packEntries(map[string][]byte{"../evil": []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Unpack(home, data, false); err == nil {
		t.Error("an entry outside home must be refused")
	}
}

func TestBackupAndRestore(t *testing.T) {
	home := t.TempDir()
	store := filepath.Join(t.TempDir(), "store.tgz")
	f := filepath.Join(home, ".config", "tool", "private.toml")
	write(t, f, "v1\n")
	s := Settings{
		File:    f,
		Backup:  "cat > " + store,
		Restore: "cat " + store,
	}
	if err := s.Save(home, nil); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.Changed(home, nil); err != nil || len(changed) != 0 {
		t.Errorf("nothing changed right after a backup: %v %v", changed, err)
	}
	write(t, f, "v2\n")
	if changed, _ := s.Changed(home, nil); !slices.Equal(changed, []string{f}) {
		t.Errorf("changed = %v", changed)
	}

	os.Remove(f)
	res, err := s.Fetch(home, false)
	if err != nil {
		t.Fatal(err)
	}
	if read(t, f) != "v1\n" || len(res.Written) != 1 {
		t.Errorf("restore = %q, %+v", read(t, f), res)
	}
}

func TestChangedBeforeAnyBackup(t *testing.T) {
	home := t.TempDir()
	f := filepath.Join(home, "private.toml")
	write(t, f, "x")
	s := Settings{File: f, Backup: "true", Restore: "true"}
	if changed, _ := s.Changed(home, []string{"~/missing"}); !slices.Equal(changed, []string{f, filepath.Join(home, "missing")}) {
		t.Errorf("without a backup every file counts as changed, got %v", changed)
	}
}

func TestCommandFailure(t *testing.T) {
	home := t.TempDir()
	f := filepath.Join(home, "private.toml")
	write(t, f, "x")
	s := Settings{File: f, Backup: "exit 3", Restore: "exit 4"}
	if err := s.Save(home, nil); err == nil {
		t.Error("a failing backup command must fail")
	}
	if _, err := s.Fetch(home, false); err == nil {
		t.Error("a failing restore command must fail")
	}
	if err := (Settings{File: f}).Save(home, nil); err == nil || !strings.Contains(err.Error(), "backup") {
		t.Errorf("a missing backup command must be named, got %v", err)
	}
}

func TestSection(t *testing.T) {
	home := t.TempDir()
	cfg := filepath.Join(home, "config.toml")
	write(t, cfg, "default = \"x\"\n[private]\nfile = \"~/p.toml\"\nrestore = \"r\"\nbackup = \"b\"\n")
	s, err := Section(cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	if s.File != filepath.Join(home, "p.toml") || s.Restore != "r" || s.Backup != "b" {
		t.Errorf("section = %+v", s)
	}
	write(t, cfg, "default = \"x\"\n")
	if s, err := Section(cfg, home); err != nil || s.File != "" {
		t.Errorf("no section: %+v %v", s, err)
	}
}
