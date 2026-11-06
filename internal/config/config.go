// Package config reads ~/.config/ai-profiles/config.toml and picks the profile
// that applies to a folder.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/LunetteOrg/ai-profiles/internal/private"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// KnownAgents are the agents ai-profile can launch, in the order shown to the user.
var KnownAgents = []string{"claude", "codex", "cursor", "gemini", "opencode", "pi"}

// Subcommands are reserved: a profile cannot take one of these names.
var Subcommands = []string{
	"which", "doctor", "new", "mcp", "plugins", "sync", "usage",
	"default", "migrate-default", "handoff", "help", "version", "statusline",
	"backup", "restore", "folder",
}

type Profile struct {
	Name string `toml:"-"`
	// Path is the file that defines the profile: config.toml or its private
	// file.
	Path      string            `toml:"-"`
	Account   string            `toml:"account"`
	Dirs      []string          `toml:"dirs"`
	Agents    []string          `toml:"agents"`
	HandoffTo []string          `toml:"handoff_to"`
	MCP       []string          `toml:"mcp"`
	Vars      map[string]string `toml:"vars"`
	Repos     []Repo            `toml:"repos"`
	// SkillsExclude hides shared skills from a profile with its own skills
	// folder (ai-profile skills split).
	SkillsExclude []string `toml:"skills_exclude"`
	// ClaudeAccounts are the Claude accounts the profile can switch between
	// (ai-profile account use); Account is the usual one.
	ClaudeAccounts []string `toml:"claude_accounts"`
	// Bitwarden names the account, configured in ai-secrets
	// ([stores.bitwarden.<name>]), of bw:// references that name none.
	Bitwarden string `toml:"bitwarden"`
	// Secrets maps environment variables to secret references, resolved at launch.
	Secrets map[string]string `toml:"secrets"`
	// Desktop gives the profile its own Claude Desktop (ai-profile desktop).
	Desktop *Desktop `toml:"desktop"`
}

// Desktop is a profile's Claude Desktop.
type Desktop struct {
	// Hue rotates the color of the Claude icon, in degrees (0-360).
	Hue float64 `toml:"hue"`
}

// Repo adds servers and plugins to one repo of a profile.
type Repo struct {
	Dir     string   `toml:"dir"`
	MCP     []string `toml:"mcp"`
	Plugins []string `toml:"plugins"`
	// DisabledPlugins turns off, in this repo only, plugins enabled everywhere.
	DisabledPlugins []string          `toml:"disabled_plugins"`
	Vars            map[string]string `toml:"vars"`
}

type Config struct {
	Path     string              `toml:"-"`
	Default  string              `toml:"default"`
	Profiles map[string]*Profile `toml:"profiles"`
	MCP      map[string]*Server  `toml:"mcp"`
	// Statusline names the user's own statusline command for each agent;
	// ai-profile runs it with the profile and account in the environment.
	Statusline struct {
		Claude string `toml:"claude"`
	} `toml:"statusline"`
	// Private names the file with the sensitive part of the configuration
	// (client profiles, emails, references) and the commands that back it up
	// and restore it; its profiles and servers are merged with these.
	Private private.Settings `toml:"private"`
	// Files are the other files of the private archive, listed in the
	// private file.
	Files []string `toml:"files"`
	// DefaultFile is the file that sets default.
	DefaultFile string `toml:"-"`
	// Folders are folders used with several profiles.
	Folders []Folder `toml:"folders"`
	// LastUsed, when set, names the profile last used in the Dir of a Folder;
	// Resolve starts from it.
	LastUsed func(dir string) string `toml:"-"`
}

// Folder is a folder that admits several profiles; without a record of the
// last one used there, the first listed applies.
type Folder struct {
	// Path is the file that lists the folder: config.toml or its private file.
	Path     string   `toml:"-"`
	Dir      string   `toml:"dir"`
	Profiles []string `toml:"profiles"`
}

// Match is the profile chosen for a folder. Dir is the configured folder that
// matched, empty when the default profile applies.
type Match struct {
	Profile *Profile
	Dir     string
	// Allowed lists the profiles of a folder in Folders; nil for a profile's
	// own folder or the default.
	Allowed []string
	// Last is true when Profile was chosen as the last used in the folder.
	Last bool
}

// Allows reports whether profile may start here: any profile outside the
// configured folders, the folder's profiles in a folder of Folders, else the
// folder's profile and its handoff_to.
func (m Match) Allows(profile string) bool {
	switch {
	case m.Dir == "":
		return true
	case m.Allowed != nil:
		return slices.Contains(m.Allowed, profile)
	}
	return profile == m.Profile.Name || slices.Contains(m.Profile.HandoffTo, profile)
}

// Admitted lists the profiles a folder admits, Profile first: the others of
// a folder in Folders in their order, else Profile's handoff_to.
func (m Match) Admitted() []string {
	out := []string{m.Profile.Name}
	others := m.Profile.HandoffTo
	if m.Allowed != nil {
		others = m.Allowed
	}
	for _, p := range others {
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// DefaultPath is AI_PROFILES_CONFIG when set, else ~/.config/ai-profiles/config.toml.
func DefaultPath(home string, getenv func(string) string) string {
	if p := getenv("AI_PROFILES_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(home, ".config", "ai-profiles", "config.toml")
}

// Expand replaces a leading ~ with home and cleans the path.
func Expand(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		p = filepath.Join(home, p[2:])
	}
	return filepath.Clean(p)
}

func Load(path, home string) (*Config, error) {
	return load(path, home, nil)
}

// load reads the configuration; swap reads a file from another path, so an
// edit is checked before it replaces the file.
func load(path, home string, swap map[string]string) (*Config, error) {
	read := func(p string, v any) error {
		if s, ok := swap[p]; ok {
			p = s
		}
		_, err := toml.DecodeFile(p, v)
		return err
	}
	cfg := &Config{Path: path}
	if err := read(path, cfg); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(cfg.Files) > 0 {
		return nil, fmt.Errorf("%s: files belongs in the private file ([private] file)", path)
	}
	cfg.DefaultFile = path
	for _, p := range cfg.Profiles {
		p.Path = path
	}
	for i := range cfg.Folders {
		cfg.Folders[i].Path = path
	}
	if cfg.Private.File != "" {
		cfg.Private.File = Expand(cfg.Private.File, home)
		if err := cfg.mergePrivate(home, read); err != nil {
			return nil, err
		}
	}
	if len(cfg.Profiles) == 0 {
		return nil, fmt.Errorf("%s: no profile in [profiles]", path)
	}
	for name, p := range cfg.Profiles {
		p.Name = name
		if slices.Contains(Subcommands, name) || slices.Contains(KnownAgents, name) {
			return nil, fmt.Errorf("%s: %q is a reserved name and cannot be a profile", path, name)
		}
		if len(p.Agents) == 0 {
			p.Agents = []string{"claude"}
		}
		for _, a := range p.Agents {
			if !slices.Contains(KnownAgents, a) {
				return nil, fmt.Errorf("%s: profile %s: unknown agent %q (allowed: %s)", path, name, a, strings.Join(KnownAgents, ", "))
			}
		}
		if d := p.Desktop; d != nil {
			if !slices.Contains(p.Agents, "claude") {
				return nil, fmt.Errorf("%s: profile %s: desktop needs the claude agent", path, name)
			}
			if d.Hue < 0 || d.Hue > 360 {
				return nil, fmt.Errorf("%s: profile %s: desktop.hue must be between 0 and 360 degrees", path, name)
			}
		}
		for _, h := range p.HandoffTo {
			if cfg.Profiles[h] == nil {
				return nil, fmt.Errorf("%s: profile %s: handoff_to names %q, which does not exist", path, name, h)
			}
		}
		for i, d := range p.Dirs {
			p.Dirs[i] = Expand(d, home)
		}
		for k, v := range p.Secrets {
			if !envName.MatchString(k) {
				return nil, fmt.Errorf("%s: profile %s: %q is not a valid variable name", path, name, k)
			}
			if !secrets.IsRef(v) {
				return nil, fmt.Errorf("%s: profile %s: secrets.%s must be a reference (op://, bw://, env:), not the value", path, name, k)
			}
			if secrets.Tool(v) == "bw" && p.Bitwarden == "" && !strings.Contains(strings.TrimPrefix(v, "bw://"), "/") {
				return nil, fmt.Errorf("%s: profile %s: secrets.%s uses Bitwarden, but the profile does not declare bitwarden = \"<account>\"", path, name, k)
			}
		}
		if err := cfg.checkServers(p.MCP); err != nil {
			return nil, fmt.Errorf("%s: profile %s: %w", path, name, err)
		}
		for i := range p.Repos {
			r := &p.Repos[i]
			if r.Dir == "" {
				return nil, fmt.Errorf("%s: profile %s: a repo without dir", path, name)
			}
			r.Dir = Expand(r.Dir, home)
			if err := cfg.checkServers(r.MCP); err != nil {
				return nil, fmt.Errorf("%s: profile %s, repo %s: %w", path, name, r.Dir, err)
			}
		}
	}
	if err := cfg.checkFolders(path, home); err != nil {
		return nil, err
	}
	for name, s := range cfg.MCP {
		if (s.Command == "") == (s.URL == "") {
			return nil, fmt.Errorf("%s: server %s: needs command or url, not both", path, name)
		}
		for k, v := range s.Env {
			// A server has no profile to take a Bitwarden account from: the
			// reference names it.
			if secrets.Tool(v) == "bw" && !strings.Contains(strings.TrimPrefix(v, "bw://"), "/") {
				return nil, fmt.Errorf("%s: server %s: env.%s: a Bitwarden reference of an MCP server names its account (bw://<account>/<item>)", path, name, k)
			}
		}
		for h, v := range s.Headers {
			if secrets.IsRef(v) {
				return nil, fmt.Errorf("%s: server %s: secrets in headers (%s) are not supported yet", path, name, h)
			}
		}
	}
	if cfg.Profiles[cfg.Default] == nil {
		return nil, fmt.Errorf("%s: default = %q is not a profile", path, cfg.Default)
	}
	return cfg, nil
}

// mergePrivate adds the profiles, servers, default and files of the private
// file; each of them is defined in one file only.
func (c *Config) mergePrivate(home string, read func(string, any) error) error {
	file := c.Private.File
	if _, err := os.Stat(file); errors.Is(err, os.ErrNotExist) {
		return &private.MissingError{File: file}
	}
	var priv struct {
		Default  string              `toml:"default"`
		Files    []string            `toml:"files"`
		Profiles map[string]*Profile `toml:"profiles"`
		MCP      map[string]*Server  `toml:"mcp"`
		Folders  []Folder            `toml:"folders"`
		Private  *private.Settings   `toml:"private"`
	}
	if err := read(file, &priv); err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}
	if priv.Private != nil {
		return fmt.Errorf("%s: [private] belongs in %s", file, c.Path)
	}
	if priv.Default != "" {
		if c.Default != "" {
			return fmt.Errorf("default is set in both %s and %s: keep one", c.Path, file)
		}
		c.Default, c.DefaultFile = priv.Default, file
	}
	if c.Profiles == nil {
		c.Profiles = map[string]*Profile{}
	}
	for name, p := range priv.Profiles {
		if c.Profiles[name] != nil {
			return fmt.Errorf("profile %s is in both %s and %s: keep one", name, c.Path, file)
		}
		p.Path = file
		c.Profiles[name] = p
	}
	if c.MCP == nil {
		c.MCP = map[string]*Server{}
	}
	for name, s := range priv.MCP {
		if c.MCP[name] != nil {
			return fmt.Errorf("server %s is in both %s and %s: keep one", name, c.Path, file)
		}
		c.MCP[name] = s
	}
	for _, f := range priv.Files {
		c.Files = append(c.Files, Expand(f, home))
	}
	for _, f := range priv.Folders {
		f.Path = file
		c.Folders = append(c.Folders, f)
	}
	return nil
}

// Names returns the profile names in alphabetical order.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Profiles))
	for n := range c.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Resolve picks the profile for dir: the profile with the deepest configured
// folder that contains dir, or the default one.
func (c *Config) Resolve(dir string) (Match, error) {
	dir = canonical(dir)
	inside := func(d string) bool {
		cd := canonical(d)
		return dir == cd || strings.HasPrefix(dir, cd+string(filepath.Separator))
	}
	var best Match
	var rival *Profile
	for _, name := range c.Names() {
		p := c.Profiles[name]
		for _, d := range p.Dirs {
			if !inside(d) {
				continue
			}
			cd := canonical(d)
			switch {
			case len(cd) > len(canonical(best.Dir)) || best.Profile == nil:
				best, rival = Match{Profile: p, Dir: d}, nil
			case cd == canonical(best.Dir) && p != best.Profile:
				rival = p
			}
		}
	}
	for _, f := range c.Folders {
		if !inside(f.Dir) || best.Profile != nil && len(canonical(f.Dir)) <= len(canonical(best.Dir)) {
			continue
		}
		best, rival = Match{Profile: c.Profiles[f.Profiles[0]], Dir: f.Dir, Allowed: f.Profiles}, nil
		if c.LastUsed != nil {
			if last := c.LastUsed(f.Dir); slices.Contains(f.Profiles, last) {
				best.Profile, best.Last = c.Profiles[last], true
			}
		}
	}
	if rival != nil {
		return Match{}, fmt.Errorf("%s is associated with two profiles, %s and %s: fix %s", best.Dir, best.Profile.Name, rival.Name, c.Path)
	}
	if best.Profile == nil {
		return Match{Profile: c.Profiles[c.Default]}, nil
	}
	return best, nil
}

// checkFolders expands the folders' paths and checks their profiles.
func (c *Config) checkFolders(path, home string) error {
	owner := map[string]string{}
	for _, name := range c.Names() {
		for _, d := range c.Profiles[name].Dirs {
			owner[d] = name
		}
	}
	seen := map[string]bool{}
	for i := range c.Folders {
		f := &c.Folders[i]
		if f.Dir == "" {
			return fmt.Errorf("%s: a [[folders]] entry without dir", path)
		}
		f.Dir = Expand(f.Dir, home)
		if len(f.Profiles) == 0 {
			return fmt.Errorf("%s: folder %s lists no profiles", path, f.Dir)
		}
		for _, p := range f.Profiles {
			if c.Profiles[p] == nil {
				return fmt.Errorf("%s: folder %s lists %q, which is not a profile", path, f.Dir, p)
			}
		}
		if p := owner[f.Dir]; p != "" {
			return fmt.Errorf("%s: %s is both in [[folders]] and in the dirs of profile %s: keep one", path, f.Dir, p)
		}
		if seen[f.Dir] {
			return fmt.Errorf("%s: folder %s is listed twice in [[folders]]", path, f.Dir)
		}
		seen[f.Dir] = true
	}
	return nil
}

// canonical resolves symlinks when the path exists, so a folder reached through
// a link matches the configured one.
func canonical(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// Exists reports whether the config file is present.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
