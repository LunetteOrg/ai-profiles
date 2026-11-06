package apply

import (
	"encoding/json"
	"hash/fnv"
	"path/filepath"
	"slices"

	"github.com/LunetteOrg/ai-profiles/internal/config"
)

// OpenCode's background service listens on one port per profile, so a
// client never reaches the service, and the data, of another profile.
const (
	portBase  = 49500
	portRange = 400
)

func openCodeEntry(s named) map[string]any {
	if s.URL != "" {
		e := map[string]any{"type": "remote", "url": s.URL}
		if len(s.Headers) > 0 {
			e["headers"] = s.Headers
		}
		return e
	}
	cmd, args := command(s)
	e := map[string]any{"type": "local", "command": append([]string{cmd}, args...)}
	if len(s.Env) > 0 {
		e["environment"] = envWithRefs(s.Env, "{env:%s}")
	}
	return e
}

func (s *syncer) openCode(p *config.Profile, servers []named, port int) error {
	base := s.profileDir("opencode", p)
	conf := filepath.Join(base, "config")
	for _, d := range []string{conf, filepath.Join(base, "data"), filepath.Join(base, "state")} {
		if err := s.mkdir(d); err != nil {
			return err
		}
	}
	if err := s.link(filepath.Join(conf, "AGENTS.md"), s.agentsMD()); err != nil {
		return err
	}
	path := filepath.Join(conf, "opencode.json")
	err := s.editJSON(path, func(m map[string]any) bool {
		changed := false
		if _, ok := m["$schema"]; !ok {
			m["$schema"] = "https://opencode.ai/config.json"
			changed = true
		}
		// OpenCode 2 keeps servers under mcp.servers, as `opencode mcp add` writes them.
		mcp := object(m, "mcp")
		obj := object(mcp, "servers")
		s.undeclared(path, keys(obj), servers)
		if s.setServers(path, obj, servers, openCodeEntry) {
			changed = true
		}
		if len(obj) == 0 {
			delete(mcp, "servers")
		}
		if len(mcp) == 0 {
			delete(m, "mcp")
		}
		return changed
	})
	if err != nil {
		return err
	}
	service := filepath.Join(conf, "service.json")
	return s.editJSON(service, func(m map[string]any) bool {
		if _, ok := m["port"]; ok {
			return false
		}
		s.act(service, "service port %d", port)
		m["port"] = port
		return true
	})
}

// openCodeRepo writes a repo's servers in its opencode.json, which OpenCode
// reads from any folder of the repo.
func (s *syncer) openCodeRepo(r *config.Repo, servers []named) error {
	path, ok, err := s.repoFile(r, "opencode.json")
	if !ok || err != nil {
		return err
	}
	return s.editJSON(path, func(m map[string]any) bool {
		changed := false
		if _, ok := m["$schema"]; !ok {
			m["$schema"] = "https://opencode.ai/config.json"
			changed = true
		}
		return s.setServers(path, object(object(m, "mcp"), "servers"), servers, openCodeEntry) || changed
	})
}

// openCodePorts gives every profile that uses OpenCode a service port: the
// one already in its service.json, or a free one derived from its name.
func (s *syncer) openCodePorts() (map[string]int, error) {
	ports, used := map[string]int{}, map[int]bool{}
	var todo []string
	for _, name := range s.Cfg.Names() {
		p := s.Cfg.Profiles[name]
		if !slices.Contains(p.Agents, "opencode") {
			continue
		}
		m, err := readJSON(filepath.Join(s.profileDir("opencode", p), "config", "service.json"))
		if err != nil {
			return nil, err
		}
		if n, ok := m["port"].(json.Number); ok {
			if v, err := n.Int64(); err == nil {
				ports[name], used[int(v)] = int(v), true
				continue
			}
		}
		todo = append(todo, name)
	}
	for _, name := range todo {
		h := fnv.New32a()
		h.Write([]byte(name))
		port := portBase + int(h.Sum32()%portRange)
		for used[port] {
			port = portBase + (port-portBase+1)%portRange
		}
		ports[name], used[port] = port, true
	}
	return ports, nil
}
