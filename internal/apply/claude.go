package apply

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/skills"
)

func claudeEntry(s named) map[string]any {
	if s.URL != "" {
		e := map[string]any{"type": "http", "url": s.URL}
		if len(s.Headers) > 0 {
			e["headers"] = s.Headers
		}
		return e
	}
	cmd, args := command(s)
	if args == nil {
		args = []string{}
	}
	return map[string]any{"type": "stdio", "command": cmd, "args": args, "env": envWithRefs(s.Env, "${%s}")}
}

// claude writes the profile's servers in user scope, the top-level
// mcpServers of the profile's .claude.json.
func (s *syncer) claude(p *config.Profile, servers []named) error {
	dir := s.profileDir("claude", p)
	if _, err := os.Stat(dir); err != nil {
		s.note("profile %s: %s does not exist (create it with new, #9)", p.Name, s.tilde(dir))
		return nil
	}
	changes, err := skills.Sync(s.Home, p.Name, p.SkillsExclude, s.DryRun)
	if err != nil {
		return err
	}
	for _, c := range changes {
		s.act(filepath.Join(dir, "skills"), "%s", c)
	}
	path := filepath.Join(dir, ".claude.json")
	return s.editJSON(path, func(m map[string]any) bool {
		obj := object(m, "mcpServers")
		s.undeclared(path, keys(obj), servers)
		return s.setServers(path, obj, servers, claudeEntry)
	})
}

// claudeRepoServers writes a repo's servers in local scope: the repo's entry
// under projects in the profile's .claude.json.
func (s *syncer) claudeRepoServers(p *config.Profile, r *config.Repo, servers []named) error {
	path := filepath.Join(s.profileDir("claude", p), ".claude.json")
	return s.editJSON(path, func(m map[string]any) bool {
		obj := object(object(object(m, "projects"), r.Dir), "mcpServers")
		return s.setServers(path+" ("+s.tilde(r.Dir)+")", obj, servers, claudeEntry)
	})
}

// claudePlugins turns the repo's plugins on or off in its
// .claude/settings.local.json.
func (s *syncer) claudePlugins(r *config.Repo) error {
	installed, err := s.installedPlugins()
	if err != nil {
		return err
	}
	want := map[string]bool{}
	for _, list := range []struct {
		names []string
		on    bool
	}{{r.Plugins, true}, {r.DisabledPlugins, false}} {
		for _, name := range list.names {
			id, err := resolvePlugin(name, installed)
			if err != nil {
				return fmt.Errorf("plugin %s: %w", name, err)
			}
			want[id] = list.on
		}
	}
	path, ok, err := s.repoFile(r, filepath.Join(".claude", "settings.local.json"))
	if !ok || err != nil {
		return err
	}
	return s.editJSON(path, func(m map[string]any) bool {
		enabled := object(m, "enabledPlugins")
		changed := false
		for _, id := range slices.Sorted(maps.Keys(want)) {
			if cur, set := enabled[id].(bool); set && cur == want[id] {
				continue
			}
			if want[id] {
				s.act(path, "enable plugin %s", id)
			} else {
				s.act(path, "disable plugin %s", id)
			}
			enabled[id] = want[id]
			changed = true
		}
		return changed
	})
}

// installedPlugins lists the plugin ids (name@marketplace) installed in the
// shared ~/.claude/plugins.
func (s *syncer) installedPlugins() ([]string, error) {
	m, err := readJSON(filepath.Join(s.Home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return nil, err
	}
	ids := keys(object(m, "plugins"))
	slices.Sort(ids)
	return ids, nil
}

// ResolvePlugin returns the installed plugin id for name (a bare name or
// name@marketplace).
func ResolvePlugin(home, name string) (string, error) {
	s := &syncer{Options: Options{Home: home}}
	installed, err := s.installedPlugins()
	if err != nil {
		return "", err
	}
	return resolvePlugin(name, installed)
}

// resolvePlugin accepts a full id or a bare name that matches one installed plugin.
func resolvePlugin(name string, installed []string) (string, error) {
	if strings.Contains(name, "@") {
		if !slices.Contains(installed, name) {
			return "", fmt.Errorf("not installed")
		}
		return name, nil
	}
	var found []string
	for _, id := range installed {
		if strings.HasPrefix(id, name+"@") {
			found = append(found, id)
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("not installed")
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("in more than one marketplace (%s): write name@marketplace", strings.Join(found, ", "))
}

// editJSON loads path, lets edit change it and writes it back when edit
// reports a change.
func (s *syncer) editJSON(path string, edit func(map[string]any) bool) error {
	m, err := readJSON(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if !edit(m) || s.DryRun {
		return nil
	}
	data, err := encodeJSON(m)
	if err != nil {
		return err
	}
	return writeFile(path, data)
}
