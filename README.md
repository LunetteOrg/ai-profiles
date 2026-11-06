# ai-profiles

Several AI agent accounts on the same Mac, one per client, active at the same
time. Each profile has its own login, MCP servers, memory and history; skills,
global instructions, settings and plugins are shared, and plugins tied to a
client are enabled only in that client's repos. The agent configured today is
Claude Code; Codex and OpenCode (with Kimi and Mistral) follow the same scheme,
described in [`docs/helper.md`](docs/helper.md).

Documents:

- [`docs/helper.md`](docs/helper.md): the helper program that automates the
  procedure for each agent, picks the profile from the folder through a
  shim (`claude`, `codex`, `opencode`), passes the client's
  secrets from 1Password, Bitwarden or environment variables, and shows the
  usage of every profile.
- [`docs/existing-tools.md`](docs/existing-tools.md):
  claude-multiprofile and the other similar tools.

## ai-profile

The program (Go, in `cmd/ai-profile`) launches Claude Code, Codex and OpenCode
with the folder's profile and brings the profiles to the state of `config.toml`:

| Command | What it does |
|---|---|
| `ai-profile [<profilo>] [<agente>] [-- <argomenti>]` | Launches the agent (default `claude`) with the folder's profile, or with the given one if the folder admits it (`handoff_to`, or the profiles of a folder in `[[folders]]`). |
| `ai-profile --auto [-- <argomenti>]` | Launches, among the profiles and agents the folder admits, the one with the most headroom according to `usage`. |
| `ai-profile handoff <profilo>\|<agente> [--session <id>]` | Continues the folder's last Claude session in another allowed profile (`--resume`), or the work with another agent of the profile, starting from a summary built from the session. From inside a session (`/handoff`, a shared skill) it takes the current session and opens the next agent in a new Orca terminal or Terminal.app window. |
| `ai-profile which [<agente>]` | Tells which profile the agent would start with, and why. |
| `ai-profile new <profilo> [--dir <cartella>…] [--agents claude,codex…] [--account <email>]` | Adds the profile to `config.toml`, creates `~/.claude-<profilo>` with the links to the shared parts, runs `sync` for the other agents and says how to log in. |
| `ai-profile sync [--dry-run]` | Writes the declared MCP servers into the configuration of each agent of the profile, creates the Codex and OpenCode folders and links, enables per-repo plugins, installs the skills and commands that come with `ai-profile` (`/handoff`). |
| `ai-profile skills list [<profilo>] \| split <profilo> \| share <profilo> <skill> \| link <skill>` | Skills seen by each profile and their origin; a skill folder of a profile's own, with `skills_exclude`; sharing a skill of the profile; linking a skill of `~/.agents/skills` into `~/.claude/skills`. |
| `ai-profile account list \| save \| use <email> [--profile <profilo>]` | Several Claude accounts in the same profile (`claude_accounts`): saves each one's credentials in the Keychain and switches from one to another, even with sessions open; inside a session `/account [<email or part>]`. |
| `ai-profile desktop [<profilo>] \| install [<profilo>]` | Opens the profile's Claude Desktop, or the folder's one, after realigning its copy of Claude.app; `install` writes the launchers in `~/Applications` and prepares the copies. `doctor --fix` repairs launchers and copies. |
| `ai-profile usage [--json]` | Usage of every profile: 5-hour and 7-day windows of Claude (from the profile's Keychain) and of Codex (from the last session), cost of the last 7 days of OpenCode. |
| `ai-profile folder add [<cartella>] <profilo>… \| rm [<cartella>] [<profilo>…] \| list` | Folders used with several profiles (`[[folders]]`), by default the current one: `add` creates the entry or adds profiles, moving the folder out of a profile's `dirs` (that profile stays first); `rm` removes profiles, or the whole entry; `list` shows each folder with its last used profile. |
| `ai-profile default <profilo>` | Makes the profile the default in `config.toml`, for the folders that no profile declares. |
| `ai-profile migrate-default <profilo> [--dry-run]` | Copies sessions, memory and history from `~/.claude` into the profile, only the missing files and without touching `~/.claude`, and makes it the default. |
| `ai-profile mcp <profilo> [--repo <cartella>] add <server>…` | Declares `[mcp]` servers in `config.toml` for the profile or one of its repos, then runs `sync`. |
| `ai-profile plugins <repo> enable\|disable <plugin>…` | Enables or disables Claude Code plugins in a single repo, records it in `config.toml` and runs `sync`. |
| `ai-profile statusline` | Claude Code's statusline, set by `sync`: runs your own command (`[statusline] claude` in `config.toml`) with Claude's input and `AI_PROFILE`, `AI_PROFILE_ACCOUNT` in its environment, or prints `@<profile> <account>` without one. |
| `ai-profile doctor [--fix]` | Checks folders, `~/.agents/AGENTS.md`, profile links, linked accounts, `~/.claude.json`, pending `sync` changes, files not allowed in credential folders (`only`), Codex login, profile choices outside the configuration, and `PATH`, including the lines of the zsh startup files and of the bash login file (`~/.bash_profile`, `~/.bash_login` or `~/.profile`, which Orca's `bash -lc` reads) that put an agent's folder before the shims, as an installer does when it appends its own `PATH` line. With `--fix` it recreates missing links, removes such a line when another one adds the same folder, or else adds the shims again right after it (keeping a copy of the file), moves a `~/.claude/CLAUDE.md` that has reappeared, removes the servers from `~/.claude.json` (keeping a copy) and runs `sync`. |

Install it with Go, or from the GitHub releases, one archive per system and
architecture with `checksums.txt`:

```sh
go install github.com/LunetteOrg/ai-profiles/cmd/...@latest   # ai-profile and ai-secrets
gh release download --repo LunetteOrg/ai-profiles --pattern '*darwin_arm64*'
```

A `v*` tag publishes the release: the `release` workflow runs the tests and
GoReleaser (`.goreleaser.yaml`), which embeds the version shown by
`ai-profile version`. To try it locally: `goreleaser release --snapshot --clean`.

The configuration is `~/.config/ai-profiles/config.toml` (or `AI_PROFILES_CONFIG`):

```toml
default = "personal"

[mcp.google-workspace]
command = "~/.local/bin/google-workspace-mcp"

[mcp.notion-client-d]
url = "https://mcp.notion.com/mcp"

[profiles.personal]
account = "me@example.com"
mcp = ["google-workspace", "notion-client-d"]

[profiles.client-d]
account = "me@client-d.example"
dirs = ["~/Developer/client-d", "~/Developer/client-e"]
agents = ["claude", "codex", "opencode"]   # default ["claude"]
handoff_to = ["personal"]                 # profiles that can be launched from here
mcp = ["google-workspace", "notion-client-d"]

[[folders]]                                # a folder used for several clients
dir = "~/Developer/shared"
profiles = ["personal", "client-d", "client-b"]
```

The deepest configured folder that contains the current one applies; from a
worktree, the main repo counts. A folder in `[[folders]]` admits only the
profiles it lists: `claude` starts with the last one started there (recorded in
`~/.local/state/ai-profile/last-used.json`), the first listed the first time,
and `ai-profile client-d` switches to another; `ai-profile which` says which one
and why. `ai-profile folder add . personal client-d client-b` writes the entry. A link named after the agent
(`claude` → `ai-profile`) in a folder that comes before the real agent in `PATH`
is the shim: it launches the real agent with the folder's
profile. A profile variable that is already set (`CLAUDE_CONFIG_DIR`,
`CODEX_HOME`, `OPENCODE_CONFIG_DIR`) is respected, with a warning if it does not
match: a variable set by a shell startup file or by a folder's `mise.toml` or
`.envrc` decides, and `doctor` reports it as a duplicate of `config.toml`.

Secrets are references (`op://…`, `bw://…`, `env:…`), never values. `ai-secrets`,
the repo's second executable, keeps them in memory for a while, so one
1Password approval covers every session until expiry (30 minutes,
configurable) and every new secret asks for its own approval; Bitwarden is
locked again after every read. An MCP server with secret references starts
with `ai-secrets run` and works however the agent is started; the provider
keys in `[profiles.<profilo>.secrets]` are requested by `ai-profile` at
launch. How each store is reached is set in `~/.config/ai-secrets/config.toml`. `sync` writes only what
`config.toml` declares: a server present in a profile but not declared is left
as it is and reported. Formats and per-agent details are in
[`docs/helper.md`](docs/helper.md).

## How it works

Claude Code reads configuration and credentials from the folder given by
`CLAUDE_CONFIG_DIR`. With the variable set, `.claude.json` (MCP servers,
account state) also lives inside that folder instead of in `~/.claude.json`, and
the credentials have their own Keychain entry.

### What is shared

`~/.claude` holds only the common parts and is not a profile; each profile has
symlinks to:

| Shared | Separate per profile |
|---|---|
| `CLAUDE.md` (→ `~/.agents/AGENTS.md`), `skills/` | login (Keychain) |
| `settings.json` (permissions, enabled plugins, hooks, statusline) | `.claude.json` (MCP servers, account state) |
| `plugins/` (plugin code) | MCP authentication, including plugin MCPs |
| `hooks/`, `statusline-command.sh`, `statusline-helpers.sh` | `projects/` (memory, sessions), `history.jsonl`, cache |

## Per-repo plugins

Installed plugins are shared; `enabledPlugins` in `~/.claude/settings.json`
enables only those needed everywhere (playwright, example-skills). A plugin
tied to a client is enabled in the repo's `.claude/settings.local.json`, which
git ignores:

```json
{
  "enabledPlugins": {
    "posthog@claude-plugins-official": true,
    "cloudflare@claude-plugins-official": true
  }
}
```

Plugin MCPs must be authenticated in every profile where the plugin is enabled.

## Claude Desktop

The desktop app supports only one account at a time
([anthropics/claude-code#32783](https://github.com/anthropics/claude-code/issues/32783)).
Being Electron, it accepts `--user-data-dir`: an instance with its own data
folder has independent login, local chats, connectors and extensions. Chats live
on claude.ai, tied to the account.

A profile with `desktop` in `config.toml` has its own instance:

```toml
[profiles.client-d]
desktop = { hue = 330 }   # rotation of the icon color, in degrees
```

`ai-profile desktop install` writes the launchers and prepares the copies,
`ai-profile desktop [<profilo>]` opens an instance, and `doctor` checks them:

| Launcher in `~/Applications` | `hue` | Data folder | `CLAUDE_CONFIG_DIR` |
|---|---|---|---|
| Claude Personal | 160 (teal) | `Claude-Personal` | `~/.claude-personal` |
| Claude Client-d | 330 (pink) | `Claude-Client-d` | `~/.claude-client-d` |
| Claude Client-b | 25 (brown) | `Claude-Client-b` | `~/.claude-client-b` |

Each launcher runs `ai-profile desktop <profilo>` and waits for it; on an error
it shows it in a dialog. How it works is in [docs/helper.md](docs/helper.md#claude-desktop).

Rules:

- pin the **launcher** from `~/Applications` to the Dock, not the icon of the
  open window: that one starts the copy without a data folder, on the default
  account (`ai-profile desktop` then offers to quit and reopen it);
- log in with only one Claude Desktop open: the `claude://` link can end up in
  the wrong instance.

## Orca

[Orca](https://github.com/stablyai/orca) opens terminals with an interactive
zsh, so the shims choose the profile as in any terminal.

- **Claude account**: Orca's selector stays on *System default*, that is, the
  shell's `CLAUDE_CONFIG_DIR`. Accounts managed by Orca apply to the whole Mac,
  not per folder, and copy `settings.json` and `CLAUDE.md` instead of using the
  symlinks.
- **Worktrees**: Orca's workspace folder is `.worktrees`, relative to the repo.
  Worktrees end up in `<repo>/.worktrees/<repo>/<nome>`, inside the client
  folder, so the folder's profile applies. With `.worktrees/` in git's global
  ignore (`~/.config/git/ignore`) it does not show up in the clients' repos.
- **Skills**: the `skills` CLI installs Claude Code skills in
  `$CLAUDE_CONFIG_DIR/skills`, which through the symlink is `~/.claude/skills`;
  install them choosing Claude Code as well
  (`npx skills add stablyai/orca -g -a claude-code -s orca-cli -s orchestration`).
  Skills installed only for the other agents stay in `~/.agents/skills`.
- **Hooks**: Orca adds its hooks to the shared `settings.json`; they activate
  only in Orca's terminals (`ORCA_AGENT_HOOK_PORT`).

## Discarded directions

- **Personal profile in `~/.claude`**: a session opened in a folder no profile
  declares would use the personal account; with the personal profile in its
  own folder, `~/.claude` has no login.
- **Separate `settings.json` per profile**: the copies diverge and `/config`
  applies only in the profile where it is used. Data isolation lies in the MCPs,
  which are already separate.
- **Claude accounts managed by Orca**: one active at a time for the whole Mac,
  with copies of the shared settings.
- **Orca worktrees in `~/orca/workspaces`**: outside the client folders, so
  sessions would start on the default profile.
- **Copying skills and `CLAUDE.md` into each profile**: the copies diverge.

## Sources

- [claude-multiprofile](https://github.com/jmdarre-v/claude-multiprofile)
- [Two Claude accounts on Mac — melkon.tech](https://melkon.tech/blog/two-claude-accounts-mac)
- [Run multiple Claude accounts — multiclaude.app](https://multiclaude.app/run-multiple-claude-accounts-mac)
- [Switching Claude accounts on Mac — zenn.dev](https://zenn.dev/5alt_nacl/articles/claude-account-switching-guide-mac?locale=en)
