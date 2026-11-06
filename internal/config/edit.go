package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The editors below change config.toml line by line, so comments, order and
// spacing stay as the user wrote them. They handle lists written on one line;
// anything else is left for a manual edit.

// AddToProfile adds value to the list key of [profiles.<profile>].
func AddToProfile(src []byte, profile, key, value string) ([]byte, bool, error) {
	lines := splitLines(src)
	start, end, ok := findTable(lines, "profiles."+profile, false, nil)
	if !ok {
		return nil, false, fmt.Errorf("[profiles.%s] not found: add it by hand", profile)
	}
	return editList(lines, start, end, key, func(items []string) []string {
		if slices.Contains(items, value) {
			return nil
		}
		return append(items, value)
	})
}

// AddToRepo adds value to the list key of the profile's repo dir, appending
// a new [[profiles.<profile>.repos]] when the repo is not there yet.
func AddToRepo(src []byte, home, profile, dir, key, value string) ([]byte, bool, error) {
	lines := splitLines(src)
	start, end, ok := findTable(lines, "profiles."+profile+".repos", true, repoIs(home, dir))
	if !ok {
		var b strings.Builder
		b.WriteString(strings.TrimRight(string(src), "\n"))
		fmt.Fprintf(&b, "\n\n[[profiles.%s.repos]]\ndir = %s\n%s = [%s]\n", profile, strconv.Quote(tildeOf(dir, home)), key, strconv.Quote(value))
		return []byte(b.String()), true, nil
	}
	return editList(lines, start, end, key, func(items []string) []string {
		if slices.Contains(items, value) {
			return nil
		}
		return append(items, value)
	})
}

// RemoveFromRepo removes value from the list key of the profile's repo dir.
func RemoveFromRepo(src []byte, home, profile, dir, key, value string) ([]byte, bool, error) {
	lines := splitLines(src)
	start, end, ok := findTable(lines, "profiles."+profile+".repos", true, repoIs(home, dir))
	if !ok {
		return src, false, nil
	}
	return editList(lines, start, end, key, func(items []string) []string {
		if !slices.Contains(items, value) {
			return nil
		}
		return slices.DeleteFunc(items, func(s string) bool { return s == value })
	})
}

// RemoveDirFromProfile removes dir from the dirs of [profiles.<profile>].
func RemoveDirFromProfile(src []byte, home, profile, dir string) ([]byte, bool, error) {
	lines := splitLines(src)
	start, end, ok := findTable(lines, "profiles."+profile, false, nil)
	if !ok {
		return nil, false, fmt.Errorf("[profiles.%s] not found: edit it by hand", profile)
	}
	return editList(lines, start, end, "dirs", func(items []string) []string {
		out := slices.DeleteFunc(slices.Clone(items), func(s string) bool { return Expand(s, home) == filepath.Clean(dir) })
		if len(out) == len(items) {
			return nil
		}
		return out
	})
}

// AddToFolder adds profiles to the [[folders]] entry of dir, appending the
// entry when dir has none.
func AddToFolder(src []byte, home, dir string, profiles []string) ([]byte, bool, error) {
	lines := splitLines(src)
	start, end, ok := findTable(lines, "folders", true, repoIs(home, dir))
	if !ok {
		var b strings.Builder
		b.WriteString(strings.TrimRight(string(src), "\n"))
		fmt.Fprintf(&b, "\n\n[[folders]]\ndir = %s\nprofiles = %s\n", strconv.Quote(tildeOf(dir, home)), formatList(profiles))
		return []byte(b.String()), true, nil
	}
	return editList(lines, start, end, "profiles", func(items []string) []string {
		out := slices.Clone(items)
		for _, p := range profiles {
			if !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
		if len(out) == len(items) {
			return nil
		}
		return out
	})
}

// RemoveFromFolder removes profiles from the [[folders]] entry of dir; the
// entry goes whole when no profile is named or none is left.
func RemoveFromFolder(src []byte, home, dir string, profiles []string) ([]byte, bool, error) {
	lines := splitLines(src)
	start, end, ok := findTable(lines, "folders", true, repoIs(home, dir))
	if !ok {
		return nil, false, nil
	}
	if len(profiles) > 0 {
		out, changed, err := editList(lines, start, end, "profiles", func(items []string) []string {
			out := slices.DeleteFunc(slices.Clone(items), func(s string) bool { return slices.Contains(profiles, s) })
			if len(out) == len(items) {
				return nil
			}
			return out
		})
		if err != nil || !changed {
			return out, changed, err
		}
		// editList drops the key of a list left empty: then the entry goes.
		lines = splitLines(out)
		_, end, _ = findTable(lines, "folders", true, repoIs(home, dir))
		if slices.ContainsFunc(lines[start:end], func(l string) bool { return profilesKey.MatchString(l) }) {
			return out, true, nil
		}
	}
	lines = slices.Delete(lines, start-1, end)
	if start-1 == len(lines) {
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
	}
	return joinLines(lines), true, nil
}

var profilesKey = regexp.MustCompile(`^\s*profiles\s*=`)

// EditFile applies edit to file, config.toml at cfgPath or its private file,
// checks that the configuration still loads, and replaces the file.
func EditFile(cfgPath, file, home string, edit func([]byte) ([]byte, bool, error)) error {
	src, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	out, changed, err := edit(src)
	if err != nil || !changed {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if _, err := load(cfgPath, home, map[string]string{file: tmp.Name()}); err != nil {
		return fmt.Errorf("the change would make the configuration invalid: %w", err)
	}
	if info, err := os.Stat(file); err == nil {
		os.Chmod(tmp.Name(), info.Mode().Perm())
	}
	return os.Rename(tmp.Name(), file)
}

func splitLines(src []byte) []string {
	return strings.Split(strings.TrimSuffix(string(src), "\n"), "\n")
}

func joinLines(lines []string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

var headerRe = regexp.MustCompile(`^\s*(\[\[?)\s*([^\]]+?)\s*\]\]?\s*(#.*)?$`)

// tableName normalizes a header: quotes around segments are dropped.
func tableName(raw string) string {
	parts := strings.Split(raw, ".")
	for i, p := range parts {
		parts[i] = strings.Trim(strings.TrimSpace(p), `"'`)
	}
	return strings.Join(parts, ".")
}

// findTable returns the line range [start, end) of the body of the table
// name (an array table when array is true) for which match, if given, accepts
// the body.
func findTable(lines []string, name string, array bool, match func([]string) bool) (int, int, bool) {
	for i := 0; i < len(lines); i++ {
		m := headerRe.FindStringSubmatch(lines[i])
		if m == nil || (m[1] == "[[") != array || tableName(m[2]) != name {
			continue
		}
		end := i + 1
		for end < len(lines) && headerRe.FindStringSubmatch(lines[end]) == nil {
			end++
		}
		if match == nil || match(lines[i+1:end]) {
			return i + 1, end, true
		}
	}
	return 0, 0, false
}

var dirRe = regexp.MustCompile(`^\s*dir\s*=\s*"([^"]*)"`)

func repoIs(home, dir string) func([]string) bool {
	return func(body []string) bool {
		for _, l := range body {
			if m := dirRe.FindStringSubmatch(l); m != nil {
				return Expand(m[1], home) == filepath.Clean(dir)
			}
		}
		return false
	}
}

func tildeOf(p, home string) string {
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return p
}

var itemRe = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)

// editList rewrites the one-line list key in lines[start:end] with change;
// change returns nil when nothing has to change.
func editList(lines []string, start, end int, key string, change func([]string) []string) ([]byte, bool, error) {
	keyRe := regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)(.*)$`)
	listRe := regexp.MustCompile(`^\[(.*)\](\s*(?:#.*)?)$`)
	for i := start; i < end; i++ {
		m := keyRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		lm := listRe.FindStringSubmatch(m[2])
		if lm == nil {
			return nil, false, fmt.Errorf("%s is not a single-line list (%q): edit it by hand", key, strings.TrimSpace(lines[i]))
		}
		var items []string
		for _, im := range itemRe.FindAllStringSubmatch(lm[1], -1) {
			v, err := strconv.Unquote(`"` + im[1] + `"`)
			if err != nil {
				return nil, false, fmt.Errorf("%s: value %q is unreadable: edit it by hand", key, im[0])
			}
			items = append(items, v)
		}
		next := change(items)
		switch {
		case next == nil:
			return nil, false, nil
		case len(next) == 0:
			// An empty list says what a missing key says.
			lines = slices.Delete(lines, i, i+1)
		default:
			lines[i] = m[1] + formatList(next) + lm[2]
		}
		return joinLines(lines), true, nil
	}
	next := change(nil)
	if next == nil {
		return nil, false, nil
	}
	at := end
	for at > start && strings.TrimSpace(lines[at-1]) == "" {
		at--
	}
	lines = slices.Insert(lines, at, key+" = "+formatList(next))
	return joinLines(lines), true, nil
}

func formatList(items []string) string {
	q := make([]string, len(items))
	for i, it := range items {
		q[i] = strconv.Quote(it)
	}
	return "[" + strings.Join(q, ", ") + "]"
}

// NewProfile is a profile to append with AppendProfile.
type NewProfile struct {
	Name    string
	Account string
	Dirs    []string // absolute
	Agents  []string
}

var profileName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// AppendProfile adds [profiles.<name>] at the end of the config. Agents equal
// to the default (claude alone) are not written.
func AppendProfile(src []byte, home string, p NewProfile) ([]byte, error) {
	if !profileName.MatchString(p.Name) {
		return nil, fmt.Errorf("%q: a profile name uses lowercase letters, digits and hyphens", p.Name)
	}
	if slices.Contains(Subcommands, p.Name) || slices.Contains(KnownAgents, p.Name) {
		return nil, fmt.Errorf("%q is a reserved name", p.Name)
	}
	lines := splitLines(src)
	if _, _, ok := findTable(lines, "profiles."+p.Name, false, nil); ok {
		return nil, fmt.Errorf("profile %s already exists", p.Name)
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(string(src), "\n"))
	fmt.Fprintf(&b, "\n\n[profiles.%s]\n", p.Name)
	if p.Account != "" {
		fmt.Fprintf(&b, "account = %s\n", strconv.Quote(p.Account))
	}
	if len(p.Dirs) > 0 {
		dirs := make([]string, len(p.Dirs))
		for i, d := range p.Dirs {
			dirs[i] = tildeOf(d, home)
		}
		fmt.Fprintf(&b, "dirs = %s\n", formatList(dirs))
	}
	if len(p.Agents) > 0 && !slices.Equal(p.Agents, []string{"claude"}) {
		fmt.Fprintf(&b, "agents = %s\n", formatList(p.Agents))
	}
	return []byte(b.String()), nil
}

var defaultRe = regexp.MustCompile(`^(\s*default\s*=\s*)("[^"]*"|'[^']*')(.*)$`)

// SetDefault rewrites the top-level default = "…" line.
func SetDefault(src []byte, profile string) ([]byte, bool, error) {
	lines := splitLines(src)
	for i, l := range lines {
		if headerRe.MatchString(l) {
			break // top-level keys come before the first table
		}
		m := defaultRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if strings.Trim(m[2], `"'`) == profile {
			return nil, false, nil
		}
		lines[i] = m[1] + strconv.Quote(profile) + m[3]
		return joinLines(lines), true, nil
	}
	return nil, false, fmt.Errorf("default = … line not found before the tables: add it by hand")
}
