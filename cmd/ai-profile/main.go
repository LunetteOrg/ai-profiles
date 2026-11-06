// ai-profile launches coding agents with the profile of the current folder.
//
// Called as ai-profile it takes subcommands; called through a link named like
// an agent (claude, codex…) it is the shim: it launches that agent with the
// folder's profile and passes every argument through.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/LunetteOrg/ai-profiles/internal/accounts"
	"github.com/LunetteOrg/ai-profiles/internal/apply"
	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/desktop"
	"github.com/LunetteOrg/ai-profiles/internal/doctor"
	"github.com/LunetteOrg/ai-profiles/internal/gitroot"
	"github.com/LunetteOrg/ai-profiles/internal/handoff"
	"github.com/LunetteOrg/ai-profiles/internal/lastused"
	"github.com/LunetteOrg/ai-profiles/internal/launch"
	"github.com/LunetteOrg/ai-profiles/internal/private"
	"github.com/LunetteOrg/ai-profiles/internal/secretagent"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
	"github.com/LunetteOrg/ai-profiles/internal/skills"
	"github.com/LunetteOrg/ai-profiles/internal/usage"
)

const helpText = `ai-profile: launches agents with the folder's profile.

  ai-profile [<profile>] [<agent>] [-- <arguments>]
                       launches the agent (default claude) with the folder's
                       profile, or with the one given
  ai-profile --auto [-- <arguments>]
                       launches, among the profiles and agents allowed in the
                       folder, the one with the most headroom according to usage
  ai-profile handoff <profile>|<agent> [--session <id>]
                       continues the last Claude session here in another
                       profile, or the work with another agent
  ai-profile which [<agent>]
                       tells which profile applies here and why
  ai-profile sync [--dry-run]
                       brings profiles and repos to the state of config.toml:
                       MCP servers, credential folders, links, plugins
  ai-profile new <profile> [--dir <folder>…] [--agents claude,codex…] [--account <email>]
                       creates the profile: entry in config.toml, Claude
                       folder with the shared parts, then sync
  ai-profile mcp <profile> [--repo <folder>] add <server>…
                       declares [mcp] servers for the profile or one of its
                       repos, then sync
  ai-profile plugins <repo> enable|disable <plugin>…
                       enables or disables Claude Code plugins in a repo,
                       then sync
  ai-profile skills list [<profile>] | split <profile> | share <profile> <skill> | link <skill>
                       skills seen by a profile; a profile's own skills;
                       sharing a skill; linking a skill from ~/.agents
  ai-profile account list | save | use <email> [--profile <profile>]
                       the profile's Claude accounts: save them and switch
                       between them, even with sessions open
  ai-profile desktop [<profile>] | install [<profile>]
                       opens the profile's Claude Desktop, by default the
                       folder's profile, updating its copy of Claude first;
                       install writes the launchers in ~/Applications and
                       prepares the copies
  ai-profile folder add [<folder>] <profile>… | rm [<folder>] [<profile>…] | list
                       folders used with several profiles ([[folders]]),
                       by default the current one; claude starts there with
                       the last used
  ai-profile usage [--json]
                       usage of every profile and every agent
  ai-profile default <profile>
                       makes the profile the default outside the associated
                       folders
  ai-profile migrate-default <profile> [--dry-run]
                       copies sessions, memory and history from ~/.claude into
                       the profile (without overwriting) and makes it the default
  ai-profile backup    saves the private file and the files it lists with the
                       [private] backup command
  ai-profile restore [--force]
                       brings them back with the [private] restore command;
                       files that differ stay as they are without --force
  ai-profile doctor [--fix]
                       checks profiles, links, accounts and PATH;
                       --fix corrects what it can without losing anything
  ai-profile version   program version

Agents: %s. Configuration: %s
A link named like an agent (e.g. claude → ai-profile) placed earlier in
PATH acts as a shim.
`

// version is set by GoReleaser (-X main.version=…); go install leaves it
// empty and the module version is used instead.
var version string

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "(devel)"
}

func main() {
	if err := run(); err != nil {
		var exit exitError
		if errors.As(err, &exit) {
			os.Exit(int(exit))
		}
		fmt.Fprintln(os.Stderr, "ai-profile:", err)
		os.Exit(1)
	}
}

type exitError int

func (e exitError) Error() string { return fmt.Sprintf("exit %d", int(e)) }

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfgPath := config.DefaultPath(home, os.Getenv)
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")

	// Shim: every argument belongs to the agent.
	if slices.Contains(config.KnownAgents, name) {
		cfg, err := loadConfig(cfgPath, home)
		if err != nil {
			return err
		}
		return start(cfg, home, launch.Request{Agent: name, Args: os.Args[1:]})
	}

	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "statusline":
			// Called by Claude Code at every refresh: it must always print
			// something, even without a readable configuration.
			cfg, _ := config.Load(cfgPath, home)
			return statusline(cfg, home, os.Getenv, os.Stdin, os.Stdout)
		case "--auto", "handoff":
			cfg, err := loadConfig(cfgPath, home)
			if err != nil {
				return err
			}
			if args[0] == "--auto" {
				return runAuto(cfg, home, args[1:])
			}
			return runHandoff(cfg, home, args[1:])
		case "version", "--version":
			fmt.Println("ai-profile", versionString())
			return nil
		case "help", "-h", "--help":
			fmt.Printf(helpText, strings.Join(config.KnownAgents, ", "), cfgPath)
			return nil
		case "restore":
			return runRestore(cfgPath, home, args[1:])
		case "backup":
			cfg, err := config.Load(cfgPath, home)
			if err != nil {
				return err
			}
			return runBackup(cfg, home, args[1:])
		case "which", "doctor", "sync", "mcp", "plugins", "new", "default", "migrate-default", "usage", "skills", "desktop", "account", "folder":
			cfg, err := loadConfig(cfgPath, home)
			if err != nil {
				return err
			}
			switch args[0] {
			case "which":
				return which(cfg, home, args[1:])
			case "sync":
				return runSync(cfg, home, args[1:])
			case "mcp":
				return runMCP(cfg, home, args[1:])
			case "plugins":
				return runPlugins(cfg, home, args[1:])
			case "new":
				return runNew(cfg, home, args[1:])
			case "default":
				return runDefault(cfg, home, args[1:])
			case "usage":
				return runUsage(cfg, home, args[1:])
			case "skills":
				return runSkills(cfg, home, args[1:])
			case "desktop":
				return runDesktop(cfg, home, args[1:])
			case "account":
				return runAccount(cfg, home, args[1:])
			case "folder":
				return runFolder(cfg, home, args[1:])
			case "migrate-default":
				return runMigrateDefault(cfg, home, args[1:])
			}
			return runDoctor(cfg, home, args[1:])
		}
		if slices.Contains(config.Subcommands, args[0]) {
			return fmt.Errorf("%s is not available yet", args[0])
		}
	}

	cfg, err := loadConfig(cfgPath, home)
	if err != nil {
		return err
	}
	req, err := parseLaunch(cfg, args)
	if err != nil {
		return err
	}
	return start(cfg, home, req)
}

// parseLaunch reads [<profile>] [<agent>] [-- <arguments>].
func parseLaunch(cfg *config.Config, args []string) (launch.Request, error) {
	req := launch.Request{Agent: "claude"}
	if i := slices.Index(args, "--"); i >= 0 {
		args, req.Args = args[:i], args[i+1:]
	}
	if len(args) > 2 {
		return req, fmt.Errorf("too many arguments before --: %s", strings.Join(args, " "))
	}
	agentSet := false
	for _, a := range args {
		switch {
		case slices.Contains(config.KnownAgents, a) && !agentSet:
			req.Agent, agentSet = a, true
		case cfg.Profiles[a] != nil && req.Profile == "" && !agentSet:
			req.Profile = a
		default:
			return req, fmt.Errorf("%q is neither a profile (%s) nor an agent (%s)",
				a, strings.Join(cfg.Names(), ", "), strings.Join(config.KnownAgents, ", "))
		}
	}
	return req, nil
}

func start(cfg *config.Config, home string, req launch.Request) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	plan, err := launch.Prepare(cfg, home, cwd, os.Getenv, req)
	if err != nil {
		return err
	}
	for _, n := range plan.Notes {
		fmt.Fprintln(os.Stderr, "ai-profile:", n)
	}
	// In a folder with several profiles the next plain launch starts from this one.
	if plan.Match.Allowed != nil && plan.Profile != nil && plan.Inherited == "" {
		if err := lastused.Set(home, plan.Match.Dir, plan.Profile.Name); err != nil {
			fmt.Fprintln(os.Stderr, "ai-profile: could not record the profile used here:", err)
		}
	}
	if err := plan.ResolveSecrets(agentSource{home}, &secrets.Resolver{Home: home, Getenv: os.Getenv, Run: secrets.Exec}, cfg, home); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	bin, err := launch.FindAgent(plan.Agent, os.Getenv("PATH"), self)
	if err != nil {
		return err
	}
	return execAgent(bin, append([]string{plan.Agent}, plan.Args...), launch.Environ(os.Environ(), plan.Env))
}

// agentSource asks the ai-secrets agent, starting it when needed.
type agentSource struct{ home string }

func (a agentSource) Get(reason string, refs map[string]string, account string) (map[string]string, error) {
	bin, err := exec.LookPath("ai-secrets")
	if err != nil {
		return nil, errors.New("ai-secrets is not in PATH: it is needed for the profile's op:// and bw:// keys")
	}
	return secretagent.NewClient(a.home, bin).Get(reason, refs, account)
}

func which(cfg *config.Config, home string, args []string) error {
	agent := "claude"
	if len(args) > 0 {
		agent = args[0]
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	plan, err := launch.Prepare(cfg, home, cwd, os.Getenv, launch.Request{Agent: agent})
	if err != nil {
		return err
	}
	primary := launch.Adapters[agent].PrimaryVar
	switch {
	case plan.Profile == plan.Match.Profile:
		fmt.Printf("profile: %s\n", plan.Profile.Name)
		fmt.Printf("why:     %s\n", launch.Why(plan.Match, cfg))
	case plan.Profile != nil:
		fmt.Printf("profile: %s\n", plan.Profile.Name)
		fmt.Printf("why:     %s is already set in the environment\n", primary)
	default:
		fmt.Printf("profile: none of %s\n", cfg.Path)
		fmt.Printf("why:     %s is already set in the environment\n", primary)
	}
	if plan.Profile != nil {
		fmt.Printf("agents:  %s\n", strings.Join(plan.Profile.Agents, ", "))
	}
	if plan.Inherited != "" {
		fmt.Printf("%s:  %s=%s (from the environment)\n", agent, primary, plan.Inherited)
	}
	for _, k := range slices.Sorted(maps.Keys(plan.Env)) {
		fmt.Printf("%s:  %s=%s\n", agent, k, plan.Env[k])
	}
	for _, k := range slices.Sorted(maps.Keys(plan.Secrets)) {
		fmt.Printf("secret:  %s from %s\n", k, plan.Secrets[k])
	}
	for _, n := range plan.Notes {
		fmt.Printf("note:    %s\n", n)
	}
	return nil
}

func runSync(cfg *config.Config, home string, args []string) error {
	dry := false
	for _, a := range args {
		switch a {
		case "-n", "--dry-run":
			dry = true
		default:
			return fmt.Errorf("sync: unknown option %q", a)
		}
	}
	return syncAndReport(cfg, home, dry)
}

func syncAndReport(cfg *config.Config, home string, dry bool) error {
	self, _ := os.Executable()
	rep, err := apply.Sync(apply.Options{Home: home, Cfg: cfg, DryRun: dry, Self: self})
	if err != nil {
		return err
	}
	printReport(home, rep, dry)
	switch {
	case len(rep.Actions) == 0:
	case !dry && slices.ContainsFunc(rep.Actions, func(a apply.Action) bool { return strings.HasSuffix(a.Path, ".claude.json") }):
		fmt.Println("Claude sessions already open see the new servers only after a restart.")
	}
	return nil
}

// printReport lists actions grouped by file, then notes.
func printReport(home string, rep *apply.Report, dry bool) {
	verb := ""
	if dry {
		verb = "to do: "
	}
	var paths []string
	byPath := map[string][]string{}
	for _, a := range rep.Actions {
		if _, seen := byPath[a.Path]; !seen {
			paths = append(paths, a.Path)
		}
		byPath[a.Path] = append(byPath[a.Path], a.What)
	}
	for _, p := range paths {
		fmt.Println(tilde(home, p))
		for _, w := range byPath[p] {
			fmt.Printf("  %s%s\n", verb, w)
		}
	}
	for _, n := range rep.Notes {
		fmt.Println("note:", n)
	}
	if len(rep.Actions) == 0 {
		fmt.Println("nothing to change")
	}
}

// runMCP: mcp <profile> [--repo <folder>] add <server>…
func runMCP(cfg *config.Config, home string, args []string) error {
	const use = "usage: ai-profile mcp <profile> [--repo <folder>] add <server>…"
	if len(args) < 3 || cfg.Profiles[args[0]] == nil {
		return errors.New(use)
	}
	profile, rest, repo := args[0], args[1:], ""
	if rest[0] == "--repo" {
		if len(rest) < 3 {
			return errors.New(use)
		}
		dir, err := absDir(rest[1], home)
		if err != nil {
			return err
		}
		repo, rest = dir, rest[2:]
	}
	if len(rest) < 2 || rest[0] != "add" {
		return errors.New(use)
	}
	for _, srv := range rest[1:] {
		if cfg.MCP[srv] == nil {
			return fmt.Errorf("server %q is not defined in [mcp] of %s", srv, cfg.Path)
		}
		file := cfg.Path
		if p := cfg.Profiles[profile]; p != nil {
			file = p.Path
		}
		err := config.EditFile(cfg.Path, file, home, func(b []byte) ([]byte, bool, error) {
			if repo != "" {
				return config.AddToRepo(b, home, profile, repo, "mcp", srv)
			}
			return config.AddToProfile(b, profile, "mcp", srv)
		})
		if err != nil {
			return err
		}
	}
	return reloadAndSync(cfg.Path, home)
}

// runPlugins: plugins <repo> enable|disable <plugin>…
func runPlugins(cfg *config.Config, home string, args []string) error {
	if len(args) < 3 || (args[1] != "enable" && args[1] != "disable") {
		return errors.New("usage: ai-profile plugins <repo> enable|disable <plugin>…")
	}
	repo, err := absDir(args[0], home)
	if err != nil {
		return err
	}
	match, err := cfg.Resolve(repo)
	if err != nil {
		return err
	}
	profile := match.Profile.Name
	add, remove := "plugins", "disabled_plugins"
	if args[1] == "disable" {
		add, remove = remove, add
	}
	for _, name := range args[2:] {
		id, err := apply.ResolvePlugin(home, name)
		if err != nil {
			return fmt.Errorf("plugin %s: %w", name, err)
		}
		err = config.EditFile(cfg.Path, match.Profile.Path, home, func(b []byte) ([]byte, bool, error) {
			changed := false
			for _, n := range slices.Compact([]string{name, id}) {
				out, ch, err := config.RemoveFromRepo(b, home, profile, repo, remove, n)
				if err != nil {
					return nil, false, err
				}
				if ch {
					b, changed = out, true
				}
			}
			out, ch, err := config.AddToRepo(b, home, profile, repo, add, id)
			if err != nil {
				return nil, false, err
			}
			if ch {
				b, changed = out, true
			}
			return b, changed, nil
		})
		if err != nil {
			return err
		}
	}
	fmt.Printf("repo %s in profile %s\n", tilde(home, repo), profile)
	return reloadAndSync(cfg.Path, home)
}

// parseNew: <profile> [--dir <folder>…] [--agents a,b…] [--account <email>]
func parseNew(args []string, home string) (config.NewProfile, error) {
	const use = "usage: ai-profile new <profile> [--dir <folder>…] [--agents claude,codex…] [--account <email>]"
	p := config.NewProfile{}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return p, errors.New(use)
	}
	p.Name = args[0]
	for i := 1; i < len(args); {
		flag := args[i]
		var vals []string
		for i++; i < len(args) && !strings.HasPrefix(args[i], "--"); i++ {
			vals = append(vals, args[i])
		}
		switch flag {
		case "--dir":
			if len(vals) == 0 {
				return p, errors.New(use)
			}
			for _, v := range vals {
				d, err := absDir(v, home)
				if err != nil {
					return p, err
				}
				p.Dirs = append(p.Dirs, d)
			}
		case "--agents":
			for _, v := range vals {
				for _, a := range strings.Split(v, ",") {
					if !slices.Contains(config.KnownAgents, a) {
						return p, fmt.Errorf("unknown agent %q (allowed: %s)", a, strings.Join(config.KnownAgents, ", "))
					}
					p.Agents = append(p.Agents, a)
				}
			}
		case "--account":
			if len(vals) != 1 {
				return p, errors.New(use)
			}
			p.Account = vals[0]
		case "--desktop":
			return p, errors.New("Claude Desktop for a profile: add desktop = { hue = <degrees> } to it in config.toml, then ai-profile desktop install <profile>")
		default:
			return p, fmt.Errorf("unknown option %q\n%s", flag, use)
		}
	}
	if len(p.Agents) == 0 {
		p.Agents = []string{"claude"}
	}
	return p, nil
}

func runNew(cfg *config.Config, home string, args []string) error {
	p, err := parseNew(args, home)
	if err != nil {
		return err
	}
	if cfg.Profiles[p.Name] != nil {
		return fmt.Errorf("profile %s already exists in %s", p.Name, tilde(home, cfg.Path))
	}
	for _, d := range p.Dirs {
		if m, err := cfg.Resolve(d); err == nil && m.Dir != "" && filepath.Clean(m.Dir) == filepath.Clean(d) {
			return fmt.Errorf("%s is already associated with profile %s", tilde(home, d), m.Profile.Name)
		}
	}
	if slices.Contains(p.Agents, "claude") {
		if _, err := os.Lstat(launch.ProfileDir(home, "claude", p.Name)); err == nil {
			return fmt.Errorf("%s already exists", tilde(home, launch.ProfileDir(home, "claude", p.Name)))
		}
	}
	// A new profile names a client: it goes into the private file, when
	// there is one.
	file := cfg.Path
	if cfg.Private.File != "" {
		file = cfg.Private.File
	}
	if err := config.EditFile(cfg.Path, file, home, func(b []byte) ([]byte, bool, error) {
		out, err := config.AppendProfile(b, home, p)
		return out, err == nil, err
	}); err != nil {
		return err
	}
	fmt.Printf("%s: added [profiles.%s]\n", tilde(home, file), p.Name)
	if slices.Contains(p.Agents, "claude") {
		rep, err := apply.CreateClaudeProfile(home, p.Name, false)
		if err != nil {
			return err
		}
		for _, a := range rep.Actions {
			fmt.Printf("%s\n  %s\n", tilde(home, a.Path), a.What)
		}
		for _, n := range rep.Notes {
			fmt.Println("note:", n)
		}
	}
	if err := reloadAndSync(cfg.Path, home); err != nil {
		return err
	}
	fmt.Println("\nLogin, on the first start of each agent:")
	for _, a := range p.Agents {
		switch a {
		case "claude":
			fmt.Printf("  ai-profile %s claude            (Claude asks for the login)\n", p.Name)
		case "codex":
			fmt.Printf("  ai-profile %s codex -- login\n", p.Name)
		case "opencode":
			fmt.Printf("  ai-profile %s opencode -- auth login\n", p.Name)
		}
	}
	return nil
}

var weekdays = [...]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// formatReset shows a reset time as 15:30 today, or "Tue 27 10:00".
func formatReset(t, now time.Time) string {
	if t.IsZero() {
		return "?"
	}
	t = t.Local()
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("15:04")
	}
	return fmt.Sprintf("%s %d %s", weekdays[t.Weekday()], t.Day(), t.Format("15:04"))
}

func runUsage(cfg *config.Config, home string, args []string) error {
	if len(args) == 2 && args[0] == "--raw" {
		if cfg.Profiles[args[1]] == nil {
			return fmt.Errorf("unknown profile %q", args[1])
		}
		body, err := usage.ClaudeRaw(home, args[1], usage.Sources{})
		if err != nil {
			return err
		}
		fmt.Println(string(body))
		return nil
	}
	asJSON := false
	for _, a := range args {
		if a != "--json" {
			return fmt.Errorf("usage: ai-profile usage [--json] | usage --raw <profile>")
		}
		asJSON = true
	}
	rows := usage.Collect(cfg, usage.Sources{Home: home})
	if asJSON {
		type jsonRow struct {
			Profile string         `json:"profile"`
			Agent   string         `json:"agent"`
			Account string         `json:"account,omitempty"`
			Active  bool           `json:"active,omitempty"`
			Windows []usage.Window `json:"windows,omitempty"`
			Detail  string         `json:"detail,omitempty"`
			AsOf    *time.Time     `json:"as_of,omitempty"`
			Error   string         `json:"error,omitempty"`
		}
		out := make([]jsonRow, 0, len(rows))
		for _, r := range rows {
			jr := jsonRow{Profile: r.Profile, Agent: r.Agent, Account: r.Account, Active: r.Active, Windows: r.Windows, Detail: r.Detail}
			if !r.AsOf.IsZero() {
				jr.AsOf = &r.AsOf
			}
			if r.Err != nil {
				jr.Error = r.Err.Error()
			}
			out = append(out, jr)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	now := time.Now()
	color := isTerminal(os.Stdout)
	paint := func(sev, text string) string {
		code := map[string]string{"warning": "33", "critical": "31"}[sev]
		if !color || code == "" {
			return text
		}
		return "\033[" + code + "m" + text + "\033[0m"
	}
	// An asterisk marks the active account where a profile has several.
	perProfile := map[string]int{}
	for _, r := range rows {
		if r.Agent == "claude" {
			perProfile[r.Profile]++
		}
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "profile\tagent\taccount\tusage")
	for _, r := range rows {
		account := r.Account
		if r.Active && perProfile[r.Profile] > 1 {
			account = "* " + account
		}
		var cells []string
		switch {
		case r.Err != nil:
			cells = append(cells, "— "+r.Err.Error())
		case r.Detail != "":
			cells = append(cells, r.Detail)
		default:
			for _, w := range r.Windows {
				cells = append(cells, paint(w.Severity, fmt.Sprintf("%s %.0f%% (resets %s)", w.Name, w.Used, formatReset(w.Resets, now))))
			}
			if !r.AsOf.IsZero() {
				cells = append(cells, "data from the last session, "+formatReset(r.AsOf, now))
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.Profile, r.Agent, account, strings.Join(cells, " · "))
	}
	return tw.Flush()
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// folderMatch is the profile of the current folder, worktrees included.
func folderMatch(cfg *config.Config) (config.Match, string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return config.Match{}, "", err
	}
	m, err := cfg.Resolve(gitroot.LookupDir(cwd))
	return m, cwd, err
}

// statusline prints the Claude Code statusline: the user's own command
// ([statusline] claude), run with Claude's input and AI_PROFILE and
// AI_PROFILE_ACCOUNT in its environment, or "@<profile> <account>" without one.
func statusline(cfg *config.Config, home string, getenv func(string) string, in io.Reader, out io.Writer) error {
	dir := getenv("CLAUDE_CONFIG_DIR")
	profile := strings.TrimPrefix(filepath.Base(dir), ".claude-")
	if cfg != nil {
		if p := launch.ProfileOwning(cfg, home, "claude", dir, getenv); p != nil {
			profile = p.Name
		}
	}
	if dir == "" {
		profile = ""
	}
	var account string
	if b, err := os.ReadFile(filepath.Join(dir, ".claude.json")); err == nil {
		var doc struct {
			OAuthAccount struct {
				Email string `json:"emailAddress"`
			} `json:"oauthAccount"`
		}
		json.Unmarshal(b, &doc)
		account = doc.OAuthAccount.Email
	}
	if cfg != nil && cfg.Statusline.Claude != "" {
		cmd := exec.Command("sh", "-c", cfg.Statusline.Claude)
		cmd.Env = append(os.Environ(), "AI_PROFILE="+profile, "AI_PROFILE_ACCOUNT="+account)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, os.Stderr
		if cmd.Run() == nil {
			return nil
		}
	}
	if profile == "" {
		_, err := fmt.Fprint(out, "\033[1;31m@no-profile\033[0m")
		return err
	}
	_, err := fmt.Fprintf(out, "\033[1;35m@%s\033[0m \033[0;35m%s\033[0m", profile, account)
	return err
}

func runAuto(cfg *config.Config, home string, args []string) error {
	var rest []string
	if len(args) > 0 {
		if args[0] != "--" {
			return errors.New("usage: ai-profile --auto [-- <arguments>]")
		}
		rest = args[1:]
	}
	m, _, err := folderMatch(cfg)
	if err != nil {
		return err
	}
	combos := handoff.Allowed(cfg, m)
	if len(combos) == 0 {
		return fmt.Errorf("no agent with an adapter in profile %s", m.Profile.Name)
	}
	rows := usage.Collect(cfg, usage.Sources{Home: home, Only: func(p, a string) bool {
		return slices.Contains(combos, handoff.Combo{Profile: p, Agent: a})
	}})
	best, why := handoff.Best(combos, rows)
	fmt.Fprintf(os.Stderr, "ai-profile: --auto picks %s with %s (%s)\n", best.Agent, best.Profile, why)
	return start(cfg, home, launch.Request{Agent: best.Agent, Profile: best.Profile, Args: rest})
}

func runHandoff(cfg *config.Config, home string, args []string) error {
	const use = "usage: ai-profile handoff <profile>|<agent> [--session <id>]"
	if len(args) != 1 && !(len(args) == 3 && args[1] == "--session") {
		return errors.New(use)
	}
	target, id := args[0], ""
	if len(args) == 3 {
		id = args[2]
	}
	// Inside an agent's session, the session to hand off is this one, and the
	// next agent needs a terminal of its own.
	current := "claude"
	switch {
	case os.Getenv("CODEX_SESSION_ID") != "":
		current = "codex"
	case os.Getenv("OPENCODE_SESSION_ID") != "":
		current = "opencode"
	}
	sessionVar := map[string]string{"claude": "CLAUDE_CODE_SESSION_ID", "codex": "CODEX_SESSION_ID", "opencode": "OPENCODE_SESSION_ID"}[current]
	inside := os.Getenv("CLAUDECODE") == "1" || current != "claude" || !isTerminal(os.Stdin)
	if id == "" && inside {
		id = os.Getenv(sessionVar)
	}
	m, cwd, err := folderMatch(cfg)
	if err != nil {
		return err
	}
	// The source profile is the one whose agent folder is in use; a folder
	// that belongs to no profile (Orca sets its own CODEX_HOME), or none set,
	// falls back to the folder's profile.
	profile := m.Profile
	srcDir := launch.ProfileDir(home, current, profile.Name)
	if dir := os.Getenv(launch.Adapters[current].PrimaryVar); dir != "" {
		if p := launch.ProfileOwning(cfg, home, current, dir, os.Getenv); p != nil {
			profile = p
		} else {
			fmt.Fprintf(os.Stderr, "ai-profile: %s is not the folder of any profile; using the folder's profile, %s\n", dir, profile.Name)
		}
		srcDir = dir
	} else if current == "opencode" {
		srcDir = filepath.Join(home, ".cache", "ai-profiles") // OpenCode without a profile
	}
	var session string
	var export []byte
	switch current {
	case "codex":
		session, err = handoff.FindCodexSession(srcDir, id)
	case "opencode":
		if id == "" {
			return errors.New("no OpenCode session id (OPENCODE_SESSION_ID is not set)")
		}
		cmd := exec.Command("opencode", "session", "export", id)
		cmd.Stderr = os.Stderr
		if export, err = cmd.Output(); err != nil {
			return fmt.Errorf("opencode session export %s: %w", id, err)
		}
		session = filepath.Join(srcDir, "handoffs", time.Now().Format("20060102-150405")+"-"+id+".json")
		if err := os.MkdirAll(filepath.Dir(session), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(session, export, 0o600); err != nil {
			return err
		}
	default:
		session, err = handoff.FindSession(srcDir, cwd, id)
	}
	if err != nil {
		return err
	}
	allowed := handoff.Allowed(cfg, m)

	switch {
	case cfg.Profiles[target] != nil:
		if current != "claude" {
			return fmt.Errorf("from %s a handoff goes to another agent (for example claude); resuming in another profile works for Claude Code sessions", current)
		}
		if !slices.Contains(allowed, handoff.Combo{Profile: target, Agent: "claude"}) {
			return fmt.Errorf("%s is not among the profiles admitted here (%s): the handoff would carry the client's data to another account", target, strings.Join(m.Admitted(), ", "))
		}
		sid, err := handoff.CopySession(session, launch.ProfileDir(home, "claude", target), cwd)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ai-profile: session %s copied into profile %s\n", sid, target)
		if inside {
			return openNext(cwd, "handoff → "+target, []string{target, "claude", "--", "--resume", sid})
		}
		return start(cfg, home, launch.Request{Agent: "claude", Profile: target, Args: []string{"--resume", sid}})

	case slices.Contains(config.KnownAgents, target):
		if !slices.Contains(allowed, handoff.Combo{Profile: profile.Name, Agent: target}) {
			return fmt.Errorf("%s is not among the agents of profile %s, or has no adapter yet", target, profile.Name)
		}
		if target == current {
			return fmt.Errorf("this is already a %s session", current)
		}
		var summary string
		switch current {
		case "codex":
			summary, err = handoff.SummaryCodex(session)
		case "opencode":
			summary, err = handoff.SummaryOpenCode(export, session)
		default:
			summary, err = handoff.Summary(session)
		}
		if err != nil {
			return err
		}
		dir := filepath.Join(srcDir, "handoffs")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		// The OpenCode export is already saved there with a timestamp: the
		// summary takes its name; other sessions get one.
		file := filepath.Join(dir, time.Now().Format("20060102-150405")+"-"+strings.TrimSuffix(filepath.Base(session), ".jsonl")+".md")
		if current == "opencode" {
			file = strings.TrimSuffix(session, ".json") + ".md"
		}
		if err := os.WriteFile(file, []byte(summary), 0o600); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "ai-profile: summary in %s\n", tilde(home, file))
		if inside {
			next := "Read the handoff summary in " + file + " and continue the work."
			args := []string{profile.Name, target, "--", next}
			if target == "opencode" {
				args = []string{profile.Name, target, "--", "--prompt", next}
			}
			return openNext(cwd, "handoff → "+target, args)
		}
		prompt := []string{summary}
		if target == "opencode" {
			prompt = []string{"--prompt", summary}
		}
		return start(cfg, home, launch.Request{Agent: target, Profile: profile.Name, Args: prompt})
	}
	return fmt.Errorf("%q is neither a profile (%s) nor an agent (%s)", target, strings.Join(cfg.Names(), ", "), strings.Join(config.KnownAgents, ", "))
}

// openNext starts "ai-profile <args>" in a new terminal (Orca, Terminal.app,
// or the clipboard) from a session that has no terminal of its own.
func openNext(cwd, title string, args []string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	has := func(name string) bool { _, err := exec.LookPath(name); return err == nil }
	t := handoff.ChooseTerminal(os.Getenv, has, runtime.GOOS)
	line := handoff.ShellCommand(cwd, append([]string{self}, args...))
	where, err := handoff.Open(t, title, line)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ai-profile: the next agent starts in %s:\n  %s\n", where, line)
	fmt.Fprintln(os.Stderr, "ai-profile: close this session (/exit), so two agents do not work on the same files.")
	return nil
}

func runSkills(cfg *config.Config, home string, args []string) error {
	const use = "usage: ai-profile skills list [<profile>] | split <profile> | share <profile> <skill> | link <skill>"
	if len(args) == 0 {
		return errors.New(use)
	}
	profile := func(name string) (*config.Profile, error) {
		p := cfg.Profiles[name]
		if p == nil || !slices.Contains(p.Agents, "claude") {
			return nil, fmt.Errorf("%q is not a profile with Claude Code (profiles: %s)", name, strings.Join(cfg.Names(), ", "))
		}
		return p, nil
	}
	switch {
	case args[0] == "list" && len(args) <= 2:
		names := cfg.Names()
		if len(args) == 2 {
			if _, err := profile(args[1]); err != nil {
				return err
			}
			names = args[1:]
		}
		origins := skillOrigins(home)
		for _, n := range names {
			p := cfg.Profiles[n]
			if !slices.Contains(p.Agents, "claude") {
				continue
			}
			mode := map[skills.ModeKind]string{skills.Shared: "shared", skills.PerProfile: "per profile", skills.Missing: "skills folder missing", skills.Other: "unexpected link"}[skills.Mode(home, n)]
			fmt.Printf("%s (%s)\n", n, mode)
			for _, s := range skills.List(home, n, p.SkillsExclude, origins) {
				fmt.Printf("  %-32s %s\n", s.Name, s.Origin)
			}
		}
		return nil
	case args[0] == "split" && len(args) == 2:
		p, err := profile(args[1])
		if err != nil {
			return err
		}
		if err := skills.Split(home, p.Name, p.SkillsExclude); err != nil {
			return err
		}
		fmt.Printf("%s/skills is now a folder of the profile, with links to the shared skills.\n", tilde(home, launch.ProfileDir(home, "claude", p.Name)))
		return nil
	case args[0] == "share" && len(args) == 3:
		if _, err := profile(args[1]); err != nil {
			return err
		}
		if err := skills.Share(home, args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("%s is now in ~/.claude/skills; run ai-profile sync to link it to the other profiles with their own skills.\n", args[2])
		return nil
	case args[0] == "link" && len(args) == 2:
		if err := skills.Link(home, args[1]); err != nil {
			return err
		}
		fmt.Printf("~/.claude/skills/%s → ~/.agents/skills/%s\n", args[1], args[1])
		return nil
	}
	return errors.New(use)
}

// skillOrigins reads which shared skills come from the skills CLI; without
// its lock file the origin is left out.
func skillOrigins(home string) skills.Origins {
	var o skills.Origins
	if b, err := os.ReadFile(filepath.Join(home, ".agents", ".skill-lock.json")); err == nil {
		var lock struct {
			Skills map[string]json.RawMessage `json:"skills"`
		}
		if json.Unmarshal(b, &lock) == nil {
			o.CLI = slices.Sorted(maps.Keys(lock.Skills))
		}
	}
	return o
}

func runAccount(cfg *config.Config, home string, args []string) error {
	const use = "usage: ai-profile account list | save | use <email> [--profile <profile>]"
	if runtime.GOOS != "darwin" {
		return errors.New("switching accounts is only available on macOS for now")
	}
	var profile string
	var rest []string
	for i := 0; i < len(args); i++ {
		if args[i] == "--profile" && i+1 < len(args) {
			profile, i = args[i+1], i+1
			continue
		}
		rest = append(rest, args[i])
	}
	if profile == "" {
		// The profile the agent runs with here: inside a session, its own.
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		plan, err := launch.Prepare(cfg, home, cwd, os.Getenv, launch.Request{Agent: "claude"})
		if err != nil {
			return err
		}
		if plan.Profile == nil {
			return fmt.Errorf("CLAUDE_CONFIG_DIR=%s is not the folder of any profile", plan.Inherited)
		}
		profile = plan.Profile.Name
	}
	if cfg.Profiles[profile] == nil {
		return fmt.Errorf("unknown profile %q", profile)
	}
	m := &accounts.Manager{Home: home, Profile: profile, Store: accounts.Keychain{Account: os.Getenv("USER")}}
	switch {
	case len(rest) == 1 && rest[0] == "list":
		list, err := m.List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Printf("no account saved in profile %s: ai-profile account save\n", profile)
		}
		for _, a := range list {
			mark := " "
			if a.Active {
				mark = "*"
			}
			fmt.Printf("%s %s\n", mark, a.Email)
		}
		for _, want := range cfg.Profiles[profile].ClaudeAccounts {
			if !slices.ContainsFunc(list, func(a accounts.Account) bool { return strings.EqualFold(a.Email, want) }) {
				fmt.Printf("  %s (in claude_accounts, not saved yet)\n", want)
			}
		}
		return nil
	case len(rest) == 1 && rest[0] == "save":
		if err := m.Save(); err != nil {
			return err
		}
		fmt.Printf("active account of profile %s saved\n", profile)
		return nil
	case len(rest) == 2 && rest[0] == "use":
		email, err := m.Match(rest[1])
		if err != nil {
			return err
		}
		if err := m.Use(email); err != nil {
			return err
		}
		fmt.Printf("profile %s now uses %s; open sessions pick it up when they reread the credentials\n", profile, email)
		return nil
	}
	return errors.New(use)
}

func runDesktop(cfg *config.Config, home string, args []string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("Claude Desktop per profile is only available on macOS")
	}
	if len(args) > 0 && args[0] == "install" {
		return runDesktopInstall(cfg, home, args[1:])
	}
	var name string
	switch len(args) {
	case 0:
		m, _, err := folderMatch(cfg)
		if err != nil {
			return err
		}
		name = m.Profile.Name
	case 1:
		name = args[0]
	default:
		return errors.New("usage: ai-profile desktop [<profile>] | install [<profile>]")
	}
	in, err := desktopInstance(cfg, home, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "ai-profile: opening %s\n", in.Label())
	did, err := desktop.Open(in, desktop.MacOS{})
	for _, d := range did {
		fmt.Fprintf(os.Stderr, "ai-profile: %s\n", d)
	}
	return err
}

func desktopInstance(cfg *config.Config, home, name string) (desktop.Instance, error) {
	p := cfg.Profiles[name]
	if p == nil {
		return desktop.Instance{}, fmt.Errorf("unknown profile %q (profiles: %s)", name, strings.Join(cfg.Names(), ", "))
	}
	if p.Desktop == nil {
		return desktop.Instance{}, fmt.Errorf("profile %s has no Claude Desktop: add desktop = { hue = <degrees> } to it in %s, then ai-profile desktop install %s", name, tilde(home, p.Path), name)
	}
	return desktop.Instance{Name: name, Home: home, Hue: p.Desktop.Hue}, nil
}

// runDesktopInstall writes the launchers in ~/Applications and prepares the
// copies of Claude, for one profile or every profile with desktop in
// config.toml.
func runDesktopInstall(cfg *config.Config, home string, args []string) error {
	var names []string
	switch len(args) {
	case 0:
		for _, n := range cfg.Names() {
			if cfg.Profiles[n].Desktop != nil {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			return fmt.Errorf("no profile has desktop in %s: add desktop = { hue = <degrees> } to a profile", tilde(home, cfg.Path))
		}
	case 1:
		names = args
	default:
		return errors.New("usage: ai-profile desktop install [<profile>]")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	aiProfile := apply.StableToolPath(self, os.Getenv)
	for _, n := range names {
		in, err := desktopInstance(cfg, home, n)
		if err != nil {
			return err
		}
		did, err := desktop.Install(in, desktop.MacOS{}, aiProfile)
		if len(did) == 0 && err == nil {
			did = []string{"already in order"}
		}
		fmt.Printf("%s: %s\n", in.Label(), strings.Join(did, "; "))
		if err != nil {
			return err
		}
	}
	return nil
}

func runDefault(cfg *config.Config, home string, args []string) error {
	if len(args) != 1 || cfg.Profiles[args[0]] == nil {
		return fmt.Errorf("usage: ai-profile default <profile> (profiles: %s)", strings.Join(cfg.Names(), ", "))
	}
	changed := false
	err := config.EditFile(cfg.Path, cfg.DefaultFile, home, func(b []byte) ([]byte, bool, error) {
		out, ch, err := config.SetDefault(b, args[0])
		changed = ch
		return out, ch, err
	})
	if err != nil {
		return err
	}
	if !changed {
		fmt.Printf("%s is already the default\n", args[0])
		return nil
	}
	fmt.Printf("%s: the default is now %s\n", tilde(home, cfg.DefaultFile), args[0])
	if os.Getenv("CLAUDE_CONFIG_DIR") != "" {
		fmt.Println("note: CLAUDE_CONFIG_DIR is set in the environment and ai-profile honors it; as long as ~/.zshrc exports it, it decides (doctor reports it)")
	}
	return nil
}

const folderUsage = "usage: ai-profile folder add [<folder>] <profile>… | rm [<folder>] [<profile>…] | list"

// runFolder edits the [[folders]] entries: add creates the entry, in the file
// of the profile whose dirs held the folder (moving it from there, first in
// the list) or else in the private file, and adds the profiles; rm removes
// profiles, or the whole entry.
func runFolder(cfg *config.Config, home string, args []string) error {
	if len(args) == 0 {
		return errors.New(folderUsage)
	}
	if args[0] == "list" {
		if len(args) > 1 {
			return errors.New(folderUsage)
		}
		if len(cfg.Folders) == 0 {
			fmt.Println("no folder admits several profiles: ai-profile folder add <folder> <profile>…")
			return nil
		}
		for _, f := range cfg.Folders {
			line := fmt.Sprintf("%s: %s", tilde(home, f.Dir), strings.Join(f.Profiles, ", "))
			if cfg.LastUsed != nil {
				if last := cfg.LastUsed(f.Dir); slices.Contains(f.Profiles, last) {
					line += " (last used: " + last + ")"
				}
			}
			fmt.Printf("%s  [%s]\n", line, tilde(home, f.Path))
		}
		return nil
	}
	if args[0] != "add" && args[0] != "rm" {
		return errors.New(folderUsage)
	}
	dir, profiles, err := folderArgs(cfg, home, args[1:])
	if err != nil {
		return err
	}
	var folder *config.Folder
	for i, f := range cfg.Folders {
		if f.Dir == dir {
			folder = &cfg.Folders[i]
		}
	}
	if args[0] == "rm" {
		if folder == nil {
			return fmt.Errorf("%s is not in [[folders]]", tilde(home, dir))
		}
		left := slices.DeleteFunc(slices.Clone(folder.Profiles), func(p string) bool { return slices.Contains(profiles, p) })
		if len(profiles) == 0 {
			left = nil
		}
		if err := config.EditFile(cfg.Path, folder.Path, home, func(b []byte) ([]byte, bool, error) {
			return config.RemoveFromFolder(b, home, dir, profiles)
		}); err != nil {
			return err
		}
		if len(left) == 0 {
			fmt.Printf("%s: %s removed from [[folders]]\n", tilde(home, folder.Path), tilde(home, dir))
		} else {
			fmt.Printf("%s: %s admits %s\n", tilde(home, folder.Path), tilde(home, dir), strings.Join(left, ", "))
		}
		return nil
	}
	if len(profiles) == 0 {
		return errors.New(folderUsage)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a folder", dir)
	}
	file, list, owner := cfg.Path, profiles, (*config.Profile)(nil)
	switch {
	case folder != nil:
		file = folder.Path
		list = slices.Clone(folder.Profiles)
		for _, p := range profiles {
			if !slices.Contains(list, p) {
				list = append(list, p)
			}
		}
	default:
		if cfg.Private.File != "" {
			file = cfg.Private.File
		}
		for _, name := range cfg.Names() {
			if slices.Contains(cfg.Profiles[name].Dirs, dir) {
				owner = cfg.Profiles[name]
			}
		}
		if owner != nil {
			// The folder keeps starting with the profile it had.
			file = owner.Path
			list = append([]string{owner.Name}, slices.DeleteFunc(slices.Clone(profiles), func(p string) bool { return p == owner.Name })...)
		}
	}
	changed := false
	err = config.EditFile(cfg.Path, file, home, func(b []byte) ([]byte, bool, error) {
		if owner != nil {
			out, _, err := config.RemoveDirFromProfile(b, home, owner.Name, dir)
			if err != nil {
				return nil, false, err
			}
			b = out
		}
		out, ch, err := config.AddToFolder(b, home, dir, list)
		changed = ch
		return out, ch, err
	})
	if err != nil {
		return err
	}
	if !changed {
		fmt.Printf("%s already admits %s\n", tilde(home, dir), strings.Join(list, ", "))
		return nil
	}
	fmt.Printf("%s: %s admits %s", tilde(home, file), tilde(home, dir), strings.Join(list, ", "))
	if owner != nil {
		fmt.Printf(" (moved from the dirs of %s)", owner.Name)
	}
	fmt.Println()
	if len(list) == 1 {
		fmt.Println("note: with one profile, the dirs of that profile say the same; add the others when you need them")
	}
	return nil
}

// folderArgs splits [<folder>] <profile>…: the folder, absolute, is the
// current one when the first argument is a profile.
func folderArgs(cfg *config.Config, home string, args []string) (string, []string, error) {
	dir := "."
	if len(args) > 0 && cfg.Profiles[args[0]] == nil {
		dir, args = args[0], args[1:]
	}
	for _, p := range args {
		if cfg.Profiles[p] == nil {
			return "", nil, fmt.Errorf("%q is not a profile (profiles: %s)", p, strings.Join(cfg.Names(), ", "))
		}
	}
	abs, err := filepath.Abs(config.Expand(dir, home))
	if err != nil {
		return "", nil, err
	}
	return abs, args, nil
}

func runMigrateDefault(cfg *config.Config, home string, args []string) error {
	dry := false
	var rest []string
	for _, a := range args {
		if a == "--dry-run" || a == "-n" {
			dry = true
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) != 1 || cfg.Profiles[rest[0]] == nil {
		return fmt.Errorf("usage: ai-profile migrate-default <profile> [--dry-run] (profiles: %s)", strings.Join(cfg.Names(), ", "))
	}
	rep, err := apply.MigrateDefault(home, rest[0], dry)
	if err != nil {
		return err
	}
	printReport(home, rep, dry)
	if dry {
		return nil
	}
	return runDefault(cfg, home, rest)
}

// loadConfig reads the configuration; when its private file is missing (on a
// new Mac, for example) it restores it first with the [private] restore
// command.
func loadConfig(path, home string) (*config.Config, error) {
	cfg, err := loadConfigFile(path, home)
	if cfg != nil {
		cfg.LastUsed = func(dir string) string { return lastused.Get(home, dir) }
	}
	return cfg, err
}

func loadConfigFile(path, home string) (*config.Config, error) {
	cfg, err := config.Load(path, home)
	var missing *private.MissingError
	if !errors.As(err, &missing) {
		return cfg, err
	}
	s, err := private.Section(path, home)
	if err != nil {
		return nil, err
	}
	if s.Restore == "" {
		return nil, fmt.Errorf("%s is missing and [private] in %s has no restore command", tilde(home, missing.File), tilde(home, path))
	}
	fmt.Fprintf(os.Stderr, "ai-profile: %s is missing, restoring it with: %s\n", tilde(home, missing.File), s.Restore)
	res, err := s.Fetch(home, false)
	if err != nil {
		return nil, err
	}
	printRestore(os.Stderr, home, res)
	return config.Load(path, home)
}

func runRestore(cfgPath, home string, args []string) error {
	force := false
	for _, a := range args {
		if a != "--force" {
			return errors.New("usage: ai-profile restore [--force]")
		}
		force = true
	}
	s, err := private.Section(cfgPath, home)
	if err != nil {
		return err
	}
	if s.File == "" {
		return fmt.Errorf("%s has no [private] section", tilde(home, cfgPath))
	}
	res, err := s.Fetch(home, force)
	if err != nil {
		return err
	}
	printRestore(os.Stdout, home, res)
	return nil
}

func printRestore(w io.Writer, home string, res private.Result) {
	for _, p := range res.Written {
		fmt.Fprintf(w, "restored %s\n", tilde(home, p))
	}
	for _, p := range res.Same {
		fmt.Fprintf(w, "%s already matches the backup\n", tilde(home, p))
	}
	for _, p := range res.Kept {
		fmt.Fprintf(w, "%s differs from the backup and stays as it is (ai-profile restore --force overwrites it)\n", tilde(home, p))
	}
}

func runBackup(cfg *config.Config, home string, args []string) error {
	if len(args) > 0 {
		return errors.New("usage: ai-profile backup")
	}
	if cfg.Private.File == "" {
		return fmt.Errorf("%s has no [private] section", tilde(home, cfg.Path))
	}
	if err := cfg.Private.Save(home, cfg.Files); err != nil {
		return err
	}
	for _, p := range append([]string{cfg.Private.File}, cfg.Files...) {
		fmt.Printf("saved %s\n", tilde(home, p))
	}
	return nil
}

func reloadAndSync(path, home string) error {
	cfg, err := config.Load(path, home)
	if err != nil {
		return err
	}
	return syncAndReport(cfg, home, false)
}

// absDir resolves a folder given on the command line.
func absDir(p, home string) (string, error) {
	p, err := filepath.Abs(config.Expand(p, home))
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(p); err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s is not a folder", p)
	}
	return p, nil
}

func tilde(home, p string) string {
	if rel, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.Join("~", rel)
	}
	return p
}

func runDoctor(cfg *config.Config, home string, args []string) error {
	fix := false
	for _, a := range args {
		if a != "--fix" {
			return fmt.Errorf("doctor: unknown option %q", a)
		}
		fix = true
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	env := doctor.Env{Home: home, Cfg: cfg, PathList: os.Getenv("PATH"), Self: self, Getenv: os.Getenv, Desktop: runtime.GOOS == "darwin"}
	if dir, err := os.UserConfigDir(); err == nil {
		env.ObsidianConfig = filepath.Join(dir, "obsidian", "obsidian.json")
	}
	results := doctor.Run(env)
	if fix {
		fixed := 0
		for _, r := range results {
			if r.Fix == nil {
				continue
			}
			msg, err := r.Fix()
			if err != nil {
				fmt.Printf("✗ %s: fix failed: %v\n", r.Area, err)
				continue
			}
			fmt.Printf("fixed, %s: %s\n", r.Area, msg)
			fixed++
		}
		if fixed == 0 {
			fmt.Println("nothing to fix automatically")
		}
		fmt.Println()
		results = doctor.Run(env)
	}
	failed, fixable := false, false
	for _, r := range results {
		line := r.String()
		if r.Fix != nil {
			line += " (fixable with --fix)"
			fixable = true
		}
		fmt.Println(line)
		failed = failed || r.Status == doctor.Fail
	}
	if fixable && !fix {
		fmt.Println("\nai-profile doctor --fix fixes the marked items.")
	}
	if failed {
		return exitError(1)
	}
	return nil
}
