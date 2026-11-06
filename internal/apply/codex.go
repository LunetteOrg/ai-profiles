package apply

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/LunetteOrg/ai-profiles/internal/config"
)

// agentsRoot marks a client folder as the project root for Codex, so it reads
// the client's AGENTS.md as well as the repo's.
const agentsRoot = ".agents-root"

func codexEntry(s named) map[string]any {
	if s.URL != "" {
		e := map[string]any{"url": s.URL}
		if len(s.Headers) > 0 {
			e["http_headers"] = s.Headers
		}
		return e
	}
	cmd, args := command(s)
	e := map[string]any{"command": cmd}
	if len(args) > 0 {
		e["args"] = args
	}
	if cmd != s.Command {
		// Through ai-secrets the server may wait for the 1Password approval:
		// more than Codex's default 10 seconds.
		e["startup_timeout_sec"] = 60
	}
	// Codex passes no environment to its servers: env: variables go by name
	// in env_vars (with their own name); op:// references go in env, for op
	// run.
	plain := map[string]string{}
	var fromEnv []string
	for _, k := range slices.Sorted(maps.Keys(s.Env)) {
		if name, ok := strings.CutPrefix(s.Env[k], "env:"); ok && name != "" {
			fromEnv = append(fromEnv, name)
		} else {
			plain[k] = s.Env[k]
		}
	}
	if len(plain) > 0 {
		e["env"] = plain
	}
	if len(fromEnv) > 0 {
		e["env_vars"] = fromEnv
	}
	return e
}

// codex writes the profile's config.toml: servers under [mcp_servers], their
// OAuth tokens in files inside the profile (the Keychain entry would be shared
// by every CODEX_HOME), and the client folders as project roots.
func (s *syncer) codex(p *config.Profile, servers []named) error {
	dir := s.profileDir("codex", p)
	if err := s.mkdir(dir); err != nil {
		return err
	}
	if err := s.link(filepath.Join(dir, "AGENTS.md"), s.agentsMD()); err != nil {
		return err
	}
	for _, d := range p.Dirs {
		if _, err := os.Stat(d); err != nil {
			continue
		}
		if err := s.touch(filepath.Join(d, agentsRoot)); err != nil {
			return err
		}
	}
	path := filepath.Join(dir, "config.toml")
	return s.editTOML(path, func(m map[string]any) bool {
		changed := false
		set := func(key string, want any, what string) {
			if !same(m[key], want) {
				s.act(path, "%s", what)
				m[key] = want
				changed = true
			}
		}
		set("mcp_oauth_credentials_store", "file", "MCP OAuth tokens in files inside the profile")
		if len(p.Dirs) > 0 {
			set("project_root_markers", []string{agentsRoot}, "client folders as project roots ("+agentsRoot+")")
		}
		obj := object(m, "mcp_servers")
		s.undeclared(path, keys(obj), servers)
		if s.setServers(path, obj, servers, codexEntry) {
			changed = true
		}
		if len(obj) == 0 {
			delete(m, "mcp_servers")
		}
		return changed
	})
}

// codexRepo writes a repo's servers in its .codex/config.toml. Codex reads a
// project's config only when the profile trusts the project, so the repo is
// marked trusted, unless the profile explicitly distrusts it.
func (s *syncer) codexRepo(p *config.Profile, r *config.Repo, servers []named) error {
	profileCfg := filepath.Join(s.profileDir("codex", p), "config.toml")
	key := r.Dir
	if real, err := filepath.EvalSymlinks(r.Dir); err == nil {
		key = real
	}
	trusted := true
	err := s.editTOML(profileCfg, func(m map[string]any) bool {
		proj := object(object(m, "projects"), key)
		switch proj["trust_level"] {
		case "trusted":
			return false
		case "untrusted":
			trusted = false
			s.note("%s: project %s is not trusted, so Codex does not read its servers", s.tilde(profileCfg), s.tilde(r.Dir))
			return false
		}
		s.act(profileCfg, "trust project %s (to read its servers)", s.tilde(r.Dir))
		proj["trust_level"] = "trusted"
		return true
	})
	if err != nil || !trusted {
		return err
	}
	path, ok, err := s.repoFile(r, filepath.Join(".codex", "config.toml"))
	if !ok || err != nil {
		return err
	}
	return s.editTOML(path, func(m map[string]any) bool {
		return s.setServers(path, object(m, "mcp_servers"), servers, codexEntry)
	})
}

// editTOML is editJSON for TOML files. Comments are not kept.
func (s *syncer) editTOML(path string, edit func(map[string]any) bool) error {
	m := map[string]any{}
	if _, err := toml.DecodeFile(path, &m); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if !edit(m) || s.DryRun {
		return nil
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(m); err != nil {
		return err
	}
	return writeFile(path, buf.Bytes())
}
