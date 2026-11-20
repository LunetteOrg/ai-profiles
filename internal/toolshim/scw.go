package toolshim

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// ScwConfigPath is the Scaleway CLI's config file: SCW_CONFIG_PATH, else
// scw/config.yaml in XDG_CONFIG_HOME or ~/.config.
func ScwConfigPath(home string, getenv func(string) string) string {
	if p := getenv("SCW_CONFIG_PATH"); p != "" {
		return p
	}
	base := getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "scw", "config.yaml")
}

// scwEntry is a key of the scw config file: Profile is "default" for the top
// level, else the name under profiles. Commented entries ("# key: value")
// carry no profile. N is the line number, from 1.
type scwEntry struct {
	N         int
	Profile   string
	Key       string
	Value     string
	Commented bool
}

// scanScw reads the block layout scw writes. A missing file has no lines.
func scanScw(path string) ([]string, []scwEntry, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	lines := strings.Split(string(b), "\n")
	var out []scwEntry
	inProfiles, profIndent, current := false, -1, ""
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" {
			continue
		}
		if c, ok := strings.CutPrefix(trimmed, "#"); ok {
			if key, value, ok := strings.Cut(strings.TrimSpace(c), ":"); ok && !strings.ContainsAny(key, " \t") {
				out = append(out, scwEntry{N: i + 1, Key: unquote(key), Value: cleanValue(value), Commented: true})
			}
			continue
		}
		indent := len(line) - len(trimmed)
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		key = unquote(strings.TrimSpace(key))
		switch {
		case indent == 0:
			inProfiles, profIndent, current = key == "profiles", -1, ""
			if key != "profiles" {
				out = append(out, scwEntry{N: i + 1, Profile: "default", Key: key, Value: cleanValue(value)})
			}
		case inProfiles:
			if profIndent < 0 {
				profIndent = indent
			}
			switch {
			case indent == profIndent:
				current = key
			case indent > profIndent && current != "":
				out = append(out, scwEntry{N: i + 1, Profile: current, Key: key, Value: cleanValue(value)})
			}
		}
	}
	return lines, out, nil
}

// ScwSecretProfiles lists the profiles of the scw config file that hold a
// secret_key: "default" for the top level, then the names under profiles.
// It never returns values, and returns nothing for a missing file.
func ScwSecretProfiles(path string) ([]string, error) {
	_, entries, err := scanScw(path)
	var out []string
	for _, e := range entries {
		if !e.Commented && e.Key == "secret_key" && e.Value != "" && !slices.Contains(out, e.Profile) {
			out = append(out, e.Profile)
		}
	}
	return out, err
}

// ScwCommentedSecrets lists the lines of the scw config file that hold a
// commented-out secret_key with a value.
func ScwCommentedSecrets(path string) ([]int, error) {
	_, entries, err := scanScw(path)
	var out []int
	for _, e := range entries {
		if e.Commented && e.Key == "secret_key" && e.Value != "" {
			out = append(out, e.N)
		}
	}
	return out, err
}

// ScwActiveProfile is the file's active_profile, else "default".
func ScwActiveProfile(path string) (string, error) {
	_, entries, err := scanScw(path)
	for _, e := range entries {
		if !e.Commented && e.Profile == "default" && e.Key == "active_profile" && e.Value != "" {
			return e.Value, err
		}
	}
	return "default", err
}

// ScwRead returns the keys of a profile ("default" for the top level).
func ScwRead(path, profile string) (map[string]string, error) {
	_, entries, err := scanScw(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, e := range entries {
		if !e.Commented && e.Profile == profile && e.Value != "" {
			out[e.Key] = e.Value
		}
	}
	return out, nil
}

// ScwRemoveSecret removes the secret_key of a profile and every
// commented-out secret_key, keeping the rest of the file and its mode.
func ScwRemoveSecret(path, profile string) error {
	lines, entries, err := scanScw(path)
	if err != nil {
		return err
	}
	drop := map[int]bool{}
	for _, e := range entries {
		if e.Key == "secret_key" && (e.Commented || e.Profile == profile) {
			drop[e.N] = true
		}
	}
	if len(drop) == 0 {
		return nil
	}
	var kept []string
	for i, l := range lines {
		if !drop[i+1] {
			kept = append(kept, l)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(strings.Join(kept, "\n")); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// cleanValue drops quotes, trailing comments and YAML's empty values.
func cleanValue(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	v = unquote(v)
	if v == "~" || v == "null" {
		return ""
	}
	return v
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
