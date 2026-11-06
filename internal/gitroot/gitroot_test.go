package gitroot

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func eval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLookupDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "clienti", "repo")
	if err := os.MkdirAll(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "init", "-q")
	git(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	outside := filepath.Join(base, "altrove", "wt")
	git(t, repo, "worktree", "add", "-q", outside)
	plain := filepath.Join(base, "niente-git")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		filepath.Join(repo, "src"): filepath.Join(repo, "src"), // inside the main repo: unchanged
		outside:                    repo,                       // worktree elsewhere: the main repo
		plain:                      plain,                      // not a repo: unchanged
	}
	for in, want := range cases {
		if got := LookupDir(in); eval(t, got) != eval(t, want) {
			t.Errorf("LookupDir(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestTrackedIgnoredExclude(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q")
	os.WriteFile(filepath.Join(repo, "opencode.json"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignorato.json\n"), 0o644)
	git(t, repo, "add", "opencode.json", ".gitignore")

	if !Tracked(repo, "opencode.json") {
		t.Errorf("opencode.json is tracked")
	}
	if Tracked(repo, "nuovo.json") {
		t.Errorf("nuovo.json is not tracked")
	}
	if !Ignored(repo, "ignorato.json") || Ignored(repo, "nuovo.json") {
		t.Errorf("Ignored is wrong")
	}

	path, err := ExcludeFile(repo)
	if err != nil {
		t.Fatal(err)
	}
	if eval(t, filepath.Dir(filepath.Dir(path))) != eval(t, filepath.Join(repo, ".git")) {
		t.Errorf("exclude file = %s", path)
	}
	if _, err := ExcludeFile(t.TempDir()); err == nil {
		t.Errorf("a folder outside git has no exclude file")
	}
}
