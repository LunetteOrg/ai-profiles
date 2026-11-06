package apply

import (
	"os"
	"path/filepath"
	"slices"
)

// claudeStatusLine points the statusLine of the shared Claude Code settings
// at "ai-profile statusline", which adds profile and account to the user's own
// statusline command ([statusline] claude in config.toml). A statusLine that
// runs something else while config.toml names no command is left as it is.
func (s *syncer) claudeStatusLine() error {
	if s.Self == "" {
		return nil
	}
	uses := false
	for _, p := range s.Cfg.Profiles {
		uses = uses || slices.Contains(p.Agents, "claude")
	}
	if !uses {
		return nil
	}
	path := filepath.Join(s.Home, ".claude", "settings.json")
	want := StableToolPath(s.Self, os.Getenv) + " statusline"
	return s.editJSON(path, func(m map[string]any) bool {
		cur, _ := object(m, "statusLine")["command"].(string)
		if cur == want {
			return false
		}
		if cur != "" && s.Cfg.Statusline.Claude == "" {
			s.note("%s: statusLine runs %q; to keep it with the profile and account from ai-profile, set [statusline] claude = %q in config.toml (leaving it)", s.tilde(path), cur, cur)
			if len(object(m, "statusLine")) == 0 {
				delete(m, "statusLine")
			}
			return false
		}
		s.act(path, "statusLine through ai-profile statusline")
		m["statusLine"] = map[string]any{"type": "command", "command": want}
		return true
	})
}
