// Package skills keeps the skills of Claude Code profiles. A profile is in
// shared mode when its skills/ links to ~/.claude/skills (it sees every
// skill), or per-profile when skills/ is a real folder: one link per shared
// skill, minus the excluded ones, plus the profile's own skills as real
// folders, which are never touched.
package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/LunetteOrg/ai-profiles/internal/launch"
)

type ModeKind int

const (
	Missing ModeKind = iota
	Shared
	PerProfile
	Other // a link somewhere else
)

// synced holds the skills claude.ai syncs for an account: each profile keeps
// its own, so it is never linked from the shared folder.
const synced = "synced"

func sharedDir(home string) string { return filepath.Join(home, ".claude", "skills") }

func profileSkills(home, profile string) string {
	return filepath.Join(launch.ProfileDir(home, "claude", profile), "skills")
}

func Mode(home, profile string) ModeKind {
	p := profileSkills(home, profile)
	info, err := os.Lstat(p)
	switch {
	case err != nil:
		return Missing
	case info.Mode()&os.ModeSymlink != 0:
		if target, _ := os.Readlink(p); filepath.Clean(target) == sharedDir(home) {
			return Shared
		}
		return Other
	case info.IsDir():
		return PerProfile
	}
	return Other
}

// sharedNames lists the skills of ~/.claude/skills, synced/ excluded.
func sharedNames(home string) []string {
	entries, _ := os.ReadDir(sharedDir(home))
	var out []string
	for _, e := range entries {
		if e.Name() == synced || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if info, err := os.Stat(filepath.Join(sharedDir(home), e.Name())); err == nil && info.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// Split turns a shared-mode profile into a per-profile one.
func Split(home, profile string, exclude []string) error {
	if m := Mode(home, profile); m != Shared {
		return fmt.Errorf("%s is not a link to %s: profile %s already has its own skills or a different setup", profileSkills(home, profile), sharedDir(home), profile)
	}
	p := profileSkills(home, profile)
	if err := os.Remove(p); err != nil {
		return err
	}
	if err := os.Mkdir(p, 0o755); err != nil {
		return err
	}
	_, err := Sync(home, profile, exclude, false)
	return err
}

// Sync links every shared skill not excluded into a per-profile folder and
// removes links to skills excluded or gone. It returns the changes, made or,
// with dryRun, to make. Shared-mode profiles need nothing.
func Sync(home, profile string, exclude []string, dryRun bool) ([]string, error) {
	if Mode(home, profile) != PerProfile {
		return nil, nil
	}
	p := profileSkills(home, profile)
	shared := sharedDir(home)
	var changes []string
	entries, err := os.ReadDir(p)
	if err != nil {
		return nil, err
	}
	present := map[string]bool{}
	for _, e := range entries {
		present[e.Name()] = true
		link := filepath.Join(p, e.Name())
		target, err := os.Readlink(link)
		if err != nil || filepath.Dir(filepath.Clean(target)) != shared {
			continue // the profile's own skill, or a link elsewhere: left alone
		}
		_, gone := os.Stat(target)
		if gone == nil && !slices.Contains(exclude, e.Name()) {
			continue
		}
		changes = append(changes, "remove the link to "+e.Name())
		delete(present, e.Name())
		if !dryRun {
			if err := os.Remove(link); err != nil {
				return changes, err
			}
		}
	}
	for _, name := range sharedNames(home) {
		if present[name] || slices.Contains(exclude, name) {
			continue
		}
		changes = append(changes, "link "+name)
		if !dryRun {
			if err := os.Symlink(filepath.Join(shared, name), filepath.Join(p, name)); err != nil {
				return changes, err
			}
		}
	}
	return changes, nil
}

// Share moves a skill of the profile into ~/.claude/skills and links it back.
func Share(home, profile, name string) error {
	src := filepath.Join(profileSkills(home, profile), name)
	info, err := os.Lstat(src)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("%s is not a skill of profile %s", name, profile)
	}
	dst := filepath.Join(sharedDir(home), name)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists among the shared skills", name)
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	return os.Symlink(dst, src)
}

func agentsDir(home string) string { return filepath.Join(home, ".agents", "skills") }

// Unlinked lists the skills of ~/.agents/skills that ~/.claude/skills does
// not have: other agents see them, Claude Code does not.
func Unlinked(home string) []string {
	entries, _ := os.ReadDir(agentsDir(home))
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Lstat(filepath.Join(sharedDir(home), e.Name())); err != nil {
			out = append(out, e.Name())
		}
	}
	return out
}

// Link links a skill of ~/.agents/skills into ~/.claude/skills, so it
// follows the updates made by other tools.
func Link(home, name string) error {
	src := filepath.Join(agentsDir(home), name)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("%s is not in %s", name, agentsDir(home))
	}
	dst := filepath.Join(sharedDir(home), name)
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists in %s", name, sharedDir(home))
	}
	return os.Symlink(src, dst)
}

// Origins tells where shared skills come from.
type Origins struct {
	CLI []string // skill names installed by the skills CLI
}

type Skill struct {
	Name   string
	Origin string
}

// List is what a profile sees, with the origin of each skill; excluded ones
// are listed as such.
func List(home, profile string, exclude []string, o Origins) []Skill {
	origin := func(name string) string {
		s := "shared"
		switch {
		case slices.Contains(o.CLI, name):
			s += ", from the skills CLI"
		default:
			if t, err := os.Readlink(filepath.Join(sharedDir(home), name)); err == nil && filepath.Dir(t) == agentsDir(home) {
				s += ", from ~/.agents/skills"
			}
		}
		return s
	}
	var out []Skill
	switch Mode(home, profile) {
	case Shared:
		for _, n := range sharedNames(home) {
			out = append(out, Skill{n, origin(n)})
		}
	case PerProfile:
		entries, _ := os.ReadDir(profileSkills(home, profile))
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			target, err := os.Readlink(filepath.Join(profileSkills(home, profile), e.Name()))
			if err == nil && filepath.Dir(filepath.Clean(target)) == sharedDir(home) {
				out = append(out, Skill{e.Name(), origin(e.Name())})
			} else if e.Name() == synced {
				out = append(out, Skill{e.Name(), "synced from claude.ai for the profile's account"})
			} else {
				out = append(out, Skill{e.Name(), "profile's own"})
			}
		}
		for _, n := range exclude {
			if slices.Contains(sharedNames(home), n) {
				out = append(out, Skill{n, "excluded"})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
