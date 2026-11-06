// Package apply brings agent profiles in line with config.toml: MCP servers,
// credential folders, shared links and per-repo plugins. It only touches what
// config.toml declares: other servers and settings are left as they are.
package apply

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/gitroot"
	"github.com/LunetteOrg/ai-profiles/internal/launch"
)

type Options struct {
	Home   string
	Cfg    *config.Config
	DryRun bool
	// Self is the ai-profile executable, for the commands sync writes into
	// agent settings (the Claude Code statusline); empty skips them.
	Self string
}

// Action is a change made, or to be made with DryRun.
type Action struct {
	Path string
	What string
}

type Report struct {
	Actions []Action
	Notes   []string
}

// syncer carries the state of one run.
type syncer struct {
	Options
	rep *Report
}

func (s *syncer) act(path, format string, args ...any) {
	s.rep.Actions = append(s.rep.Actions, Action{Path: path, What: fmt.Sprintf(format, args...)})
}

func (s *syncer) note(format string, args ...any) {
	s.rep.Notes = append(s.rep.Notes, fmt.Sprintf(format, args...))
}

func Sync(o Options) (*Report, error) {
	s := &syncer{Options: o, rep: &Report{}}
	if err := s.installAssets(); err != nil {
		return nil, err
	}
	if err := s.claudeStatusLine(); err != nil {
		return nil, err
	}
	ports, err := s.openCodePorts()
	if err != nil {
		return nil, err
	}
	for _, name := range o.Cfg.Names() {
		p := o.Cfg.Profiles[name]
		servers, err := s.render(p, nil)
		if err != nil {
			return nil, fmt.Errorf("profile %s: %w", name, err)
		}
		for _, agent := range p.Agents {
			switch agent {
			case "claude":
				err = s.claude(p, servers)
			case "codex":
				err = s.codex(p, servers)
			case "opencode":
				err = s.openCode(p, servers, ports[name])
			default:
				if len(servers) > 0 {
					s.note("profile %s: sync for %s is not available yet", name, agent)
				}
			}
			if err != nil {
				return nil, fmt.Errorf("profile %s, %s: %w", name, agent, err)
			}
		}
		for i := range p.Repos {
			if err := s.repo(p, &p.Repos[i]); err != nil {
				return nil, fmt.Errorf("profile %s, repo %s: %w", name, p.Repos[i].Dir, err)
			}
		}
	}
	return s.rep, nil
}

// named is a rendered server with its name, in the order of config.toml.
type named struct {
	Name    string
	Profile string
	*config.Server
}

// render resolves the placeholders of the profile's servers, or the repo's,
// and creates their credential folders.
func (s *syncer) render(p *config.Profile, r *config.Repo) ([]named, error) {
	list := p.MCP
	if r != nil {
		list = r.MCP
	}
	var out []named
	for _, n := range list {
		srv, err := s.Cfg.MCP[n].Render(p.VarsFor(r), s.Home)
		if err != nil {
			return nil, fmt.Errorf("server %s: %w", n, err)
		}
		for _, d := range srv.Dirs {
			if err := s.mkdir(d.Path); err != nil {
				return nil, err
			}
		}
		out = append(out, named{n, p.Name, srv})
	}
	return out, nil
}

func (s *syncer) repo(p *config.Profile, r *config.Repo) error {
	if _, err := os.Stat(r.Dir); err != nil {
		s.note("repo %s (profile %s) does not exist: skipping it", s.tilde(r.Dir), p.Name)
		return nil
	}
	servers, err := s.render(p, r)
	if err != nil {
		return err
	}
	if len(servers) > 0 {
		for _, agent := range p.Agents {
			switch agent {
			case "claude":
				err = s.claudeRepoServers(p, r, servers)
			case "codex":
				err = s.codexRepo(p, r, servers)
			case "opencode":
				err = s.openCodeRepo(r, servers)
			default:
				s.note("repo %s: per-repo servers with %s are not available yet", s.tilde(r.Dir), agent)
			}
			if err != nil {
				return fmt.Errorf("%s: %w", agent, err)
			}
		}
	}
	if len(r.Plugins)+len(r.DisabledPlugins) > 0 {
		if !slices.Contains(p.Agents, "claude") {
			s.note("repo %s: plugins belong to Claude Code, which profile %s does not use", s.tilde(r.Dir), p.Name)
			return nil
		}
		return s.claudePlugins(r)
	}
	return nil
}

// repoFile prepares a file that sync writes inside a repo: a file git already
// tracks belongs to the team and is left alone (ok is false); otherwise it is
// added to the clone's .git/info/exclude so it never ends up in a commit.
func (s *syncer) repoFile(r *config.Repo, rel string) (path string, ok bool, err error) {
	path = filepath.Join(r.Dir, rel)
	exclude, err := gitroot.ExcludeFile(r.Dir)
	if err != nil {
		return path, true, nil // not a git repo: nothing to protect
	}
	if gitroot.Tracked(r.Dir, rel) {
		s.note("%s is tracked by git in the repo: leaving it alone, add what is needed by hand", s.tilde(path))
		return path, false, nil
	}
	if gitroot.Ignored(r.Dir, rel) {
		return path, true, nil
	}
	s.act(exclude, "ignore /%s", filepath.ToSlash(rel))
	if s.DryRun {
		return path, true, nil
	}
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return path, false, err
	}
	f, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return path, false, err
	}
	defer f.Close()
	if info, err := f.Stat(); err == nil && info.Size() > 0 {
		if b, _ := os.ReadFile(exclude); len(b) > 0 && b[len(b)-1] != '\n' {
			f.WriteString("\n")
		}
	}
	_, err = f.WriteString("/" + filepath.ToSlash(rel) + "\n")
	return path, err == nil, err
}

func (s *syncer) mkdir(dir string) error {
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return nil
	}
	s.act(dir, "create the folder")
	if s.DryRun {
		return nil
	}
	return os.MkdirAll(dir, 0o700)
}

// link points path at target when path is missing; anything else already
// there is left alone and reported.
func (s *syncer) link(path, target string) error {
	if cur, err := os.Readlink(path); err == nil {
		if cur != target {
			s.note("%s points to %s instead of %s: leaving it", s.tilde(path), cur, s.tilde(target))
		}
		return nil
	}
	if _, err := os.Lstat(path); err == nil {
		s.note("%s exists and is not a link to %s: leaving it", s.tilde(path), s.tilde(target))
		return nil
	}
	s.act(path, "link to %s", s.tilde(target))
	if s.DryRun {
		return nil
	}
	return os.Symlink(target, path)
}

// touch creates an empty file when missing.
func (s *syncer) touch(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return nil
	}
	s.act(path, "create the placeholder file")
	if s.DryRun {
		return nil
	}
	return os.WriteFile(path, nil, 0o644)
}

func (s *syncer) agentsMD() string {
	return filepath.Join(s.Home, ".agents", "AGENTS.md")
}

func (s *syncer) profileDir(agent string, p *config.Profile) string {
	return launch.ProfileDir(s.Home, agent, p.Name)
}

func (s *syncer) tilde(p string) string {
	if rel, err := filepath.Rel(s.Home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return p
}

// undeclared reports the servers found in a file that config.toml does not
// declare for it.
func (s *syncer) undeclared(path string, found []string, servers []named) {
	for _, n := range found {
		if !slices.ContainsFunc(servers, func(d named) bool { return d.Name == n }) {
			s.note("%s: server %s is not in config.toml: leaving it", s.tilde(path), n)
		}
	}
}
