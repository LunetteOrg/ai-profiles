package apply

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/launch"
)

// migrated are the folders of ~/.claude that hold sessions, memory and file
// history; history.jsonl is merged separately.
var migrated = []string{"projects", "file-history", "todos"}

// MigrateDefault copies sessions, memory and history from ~/.claude into the
// profile folder: only files the profile does not have, nothing overwritten,
// nothing removed from ~/.claude.
func MigrateDefault(home, profile string, dryRun bool) (*Report, error) {
	src := filepath.Join(home, ".claude")
	dst := launch.ProfileDir(home, "claude", profile)
	if _, err := os.Stat(dst); err != nil {
		return nil, fmt.Errorf("%s does not exist: create it with ai-profile new %s", dst, profile)
	}
	s := &syncer{Options: Options{Home: home, DryRun: dryRun}, rep: &Report{}}
	kept := 0
	for _, dir := range migrated {
		root := filepath.Join(src, dir)
		if _, err := os.Stat(root); err != nil {
			continue
		}
		// One action per first-level folder (a project), not per file.
		counts := map[string]int{}
		var order []string
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(src, p)
			target := filepath.Join(dst, rel)
			if _, err := os.Lstat(target); err == nil {
				kept++
				return nil
			}
			group := filepath.Join(dir, strings.SplitN(strings.TrimPrefix(rel, dir+string(filepath.Separator)), string(filepath.Separator), 2)[0])
			if counts[group] == 0 {
				order = append(order, group)
			}
			counts[group]++
			if dryRun {
				return nil
			}
			return copyEntry(p, target, d)
		})
		if err != nil {
			return nil, err
		}
		for _, g := range order {
			s.act(filepath.Join(dst, g), "copy %s from %s", plural(counts[g], "file", "files"), s.tilde(filepath.Join(src, g)))
		}
	}
	if kept > 0 {
		s.note("%s already in %s: left as they are", plural(kept, "file", "files"), s.tilde(dst))
	}
	if err := s.mergeHistory(filepath.Join(src, "history.jsonl"), filepath.Join(dst, "history.jsonl")); err != nil {
		return nil, err
	}
	return s.rep, nil
}

func copyEntry(src, dst string, d fs.DirEntry) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	if d.Type()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chtimes(dst, info.ModTime(), info.ModTime())
}

// mergeHistory joins two history.jsonl files: every distinct line once,
// ordered by its timestamp.
func (s *syncer) mergeHistory(src, dst string) error {
	from, err := readLines(src)
	if err != nil || len(from) == 0 {
		return err
	}
	to, err := readLines(dst)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var all []string
	for _, l := range append(to, from...) {
		if l != "" && !seen[l] {
			seen[l] = true
			all = append(all, l)
		}
	}
	if len(all) == len(to) {
		return nil
	}
	sort.SliceStable(all, func(i, j int) bool { return timestamp(all[i]) < timestamp(all[j]) })
	s.act(dst, "merge %d history lines from %s", len(all)-len(to), s.tilde(src))
	if s.DryRun {
		return nil
	}
	return writeFile(dst, []byte(strings.Join(all, "\n")+"\n"))
}

func readLines(p string) ([]string, error) {
	f, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, sc.Err()
}

func timestamp(line string) float64 {
	var v struct {
		Timestamp float64 `json:"timestamp"`
	}
	json.Unmarshal([]byte(line), &v)
	return v.Timestamp
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
