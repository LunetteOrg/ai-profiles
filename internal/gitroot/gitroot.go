// Package gitroot maps a git worktree back to its main repository, so a
// worktree created outside a client folder still gets the client's profile.
package gitroot

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// LookupDir returns the folder to use for profile lookup. Inside a linked
// worktree it is the main repository; otherwise dir itself.
func LookupDir(dir string) string {
	top, err1 := gitOut(dir, "rev-parse", "--path-format=absolute", "--show-toplevel")
	common, err2 := gitOut(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err1 != nil || err2 != nil || filepath.Base(common) != ".git" {
		return dir
	}
	main := filepath.Dir(common)
	if same(main, top) {
		return dir
	}
	return main
}

// Tracked reports whether rel, relative to repo, is in the git index.
func Tracked(repo, rel string) bool {
	_, err := gitOut(repo, "ls-files", "--error-unmatch", "--", rel)
	return err == nil
}

// Ignored reports whether git ignores rel, relative to repo.
func Ignored(repo, rel string) bool {
	_, err := gitOut(repo, "check-ignore", "-q", "--", rel)
	return err == nil
}

// ExcludeFile is the repository's info/exclude: patterns git ignores in this
// clone only, without touching .gitignore. Worktrees share the main one.
func ExcludeFile(repo string) (string, error) {
	p, err := gitOut(repo, "rev-parse", "--path-format=absolute", "--git-path", "info/exclude")
	if err != nil {
		return "", fmt.Errorf("%s is not in a git repo", repo)
	}
	return p, nil
}

func gitOut(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func same(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}
