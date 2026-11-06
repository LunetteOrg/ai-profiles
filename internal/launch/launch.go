// Package launch turns a request ("claude here", "codex with client-d") into the
// real agent binary, its arguments and the environment of the profile.
package launch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/gitroot"
	"github.com/LunetteOrg/ai-profiles/internal/secrets"
)

// Adapter translates a profile into the variables of one agent.
type Adapter struct {
	// PrimaryVar is the variable that selects the profile folder; when it is
	// already set in the environment, ai-profile leaves every variable alone.
	PrimaryVar string
	Env        func(home, profile string, getenv func(string) string) map[string]string
}

var Adapters = map[string]Adapter{
	"claude": {
		PrimaryVar: "CLAUDE_CONFIG_DIR",
		Env: func(home, profile string, _ func(string) string) map[string]string {
			return map[string]string{"CLAUDE_CONFIG_DIR": ProfileDir(home, "claude", profile)}
		},
	},
	"codex": {
		PrimaryVar: "CODEX_HOME",
		Env: func(home, profile string, _ func(string) string) map[string]string {
			return map[string]string{"CODEX_HOME": ProfileDir(home, "codex", profile)}
		},
	},
	"opencode": {
		PrimaryVar: "OPENCODE_CONFIG_DIR",
		Env:        openCodeEnv,
	},
}

// openCodeEnv moves OpenCode's database, logs and service registration into
// the profile through XDG_DATA_HOME and XDG_STATE_HOME. Every program OpenCode
// starts inherits them, so the data folders of mise and uv are pinned to where
// they were, unless already set.
func openCodeEnv(home, profile string, getenv func(string) string) map[string]string {
	base := ProfileDir(home, "opencode", profile)
	env := map[string]string{
		"OPENCODE_CONFIG_DIR": filepath.Join(base, "config"),
		"XDG_DATA_HOME":       filepath.Join(base, "data"),
		"XDG_STATE_HOME":      filepath.Join(base, "state"),
	}
	xdg := func(v, def string) string {
		if p := getenv(v); p != "" {
			return p
		}
		return filepath.Join(home, def)
	}
	data, state := xdg("XDG_DATA_HOME", ".local/share"), xdg("XDG_STATE_HOME", ".local/state")
	for k, v := range map[string]string{
		"MISE_DATA_DIR":         filepath.Join(data, "mise"),
		"MISE_STATE_DIR":        filepath.Join(state, "mise"),
		"UV_PYTHON_INSTALL_DIR": filepath.Join(data, "uv", "python"),
		"UV_TOOL_DIR":           filepath.Join(data, "uv", "tools"),
	} {
		if getenv(k) == "" {
			env[k] = v
		}
	}
	return env
}

// ClaudeKeychainService is the Keychain entry where Claude Code keeps the
// login of a config folder: the first 8 hex characters of its sha256.
func ClaudeKeychainService(configDir string) string {
	sum := sha256.Sum256([]byte(configDir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

// ProfileDir is ~/.<agent>-<profile>.
func ProfileDir(home, agent, profile string) string {
	return filepath.Join(home, "."+agent+"-"+profile)
}

type Request struct {
	Agent   string
	Profile string // explicit profile, empty to use the folder's
	Args    []string
}

type Plan struct {
	Agent string
	// Profile is the profile the agent will run with: nil when an inherited
	// PrimaryVar points at a folder that belongs to no profile.
	Profile   *config.Profile
	Match     config.Match // the folder's profile, which can differ from Profile
	Inherited string       // PrimaryVar kept from the environment, if any
	Env       map[string]string
	// Secrets maps variables to the references to resolve just before the
	// agent starts (see Resolve); Prepare only lists them.
	Secrets map[string]string
	// Wrap is the command the agent runs inside: op run for op:// keys.
	Wrap  []string
	Args  []string
	Notes []string // warnings for stderr
}

func Prepare(cfg *config.Config, home, cwd string, getenv func(string) string, req Request) (*Plan, error) {
	match, err := cfg.Resolve(gitroot.LookupDir(cwd))
	if err != nil {
		return nil, err
	}
	profile := match.Profile
	if req.Profile != "" {
		explicit := cfg.Profiles[req.Profile]
		if explicit == nil {
			return nil, fmt.Errorf("unknown profile %q (profiles: %s)", req.Profile, strings.Join(cfg.Names(), ", "))
		}
		if !match.Allows(explicit.Name) {
			if match.Allowed != nil {
				return nil, fmt.Errorf("%s admits only %s: add %s to its profiles in [[folders]] to use it here",
					match.Dir, strings.Join(match.Allowed, ", "), explicit.Name)
			}
			return nil, fmt.Errorf("%s applies here (%s): %s starts only if %s lists it in handoff_to",
				match.Profile.Name, match.Dir, explicit.Name, match.Profile.Name)
		}
		profile = explicit
	}
	if !slices.Contains(profile.Agents, req.Agent) {
		return nil, fmt.Errorf("%s is not among the agents of profile %s (%s); %s",
			req.Agent, profile.Name, strings.Join(profile.Agents, ", "), Why(match, cfg))
	}
	adapter, ok := Adapters[req.Agent]
	if !ok {
		return nil, fmt.Errorf("the adapter for %s is not available yet", req.Agent)
	}

	plan := &Plan{Agent: req.Agent, Profile: profile, Match: match, Args: req.Args, Env: adapter.Env(home, profile.Name, getenv)}
	want := plan.Env[adapter.PrimaryVar]
	if cur := getenv(adapter.PrimaryVar); cur != "" && req.Profile == "" {
		plan.Env = map[string]string{}
		plan.Inherited = cur
		if filepath.Clean(cur) != filepath.Clean(want) {
			plan.Profile = profileOwning(cfg, home, adapter, cur, getenv)
			plan.Notes = append(plan.Notes, fmt.Sprintf("%s is already set to %s and is honored; the folder's profile would be %s (%s)",
				adapter.PrimaryVar, cur, profile.Name, want))
		}
	}
	if plan.Profile != nil {
		plan.Secrets = cfg.SecretsFor(plan.Profile)
	}
	return plan, nil
}

// SecretSource gives the values of op:// and bw:// references: the ai-secrets
// agent, which keeps them in memory so one approval covers many launches.
type SecretSource interface {
	Get(reason string, refs map[string]string, account string) (map[string]string, error)
}

// ResolveSecrets puts the provider keys in Env: op:// and bw:// from src, with
// the profile's Bitwarden account; env: read with r. A key overrides a
// variable of the same name already in the environment.
func (p *Plan) ResolveSecrets(src SecretSource, r *secrets.Resolver, cfg *config.Config, home string) error {
	if len(p.Secrets) == 0 {
		return nil
	}
	fromAgent := map[string]string{}
	for _, k := range slices.Sorted(maps.Keys(p.Secrets)) {
		ref := p.Secrets[k]
		if secrets.Tool(ref) != "" {
			fromAgent[k] = ref
			continue
		}
		v, err := r.Resolve(ref)
		if err != nil {
			return fmt.Errorf("secret %s of profile %s: %w", k, p.Profile.Name, err)
		}
		p.Env[k] = v
	}
	if len(fromAgent) == 0 {
		return nil
	}
	reason := fmt.Sprintf("%s with profile %s", p.Agent, p.Profile.Name)
	values, err := src.Get(reason, fromAgent, p.Profile.Bitwarden)
	if err != nil {
		return fmt.Errorf("keys of profile %s: %w", p.Profile.Name, err)
	}
	maps.Copy(p.Env, values)
	return nil
}

// ProfileOwning returns the profile whose folder for agent is dir (the value
// of the agent's PrimaryVar), or nil.
func ProfileOwning(cfg *config.Config, home, agent, dir string, getenv func(string) string) *config.Profile {
	a, ok := Adapters[agent]
	if !ok {
		return nil
	}
	return profileOwning(cfg, home, a, dir, getenv)
}

// profileOwning returns the profile whose PrimaryVar would be dir, or nil.
func profileOwning(cfg *config.Config, home string, a Adapter, dir string, getenv func(string) string) *config.Profile {
	for _, name := range cfg.Names() {
		if filepath.Clean(a.Env(home, name, getenv)[a.PrimaryVar]) == filepath.Clean(dir) {
			return cfg.Profiles[name]
		}
	}
	return nil
}

// Why explains in one phrase why a profile applies.
func Why(m config.Match, cfg *config.Config) string {
	if m.Dir == "" {
		return fmt.Sprintf("%s applies here because no folder in %s contains this one (default profile)", m.Profile.Name, cfg.Path)
	}
	if m.Allowed != nil {
		how := "the first listed"
		if m.Last {
			how = "the last used there"
		}
		return fmt.Sprintf("%s applies here as %s among the profiles of %s (%s); another one: ai-profile <profile>",
			m.Profile.Name, how, m.Dir, strings.Join(m.Allowed, ", "))
	}
	return fmt.Sprintf("%s applies here because the folder is inside %s", m.Profile.Name, m.Dir)
}

// FindAgent looks name up in pathList, skipping entries that resolve to self:
// the shim named like the agent is ai-profile itself.
func FindAgent(name, pathList, self string) (string, error) {
	selfReal, err := filepath.EvalSymlinks(self)
	if err != nil {
		selfReal = self
	}
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		for _, cand := range candidates(filepath.Join(dir, name)) {
			info, err := os.Stat(cand)
			if err != nil || info.IsDir() || (runtime.GOOS != "windows" && info.Mode()&0o111 == 0) {
				continue
			}
			if real, err := filepath.EvalSymlinks(cand); err == nil && real == selfReal {
				continue
			}
			return cand, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH (besides the ai-profile shim)", name)
}

func candidates(p string) []string {
	if runtime.GOOS != "windows" {
		return []string{p}
	}
	return []string{p + ".exe", p + ".cmd", p + ".bat"}
}

// Environ merges extra into base ("K=V" entries), replacing existing keys.
func Environ(base []string, extra map[string]string) []string {
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, replaced := extra[k]; !replaced {
			out = append(out, kv)
		}
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}
