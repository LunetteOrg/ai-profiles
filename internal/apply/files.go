package apply

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

// readJSON decodes a JSON object, or returns an empty one when the file is
// missing. Numbers stay json.Number so they are written back unchanged.
func readJSON(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func encodeJSON(m map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFile replaces path atomically, keeping the mode of the file it
// replaces (0600 for a new one).
func writeFile(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// object returns m[key] as an object, creating it when missing.
func object(m map[string]any, key string) map[string]any {
	if o, ok := m[key].(map[string]any); ok {
		return o
	}
	o := map[string]any{}
	m[key] = o
	return o
}

// same compares two decoded values ignoring empty lists and objects, which
// agents write or omit freely.
func same(a, b any) bool {
	return reflect.DeepEqual(normalize(a), normalize(b))
}

func normalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, x := range t {
			if n := normalize(x); n != nil {
				out[k] = n
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		if len(t) == 0 {
			return nil
		}
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = normalize(x)
		}
		return out
	case []string:
		if len(t) == 0 {
			return nil
		}
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = x
		}
		return out
	case map[string]string:
		if len(t) == 0 {
			return nil
		}
		out := map[string]any{}
		for k, x := range t {
			out[k] = x
		}
		return out
	case json.Number:
		return t.String()
	case int64:
		return strconv.FormatInt(t, 10)
	case int:
		return strconv.Itoa(t)
	}
	return v
}

// setServers writes servers into obj (a map of name → entry), recording an
// action for each one added or changed. It returns whether obj changed.
func (s *syncer) setServers(path string, obj map[string]any, servers []named, entry func(named) map[string]any) bool {
	changed := false
	for _, srv := range servers {
		want := entry(srv)
		cur, ok := obj[srv.Name]
		switch {
		case !ok:
			s.act(path, "add server %s", srv.Name)
		case !same(cur, want):
			s.act(path, "update server %s", srv.Name)
		default:
			continue
		}
		obj[srv.Name] = want
		changed = true
	}
	return changed
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// envWithRefs copies env for an agent: an env: reference becomes the agent's
// syntax for reading that variable (format holds one %s); op:// references
// stay as they are, for op run to resolve when the server starts.
func envWithRefs(env map[string]string, format string) map[string]string {
	out := map[string]string{}
	for k, v := range env {
		if name, ok := strings.CutPrefix(v, "env:"); ok && name != "" {
			v = fmt.Sprintf(format, name)
		}
		out[k] = v
	}
	return out
}

// command returns how to start a stdio server: through ai-secrets when its
// env holds op:// references, so one approval covers every session of every
// agent for a while. ai-secrets is written with its full path, since apps
// opened from the Dock have a short PATH.
func command(s named) (string, []string) {
	for _, v := range s.Env {
		if secrets.IsOp(v) || secrets.Tool(v) == "bw" {
			reason := fmt.Sprintf("server %s, profile %s", s.Name, s.Profile)
			return aiSecretsPath(), append([]string{"run", "--reason", reason, "--", s.Command}, s.Args...)
		}
	}
	return s.Command, s.Args
}

var aiSecretsPath = func() string {
	if p, err := exec.LookPath("ai-secrets"); err == nil {
		return StableToolPath(p, os.Getenv)
	}
	return "ai-secrets"
}

// StableToolPath swaps a tool installed by mise for its mise shim: the
// installed path names the version and disappears with the next upgrade.
func StableToolPath(p string, getenv func(string) string) string {
	data := getenv("MISE_DATA_DIR")
	if data == "" {
		if x := getenv("XDG_DATA_HOME"); x != "" {
			data = filepath.Join(x, "mise")
		} else if home, err := os.UserHomeDir(); err == nil {
			data = filepath.Join(home, ".local", "share", "mise")
		}
	}
	installs := filepath.Join(data, "installs") + string(filepath.Separator)
	if data == "" || !strings.HasPrefix(p, installs) {
		return p
	}
	shim := filepath.Join(data, "shims", filepath.Base(p))
	if _, err := os.Stat(shim); err != nil {
		return p
	}
	return shim
}

// EditJSONFile loads the JSON object in path, lets edit change it and writes
// it back atomically, keeping every other key, when edit reports a change.
func EditJSONFile(path string, edit func(map[string]any) bool) error {
	s := &syncer{rep: &Report{}}
	return s.editJSON(path, edit)
}
