package config

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// Server is an MCP server defined once in [mcp.<name>]. Values can hold
// placeholders: {profilo} and the keys of the profile's and repo's vars.
type Server struct {
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	URL     string            `toml:"url"`
	Headers map[string]string `toml:"headers"`
	Dirs    []ServerDir       `toml:"dirs"`
}

// ServerDir is a credentials folder of a server; Only lists the files it may hold.
type ServerDir struct {
	Path string   `toml:"path"`
	Only []string `toml:"only"`
}

func (c *Config) checkServers(names []string) error {
	for _, n := range names {
		if c.MCP[n] == nil {
			return fmt.Errorf("MCP server %q is not defined in [mcp]", n)
		}
	}
	return nil
}

// VarsFor returns the placeholders of the profile, with repo's vars on top
// when repo is not nil.
func (p *Profile) VarsFor(repo *Repo) map[string]string {
	vars := map[string]string{"profilo": p.Name}
	maps.Copy(vars, p.Vars)
	if repo != nil {
		maps.Copy(vars, repo.Vars)
	}
	return vars
}

// SecretsFor returns the provider keys ai-profile resolves when launching an
// agent with the profile. Secrets of MCP servers are resolved by the servers
// themselves (op run) and are not part of it.
func (c *Config) SecretsFor(p *Profile) map[string]string {
	out := map[string]string{}
	maps.Copy(out, p.Secrets)
	return out
}

var placeholder = regexp.MustCompile(`\{[^{}]+\}`)

// Render returns a copy of the server with placeholders replaced and ~
// expanded in the command, in env values and in dirs.
func (s *Server) Render(vars map[string]string, home string) (*Server, error) {
	var missing []string
	sub := func(v string) string {
		return placeholder.ReplaceAllStringFunc(v, func(m string) string {
			if val, ok := vars[m[1:len(m)-1]]; ok {
				return val
			}
			if !slices.Contains(missing, m) {
				missing = append(missing, m)
			}
			return m
		})
	}
	path := func(v string) string {
		v = sub(v)
		if v == "~" || strings.HasPrefix(v, "~/") {
			return Expand(v, home)
		}
		return v
	}
	out := &Server{Command: path(s.Command), URL: sub(s.URL)}
	for _, a := range s.Args {
		out.Args = append(out.Args, sub(a))
	}
	if len(s.Env) > 0 {
		out.Env = map[string]string{}
		for k, v := range s.Env {
			out.Env[k] = path(v)
		}
	}
	if len(s.Headers) > 0 {
		out.Headers = map[string]string{}
		for k, v := range s.Headers {
			out.Headers[k] = sub(v)
		}
	}
	for _, d := range s.Dirs {
		rd := ServerDir{Path: Expand(sub(d.Path), home)}
		for _, o := range d.Only {
			rd.Only = append(rd.Only, sub(o))
		}
		out.Dirs = append(out.Dirs, rd)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("placeholders without a value: %s (add them to vars)", strings.Join(missing, ", "))
	}
	return out, nil
}
