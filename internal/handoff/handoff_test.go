package handoff

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, p, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestProjectDir(t *testing.T) {
	if got := ProjectDir("/Users/io/Developer/ilmeskio/claude-profiles"); got != "-Users-io-Developer-ilmeskio-claude-profiles" {
		t.Errorf("got %q", got)
	}
	if got := ProjectDir("/private/tmp/x_y.z"); got != "-private-tmp-x-y-z" {
		t.Errorf("got %q", got)
	}
}

const session = `{"type":"user","message":{"role":"user","content":"Aggiungi il login con Google"},"cwd":"/r"}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"..."},{"type":"text","text":"Parto dal controller."},{"type":"tool_use","name":"Edit","input":{"file_path":"/r/app/auth.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Write","input":{"file_path":"/r/app/google.go"}},{"type":"tool_use","name":"Edit","input":{"file_path":"/r/app/auth.go"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"ora i test"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Test scritti, due falliscono."}]}}
{"type":"attachment"}
`

func TestFindAndCopySession(t *testing.T) {
	home := t.TempDir()
	cwd := "/r"
	src := filepath.Join(home, ".claude-client-d")
	dst := filepath.Join(home, ".claude-personal")
	proj := filepath.Join(src, "projects", ProjectDir(cwd))
	write(t, filepath.Join(proj, "vecchia.jsonl"), "{}\n")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(filepath.Join(proj, "vecchia.jsonl"), old, old)
	write(t, filepath.Join(proj, "nuova.jsonl"), session)
	write(t, filepath.Join(proj, "nuova", "subagents", "a.jsonl"), "{}\n")

	path, err := FindSession(src, cwd, "")
	if err != nil || filepath.Base(path) != "nuova.jsonl" {
		t.Fatalf("latest session: %q, %v", path, err)
	}
	if p, err := FindSession(src, cwd, "vecchia"); err != nil || filepath.Base(p) != "vecchia.jsonl" {
		t.Errorf("by id: %q, %v", p, err)
	}
	if _, err := FindSession(src, "/altrove", ""); err == nil {
		t.Errorf("a folder without sessions is an error")
	}

	id, err := CopySession(path, dst, cwd)
	if err != nil || id != "nuova" {
		t.Fatalf("copy: %q, %v", id, err)
	}
	for _, p := range []string{"nuova.jsonl", "nuova/subagents/a.jsonl"} {
		if _, err := os.Stat(filepath.Join(dst, "projects", ProjectDir(cwd), p)); err != nil {
			t.Errorf("%s not copied: %v", p, err)
		}
	}
	if _, err := CopySession(path, dst, cwd); err != nil {
		t.Errorf("copying again (resuming twice) is fine: %v", err)
	}
}

func TestSummary(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	write(t, p, session)
	s, err := Summary(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Aggiungi il login con Google", "ora i test", "Test scritti, due falliscono.", "/r/app/auth.go", "/r/app/google.go"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary misses %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "/r/app/auth.go") != 1 {
		t.Errorf("each file once:\n%s", s)
	}
	if strings.Contains(s, "...") || strings.Contains(s, "tool_result") {
		t.Errorf("thinking and tool results stay out:\n%s", s)
	}
}
