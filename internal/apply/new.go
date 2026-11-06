package apply

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LunetteOrg/ai-profiles/internal/launch"
)

// ClaudeSharedLinks are the entries every Claude Code profile links to: the
// first to ~/.agents/AGENTS.md, the others to the same name in ~/.claude.
var ClaudeSharedLinks = []string{
	"CLAUDE.md", "skills", "settings.json", "plugins", "hooks",
	"statusline-command.sh", "statusline-helpers.sh",
}

// ClaudeLinkTarget is where entry of ClaudeSharedLinks points.
func ClaudeLinkTarget(home, entry string) string {
	if entry == "CLAUDE.md" {
		return filepath.Join(home, ".agents", "AGENTS.md")
	}
	return filepath.Join(home, ".claude", entry)
}

// CreateClaudeProfile creates ~/.claude-<name> with the links to the shared
// parts that exist. The login happens at the first launch.
func CreateClaudeProfile(home, name string, dryRun bool) (*Report, error) {
	dir := launch.ProfileDir(home, "claude", name)
	if _, err := os.Lstat(dir); err == nil {
		return nil, fmt.Errorf("%s already exists", dir)
	}
	s := &syncer{Options: Options{Home: home, DryRun: dryRun}, rep: &Report{}}
	if err := s.mkdir(dir); err != nil {
		return nil, err
	}
	for _, entry := range ClaudeSharedLinks {
		target := ClaudeLinkTarget(home, entry)
		if _, err := os.Stat(target); err != nil {
			s.note("%s does not exist: %s is left out", s.tilde(target), entry)
			continue
		}
		if err := s.link(filepath.Join(dir, entry), target); err != nil {
			return nil, err
		}
	}
	return s.rep, nil
}
