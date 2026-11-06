// Package doctor checks that profiles, shared links, accounts and PATH still
// match config.toml, and says what is off.
package doctor

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/apply"
	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/desktop"
	"github.com/LunetteOrg/ai-profiles/internal/launch"
	"github.com/LunetteOrg/ai-profiles/internal/private"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
	"github.com/LunetteOrg/ai-profiles/internal/skills"
)

type Status int

const (
	OK Status = iota
	Warn
	Fail
)

func (s Status) String() string {
	return [...]string{"ok", "warning", "error"}[s]
}

type Result struct {
	Area   string
	Status Status
	Msg    string
	// Fix, when set, corrects the problem without losing anything (doctor
	// --fix) and says what it did.
	Fix func() (string, error)
}

func (r Result) String() string {
	mark := [...]string{"✓", "!", "✗"}[r.Status]
	return fmt.Sprintf("%s %s: %s", mark, r.Area, r.Msg)
}

type Env struct {
	Home     string
	Cfg      *config.Config
	PathList string // PATH to inspect
	Self     string // path of the ai-profile executable
	Getenv   func(string) string
	// Desktop enables the Claude Desktop checks (macOS); DesktopSystem is
	// how they reach the Mac, nil for desktop.MacOS, and DesktopSource the
	// Claude.app the copies come from, empty for /Applications/Claude.app.
	Desktop       bool
	DesktopSystem desktop.System
	DesktopSource string
	// BitwardenStatus returns bw status for an account, nil for the bw CLI.
	BitwardenStatus func(*secrets.Bitwarden) (string, error)
	// ObsidianConfig is Obsidian's obsidian.json, which lists the vaults
	// whose Claudian settings are checked; empty to skip them.
	ObsidianConfig string
}

func Run(e Env) []Result {
	var res []Result
	add := func(area string, s Status, format string, args ...any) {
		res = append(res, Result{Area: area, Status: s, Msg: fmt.Sprintf(format, args...)})
	}
	// fixable attaches a fix to the result just added.
	fixable := func(fix func() (string, error)) {
		res[len(res)-1].Fix = fix
	}
	stamp := time.Now().Format("20060102-150405")
	tilde := func(p string) string {
		if rel, err := filepath.Rel(e.Home, p); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
		return p
	}

	// Folders declared in config.toml.
	owner := map[string]string{}
	for _, name := range e.Cfg.Names() {
		for _, d := range e.Cfg.Profiles[name].Dirs {
			if prev, dup := owner[d]; dup && prev != name {
				add("Folders", Fail, "%s is associated with two profiles, %s and %s", tilde(d), prev, name)
			}
			owner[d] = name
			if _, err := os.Stat(d); err != nil {
				add("Folders", Warn, "%s (profile %s) does not exist", tilde(d), name)
			}
		}
	}

	// Folders that admit several profiles.
	for _, f := range e.Cfg.Folders {
		if len(f.Profiles) == 1 {
			add("Folders", Warn, "%s lists only %s in [[folders]]: dirs of profile %s says the same", tilde(f.Dir), f.Profiles[0], f.Profiles[0])
		}
		if _, err := os.Stat(f.Dir); err != nil {
			add("Folders", Warn, "%s does not exist ([[folders]])", tilde(f.Dir))
		}
	}
	// A mise or direnv file that sets an agent's folder overrides the profile
	// ai-profile would choose, in the folder and its projects.
	var configured []string
	for _, name := range e.Cfg.Names() {
		configured = append(configured, e.Cfg.Profiles[name].Dirs...)
	}
	for _, f := range e.Cfg.Folders {
		configured = append(configured, f.Dir)
	}
	for _, hit := range forcedAgentDirs(configured) {
		add("Folders", Fail, "%s sets %s: it overrides the profile ai-profile chooses there; remove it and let config.toml decide", tilde(hit.file), hit.variable)
	}

	// Shared instructions.
	agents := filepath.Join(e.Home, ".agents", "AGENTS.md")
	if _, err := os.Stat(agents); err != nil {
		add("Shared parts", Fail, "%s is missing: agents are left without shared instructions", tilde(agents))
	} else {
		add("Shared parts", OK, "%s present", tilde(agents))
	}
	if old := filepath.Join(e.Home, ".claude", "CLAUDE.md"); exists(old) {
		add("Shared parts", Fail, "~/.claude/CLAUDE.md exists: it counts as project instructions in every repo under home (issue #4); move its content to ~/.agents/AGENTS.md and remove it")
		fixable(func() (string, error) {
			bak := old + ".ai-profile-" + stamp
			if err := os.Rename(old, bak); err != nil {
				return "", err
			}
			return fmt.Sprintf("moved to %s: bring what you need into ~/.agents/AGENTS.md", tilde(bak)), nil
		})
	}

	// The private part of the configuration and its backup.
	if e.Cfg.Private.File != "" {
		res = append(res, privateChecks("Private files", "ai-profile backup", e.Home, e.Cfg.Private, e.Cfg.Files, tilde)...)
	}

	// The private part of the ai-secrets configuration.
	if stores, err := secrets.LoadStores(e.Home); err == nil && stores.Private.File != "" {
		res = append(res, privateChecks("Secrets private files", "ai-secrets backup", e.Home, stores.Private, stores.Files, tilde)...)
	} else if missing := (*private.MissingError)(nil); errors.As(err, &missing) {
		add("Secrets private files", Fail, "%s is missing: ai-secrets restore", tilde(missing.File))
	}

	// Claude Code profiles: links and accounts.
	seen := map[string]string{} // accountUuid → profile
	for _, name := range e.Cfg.Names() {
		p := e.Cfg.Profiles[name]
		if !slices.Contains(p.Agents, "claude") {
			continue
		}
		dir := launch.ProfileDir(e.Home, "claude", name)
		area := "Claude " + name
		if _, err := os.Stat(dir); err != nil {
			add(area, Fail, "%s does not exist", tilde(dir))
			continue
		}
		broken := 0
		perProfile := skills.Mode(e.Home, name) == skills.PerProfile
		if !perProfile && len(p.SkillsExclude) > 0 {
			add(area, Warn, "skills_exclude applies only with per-profile skills: ai-profile skills split %s", name)
		}
		for _, entry := range apply.ClaudeSharedLinks {
			if entry == "skills" && perProfile {
				continue // its own folder, kept by sync
			}
			want := apply.ClaudeLinkTarget(e.Home, entry)
			link := filepath.Join(dir, entry)
			msg, missing := checkLink(link, want, tilde)
			if msg == "" {
				continue
			}
			add(area, Fail, "%s", msg)
			broken++
			if missing && exists(want) {
				fixable(func() (string, error) {
					return fmt.Sprintf("%s → %s", tilde(link), tilde(want)), os.Symlink(want, link)
				})
			}
		}
		if broken == 0 {
			add(area, OK, "links to the shared parts in order")
		}

		email, uuid, err := claudeAccount(filepath.Join(dir, ".claude.json"))
		switch {
		case err != nil:
			add(area, Warn, "no login recorded (%v)", err)
		case p.Account != "" && !strings.EqualFold(email, p.Account) && !slices.ContainsFunc(p.ClaudeAccounts, func(a string) bool { return strings.EqualFold(a, email) }):
			add(area, Fail, "logged in as %s, expected %s", email, strings.Join(append([]string{p.Account}, p.ClaudeAccounts...), " or "))
		default:
			add(area, OK, "logged in as %s", email)
		}
		// An account listed in claude_accounts is meant to be shared with
		// the profile it belongs to (ai-profile account use).
		shared := func(name string) bool {
			return slices.ContainsFunc(e.Cfg.Profiles[name].ClaudeAccounts, func(a string) bool { return strings.EqualFold(a, email) })
		}
		if uuid != "" {
			if other, dup := seen[uuid]; dup && !shared(name) && !shared(other) {
				add(area, Fail, "same Claude account as profile %s (%s)", other, email)
			}
			seen[uuid] = name
		}
	}

	// Skills that other agents see and Claude Code does not.
	for _, n := range skills.Unlinked(e.Home) {
		add("Skills", Warn, "%s is in ~/.agents/skills but not in ~/.claude/skills: ai-profile skills link %s", n, n)
	}

	// Codex profiles: a login in the profile folder.
	for _, name := range e.Cfg.Names() {
		if !slices.Contains(e.Cfg.Profiles[name].Agents, "codex") {
			continue
		}
		dir := launch.ProfileDir(e.Home, "codex", name)
		if !exists(filepath.Join(dir, "auth.json")) {
			add("Codex "+name, Warn, "no login in %s: ai-profile %s codex -- login", tilde(dir), name)
		} else {
			add("Codex "+name, OK, "logged in")
		}
	}

	// Claude Desktop of the profiles that declare desktop in config.toml.
	if e.Desktop {
		res = append(res, desktopChecks(e, tilde)...)
	}

	// Claudian runs Claude inside Obsidian and finds sessions and credentials
	// through its own CLAUDE_CONFIG_DIR: the shim in cliPath does not reach it.
	for _, vault := range obsidianVaults(e.ObsidianConfig) {
		env, ok := claudianEnv(filepath.Join(vault, ".claudian", "claudian-settings.json"))
		if !ok {
			continue
		}
		m, err := e.Cfg.Resolve(vault)
		if err != nil || m.Profile == nil || !slices.Contains(m.Profile.Agents, "claude") {
			continue
		}
		want := launch.ProfileDir(e.Home, "claude", m.Profile.Name)
		area := "Claudian " + tilde(vault)
		switch got := env["CLAUDE_CONFIG_DIR"]; {
		case got == "":
			add(area, Fail, "no CLAUDE_CONFIG_DIR in Claudian's environment variables: it looks for sessions in ~/.claude; set CLAUDE_CONFIG_DIR=%s (profile %s)", want, m.Profile.Name)
		case filepath.Clean(got) != want:
			add(area, Fail, "Claudian uses CLAUDE_CONFIG_DIR=%s, but the vault belongs to profile %s (%s): change one of the two on purpose", tilde(got), m.Profile.Name, tilde(want))
		default:
			add(area, OK, "Claudian uses profile %s", m.Profile.Name)
		}
	}

	// Sessions without a profile must not see MCP servers.
	if global := filepath.Join(e.Home, ".claude.json"); len(mcpServers(global)) > 0 {
		names := mcpServers(global)
		add("MCP", Fail, "~/.claude.json has MCP servers visible to sessions without a profile: %s", strings.Join(names, ", "))
		fixable(func() (string, error) {
			bak := global + ".ai-profile-" + stamp
			data, err := os.ReadFile(global)
			if err != nil {
				return "", err
			}
			if err := os.WriteFile(bak, data, 0o600); err != nil {
				return "", err
			}
			err = apply.EditJSONFile(global, func(m map[string]any) bool {
				delete(m, "mcpServers")
				return true
			})
			return fmt.Sprintf("removed %s from ~/.claude.json (copy in %s): declare them in config.toml if a profile needs them", strings.Join(names, ", "), tilde(bak)), err
		})
	} else {
		add("MCP", OK, "no MCP servers in ~/.claude.json")
	}

	// Profiles in line with config.toml: what sync would change.
	switch rep, err := apply.Sync(apply.Options{Home: e.Home, Cfg: e.Cfg, DryRun: true}); {
	case err != nil:
		add("Sync", Fail, "%v", err)
	case len(rep.Actions) > 0:
		add("Sync", Warn, "%d pending changes (ai-profile sync --dry-run lists them): run ai-profile sync", len(rep.Actions))
		fixable(func() (string, error) {
			done, err := apply.Sync(apply.Options{Home: e.Home, Cfg: e.Cfg})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("sync: %d changes", len(done.Actions)), nil
		})
	default:
		add("Sync", OK, "profiles in line with config.toml")
	}

	// Credential folders hold only the files their server allows.
	for _, name := range e.Cfg.Names() {
		p := e.Cfg.Profiles[name]
		scopes := []*config.Repo{nil}
		for i := range p.Repos {
			scopes = append(scopes, &p.Repos[i])
		}
		for _, r := range scopes {
			list := p.MCP
			if r != nil {
				list = r.MCP
			}
			for _, srv := range list {
				rendered, err := e.Cfg.MCP[srv].Render(p.VarsFor(r), e.Home)
				if err != nil {
					continue // reported by Sync
				}
				for _, d := range rendered.Dirs {
					for _, f := range strangers(d) {
						add("MCP "+srv, Warn, "%s contains %s, not allowed by only: it may be another client's account", tilde(d.Path), f)
					}
				}
			}
		}
	}

	// Secrets: the CLI each reference needs, env: variables and Bitwarden
	// logins, without reading 1Password or Bitwarden (that would ask for
	// approval).
	getenv := e.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	for _, name := range e.Cfg.Names() {
		p := e.Cfg.Profiles[name]
		refs := e.Cfg.SecretsFor(p)
		// MCP servers with op:// references start inside op run.
		for _, srv := range profileServers(p) {
			for _, v := range e.Cfg.MCP[srv].Env {
				if secrets.IsOp(v) {
					refs["server "+srv] = v
				}
			}
		}
		if len(refs) == 0 {
			continue
		}
		area := "Secrets " + name
		problems := 0
		tools := map[string]bool{}
		for _, k := range slices.Sorted(maps.Keys(refs)) {
			ref := refs[k]
			switch tool := secrets.Tool(ref); tool {
			case "":
				if _, err := (&secrets.Resolver{Home: e.Home, Getenv: getenv}).Resolve(ref); err != nil {
					add(area, Warn, "%s: %v", k, err)
					problems++
				}
			default:
				// ai-secrets asks the password manager and keeps the values.
				tools[tool], tools["ai-secrets"] = true, true
			}
		}
		for _, tool := range slices.Sorted(maps.Keys(tools)) {
			if lookFirst(tool, e.PathList) == "" {
				add(area, Fail, "references need %s, which is not in PATH", tool)
				problems++
				continue
			}
			if tool != "bw" {
				continue
			}
			if p := nodeScript(lookFirst(tool, e.PathList)); p != "" {
				add(area, Warn, "bw is a Node script (%s): it runs with the node of the folder it starts in; install the native binary (mise: github:bitwarden/clients)", tilde(p))
			}
			// Accounts live in the ai-secrets configuration.
			stores, err := secrets.LoadStores(e.Home)
			if err != nil {
				add(area, Fail, "%v", err)
				problems++
				continue
			}
			names := map[string]bool{}
			for _, ref := range refs {
				if secrets.Tool(ref) != "bw" {
					continue
				}
				name, _ := secrets.SplitBitwardenRef(ref, stores.Bitwarden)
				if name == "" {
					name = p.Bitwarden
				}
				names[name] = true
			}
			status := e.BitwardenStatus
			if status == nil {
				status = bitwardenStatus
			}
			for _, name := range slices.Sorted(maps.Keys(names)) {
				b := stores.Bitwarden[name]
				if b == nil {
					add(area, Fail, "Bitwarden account %q is not in %s ([stores.bitwarden.%s])", name, tilde(stores.Path), name)
					problems++
					continue
				}
				if st, err := status(b); err != nil || st == "unauthenticated" {
					add(area, Fail, "Bitwarden %s not logged in: BITWARDENCLI_APPDATA_DIR=%s bw login", b.Name, tilde(b.DataDir))
					problems++
				}
			}
		}
		if problems == 0 {
			add(area, OK, "%d references reachable", len(refs))
		}
	}

	// Profile selection still done outside config.toml.
	if b, err := os.ReadFile(filepath.Join(e.Home, ".zshrc")); err == nil && strings.Contains(string(b), "export CLAUDE_CONFIG_DIR") {
		add("Profile selection", Warn, "~/.zshrc exports CLAUDE_CONFIG_DIR: the shim honors the variable and does not pick from the folder")
	}
	for _, d := range slices.Sorted(maps.Keys(owner)) {
		mise := filepath.Join(d, "mise.toml")
		if b, err := os.ReadFile(mise); err == nil && strings.Contains(string(b), "CLAUDE_CONFIG_DIR") {
			add("Profile selection", Warn, "%s sets CLAUDE_CONFIG_DIR: a duplicate of config.toml", tilde(mise))
		}
	}

	// Agents in PATH: the shim must come first, the real agent must exist.
	used := map[string]bool{}
	for _, p := range e.Cfg.Profiles {
		for _, a := range p.Agents {
			used[a] = true
		}
	}
	for _, a := range config.KnownAgents {
		if !used[a] {
			continue
		}
		real, err := launch.FindAgent(a, e.PathList, e.Self)
		if err != nil {
			add("PATH", Fail, "%s", err)
			continue
		}
		first := lookFirst(a, e.PathList)
		if first == "" || !sameFile(first, e.Self) {
			add("PATH", Warn, "%s starts from %s without going through ai-profile: the shim is not installed or does not come first", a, tilde(real))
		} else {
			add("PATH", OK, "%s goes through ai-profile, then %s", a, tilde(real))
		}
	}
	// Startup files that undo the shims in the next shells, as an installer
	// does when it appends its own PATH line.
	var usedAgents []string
	for _, a := range config.KnownAgents {
		if used[a] {
			usedAgents = append(usedAgents, a)
		}
	}
	res = append(res, startupPathChecks(e.Home, e.Self, usedAgents, stamp, tilde)...)
	return res
}

// profileServers lists the MCP servers of a profile and of its repos.
func profileServers(p *config.Profile) []string {
	out := slices.Clone(p.MCP)
	for _, r := range p.Repos {
		for _, s := range r.MCP {
			if !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// bitwardenStatus reads the login state of an account from the bw CLI,
// offline.
func bitwardenStatus(b *secrets.Bitwarden) (string, error) {
	cmd := exec.Command("bw", "status")
	cmd.Env = append(os.Environ(), b.Env()...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	var st struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return "", err
	}
	return st.Status, nil
}

// strangers lists the entries of a credential folder that match none of its
// only patterns.
func strangers(d config.ServerDir) []string {
	if len(d.Only) == 0 {
		return nil
	}
	entries, err := os.ReadDir(d.Path)
	if err != nil {
		return nil
	}
	var out []string
	for _, en := range entries {
		if !slices.ContainsFunc(d.Only, func(pat string) bool {
			ok, _ := filepath.Match(pat, en.Name())
			return ok
		}) {
			out = append(out, en.Name())
		}
	}
	return out
}

// checkLink describes what is wrong with the link p, empty when it points
// at want; missing is true when nothing is at p.
func checkLink(p, want string, tilde func(string) string) (msg string, missing bool) {
	info, err := os.Lstat(p)
	if err != nil {
		return fmt.Sprintf("%s is missing (it should point to %s)", tilde(p), tilde(want)), true
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Sprintf("%s is a real file instead of a link to %s: changes do not reach the other profiles", tilde(p), tilde(want)), false
	}
	target, _ := os.Readlink(p)
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(p), target)
	}
	if filepath.Clean(target) != filepath.Clean(want) {
		return fmt.Sprintf("%s points to %s instead of %s", tilde(p), tilde(target), tilde(want)), false
	}
	if _, err := os.Stat(p); err != nil {
		return fmt.Sprintf("%s points to %s, which does not exist", tilde(p), tilde(target)), false
	}
	return "", false
}

// privateChecks reports private files that are missing or changed since
// the last backup, which backup saves.
func privateChecks(area, backup, home string, s private.Settings, files []string, tilde func(string) string) []Result {
	var res []Result
	add := func(st Status, format string, args ...any) {
		res = append(res, Result{Area: area, Status: st, Msg: fmt.Sprintf(format, args...)})
	}
	if s.Restore == "" || s.Backup == "" {
		add(Warn, "[private] needs both a restore and a backup command")
	}
	var missing []string
	for _, f := range append([]string{s.File}, files...) {
		if !exists(f) {
			missing = append(missing, tilde(f))
		}
	}
	if len(missing) > 0 {
		add(Fail, "listed but missing, so %s fails: %s", backup, strings.Join(missing, ", "))
		return res
	}
	changed, err := s.Changed(home, files)
	switch {
	case err != nil:
		add(Warn, "%v", err)
	case len(changed) > 0:
		for i, c := range changed {
			changed[i] = tilde(c)
		}
		add(Warn, "changed since the last backup: %s: run %s", strings.Join(changed, ", "), backup)
	default:
		add(OK, "%d files match the last backup", len(files)+1)
	}
	return res
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func claudeAccount(path string) (email, uuid string, err error) {
	var doc struct {
		OAuthAccount *struct {
			EmailAddress string `json:"emailAddress"`
			AccountUUID  string `json:"accountUuid"`
		} `json:"oauthAccount"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("%s is missing", filepath.Base(path))
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return "", "", fmt.Errorf("%s is unreadable", filepath.Base(path))
	}
	if doc.OAuthAccount == nil || doc.OAuthAccount.EmailAddress == "" {
		return "", "", fmt.Errorf("oauthAccount missing")
	}
	return doc.OAuthAccount.EmailAddress, doc.OAuthAccount.AccountUUID, nil
}

// obsidianVaults lists the vault folders in Obsidian's obsidian.json.
func obsidianVaults(path string) []string {
	var doc struct {
		Vaults map[string]struct {
			Path string `json:"path"`
		} `json:"vaults"`
	}
	b, err := os.ReadFile(path)
	if path == "" || err != nil || json.Unmarshal(b, &doc) != nil {
		return nil
	}
	var out []string
	for _, v := range doc.Vaults {
		if v.Path != "" {
			out = append(out, v.Path)
		}
	}
	slices.Sort(out)
	return out
}

// claudianEnv reads the environment variables Claudian passes to Claude, one
// KEY=VALUE per line; ok is false when the vault has no Claudian settings.
func claudianEnv(path string) (env map[string]string, ok bool) {
	var doc struct {
		ProviderConfigs struct {
			Claude struct {
				EnvironmentVariables string `json:"environmentVariables"`
			} `json:"claude"`
		} `json:"providerConfigs"`
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &doc) != nil {
		return nil, false
	}
	env = map[string]string{}
	for _, line := range strings.Split(doc.ProviderConfigs.Claude.EnvironmentVariables, "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "export ")
		if k, v, found := strings.Cut(line, "="); found && !strings.HasPrefix(k, "#") {
			env[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return env, true
}

func mcpServers(path string) []string {
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &doc) != nil {
		return nil
	}
	return slices.Sorted(maps.Keys(doc.MCPServers))
}

// lookFirst is the first executable named name in pathList, shim included.
func lookFirst(name, pathList string) string {
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// nodeScript returns the file that runs as path when it is a Node script or
// an npm package, else "". A mise shim stands for the tool mise picks for the current folder.
func nodeScript(path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	if filepath.Base(real) == "mise" {
		out, err := exec.Command("mise", "which", filepath.Base(path)).Output()
		if err != nil {
			return ""
		}
		if real, err = filepath.EvalSymlinks(strings.TrimSpace(string(out))); err != nil {
			return ""
		}
	}
	// Package managers wrap the script in a shell shim inside node_modules.
	if strings.Contains(real, string(filepath.Separator)+"node_modules"+string(filepath.Separator)) {
		return real
	}
	f, err := os.Open(real)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, 128)
	n, _ := f.Read(head)
	line, _, _ := strings.Cut(string(head[:n]), "\n")
	if strings.HasPrefix(line, "#!") && strings.Contains(line, "node") {
		return real
	}
	return ""
}

func sameFile(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}

func desktopChecks(e Env, tilde func(string) string) []Result {
	var res []Result
	add := func(area string, s Status, format string, args ...any) {
		res = append(res, Result{Area: area, Status: s, Msg: fmt.Sprintf(format, args...)})
	}
	fixable := func(fix func() (string, error)) {
		res[len(res)-1].Fix = fix
	}
	sys := e.DesktopSystem
	if sys == nil {
		sys = desktop.MacOS{}
	}
	getenv := e.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	aiProfile := apply.StableToolPath(e.Self, getenv)
	for _, name := range e.Cfg.Names() {
		p := e.Cfg.Profiles[name]
		area := "Desktop " + name
		if p.Desktop == nil {
			in := desktop.Instance{Name: name, Home: e.Home}
			if slices.Contains(p.Agents, "claude") && exists(in.Launcher()) {
				add(area, Warn, "%s exists, but the profile has no desktop in config.toml: add desktop = { hue = <degrees> } to profile %s, then ai-profile desktop install %s", tilde(in.Launcher()), name, name)
			}
			continue
		}
		in := desktop.Instance{Name: name, Home: e.Home, Hue: p.Desktop.Hue, Source: e.DesktopSource}
		st := desktop.Check(in, sys)
		problems := 0
		install := func() (string, error) {
			did, err := desktop.Install(in, sys, aiProfile)
			return strings.Join(did, "; "), err
		}
		switch st.Launcher {
		case desktop.LauncherMissing:
			problems++
			add(area, Warn, "no launcher %s: ai-profile desktop install %s", tilde(in.Launcher()), name)
			fixable(install)
		case desktop.LauncherForeign:
			problems++
			add(area, Warn, "%s was not written by ai-profile (claude-multiprofile's?): ai-profile desktop install %s replaces it and moves the old one to the Trash", tilde(in.Launcher()), name)
			fixable(install)
		}
		if len(st.WithoutProfile) > 0 {
			problems++
			add(area, Fail, "%s is open without its profile (pid %s): it shows the default Claude's data and account; ai-profile desktop %s offers to quit and reopen it", in.Label(), joinInts(st.WithoutProfile), name)
		}
		repair := func() (string, error) { return desktop.Repair(in, sys) }
		var stale []string
		for _, l := range st.Leftovers {
			stale = append(stale, tilde(l))
		}
		if len(stale) > 0 {
			problems++
			add(area, Warn, "an update of the copy stopped halfway and left %s", strings.Join(stale, ", "))
			if !st.Running {
				fixable(repair)
			}
		}
		if st.Behind() {
			problems++
			add(area, Warn, "the copy is at Claude %s, %s is at %s: it is updated when it next opens with ai-profile desktop %s", st.Copy, desktopSource(e), st.Source, name)
			if !st.Running {
				fixable(repair)
			}
		}
		if st.Copy != "" && !st.Tinted {
			problems++
			add(area, Warn, "the copy has lost the profile's icon (replaced by Claude's own updater?)")
			fixable(repair)
		}
		if st.Copy == "" && st.Launcher == desktop.LauncherOK && len(stale) == 0 {
			problems++
			add(area, Warn, "no copy of Claude yet: ai-profile desktop install %s makes it", name)
			fixable(repair)
		}
		if problems == 0 {
			add(area, OK, "%s, Claude %s on %s", tilde(in.Launcher()), st.Copy, tilde(in.DataDir()))
		}
	}
	if old := filepath.Join(e.Home, "Library", "Application Support", "claude-multiprofile"); exists(old) {
		add("Desktop", Warn, "%s is left by claude-multiprofile: once the launchers are ai-profile's, quit its copies of Claude and remove the folder", tilde(old))
	}
	return res
}

func desktopSource(e Env) string {
	if e.DesktopSource != "" {
		return e.DesktopSource
	}
	return desktop.DefaultSource
}

func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = fmt.Sprint(n)
	}
	return strings.Join(s, ", ")
}

type forcedDir struct{ file, variable string }

// forcedAgentDirs looks in dirs and their subfolders for mise and direnv
// files that set the folder variable of an agent (CLAUDE_CONFIG_DIR,
// CODEX_HOME…).
func forcedAgentDirs(dirs []string) []forcedDir {
	var vars []string
	for _, a := range launch.Adapters {
		if a.PrimaryVar != "" && !slices.Contains(vars, a.PrimaryVar) {
			vars = append(vars, a.PrimaryVar)
		}
	}
	slices.Sort(vars)
	names := []string{"mise.toml", "mise.local.toml", ".mise.toml", ".mise.local.toml", ".envrc"}
	var out []forcedDir
	seen := map[string]bool{}
	for _, d := range dirs {
		places := []string{d}
		if entries, err := os.ReadDir(d); err == nil {
			for _, en := range entries {
				if en.IsDir() {
					places = append(places, filepath.Join(d, en.Name()))
				}
			}
		}
		for _, p := range places {
			for _, n := range names {
				file := filepath.Join(p, n)
				b, err := os.ReadFile(file)
				if err != nil || seen[file] {
					continue
				}
				seen[file] = true
				for _, v := range vars {
					if regexp.MustCompile(`(?m)^\s*(export\s+)?` + v + `\s*=`).Match(b) {
						out = append(out, forcedDir{file, v})
					}
				}
			}
		}
	}
	return out
}
