package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// shellKinds are the startup files each kind of shell reads, in order: zsh
// as a login shell (the apps that start agents), zsh as an interactive login
// shell (Terminal), bash as a login shell (Orca's bash -lc), which reads only
// the first of its three files that exists.
func shellKinds(home string) [][]string {
	kinds := [][]string{
		{".zshenv", ".zprofile", ".zlogin"},
		{".zshenv", ".zprofile", ".zshrc", ".zlogin"},
	}
	for _, f := range []string{".bash_profile", ".bash_login", ".profile"} {
		if _, err := os.Stat(filepath.Join(home, f)); err == nil {
			kinds = append(kinds, []string{f})
			break
		}
	}
	return kinds
}

// pathLine is a line of a startup file that adds folders to PATH.
type pathLine struct {
	file    string // absolute path
	n       int    // 1-based line number
	text    string
	dirs    []string // folders added, expanded
	prepend bool     // the folders go before the PATH so far
}

var (
	exportPath = regexp.MustCompile(`^\s*(?:export\s+)?PATH=(.*)$`)
	arrayPath  = regexp.MustCompile(`^\s*path=\((.*)\)\s*$`)
)

// parsePathLine reads the folders a line adds to PATH, in the forms
// PATH="a:b:$PATH", PATH="$PATH:a" and path=(a $path); ok is false for any
// other line.
func parsePathLine(line, home string) (dirs []string, prepend, ok bool) {
	var parts []string
	var self string
	if m := exportPath.FindStringSubmatch(line); m != nil {
		v := strings.TrimSpace(m[1])
		if i := strings.IndexAny(v, " ;#"); i >= 0 && !strings.HasPrefix(v, `"`) && !strings.HasPrefix(v, "'") {
			v = v[:i]
		}
		parts = strings.Split(strings.Trim(v, `"'`), ":")
		self = "PATH"
	} else if m := arrayPath.FindStringSubmatch(line); m != nil {
		parts = strings.Fields(m[1])
		self = "path"
	} else {
		return nil, false, false
	}
	isSelf := func(s string) bool {
		s = strings.Trim(s, `"'`)
		return s == "$"+self || s == "${"+self+"}" || s == "$"+self+"[@]" || s == "${"+self+"[@]}"
	}
	switch {
	case len(parts) > 1 && isSelf(parts[len(parts)-1]):
		prepend, parts = true, parts[:len(parts)-1]
	case len(parts) > 1 && isSelf(parts[0]):
		parts = parts[1:]
	default:
		return nil, false, false
	}
	for _, p := range parts {
		p = strings.Trim(p, `"'`)
		for _, h := range []string{"$HOME", "${HOME}", "~"} {
			if p == h || strings.HasPrefix(p, h+"/") {
				p = home + p[len(h):]
				break
			}
		}
		if strings.Contains(p, "$") || !filepath.IsAbs(p) {
			continue // a folder that depends on other variables
		}
		dirs = append(dirs, filepath.Clean(p))
	}
	return dirs, prepend, len(dirs) > 0
}

// startupPathChecks finds the lines of the startup files that put a folder
// holding one of agents before the ai-profile shims: after the last line
// that adds the shims, or anywhere when a kind of shell has no such line and
// takes the shims from the PATH it inherits. There the agent starts without
// ai-profile.
func startupPathChecks(home, self string, agents []string, stamp string, tilde func(string) string) []Result {
	kinds := shellKinds(home)
	var files []string
	for _, k := range kinds {
		for _, f := range k {
			if !slices.Contains(files, f) {
				files = append(files, f)
			}
		}
	}
	var lines []pathLine
	for _, name := range files {
		f := filepath.Join(home, name)
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for i, text := range strings.Split(string(b), "\n") {
			if dirs, prepend, ok := parsePathLine(text, home); ok {
				lines = append(lines, pathLine{file: f, n: i + 1, text: text, dirs: dirs, prepend: prepend})
			}
		}
	}
	holdsShim := func(dir string) bool {
		for _, a := range agents {
			if sameFile(filepath.Join(dir, a), self) {
				return true
			}
		}
		return false
	}
	realAgent := func(dir string) string {
		for _, a := range agents {
			p := filepath.Join(dir, a)
			if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 && !sameFile(p, self) {
				return a
			}
		}
		return ""
	}
	isShims := func(l pathLine) bool { return l.prepend && slices.ContainsFunc(l.dirs, holdsShim) }
	// The shims line the fix adds, in a form both bash and zsh read.
	shimsText := ""
	if i := slices.IndexFunc(lines, isShims); i >= 0 {
		dir := lines[i].dirs[slices.IndexFunc(lines[i].dirs, holdsShim)]
		if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
			dir = "$HOME/" + rel
		}
		shimsText = fmt.Sprintf(`export PATH="%s:$PATH"`, dir)
	}

	type badLine struct {
		dir, agent string
		kinds      [][]int // for each kind the line is wrong in, the other lines of the kind
	}
	bad := map[int]*badLine{}
	for _, kind := range kinds {
		var seq []int
		for i, l := range lines {
			if slices.Contains(kind, filepath.Base(l.file)) {
				seq = append(seq, i)
			}
		}
		from := 0
		for k, i := range seq {
			if isShims(lines[i]) {
				from = k + 1
			}
		}
		for _, i := range seq[from:] {
			if !lines[i].prepend {
				continue
			}
			for _, d := range lines[i].dirs {
				if a := realAgent(d); a != "" {
					if bad[i] == nil {
						bad[i] = &badLine{dir: d, agent: a}
					}
					bad[i].kinds = append(bad[i].kinds, slices.DeleteFunc(slices.Clone(seq), func(j int) bool { return j == i }))
					break
				}
			}
		}
	}
	// The line can go when, in every kind of shell it is wrong in, a right
	// line adds the same folder; otherwise the fix adds the shims again
	// right after it.
	removable := func(b *badLine) bool {
		for _, others := range b.kinds {
			if !slices.ContainsFunc(others, func(j int) bool { return bad[j] == nil && slices.Contains(lines[j].dirs, b.dir) }) {
				return false
			}
		}
		return true
	}
	var res []Result
	for i, l := range lines {
		b := bad[i]
		if b == nil {
			continue
		}
		r := Result{Area: "PATH", Status: Warn, Msg: fmt.Sprintf("%s:%d puts %s, where %s is, before the shims: %s starts without ai-profile in new shells", tilde(l.file), l.n, tilde(b.dir), b.agent, b.agent)}
		switch {
		case len(l.dirs) == 1 && removable(b):
			r.Fix = func() (string, error) { return removePathLine(l, stamp, tilde) }
		case shimsText != "":
			r.Fix = func() (string, error) { return addShimsAfter(l, shimsText, stamp, tilde) }
		default:
			r.Msg += "; add the shims to PATH after it"
		}
		res = append(res, r)
	}
	return res
}

// removePathLine removes the line, and the comment right above it, from its
// file, keeping a copy of the file.
func removePathLine(l pathLine, stamp string, tilde func(string) string) (string, error) {
	err := editLines(l, stamp, tilde, func(text []string, i int) []string {
		start := i
		for start > 0 && strings.HasPrefix(strings.TrimSpace(text[start-1]), "#") {
			start--
		}
		return slices.Delete(text, start, i+1)
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("removed %q from %s (copy in %s); open a new shell", strings.TrimSpace(l.text), tilde(l.file), tilde(backupPath(l.file, stamp))), nil
}

// addShimsAfter adds the shims line right after the line, keeping a copy of
// the file.
func addShimsAfter(l pathLine, shims, stamp string, tilde func(string) string) (string, error) {
	err := editLines(l, stamp, tilde, func(text []string, i int) []string {
		return slices.Insert(text, i+1, "# ai-profile shims first (ai-profile doctor --fix)", shims)
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("added %q after line %d of %s (copy in %s); open a new shell", shims, l.n, tilde(l.file), tilde(backupPath(l.file, stamp))), nil
}

func backupPath(file, stamp string) string { return file + ".ai-profile-" + stamp }

// editLines rewrites the file of l with edit, which gets the lines and the
// index of l; one copy of the file per run keeps it as it was before the
// first fix.
func editLines(l pathLine, stamp string, tilde func(string) string, edit func([]string, int) []string) error {
	b, err := os.ReadFile(l.file)
	if err != nil {
		return err
	}
	text := strings.Split(string(b), "\n")
	i := l.n - 1
	if i >= len(text) || text[i] != l.text {
		if i = slices.Index(text, l.text); i < 0 {
			return fmt.Errorf("%s changed: the line is no longer there", tilde(l.file))
		}
	}
	bak := backupPath(l.file, stamp)
	if _, err := os.Stat(bak); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(bak, b, 0o644); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	info, err := os.Stat(l.file)
	if err != nil {
		return err
	}
	return os.WriteFile(l.file, []byte(strings.Join(edit(text, i), "\n")), info.Mode().Perm())
}
