// Package toolshim runs a CLI through ai-secrets: a link named after the tool,
// pointing to ai-secrets, in a folder that comes before the real tool in
// PATH. Called by that name, ai-secrets resolves the references of the
// tool's section in the folder's profile ([profiles.<p>.tools.<name>]) and
// execs the real tool with the values in its environment only.
package toolshim

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/config"
	"github.com/LunetteOrg/ai-profiles/internal/gitroot"
)

// Section is what the shim needs from a tool's section of a profile.
type Section struct {
	Profile string
	// Reason is shown when the password manager asks: the section's reason,
	// or the tool's name, with the profile.
	Reason string
	// Refs maps variables to references.
	Refs map[string]string
	// Account is the profile's Bitwarden account, for bw:// references that
	// name none.
	Account  string
	DenyArgs [][]string
}

// Lookup returns the section of tool in the profile of dir (worktrees
// included, as ai-profile chooses it), nil when that profile does not
// declare the tool.
func Lookup(cfg *config.Config, dir, tool string) (*Section, error) {
	m, err := cfg.Resolve(gitroot.LookupDir(dir))
	if err != nil {
		return nil, err
	}
	t := m.Profile.Tools[tool]
	if t == nil {
		return nil, nil
	}
	reason := t.Reason
	if reason == "" {
		reason = tool
	}
	return &Section{
		Profile:  m.Profile.Name,
		Reason:   fmt.Sprintf("%s (profile %s)", reason, m.Profile.Name),
		Refs:     t.Env,
		Account:  m.Profile.Bitwarden,
		DenyArgs: t.DenyArgs,
	}, nil
}

// Denied returns the first prefix of deny that args start with, global flags
// aside, or nil. A flag without "=" may or may not take the next argument as
// its value: args are denied when either reading matches, so an unknown
// boolean flag before the subcommand does not let it through.
func Denied(args []string, deny [][]string) []string {
	for _, prefix := range deny {
		if matches(args, prefix) {
			return prefix
		}
	}
	return nil
}

func matches(args, prefix []string) bool {
	// seen[i] is true once position i of args has been tried for prefix[j];
	// the readings of flags meet again, so each pair is tried once.
	seen := map[[2]int]bool{}
	var try func(i, j int) bool
	try = func(i, j int) bool {
		if j == len(prefix) {
			return true
		}
		if i >= len(args) || seen[[2]int{i, j}] {
			return false
		}
		seen[[2]int{i, j}] = true
		a := args[i]
		if strings.HasPrefix(a, "-") && a != "-" {
			if try(i+1, j) {
				return true
			}
			return !strings.Contains(a, "=") && try(i+2, j)
		}
		return a == prefix[j] && try(i+1, j+1)
	}
	return try(0, 0)
}

// DeniedError is the refusal to run a denied subcommand.
type DeniedError struct {
	Tool    string
	Profile string
	Prefix  []string
}

func (e *DeniedError) Error() string {
	return fmt.Sprintf("%s %s is denied in profile %s (deny_args): it would print the configuration with the secret; run it without ai-secrets only after removing the secret from the tool's config file",
		e.Tool, strings.Join(e.Prefix, " "), e.Profile)
}

// Shim runs a tool through ai-secrets.
type Shim struct {
	Name string // the tool, argv[0] without folder
	Real string // the real tool, found by FindReal
	Dir  string // the current folder
	// Cfg is the ai-profiles configuration, nil when there is none.
	Cfg     *config.Config
	Environ []string
	// Get asks ai-secrets for the values of refs (variable → reference).
	Get func(reason string, refs map[string]string, account string) (map[string]string, error)
	// Exec replaces the process with bin, given the arguments after argv[0].
	Exec func(bin string, args, env []string) error
}

// Run execs the real tool with args: unchanged without a section for the
// tool, else with the section's secrets in its environment, unless args are
// denied, in which case it neither asks for the secrets nor runs the tool.
func (s *Shim) Run(args []string) error {
	var sec *Section
	if s.Cfg != nil {
		var err error
		if sec, err = Lookup(s.Cfg, s.Dir, s.Name); err != nil {
			return err
		}
	}
	if sec == nil {
		return s.Exec(s.Real, args, s.Environ)
	}
	if p := Denied(args, sec.DenyArgs); p != nil {
		return &DeniedError{Tool: s.Name, Profile: sec.Profile, Prefix: p}
	}
	values, err := s.Get(sec.Reason, sec.Refs, sec.Account)
	if err != nil {
		return err
	}
	return s.Exec(s.Real, args, environ(s.Environ, values))
}

// environ returns base with the variables of extra set.
func environ(base []string, extra map[string]string) []string {
	out := make([]string, 0, len(base)+len(extra))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := extra[k]; !ok {
			out = append(out, kv)
		}
	}
	for _, k := range slices.Sorted(maps.Keys(extra)) {
		out = append(out, k+"="+extra[k])
	}
	return out
}

// ShimDir is the folder of the shim called as argv0: its folder when argv0
// is a path, else the first folder of pathList that holds it, as the shell
// found it. Empty when not found.
func ShimDir(argv0, pathList string) string {
	if strings.ContainsRune(argv0, filepath.Separator) || strings.Contains(argv0, "/") {
		if abs, err := filepath.Abs(argv0); err == nil {
			return filepath.Dir(abs)
		}
		return filepath.Dir(argv0)
	}
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" {
			continue
		}
		for _, c := range candidates(filepath.Join(dir, argv0)) {
			if executable(c) {
				return dir
			}
		}
	}
	return ""
}

// FindReal is the first name in pathList outside shimDir that is not
// ai-secrets itself (self): another link to ai-secrets would loop.
func FindReal(name, pathList, shimDir, self string) (string, error) {
	selfReal := canonical(self)
	own := canonical(shimDir)
	for _, dir := range filepath.SplitList(pathList) {
		if dir == "" || (own != "" && canonical(dir) == own) {
			continue
		}
		for _, c := range candidates(filepath.Join(dir, name)) {
			if !executable(c) || canonical(c) == selfReal {
				continue
			}
			return c, nil
		}
	}
	return "", fmt.Errorf("%s not found in PATH besides the ai-secrets tool shim", name)
}

// IsShim reports whether p is a link to ai-secrets (or a copy of it).
func IsShim(p string) bool {
	return strings.TrimSuffix(filepath.Base(canonical(p)), ".exe") == "ai-secrets"
}

func candidates(p string) []string {
	if runtime.GOOS != "windows" {
		return []string{p}
	}
	return []string{p + ".exe", p + ".cmd", p + ".bat"}
}

func executable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0)
}

func canonical(p string) string {
	if p == "" {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}
