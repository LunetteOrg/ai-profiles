# Existing tools

Situation as of October 2026.

## claude-multiprofile

[jmdarre-v/claude-multiprofile](https://github.com/jmdarre-v/claude-multiprofile):
Node, MIT license, `npm install -g claude-multiprofile`.

| | |
|---|---|
| Platforms | Claude Code on macOS and Linux; Claude Desktop only on macOS. No Windows. |
| Claude Code profile | `~/.claude-<nome>` and a shell alias `claude-<nome>` that sets `CLAUDE_CONFIG_DIR`. |
| Desktop profile | `.app` launcher with `--user-data-dir`, an APFS copy of Claude.app per profile (colored icon), automatic updates blocked in the copy and performed by the launcher. |
| Per-folder choice | Not built in: the README suggests `.envrc` with direnv. |
| Shared skills | Not built in: the README suggests a symlink of `skills/`. |
| Read protection | `permissions.deny` rules in each `settings.json` toward the other profiles' folders. Skipped if `settings.json` is a symlink. |
| GitHub | `GH_CONFIG_DIR` per profile, separate `gh` login. |
| Diagnostics | `doctor` / `doctor --fix` / `--undo`: active `claude` in `PATH`, launchers, two profiles on the same account, Desktop started without `--user-data-dir`. |

Useful details:

- the Claude Code login is in the Keychain under a key derived from the profile
  folder: renaming the folder loses the login;
- with two Desktop windows open, the login via `claude://` can end up in the
  wrong instance: a profile opens the right folder with the wrong account;
- claude.ai conversations and Projects do not move from one account to another.

## aimux

[Digital-Threads/aimux](https://github.com/Digital-Threads/aimux): TypeScript,
MIT license, `npm install -g @digital-threads/aimux`, Node 22+. It manages
Claude Code, Codex and Gemini CLI.

| | |
|---|---|
| Profiles | `~/.aimux/profiles/<nome>`; `~/.claude` is the source of the shared parts. Configuration in `~/.aimux/config.yaml`. |
| Sharing | Shares via symlink **everything except** a `private` list (credentials, `.claude.json`, cache, session state). By default transcripts and `memory/` are shared too; `aimux migrate isolate` makes `projects/`, `jobs/`, `daemon/` private. |
| Plugins | A real `plugins/` per profile: `marketplaces/` and `cache/` are symlinks, `known_marketplaces.json` and `installed_plugins.json` are copies with rewritten paths, because Claude Code wants the install path inside the profile folder. |
| Launch | `aimux run <profilo>` (also by prefix), `aimux use <profilo>` to activate a profile in the current shell, `aimux run --auto` to pick the subscription with the most headroom. |
| Usage limits | `aimux status`: 5-hour and 7-day percentages of all subscriptions. In sessions started by aimux, a Claude Code mod shows the usage of all subscriptions above the prompt, warns at 90% and names the one with the most headroom. |
| Running out of tokens | When a session stops at the limit, it offers to resume it on the subscription with the most headroom (native `--resume` between Claude profiles; a summary between different CLIs with `aimux handoff`). |
| Other | `aimux split` opens a tmux pane per subscription; `aimux agents` shows the background sessions of all profiles; profiles pointing at Anthropic-compatible APIs. |
| Per-folder choice | None. |

Compared with this project: it covers shared skills, usage limits and switching
when tokens run out. It is, however, designed to spread work across several
subscriptions, not to separate clients: by default it shares transcripts and
memory, and switching when tokens run out moves the conversation to any
subscription with headroom. It manages symlinks itself (`aimux rebuild`), so it
overlaps with chezmoi.

## Others

| Tool | Desktop | Code | Accounts at once | Notes |
|---|---|---|---|---|
| [ajipurn/claude-profiles](https://github.com/ajipurn/claude-profiles) | yes | yes | one at a time | Menu bar app; per-account usage limits from the local cache. |
| [calebbarzee/claude-profiles](https://github.com/calebbarzee/claude-profiles) | yes | sessions only | yes, Desktop | Moves Claude Code sessions into Desktop profiles. |
| [odahcam/claude-desktop-profiles](https://github.com/odahcam/claude-desktop-profiles) | yes | no | yes | Dock colors per profile. |
| [burakdede/aisw](https://github.com/burakdede/aisw) | no | yes | one at a time | Also Codex, Gemini and Antigravity CLI. |
