// Package secrets reads the secret references of config.toml: 1Password
// (op://), Bitwarden (bw://, with the profile's account unlocked by a password
// kept in 1Password) and the environment or ~/.env (env:). Keys reach the
// processes that need them without being written to disk.
package secrets

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Runner runs a CLI with extra environment variables and returns its
// standard output.
type Runner func(extraEnv []string, name string, args ...string) (string, error)

// Exec is the Runner that runs the real CLI; its prompts (1Password approval)
// go to the terminal.
func Exec(extraEnv []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	out, err := cmd.Output()
	return string(out), err
}

var schemes = []struct{ prefix, tool string }{
	{"op://", "op"},
	{"bw://", "bw"},
	{"env:", ""},
}

// IsRef reports whether v is a secret reference rather than a plain value.
func IsRef(v string) bool {
	for _, s := range schemes {
		if strings.HasPrefix(v, s.prefix) && len(v) > len(s.prefix) {
			return true
		}
	}
	return false
}

// IsOp reports whether v is a 1Password reference, which op run resolves.
func IsOp(v string) bool { return strings.HasPrefix(v, "op://") && len(v) > len("op://") }

// Tool is the CLI a reference needs, empty for env:.
func Tool(ref string) string {
	for _, s := range schemes {
		if strings.HasPrefix(ref, s.prefix) {
			return s.tool
		}
	}
	return ""
}

// Bitwarden is a Bitwarden account: its CLI data folder and the 1Password
// reference of its master password.
type Bitwarden struct {
	Name          string
	DataDir       string
	Server        string
	Unlock        string // "password" (master password from 1Password) or "biometric"
	Password      string // op://…, for Unlock "password"
	LockAfterRead bool   // bw lock after every read: never left unlocked
}

// Env is what the bw CLI needs to work on this account.
func (b *Bitwarden) Env() []string {
	return []string{"BITWARDENCLI_APPDATA_DIR=" + b.DataDir}
}

type Resolver struct {
	Home   string
	Getenv func(string) string
	Run    Runner
	// RunIsolated runs a command in a process session of its own: a
	// 1Password approval asked there covers nothing else. Nil uses Run.
	RunIsolated Runner
	// Stores says how to reach each password manager; nil reads op:// with
	// Run and knows no Bitwarden account.
	Stores *Stores
	// Account is the Bitwarden account of bw:// references that name none.
	Account string
	Now     func() time.Time

	sessions map[string]bwSession // unlocked accounts, when not locked after each read
	held     bool                 // inside a request: one login serves all its reads
}

// Hold starts a request: the reads until Release share one login per
// account, also for accounts locked after each read.
func (r *Resolver) Hold() { r.held = true }

// Release ends a request and locks the accounts locked after each read.
func (r *Resolver) Release() {
	r.held = false
	for name, s := range r.sessions {
		if b := r.Stores.Bitwarden[name]; b != nil && b.LockAfterRead {
			r.Run(append(b.Env(), "BW_SESSION="+s.key), "bw", "lock")
			delete(r.sessions, name)
		}
	}
}

// keeps reports whether an account's session outlives a read.
func (r *Resolver) keeps(b *Bitwarden) bool { return !b.LockAfterRead || r.held }

type bwSession struct {
	key   string
	until time.Time
}

func (r *Resolver) isolated() Runner {
	if r.RunIsolated != nil {
		return r.RunIsolated
	}
	return r.Run
}

func (r *Resolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Resolver) Resolve(ref string) (string, error) {
	switch {
	case IsOp(ref):
		run := r.Run
		if r.Stores != nil && r.Stores.OpIsolated {
			run = r.isolated()
		}
		out, err := run(nil, "op", "read", "--no-newline", ref)
		if err != nil {
			return "", fmt.Errorf("%s: op read failed (1Password approved?): %w", ref, err)
		}
		return out, nil
	case strings.HasPrefix(ref, "bw://"):
		return r.bitwarden(ref)
	case strings.HasPrefix(ref, "env:"):
		name := strings.TrimPrefix(ref, "env:")
		if v := r.Getenv(name); v != "" {
			return v, nil
		}
		if v, ok := dotEnv(filepath.Join(r.Home, ".env"))[name]; ok && v != "" {
			return v, nil
		}
		return "", fmt.Errorf("%s: %s is neither in the environment nor in ~/.env", ref, name)
	}
	return "", fmt.Errorf("%q is not a secret reference (op://, bw://, env:)", ref)
}

// bitwarden reads an item. Unless the account keeps a session for the ttl,
// every read is a full login: master password from 1Password and bw unlock
// in isolation, bw get, bw lock. The master password is never kept.
func (r *Resolver) bitwarden(ref string) (string, error) {
	var accounts map[string]*Bitwarden
	if r.Stores != nil {
		accounts = r.Stores.Bitwarden
	}
	name, item := SplitBitwardenRef(ref, accounts)
	if name == "" {
		name = r.Account
	}
	b := accounts[name]
	if b == nil {
		if name == "" {
			return "", fmt.Errorf("%s: no Bitwarden account: name it (bw://<account>/<item>) or set bitwarden = \"<account>\" in the profile", ref)
		}
		return "", fmt.Errorf("%s: Bitwarden account %q is not in %s ([stores.bitwarden.%s])", ref, name, StoresPath(r.Home), name)
	}
	if b.Unlock == "biometric" {
		return "", fmt.Errorf("%s: the Bitwarden CLI has no biometric unlock yet: set unlock = \"password\" for account %s", ref, b.Name)
	}
	key, err := r.unlock(b)
	if err != nil {
		return "", err
	}
	env := append(b.Env(), "BW_SESSION="+key)
	r.Run(env, "bw", "sync")
	item, part, _ := strings.Cut(item, "#")
	out, err := r.readPart(env, item, part)
	if !r.keeps(b) {
		r.Run(env, "bw", "lock")
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w (item %q in account %s?)", ref, err, item, b.Name)
	}
	return out, nil
}

// readPart reads one part of an item: the password (no part), username,
// notes or totp, a custom field (field=<name>) or an attachment's content
// (attachment=<file name>). Errors never carry values.
func (r *Resolver) readPart(env []string, item, part string) (string, error) {
	kind, arg, _ := strings.Cut(part, "=")
	switch kind {
	case "", "password", "username", "notes", "totp":
		if kind == "" {
			kind = "password"
		}
		out, err := r.Run(env, "bw", "get", kind, item)
		if err != nil {
			return "", fmt.Errorf("bw get %s failed", kind)
		}
		return strings.TrimRight(out, "\n"), nil
	case "field", "attachment":
		raw, err := r.Run(env, "bw", "get", "item", item)
		if err != nil {
			return "", errors.New("bw get item failed")
		}
		var it struct {
			ID     string `json:"id"`
			Fields []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"fields"`
		}
		if json.Unmarshal([]byte(raw), &it) != nil {
			return "", errors.New("unreadable item")
		}
		if kind == "field" {
			for _, f := range it.Fields {
				if f.Name == arg {
					return f.Value, nil
				}
			}
			return "", fmt.Errorf("no custom field %q", arg)
		}
		out, err := r.Run(env, "bw", "get", "attachment", arg, "--itemid", it.ID, "--raw")
		if err != nil {
			return "", fmt.Errorf("no attachment %q", arg)
		}
		return out, nil
	}
	return "", fmt.Errorf("unknown part %q: use username, notes, totp, field=<name> or attachment=<file>", part)
}

// unlock returns a session key: a kept one when the account stays unlocked
// for the ttl, otherwise a new login.
func (r *Resolver) unlock(b *Bitwarden) (string, error) {
	if r.keeps(b) {
		if s, ok := r.sessions[b.Name]; ok {
			if r.now().Before(s.until) {
				return s.key, nil
			}
			r.Run(append(b.Env(), "BW_SESSION="+s.key), "bw", "lock")
			delete(r.sessions, b.Name)
		}
	}
	login := r.isolated()
	pw, err := login(nil, "op", "read", "--no-newline", b.Password)
	if err != nil || pw == "" {
		return "", fmt.Errorf("Bitwarden %s master password from 1Password: %v", b.Name, err)
	}
	out, err := login(append(b.Env(), "BW_PASSWORD="+pw), "bw", "unlock", "--raw", "--passwordenv", "BW_PASSWORD")
	pw = ""
	key := strings.TrimSpace(out)
	if err != nil || key == "" {
		return "", fmt.Errorf("unlocking Bitwarden %s failed (logged in? BITWARDENCLI_APPDATA_DIR=%s bw login): %v", b.Name, b.DataDir, err)
	}
	if r.keeps(b) {
		if r.sessions == nil {
			r.sessions = map[string]bwSession{}
		}
		ttl := DefaultTTL
		if r.Stores != nil {
			ttl = r.Stores.TTL
		}
		r.sessions[b.Name] = bwSession{key: key, until: r.now().Add(ttl)}
	}
	return key, nil
}

// Sessions is how many Bitwarden accounts are unlocked now.
func (r *Resolver) Sessions() int { return len(r.sessions) }

// LockAll locks every unlocked Bitwarden account.
func (r *Resolver) LockAll() {
	for name, s := range r.sessions {
		if b := r.Stores.Bitwarden[name]; b != nil {
			r.Run(append(b.Env(), "BW_SESSION="+s.key), "bw", "lock")
		}
	}
	r.sessions = nil
}

// dotEnv reads KEY=VALUE lines, with optional export and quotes.
func dotEnv(path string) map[string]string {
	vars := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return vars
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		vars[strings.TrimSpace(k)] = v
	}
	return vars
}
