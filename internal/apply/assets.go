package apply

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// assets are the skills and commands that come with ai-profile and that sync
// installs where each agent reads them: skills in ~/.agents/skills (Codex and
// others), linked into ~/.claude/skills for Claude Code; OpenCode commands in
// ~/.config/opencode/commands. A file is rewritten only while it carries the
// marker, so a file of the user's with the same name is left alone.
//
//go:embed assets
var assets embed.FS

const assetMarker = "Installed by ai-profile sync"

func (s *syncer) installAssets() error {
	skills, _ := fs.ReadDir(assets, "assets/skills")
	for _, sk := range skills {
		dir := filepath.Join(s.Home, ".agents", "skills", sk.Name())
		err := fs.WalkDir(assets, path.Join("assets/skills", sk.Name()), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(path.Join("assets/skills", sk.Name()), p)
			return s.installFile(p, filepath.Join(dir, rel))
		})
		if err != nil {
			return err
		}
		if claude := filepath.Join(s.Home, ".claude", "skills"); isDir(claude) {
			if err := s.link(filepath.Join(claude, sk.Name()), dir); err != nil {
				return err
			}
		}
	}
	commands, _ := fs.ReadDir(assets, "assets/opencode/commands")
	for _, c := range commands {
		dst := filepath.Join(s.Home, ".config", "opencode", "commands", c.Name())
		if err := s.installFile(path.Join("assets/opencode/commands", c.Name()), dst); err != nil {
			return err
		}
	}
	return nil
}

func (s *syncer) installFile(src, dst string) error {
	data, err := assets.ReadFile(src)
	if err != nil {
		return err
	}
	cur, err := os.ReadFile(dst)
	switch {
	case err == nil && bytes.Equal(cur, data):
		return nil
	case err == nil && !bytes.Contains(cur, []byte(assetMarker)):
		s.note("%s is not ai-profile's (no marker): leaving it", s.tilde(dst))
		return nil
	case err == nil:
		s.act(dst, "update")
	default:
		s.act(dst, "install")
	}
	if s.DryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
