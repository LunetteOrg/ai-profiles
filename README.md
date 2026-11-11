# ai-profiles

Several AI agent accounts on the same computer, one profile per client, active
at the same time. Each profile has its own login, MCP servers, memory and
history; skills, global instructions, settings and plugins are shared, and
plugins tied to a client are enabled only in that client's repos.

`ai-profile` creates the profiles, launches each agent (Claude Code, Codex,
Cursor, Gemini CLI, OpenCode, Pi) with the profile of the folder it starts in,
passes it the client's secrets, shows the usage of every account and checks
that everything is in order. `ai-secrets`, in the same repo, keeps the secrets
in memory for a while, like `ssh-agent`. Both run on macOS, Linux and Windows;
Claude Desktop instances and account switching are macOS only.

[`docs/existing-tools.md`](docs/existing-tools.md) compares claude-multiprofile
and the other similar tools.

## Install

```sh
go install github.com/LunetteOrg/ai-profiles/cmd/...@latest   # ai-profile and ai-secrets
gh release download --repo LunetteOrg/ai-profiles --pattern '*darwin_arm64*'
```

The GitHub releases have one archive per system and architecture, with
`checksums.txt`. A `v*` tag publishes a release: the `release` workflow runs the
tests and GoReleaser (`.goreleaser.yaml`), which embeds the version shown by
`ai-profile version`. To try it locally: `goreleaser release --snapshot --clean`.

The shims are links named after the agents, pointing to `ai-profile`, in a
folder that comes before the real agents in `PATH`:

```sh
mkdir -p ~/.local/share/ai-profiles/shims
for a in claude codex; do ln -s "$(command -v ai-profile)" ~/.local/share/ai-profiles/shims/$a; done
export PATH="$HOME/.local/share/ai-profiles/shims:$PATH"   # in the shell startup file
```

`ai-profile doctor` says when a startup file puts an agent's folder before the
shims.

The tool shims are links named after the CLIs declared in `tools.` (below,
Secrets), pointing to `ai-secrets`, in a folder of their own that also comes
before the real tools in `PATH`:

```sh
mkdir -p ~/.local/share/ai-profiles/tool-shims
for t in scw; do ln -s "$(command -v ai-secrets)" ~/.local/share/ai-profiles/tool-shims/$t; done
export PATH="$HOME/.local/share/ai-profiles/tool-shims:$PATH"   # in the shell startup file
```

## Commands

| Command | What it does |
|---|---|
| `ai-profile [<profile>] [<agent>] [-- <arguments>]` | Launches the agent (default `claude`) with the folder's profile, or with the given one if the folder admits it (`handoff_to`, or the profiles of a `[[folders]]` entry). |
| `ai-profile --auto [-- <arguments>]` | Launches, among the profiles and agents the folder admits, the one with the most headroom according to `usage`. |
| `ai-profile handoff <profile>\|<agent> [--session <id>]` | Continues the folder's last Claude session in another allowed profile, or the work with another agent of the profile (below, Switching). |
| `ai-profile which [<agent>]` | Tells which profile the agent would start with, and why. |
| `ai-profile new <profile> [--dir <folder>…] [--agents claude,codex…] [--account <email>]` | Adds the profile to the configuration, creates its folder for each agent with the links to the shared parts, runs `sync` and says how to log in. |
| `ai-profile sync [--dry-run]` | Brings profiles and repos to the state of the configuration, for each agent: MCP servers, credential folders, links, per-repo plugins, the skills and commands that come with `ai-profile`. |
| `ai-profile mcp <profile> [--repo <folder>] add <server>…` | Declares servers for the profile or one of its repos, then runs `sync`. |
| `ai-profile plugins <repo> enable\|disable <plugin>…` | Enables or disables Claude Code plugins in a single repo, records it in the configuration and runs `sync`. |
| `ai-profile skills list [<profile>] \| split <profile> \| share <profile> <skill> \| link <skill>` | Skills seen by each profile and their origin; a skills folder of the profile's own; sharing one of its skills; linking a skill of `~/.agents/skills` into `~/.claude/skills`. |
| `ai-profile account list \| save \| use <email> [--profile <profile>]` | Several Claude accounts in the same profile, switched even with sessions open; inside a session `/account [<email or part>]`. |
| `ai-profile desktop [<profile>] \| install [<profile>]` | Opens the profile's Claude Desktop, after realigning its copy of Claude.app; `install` writes the launchers in `~/Applications` and prepares the copies. |
| `ai-profile usage [--json]` | Usage of every profile and every agent. |
| `ai-profile folder add [<folder>] <profile>… \| rm [<folder>] [<profile>…] \| list` | Folders used with several profiles (`[[folders]]`), by default the current one. |
| `ai-profile default <profile>` | Makes the profile the default, for the folders that no profile declares. |
| `ai-profile migrate-default <profile> [--dry-run]` | Copies sessions, memory and history from `~/.claude` into the profile, only the missing files and without touching `~/.claude`, merges the histories in date order and makes it the default. |
| `ai-profile backup` / `restore [--force]` | Saves and brings back the private file and the files it lists (below, Private part). |
| `ai-profile statusline` | Claude Code's statusline, set by `sync` (below). |
| `ai-profile doctor [--fix]` | Checks profiles, accounts, secrets, MCP, links, folders, `PATH`, Desktop and Claudian; `--fix` repairs what it can without losing anything. |

The names of the subcommands and of the agents are reserved: `new` rejects a
profile with one of those names.

## Configuration

The configuration is `~/.config/ai-profiles/config.toml` (or
`AI_PROFILES_CONFIG`):

```toml
default = "personal"

[profiles.personal]
account = "me@example.com"
agents = ["claude", "codex", "opencode", "pi"]
handoff_to = ["client-a"]                 # profiles that can be launched from here

[profiles.client-a]
account = "me@client-a.example"
dirs = ["~/Developer/client-a"]
agents = ["claude", "codex"]               # default ["claude"]

[[folders]]                                # a folder used for several clients
dir = "~/Developer/shared"
profiles = ["personal", "client-a"]
```

The deepest configured folder that contains the current one applies; from a
worktree, the main repo counts (`git rev-parse --git-common-dir`). Elsewhere the
`default` profile applies. An agent not listed in the profile's `agents` does
not start, and the message says which profile applies and why.

A folder in `[[folders]]` admits only the profiles it lists, whatever their
`handoff_to`: `claude` starts with the last one started there (recorded in
`~/.local/state/ai-profile/last-used.json`), the first listed the first time,
and `ai-profile <profile>` starts another, which becomes the last used. Each
profile keeps its own sessions of the folder. A folder is either in
`[[folders]]` or in the `dirs` of one profile: `folder add` moves it out of the
`dirs` into the entry, with that profile first, so `claude` keeps starting with
it.

`new`, `mcp add`, `plugins`, `default` and `folder` edit the file line by line,
keeping comments and order; a list written over several lines must be edited by
hand. A dotfiles manager that keeps a copy of the file must take the change back
(for chezmoi, `chezmoi re-add`).

### Private part

`config.toml` can live in a published dotfiles repo when what names clients,
emails and vault items sits in a private file next to it:

```toml
# config.toml
[private]
file    = "~/.config/ai-profiles/private.toml"
restore = "op document get ai-profiles --vault Work"    # archive on stdout
backup  = "op document edit ai-profiles --vault Work -" # archive on stdin
```

```toml
# private.toml
default = "client-a"
files = ["~/Developer/client-a/AGENTS.md"]   # other files of the archive

[profiles.client-a]
dirs = ["~/Developer/client-a"]
```

The private file holds profiles, `[mcp]` servers, `[[folders]]`, `default` and
`files`; `ai-profile` merges it with `config.toml`, and each item is defined in
one of the two files only. `new` writes a new profile into the private file; the
other commands edit the file that defines what they change.

`backup` packs the private file and `files` into one gzipped tar and pipes it to
the `backup` command; `restore` runs the `restore` command and unpacks what it
prints, paths relative to home. When the private file is missing, as on a new
computer, any `ai-profile` command restores it first. The commands are the
user's: `ai-profile` knows neither the password manager nor the dotfiles
manager. `doctor` reports the private files changed since the last backup or
restore. `ai-secrets` does the same with its own `[private]` section, archive,
`ai-secrets backup` and `ai-secrets restore`.

### MCP servers

The agent's login is one per profile, but MCP servers use other accounts, and
each credential must be reachable only from the profile, or the repo, of the
client it belongs to. Each server is defined once, with placeholders in the
values, and each profile or repo declares which servers it uses:

```toml
[mcp.notion-client-a]
url = "https://mcp.notion.com/mcp"

[mcp.example]
command = "uvx"
args = ["example-mcp"]
env.EXAMPLE_TOKEN_DIR = "~/.example/{profilo}"
dirs = [{ path = "~/.example/{profilo}", only = ["{email}.json"] }]

[profiles.client-a]
mcp = ["notion-client-a", "example"]
vars = { email = "me@client-a.example" }

[[profiles.personal.repos]]
dir = "~/Developer/personal/app"
mcp = ["example"]
plugins = ["posthog", "cloudflare"]
```

The placeholders are `{profilo}` and the keys of `vars`; a repo's `vars` are
added to those of its profile. For each declared server `sync`:

1. replaces the placeholders and creates the `dirs` folders with permissions
   `700`; `only` lists the files a folder may contain, because a server can add
   credentials by itself and a login approved by mistake puts another client's
   account in the profile;
2. writes the server into the configuration of each agent of the profile, in the
   agent's format: for all projects if declared in the profile; if declared in a
   repo, in local scope for Claude Code, in the repo's `.codex/config.toml` for
   Codex (marked `trust_level = "trusted"`, unless the profile declares it
   untrusted) and in the repo's `opencode.json` for OpenCode, where it replaces
   the profile's server with the same name;
3. updates an existing server whose configuration no longer matches.

The files `sync` writes inside a repo go into `.git/info/exclude` when git does
not already ignore them; a file the repo tracks belongs to the team, and `sync`
leaves it alone and reports it. Servers the configuration does not declare stay
where they are and are reported. `sync` does not touch tokens: each service's
login starts on first use.

A repo's `plugins` go into its `.claude/settings.local.json` and its
`disabled_plugins` turn off there the plugins enabled everywhere; installed
plugins are shared, and `enabledPlugins` in the shared `settings.json` enables
those used everywhere. Plugin MCPs are authenticated in every profile that
enables the plugin.

### Secrets

The configuration holds only references to secrets, never the values:

| Reference | Source | Where it applies |
|---|---|---|
| `op://<vault>/<item>/<field>` | 1Password, with the app's approval | MCP servers and provider keys |
| `bw://<account>/<item>`, `bw://<item>` | Bitwarden, the account named in the reference or the profile's (`bitwarden = "<account>"`) | MCP servers (with the account named) and provider keys |
| `env:<VARIABLE>` | environment or `~/.env` | MCP servers and provider keys |

A 1Password item id is better than the title: it stays valid when the item is
renamed and is not ambiguous when two items share a title. `bw://` references
read the password; `#username`, `#notes`, `#totp`, `#field=<name>` and
`#attachment=<file name>` read the other parts.

**MCP servers** with references in their `env` start through `ai-secrets`:

```toml
[mcp.budget]
command = "uv"
args = ["tool", "run", "mcp-budget"]
env.BUDGET_API_KEY = "op://Private/Budget/credential"
```

For each agent `sync` writes `~/.local/bin/ai-secrets run --reason "server
budget, profile personal" -- uv tool run mcp-budget`, with the reference in the
`env`, so the server works however the agent is started. For Codex it adds
`startup_timeout_sec = 60`, since the server may wait for an approval. An `env:`
reference becomes `${VAR}` for Claude Code, `env_vars` for Codex and
`{env:VAR}` for OpenCode. References in the headers of remote servers are not
supported.

**Provider keys** that the agent reads from its environment (Kimi, Mistral,
OpenAI for OpenCode) are in the profile; `ai-profile` asks `ai-secrets` for them
at launch, so they apply to launches through `ai-profile` or a shim:

```toml
[profiles.client-a]
secrets.MOONSHOT_API_KEY = "op://Client-a/Kimi/credential"
secrets.MISTRAL_API_KEY = "bw://Mistral"
```

**Tool keys** are for CLIs that read a key from the environment before their
own config file, such as the Scaleway CLI (`SCW_SECRET_KEY` before
`~/.config/scw/config.yaml`). Called through its tool shim, the CLI gets the
key from `ai-secrets` in its own environment only: the key is neither in the
file nor in the agent's environment.

```toml
[profiles.client-a.tools.scw]
env.SCW_SECRET_KEY = "op://Client-a/Scaleway/secret key"
reason = "Scaleway CLI"
deny_args = [["config", "info"], ["config", "dump"], ["config", "get", "secret-key"], ["config", "get", "secret_key"]]
```

`deny_args` lists the subcommands the shim refuses to run: `scw config info`,
`dump` and `get secret-key` print the active configuration, environment
included, so they would print the key. A subcommand matches after the global
flags (`scw -p prod config info` is refused); a refused command exits with an
error and neither asks for the key nor runs `scw`.

The shim finds the folder's profile as `ai-profile` does. When that profile
declares the tool, it resolves the `env.` references as `ai-secrets run` does,
with the same approval and `ttl`, `reason` (default: the tool's name) and the
profile as the reason, then execs the next tool of that name in `PATH`, outside
its own folder. Otherwise it execs the tool unchanged.

**ai-secrets** answers on a socket in `~/.cache/ai-secrets/`, a folder only the
user can enter, and starts by itself on the first request:

| Command | What it does |
|---|---|
| `ai-secrets run [--reason <reason>] -- <command>` | replaces the `op://` and `bw://<account>/<item>` references in the environment with their values and launches the command |
| `ai-secrets status` | secrets in memory, with variable names and expiry, without values |
| `ai-secrets lock` | forgets all secrets |
| `ai-secrets stop` | stops the agent |

Every new or expired secret asks for its own approval, after a notification
with the reason and the variables; an approved value stays in memory for the
`ttl` and is reused without asking. No command prints a value, error messages
name references, never values, and values reach only the environment of the
command `ai-secrets run` launches. How it reaches each store is set in
`~/.config/ai-secrets/config.toml`:

```toml
ttl = "30m"

[stores.1password]
read = "isolated"                 # or "shared"

[stores.bitwarden.client-a]
server = "https://vault.bitwarden.eu"
unlock = "password"               # or "biometric", when the bw CLI has it
password = "op://Private/Bitwarden/password"
lock_after_read = true
```

- **1Password, `read = "isolated"`**: every `op read` runs in a process session
  of its own, and 1Password scopes an approval to the session that asked, so
  each new secret gets its own approval. With `"shared"` one approval covers the
  reads of the following ten minutes.
- **Bitwarden, `lock_after_read = true`**: every request is a full login: the
  master password is read from 1Password, used for `bw unlock` and dropped, the
  values are read, then `bw lock`. With `false` the account stays unlocked for
  the `ttl`, with the session key only in the agent's memory.
- **Bitwarden accounts** have their CLI data folder in
  `~/.config/ai-secrets/bitwarden/<account>`; the login is done once with
  `BITWARDENCLI_APPDATA_DIR=… bw login`.

### Several Claude accounts in one profile

A profile can have several Claude accounts for the same client, for example two
subscriptions, and switch between them without closing sessions:

```toml
[profiles.personal]
account = "me@example.com"
claude_accounts = ["me@example.com", "me@other.example"]
```

| Command | What it does |
|---|---|
| `account save` | saves the profile's active account in a Keychain entry of its own (`ai-profile-account:<profile>:<email>`) |
| `account use <email>` | saves the active account's current token, loads the other into the profile's entry and updates `oauthAccount` in `.claude.json`; a part of the email that only one saved account contains is enough |
| `account list` | saved accounts, the active one marked, and those in `claude_accounts` not saved yet |

To add an account: `account save`, `/login` with the other account in a session
of the profile, then `account save` again. Each profile has its own login for
each account, because the same token in two Keychain entries breaks at the
first rotation. Claude Code rereads the credentials while it works, so open
sessions move to the new account; MCP servers, settings and transcripts stay
with the profile. Inside a session the `account` skill, installed by `sync`,
makes this `/account`. `doctor` accepts as linked both `account` and the
accounts in `claude_accounts`, also when another profile uses them.

### Statusline

`sync` sets `statusLine` in the shared `~/.claude/settings.json` to
`ai-profile statusline`, which finds the profile from `CLAUDE_CONFIG_DIR` and
the account from `oauthAccount`, then runs the user's command with Claude Code's
input and `AI_PROFILE` and `AI_PROFILE_ACCOUNT` in its environment; without a
command it prints `@<profile> <account>`:

```toml
[statusline]
claude = "sh ~/.claude/statusline-command.sh"
```

### Claude Desktop

Claude Desktop accepts only one account, but as an Electron app it takes
`--user-data-dir`: each instance has its own login, chats, connectors and
extensions. A profile with `desktop` has its own instance:

```toml
[profiles.client-a]
desktop = { hue = 330 }   # rotation of the icon color, in degrees
```

- a **copy of Claude.app** in
  `~/Library/Application Support/ai-profile/desktop/Claude <Profile>.app`, with
  the icon tinted by `hue` degrees;
- a **data folder** `~/Library/Application Support/Claude-<Profile>`;
- a **launcher** `~/Applications/Claude <Profile>.app`, written by
  `desktop install`: an AppleScript applet, signed ad hoc, that runs
  `ai-profile desktop <profile>`, waits for it and shows its error in a dialog.
  It is an applet because Spotlight lists only apps with a real executable.

`ai-profile desktop [<profile>]` first offers to quit and reopen a copy open
without its data folder (started by Claude's own updater or by a Dock icon of
the open window), which shows the default account; then realigns the copy to
`/Applications/Claude.app`, which keeps updating itself, by tinting a new APFS
clone beside it and swapping it in once complete; then starts it with
`CLAUDE_CONFIG_DIR` set, so the Code tab uses the profile too. A lock keeps two
opens of the same profile from rebuilding the copy at once.

Pin the launcher to the Dock, not the icon of the open window, and log in with
only one Claude Desktop open: the `claude://` link can end up in the wrong
instance.

### Skills

A Claude Code profile sees the shared skills through its `skills/` link to
`~/.claude/skills`. After `skills split <profile>`, `skills/` is a real folder
with a link for each shared skill, except those in `skills_exclude`, and the
profile's own skills; `sync` adds links to new shared skills and removes those
to excluded or vanished ones. What installers write into
`$CLAUDE_CONFIG_DIR/skills` then stays in the profile, and `skills share` moves
it among the shared ones.

```toml
[profiles.client-a]
skills_exclude = ["budget"]
```

`~/.agents/skills` is read by Codex, Cursor and Pi in every profile: it is fine
for common skills, not for a client's.

## How it works

### Agents

Each agent has an adapter that translates the profile into the agent's
mechanisms. Profile folders follow the pattern `~/.<agent>-<profile>`.

| Agent | Profile folder | Login | MCP servers |
|---|---|---|---|
| Claude Code | `CLAUDE_CONFIG_DIR=~/.claude-<profile>` | Keychain, one entry per folder (`Claude Code-credentials-<path hash>`); `.credentials.json` outside macOS | `.claude.json` |
| Codex | `CODEX_HOME=~/.codex-<profile>` (sessions, history and database too) | `auth.json` in the folder | `config.toml`, `[mcp_servers.<name>]`; OAuth tokens in files (`mcp_oauth_credentials_store = "file"`) |
| Cursor, `agent` CLI | `CURSOR_CONFIG_DIR=~/.cursor-<profile>` (only `cli-config.json`) | `CURSOR_API_KEY` from the secrets | `~/.cursor/mcp.json`, shared by every profile |
| Gemini CLI | `GEMINI_CLI_HOME=~/.gemini-<profile>` | `oauth_creds.json` in the folder | `settings.json`, `mcpServers` |
| OpenCode | `~/.opencode-<profile>/`, with `OPENCODE_CONFIG_DIR`, `XDG_DATA_HOME` and `XDG_STATE_HOME` inside it | provider keys from the secrets | `opencode.json` |
| Pi | `PI_CODING_AGENT_DIR=~/.pi-<profile>` | `auth.json` in the folder | `mcp.json` in the folder |

| Agent | Common instructions | Client folder's `AGENTS.md` | Linked account |
|---|---|---|---|
| Claude Code | `CLAUDE.md` → `~/.agents/AGENTS.md` | read (`instructionFiles: claude-md-and-agents-md`) | `oauthAccount` in `.claude.json` |
| Codex | `AGENTS.md` → `~/.agents/AGENTS.md` | read through a root marker (below) | `email` in the `id_token` of `auth.json` |
| Cursor | user rules in the interface only | not read | — |
| Gemini CLI | `.gemini/AGENTS.md` → `~/.agents/AGENTS.md` | read through a root marker (below) | `active` in `google_accounts.json` |
| OpenCode | `config/AGENTS.md` → `~/.agents/AGENTS.md` | read: it goes up to `$HOME` | — (API keys) |
| Pi | `AGENTS.md` → `~/.agents/AGENTS.md` | read: it goes up to the root of the disk | — |

The common instructions are in `~/.agents/AGENTS.md`, and each agent reaches
them through a link from its own global file. There is no `CLAUDE.md` in
`~/.claude/`: with `CLAUDE_CONFIG_DIR` pointing elsewhere it would count as a
project instruction in every repo under home. Codex and Gemini CLI do not go
above the repo root, so `sync` puts a placeholder file (`.agents-root`) in the
client folder and declares it as a root marker: the search starts there and
finds both the client's `AGENTS.md` and the repo's.

Some agents keep state outside the profile folder; the adapter brings it inside
or `doctor` reports it:

- **OpenCode** starts a background service on a fixed port: `sync` gives each
  profile its own port in `config/service.json`, derived from the name (49500 to
  49899), and keeps a port already chosen. The adapter pins the data folders of
  mise and uv to where they are without a profile, since OpenCode's programs
  inherit the profile's `XDG_DATA_HOME` and `XDG_STATE_HOME`.
- **Codex** stores MCP OAuth tokens in files, not in the Keychain, whose key
  does not depend on `CODEX_HOME`.
- **Gemini CLI** with encrypted storage uses a fixed Keychain entry, so the
  adapter keeps file storage. Sandbox, ACP and completion still read
  `~/.gemini`.
- **Cursor** keeps `mcp.json`, chats and skills in `~/.cursor`, common to IDE,
  CLI and instances.

### Profile choice and shims

A shim finds the folder's profile, sets the adapter's variables, resolves the
profile's provider keys and launches the real agent with the same arguments. It
works outside interactive terminals too: Orca, Claudian, editors and any program
that launches the agent from `PATH`. A profile variable that is already set
(`CLAUDE_CONFIG_DIR`, `CODEX_HOME`, `OPENCODE_CONFIG_DIR`) is respected, with a
warning when it does not match; `doctor` reports the shell startup files and the
folders' `mise.toml` or `.envrc` that set one.

`~/.claude` holds only the common parts and is not a profile. Each Claude Code
profile has links to it:

| Shared | Separate per profile |
|---|---|
| `CLAUDE.md` (→ `~/.agents/AGENTS.md`), `skills/` | login (Keychain) |
| `settings.json` (permissions, enabled plugins, hooks, statusline) | `.claude.json` (MCP servers, account state) |
| `plugins/` (plugin code) | MCP authentication, including plugin MCPs |
| `hooks/`, `statusline-command.sh`, `statusline-helpers.sh` | `projects/` (memory, sessions), `history.jsonl`, cache |

### Switching

A running session cannot change account, but the work can continue elsewhere:

- **same agent, another profile** (Claude Code): `handoff <profile>` copies the
  session file into the destination profile and launches `claude --resume <id>`
  there;
- **another agent, same profile**: `handoff <agent>` builds a summary from the
  session file (first request, last exchanges, modified files) without spending
  tokens, saves it in `handoffs/` in the profile folder and launches the other
  agent with it as the first message;
- **automatic choice**: `--auto` launches, among the allowed combinations, the
  one with the most headroom.

A switch moves the client's data: resuming a client's session in the personal
profile sends the whole conversation to the personal account. So each profile
lists in `handoff_to` the profiles it can switch to, none by default, and an
explicitly given profile that does not match the folder's follows the same rule.
`--auto` and `handoff` choose only among the folder's profile and its
`handoff_to`, or the profiles of its `[[folders]]` entry, each with its own
agents.

From inside a session (`/handoff`, a skill installed by `sync`), `handoff` takes
the current session and starts the next agent in a new terminal: a terminal of
the same Orca worktree, a Terminal.app window outside Orca, or the clipboard.
Inside Codex and OpenCode it reads the current session from
`CODEX_SESSION_ID` and `OPENCODE_SESSION_ID`. The copy is a snapshot, so the old
session is closed with `/exit`.

### Usage

`usage` reads with each profile's credentials, without writing them:

| Agent | What it shows | Source |
|---|---|---|
| Claude Code | 5-hour, 7-day and per-model weekly windows, with reset and severity; one row per saved account | `GET https://api.anthropic.com/api/oauth/usage` with the profile's OAuth token |
| Codex | 5-hour and weekly windows, as of the last session | the `rate_limits` of the profile's session files |
| Gemini CLI | remaining quota and reset | Code Assist's `retrieveUserQuota`, as `/stats` does |
| OpenCode | cost and tokens of the last days; provider balance | `opencode stats --cost --json`; Kimi's balance API |
| Pi | tokens and cost per session | the profile's session files |

The Claude, Codex and Gemini endpoints are not documented as public APIs:
`usage` reports when a source stops answering.

### doctor

`doctor` checks that the configuration is still the intended one; `--fix`
repairs what can be repaired safely and keeps a copy of what it changes.

| Check | Example problem |
|---|---|
| Accounts | A profile logged in with an account it does not list; two profiles on the same account. |
| Shared parts | A link broken or replaced by a real file; an agent without its link to `~/.agents/AGENTS.md`; a `~/.claude/CLAUDE.md` that has reappeared. |
| Secrets | A reference that does not resolve; `ai-secrets`, `op` or `bw` missing from `PATH`; a Bitwarden account not logged in; a missing `env:` variable. |
| MCP | Pending `sync` changes; servers in `~/.claude.json`, visible to sessions without a profile; a credential folder with files `only` does not allow. |
| Folders | A `[[folders]]` entry with one profile or a missing folder; a `mise.toml`, `mise.local.toml` or `.envrc` that sets an agent's folder. |
| Tools | A tool shim missing, behind the real tool, or with no real tool after it in `PATH`; a tool's config file that still holds the secret (for `scw`, a `secret_key` in any profile of its `config.yaml`, named without the value). |
| `PATH` | A shim that does not come before the real agent, including lines of the zsh startup files and of the bash login file that put an agent's folder first. |
| Desktop | A copy open without its data folder, behind Claude.app, without its icon or with leftovers of a swap; a launcher missing or not written by `ai-profile`. |
| Claudian | A vault whose Claudian does not pass the vault's profile in `CLAUDE_CONFIG_DIR`. |
| Private files | Files changed since the last backup. |

The account is read from what each app records: `oauthAccount` in the profile's
`.claude.json`, the `id_token` of Codex's `auth.json`, `active` in Gemini CLI's
`google_accounts.json`, `lastKnownAccountUuid` in a Desktop instance's
`config.json`.

## Platforms

| | macOS | Linux | Windows |
|---|---|---|---|
| Claude Code login | Keychain, one entry per profile folder | `.credentials.json` in the profile | to be checked |
| Codex and OpenCode login | files and database in the profile folder | as macOS | as macOS |
| Shared parts | symlinks | symlinks | junctions for folders, hardlinks or copies for files |
| Shims | links in `PATH` | links in `PATH` | `.cmd` in `PATH` |
| Tool shims | links in `PATH` | links in `PATH` | not available (`doctor` reports it) |
| Claude Desktop instances, `account` | yes | no | no |

## Orca

[Orca](https://github.com/stablyai/orca) launches agents from `PATH`, so the
shims choose the profile in its terminals as anywhere else.

- **Claude account**: Orca's selector stays on *System default*, the shell's
  `CLAUDE_CONFIG_DIR`. Accounts managed by Orca apply to the whole computer, not
  per folder, and copy `settings.json` and `CLAUDE.md` instead of using the
  links.
- **Worktrees**: with Orca's workspace folder set to `.worktrees`, relative to
  the repo, worktrees end up inside the repo, so the folder's profile applies.
  With `.worktrees/` in git's global ignore they do not show up in the repos.
- **Skills**: the `skills` CLI installs Claude Code skills in
  `$CLAUDE_CONFIG_DIR/skills`, which through the link is `~/.claude/skills`;
  install Orca's choosing Claude Code as well
  (`npx skills add stablyai/orca -g -a claude-code -s orca-cli -s orchestration`).
- **Hooks**: Orca adds its hooks to the shared `settings.json`; they activate
  only in Orca's terminals (`ORCA_AGENT_HOOK_PORT`).

## Discarded directions

- **Tool keys in a profile's `secrets.`**: the key would be in the whole
  environment of the agent, which can print it.
- **Tool shims in the agents' shim folder**: the tool shims are links to
  `ai-secrets`, the agent shims to `ai-profile`; a folder each keeps the
  installation and the `PATH` order of the two apart.
- **Personal profile in `~/.claude`**: a session opened in a folder no profile
  declares would use the personal account; with the personal profile in its own
  folder, `~/.claude` has no login.
- **Separate `settings.json` per profile**: the copies diverge and `/config`
  applies only in the profile where it is used. Data isolation lies in the MCP
  servers, which are already separate.
- **Copying skills and `CLAUDE.md` into each profile**: the copies diverge.
- **Claude accounts managed by Orca**: one active at a time for the whole
  computer, with copies of the shared settings.
- **Orca worktrees outside the repos**: sessions would start on the default
  profile.
- **Adopting claude-multiprofile**: it does not share skills and instructions,
  does not associate folders with profiles, does not work on Windows and skips
  read protection when `settings.json` is shared. Its per-profile Claude Desktop
  instances are the origin of `ai-profile desktop`.
- **Adopting aimux**: it spreads work across subscriptions instead of separating
  clients: by default it shares transcripts and memory and does not associate
  folders with profiles. Its `usage`, `--auto` and `handoff` are taken up here
  within the folder's constraints.
- **Blocking Claude's updater in the Desktop copies**: the `disableAutoUpdates`
  policy is read from the bundle identifier the copies share with
  `/Applications/Claude.app`, which must keep updating; the copy is realigned at
  every open instead.
- **A launcher that updates in the background**: the update was cut short when
  the launcher exited, leaving copies half swapped.
- **`launchctl setenv CLAUDE_CONFIG_DIR`** for the default in graphical apps:
  every app would inherit the variable, and the shim, which respects a variable
  already set, would no longer pick the profile from the folder.
- **MCP server secrets read by `ai-profile` at launch**: they would apply only to
  launches through `ai-profile`, and a `claude` launched directly would start the
  server without a key.
- **`op run` around each MCP server**: for 1Password every process without a
  terminal is a separate request, and with many sessions open it asks for dozens
  of approvals. `ai-secrets` asks for one per period.
- **Bitwarden Secrets Manager (`bws run`)**: a separate product that requires an
  access token, itself a secret to keep elsewhere.
- **Summary requested from the current agent** for `handoff`: the switch is
  needed mostly when tokens have run out, and then the agent cannot answer.
- **Commands dedicated to one MCP server** (moving its tokens): they tie the
  program to one server and to the format of its folders; definitions with
  placeholders cover any server.
- **Only `OPENCODE_DB` per profile**: logs, prompt history and the background
  service registration would stay shared.
- **API keys in the agents' configurations**: they would end up on disk in clear
  text, in files the agents can read and rewrite.

## Sources

- [claude-multiprofile](https://github.com/jmdarre-v/claude-multiprofile)
- [Two Claude accounts on Mac — melkon.tech](https://melkon.tech/blog/two-claude-accounts-mac)
- [Run multiple Claude accounts — multiclaude.app](https://multiclaude.app/run-multiple-claude-accounts-mac)
- [Switching Claude accounts on Mac — zenn.dev](https://zenn.dev/5alt_nacl/articles/claude-account-switching-guide-mac?locale=en)
