package toolshim

import (
	"bufio"
	"errors"
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

// ScwSecretProfiles lists the profiles of the scw config file that hold a
// secret_key: "default" for the top level, then the names under profiles.
// It reads the block layout scw writes, never returns values, and returns
// nothing for a missing file.
func ScwSecretProfiles(path string) ([]string, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	add := func(p string) {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	inProfiles, profIndent, current := false, -1, ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimLeft(line, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
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
			if key == "secret_key" && hasValue(value) {
				add("default")
			}
		case inProfiles:
			if profIndent < 0 {
				profIndent = indent
			}
			switch {
			case indent == profIndent:
				current = key
			case indent > profIndent && current != "" && key == "secret_key" && hasValue(value):
				add(current)
			}
		}
	}
	return out, sc.Err()
}

func hasValue(v string) bool {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, " #"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	v = unquote(v)
	return v != "" && v != "~" && v != "null"
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}
