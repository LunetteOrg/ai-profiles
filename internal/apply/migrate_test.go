package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateDefault(t *testing.T) {
	home := t.TempDir()
	src := filepath.Join(home, ".claude")
	dst := filepath.Join(home, ".claude-personal")
	write(t, mk(t, filepath.Join(src, "projects", "-Users-io-a", "s1.jsonl")), "vecchia sessione\n")
	write(t, mk(t, filepath.Join(src, "projects", "-Users-io-a", "memory", "MEMORY.md")), "ricordo\n")
	write(t, mk(t, filepath.Join(src, "projects", "-Users-io-b", "s2.jsonl")), "versione di ~/.claude\n")
	write(t, mk(t, filepath.Join(dst, "projects", "-Users-io-b", "s2.jsonl")), "versione del profilo\n")
	write(t, mk(t, filepath.Join(src, "file-history", "x", "1")), "f\n")
	write(t, filepath.Join(src, "history.jsonl"),
		`{"display":"vecchio","timestamp":100}`+"\n"+`{"display":"comune","timestamp":300}`+"\n")
	write(t, filepath.Join(dst, "history.jsonl"),
		`{"display":"nuovo","timestamp":200}`+"\n"+`{"display":"comune","timestamp":300}`+"\n")

	dry, err := MigrateDefault(home, "personal", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Actions) == 0 {
		t.Fatalf("dry run should list the copies")
	}
	if _, err := os.Stat(filepath.Join(dst, "projects", "-Users-io-a")); err == nil {
		t.Fatalf("dry run copied files")
	}

	rep, err := MigrateDefault(home, "personal", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"projects/-Users-io-a/s1.jsonl", "projects/-Users-io-a/memory/MEMORY.md", "file-history/x/1"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("%s not copied: %v", p, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "projects", "-Users-io-b", "s2.jsonl")); string(b) != "versione del profilo\n" {
		t.Errorf("a file already in the profile must not be overwritten, got %q", b)
	}
	if !hasNote(rep, "1 file") {
		t.Errorf("files left as they were should be counted: %v", rep.Notes)
	}
	h, _ := os.ReadFile(filepath.Join(dst, "history.jsonl"))
	want := `{"display":"vecchio","timestamp":100}` + "\n" + `{"display":"nuovo","timestamp":200}` + "\n" + `{"display":"comune","timestamp":300}` + "\n"
	if string(h) != want {
		t.Errorf("history:\n%s\nwant:\n%s", h, want)
	}
	if _, err := os.Stat(filepath.Join(src, "projects", "-Users-io-a", "s1.jsonl")); err != nil {
		t.Errorf("~/.claude must be left untouched")
	}
	if again, _ := MigrateDefault(home, "personal", false); len(again.Actions) != 0 {
		t.Errorf("a second migration should copy nothing, got %v", again.Actions)
	}
	if _, err := MigrateDefault(home, "manca", false); err == nil || !strings.Contains(err.Error(), "claude-manca") {
		t.Errorf("a missing profile folder is an error, got %v", err)
	}
}

func mk(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}
