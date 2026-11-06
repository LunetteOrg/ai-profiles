# Helper program

`ai-profile` does what the README describes today as a manual procedure,
for each agent used by the profile (Claude Code, Codex, Cursor, Gemini CLI,
OpenCode, Pi): it creates the
profiles, launches the agent with the right profile, passes it the client's
credentials, shows usage and checks that everything is in order. It works on
macOS, Linux and Windows.

## Commands

| Command | What it does |
|---|---|
| `ai-profile [<agente>] [-- <argomenti>]` | Launches the agent (default `claude`) with the current folder's profile. The agents are `claude`, `codex`, `cursor`, `gemini`, `opencode`, `pi`. |
| `ai-profile <profilo> [<agente>] [-- <argomenti>]` | Launches the agent with the given profile. |
| `new <profilo> [--dir <cartella>…] [--agents <agente>…] [--account <email>]` | Creates the profile's folders for each agent, with the links to the shared parts, and associates the folders with the profile. |
| `desktop [<profilo>] \| install [<profilo>]` | Opens the profile's Claude Desktop, or that of the current folder's profile; `install` writes the launchers and prepares the copies (below, Claude Desktop). |
| `mcp <profilo> [--repo <cartella>] add <server>` | Adds to the profile, or to a single repo, an MCP server defined in `config.toml` and records it there (below). |
| `plugins <repo> enable\|disable <plugin>…` | Enables or disables Claude Code plugins in a single repo, in its `.claude/settings.local.json`, and records it in `config.toml`. |
| `sync` | Brings profiles and repos to the state of `config.toml`, for each agent: MCP servers, credential folders, links, per-repo plugins. |
| `usage` | Usage of all profiles and all agents (below). |
| `migrate-default <profilo> [--dry-run]` | Copies memory, sessions and history from `~/.claude` into the given profile, only the missing files and without removing anything from `~/.claude`, merges the histories in date order and makes the profile the default. |
| `default <profilo>` | Makes the profile the default in `config.toml`: it applies wherever the shim is used, graphical apps included. |
| `folder add [<cartella>] <profilo>… \| rm [<cartella>] [<profilo>…] \| list` | Edits the `[[folders]]` entries, by default of the current folder (below). |
| `which` | Tells which profile applies in the current folder, and why. |
| `backup` | Saves the private file and the files it lists with the `[private] backup` command (below, Private part). |
| `restore [--force]` | Brings them back with the `[private] restore` command; without `--force` a file that differs from the backup stays as it is. |
| `doctor [--fix]` | Checks profiles, accounts, secrets, MCP, links, folders, Desktop and Claudian (below). |
| `handoff <profilo>\|<agente>` | Continues the work in another profile or with another agent allowed in the folder; from inside a session it opens the next agent in a new terminal (below). |
| `--auto` | With a launch: picks, among the agents and profiles allowed in the folder, the one with the most headroom. |

The names of the subcommands (`new`, `mcp`, `which`…) and of the agents are
reserved: `new` rejects a profile with one of those names.

## Agents

A profile is a client: folders, accounts, MCP servers and keys. Each agent has
an adapter that translates the profile into the agent's mechanisms. Profile
folders follow the pattern `~/.<agente>-<profilo>`.

| Agent | Profile folder | Login | MCP servers |
|---|---|---|---|
| Claude Code | `CLAUDE_CONFIG_DIR=~/.claude-<profilo>` | Keychain, one entry per folder (`Claude Code-credentials-<hash del percorso>`) | `.claude.json` |
| Codex | `CODEX_HOME=~/.codex-<profilo>` (also moves sessions, history and database) | `auth.json` in the folder | `config.toml`, `[mcp_servers.<nome>]`; OAuth tokens in files with `mcp_oauth_credentials_store = "file"` |
| Cursor, `agent` CLI | `CURSOR_CONFIG_DIR=~/.cursor-<profilo>` (only `cli-config.json`) | `CURSOR_API_KEY` from the *Secrets* section: the interactive login is in the Keychain under a fixed name, one per macOS user | `~/.cursor/mcp.json`, shared (*Open questions*) |
| Cursor, IDE | instance with its own `--user-data-dir` and `--extensions-dir` and a launcher, like Claude Desktop | in the instance's data folder | `~/.cursor/mcp.json`, shared (*Open questions*) |
| Gemini CLI | `GEMINI_CLI_HOME=~/.gemini-<profilo>`, actual folder `…/.gemini/` | `oauth_creds.json` in the folder (file storage, not the Keychain, where the entry is unique) | `settings.json`, `mcpServers`; OAuth tokens in `mcp-oauth-tokens.json` in the folder |
| OpenCode | `~/.opencode-<profilo>/`, with `OPENCODE_CONFIG_DIR=…/config`, `XDG_DATA_HOME=…/data`, `XDG_STATE_HOME=…/state` | keys from the *Secrets* section; the rest in the `credential` table of `opencode.db` | `opencode.json`, key `mcp.servers` |
| Pi | `PI_CODING_AGENT_DIR=~/.pi-<profilo>` | `auth.json` in the folder, permissions `600` | `mcp.json` in the folder |

| Agent | Common instructions | Client folder's `AGENTS.md` | Usage | Linked account |
|---|---|---|---|---|
| Claude Code | `CLAUDE.md` → `~/.agents/AGENTS.md` | read (`instructionFiles: claude-md-and-agents-md`) | 5-hour and 7-day windows | `oauthAccount` in `.claude.json` |
| Codex | `AGENTS.md` → `~/.agents/AGENTS.md` | not read: it searches from the repo root downward; `project_root_markers` with a placeholder file in the client folder moves the root up there | 5-hour and weekly windows | `email` in the `id_token` of `auth.json` |
| Cursor | only user rules in the interface | not read | no public source, dashboard only | to be checked |
| Gemini CLI | `.gemini/AGENTS.md` → `~/.agents/AGENTS.md`, with `context.fileName = ["AGENTS.md"]` | not read: it stops at `.git`; `context.memoryBoundaryMarkers` with a placeholder file, or `context.includeDirectories` with `loadMemoryFromIncludeDirectories` | quotas with percentage and reset | `active` in `google_accounts.json` |
| OpenCode | `config/AGENTS.md` → `~/.agents/AGENTS.md` | read: it goes up to `$HOME` | cost per session; provider balance where an API exists | — (API keys) |
| Pi | `AGENTS.md` in the folder → `~/.agents/AGENTS.md` | read: it goes up to the root of the disk | tokens and cost per session | — (not recorded) |

The common instructions are in `~/.agents/AGENTS.md`, and
each agent reaches it through a link from its own global file. There is no
`CLAUDE.md` in `~/.claude/`, so no ancestor file of the projects counts as a
project instruction.

For Codex and Gemini CLI, which do not go above the repo root, `sync` puts a
placeholder file (`.agents-root`) in the client folder and declares it as a
root marker in the profile's configuration: the search starts there and finds
both the client's `AGENTS.md` and the repo's.

**State shared between profiles.** Some agents keep data outside the profile
folder; the adapter brings it inside or reports it:

- **OpenCode v2** starts a background service on a fixed port, the same for
  everyone: two profiles would compete for the port, and a client could reach
  another profile's service, with its database, credentials and MCP.
  `sync` assigns each profile its own port in `config/service.json`, derived
  from the name (between 49500 and 49899), and does not change a port already
  chosen. The default port can also be taken by other programs: on this Mac
  Logi Options+ uses it.
- **OpenCode and the programs it starts** inherit the profile's `XDG_DATA_HOME`
  and `XDG_STATE_HOME`. The adapter pins the data folders of mise
  (`MISE_DATA_DIR`, `MISE_STATE_DIR`) and of uv (`UV_PYTHON_INSTALL_DIR`,
  `UV_TOOL_DIR`) to where they are without a profile, if they are not already
  set; other programs that read those variables look for their data in the
  profile.
- **Codex** by default stores the MCP OAuth tokens in the Keychain with a key
  that does not depend on `CODEX_HOME`: `sync` sets
  `mcp_oauth_credentials_store = "file"`.
- **Gemini CLI** with encrypted storage uses a fixed Keychain entry: the adapter
  does not set `GEMINI_FORCE_ENCRYPTED_FILE_STORAGE`. Sandbox, ACP and
  completion still read `~/.gemini`.
- **Cursor** keeps `mcp.json`, chats and skills in `~/.cursor`, common to IDE,
  CLI and instances.
- **`~/.agents/skills`** is read by Codex, Cursor and Pi regardless of the
  profile: it is fine for common skills, not for a client's skills.

## Launching with a profile

`ai-profile client-d opencode` sets the OpenCode adapter's variables for client-d,
resolves the profile's secrets and launches the real `opencode` with the
arguments that follow `--`. Without a profile it uses the current folder's one
(below).

An explicitly given profile that does not match the folder's moves the client's
data like a `handoff`, and follows the same rule:
`ai-profile personal` inside `~/Developer/client-d` starts only if client-d lists
`personal` in `handoff_to`.

## Shims

`ai-profile` installs, for each enabled agent, an executable with the same name
(`claude`, `codex`, `opencode`) that comes before the real one in `PATH`, so the
right profile starts even when the agent's name is typed. At startup:

1. it goes up from the current folder and looks for the first one associated
   with a profile, in `~/.config/ai-profiles/config.toml`; from a worktree it
   first goes up to the main repo (`git rev-parse --git-common-dir`):

   ```toml
   default = "personal"

   [profiles.client-d]
   dirs = ["~/Developer/client-d", "~/Developer/client-e"]
   agents = ["claude", "codex", "opencode"]

   [profiles.client-b]
   dirs = ["~/Developer/client-b"]
   agents = ["claude"]
   ```

2. it sets the adapter's variables, resolves the secrets and launches the real
   agent with the same arguments;
3. if the agent's main variable (`CLAUDE_CONFIG_DIR`, `CODEX_HOME`,
   `OPENCODE_CONFIG_DIR`) is already set explicitly, it respects it;
4. an agent not listed in the profile's `agents` does not start, and the message
   says which profile applies and why.

This also works outside interactive terminals: Orca, Claudian, editors and any
program that launches the agent from `PATH`. The associations live in
a single file instead of in one `mise.toml` per client. `new`, `mcp add`,
`plugins` and `default` edit that file in place: a dotfiles manager that keeps
a copy of it must take the change back (for chezmoi, `chezmoi re-add`).
In tools that launch agents from a configurable list, such as Orca, the
shims are registered in place of the real agents.

## Secrets

`config.toml` contains only references to secrets, never the values. Reading
them is always the password manager's job, and holding them for a while is the
job of `ai-secrets`, a separate executable in the same repo: an in-memory agent,
like `ssh-agent`.

| Reference | Source | Where it applies |
|---|---|---|
| `op://<cassaforte>/<elemento>/<campo>` | 1Password, with the app's approval (Touch ID) | MCP servers and provider keys |
| `bw://<account>/<item>`, `bw://<item>` | Bitwarden, the account named in the reference or the profile's, unlocked by a master password kept in 1Password | MCP servers (with the account named) and provider keys |
| `env:<VARIABILE>` | environment or `~/.env` | MCP servers and provider keys |

Prefer the 1Password item id to the title
(`op://Private/Budget/credential`): it stays valid if the
item is renamed, and it is not ambiguous when two items have the same title.

### ai-secrets

| Command | What it does |
|---|---|
| `ai-secrets run [--reason <reason>] -- <command>` | replaces the `op://` and `bw://<account>/<item>` references in the environment with their values and launches the command |
| `ai-secrets status` | secrets in memory, with the variable names and the expiry, without values |
| `ai-secrets lock` | forgets all secrets |
| `ai-secrets stop` | stops the agent |

`ai-secrets` is available to agents too, under two rules:

- **always authorized**: every new or expired secret asks for its own
  approval, after a notification with the reason and the variables ("server
  budget, profile personal asks for BUDGET_API_KEY (valid for 30 min)"); a value
  approved once stays in memory for the ttl and is reused without asking;
- **never in clear**: no command prints a value, and error messages name
  references, never values; values reach only the environment of the command
  that `ai-secrets run` launches.

The agent starts by itself on the first request and answers on a socket in
`~/.cache/ai-secrets/`, a folder only the user can enter. How it reaches each
store is set in `~/.config/ai-secrets/config.toml`; the defaults are the safe
ones:

```toml
ttl = "30m"

[stores.1password]
read = "isolated"                 # or "shared"

[stores.bitwarden.client-e]
server = "https://vault.bitwarden.eu"
unlock = "password"               # or "biometric", when the bw CLI has it
password = "op://Private/Bitwarden/password"
lock_after_read = true
```

- **1Password, `read = "isolated"`**: every `op read` runs in a process session
  of its own (`setsid`). 1Password scopes an approval to the session that asked
  and its children, so each new secret gets its own approval. With `"shared"`
  the reads run in the agent's session, and one approval covers the reads of
  the following ten minutes.
- **Bitwarden parts**: `bw://<account>/<item>` reads the password;
  `#username`, `#notes`, `#totp`, `#field=<name>` (a custom field) and
  `#attachment=<file name>` (the attachment's content) read the other parts.
- **Bitwarden, `lock_after_read = true`**: every request is a full login. The
  master password is read from 1Password in isolation, used for `bw unlock` and
  dropped; then `bw get` for every value of the request (password, username and
  attachment asked together share the login), then `bw lock`. Bitwarden is
  never left unlocked between requests, and a `bw` run by anything else finds it locked. With
  `false` the account stays unlocked for the ttl (the session key only in the
  agent's memory) and is locked when it expires or on `lock`/`stop`.
- **Bitwarden accounts** live here, with their CLI data folder in
  `~/.config/ai-secrets/bitwarden/<account>` (the login is done once with
  `BITWARDENCLI_APPDATA_DIR=… bw login`). A reference names its account
  (`bw://client-e/<item>`), or takes the profile's (`bitwarden = "client-e"` in
  `ai-profile`). `unlock = "biometric"` is where a future Touch ID unlock of
  the bw CLI goes, without 1Password as a bridge.

Example, with the defaults: at 10:00 a session starts the budget server
(notification, approval, value in memory until 10:30); at 10:05 an agent opens
the VPN (notification, approval for the Bitwarden login, unlock, read,
lock); at 10:08 the same agent reads an SSH passphrase from Bitwarden
(Bitwarden is locked: another login and approval); at 10:12 another session
starts the budget server (from memory, no approval); at 10:40 budget has expired
and asks again.

### MCP servers

A server with `op://` references in its `env` values starts through
`ai-secrets`:

```toml
[mcp.budget]
command = "uv"
args = ["tool", "run", "mcp-budget"]
env.BUDGET_API_KEY = "op://Private/Budget/credential"
```

For each agent `sync` writes `~/.local/bin/ai-secrets run --reason "server
budget, profile personal" -- uv tool run mcp-budget`, with the reference in the
`env`. `ai-secrets` is given with its full path because apps opened from the
Dock have a reduced `PATH`. For Codex `sync` adds `startup_timeout_sec = 60`,
because the server may wait for the 1Password approval beyond the default 10
seconds. This way the server works however the agent is started: `ai-profile`,
`claude` directly, Claudian. An `env:` reference becomes `${VAR}` for Claude
Code, `env_vars` for Codex and `{env:VAR}` for OpenCode.

### Provider keys

The keys that the agent itself reads from the environment (Kimi, Mistral, OpenAI
for OpenCode) are in the profile; `ai-profile` asks `ai-secrets` for them at
launch and puts them in the agent's environment:

```toml
[profiles.client-d]
bitwarden = "client-e"
secrets.MOONSHOT_API_KEY = "op://Client-d/Kimi/credential"
secrets.MISTRAL_API_KEY = "bw://Mistral"
```

For `bw://` references the account comes from the reference or from the
profile's `bitwarden`, and `ai-secrets` reads it as its configuration says
(above). Provider keys apply to launches that go through `ai-profile` or the
shim.

References in the headers of remote servers are not supported yet.
`which` lists the profile's provider keys and their source; `doctor` checks that
`ai-secrets`, `op` and `bw` are in `PATH`, that every Bitwarden account has
logged in and that the `env:` variables exist, without reading 1Password or
Bitwarden.

## Private part

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
default = "acme"
files = ["~/Developer/acme/AGENTS.md"]   # other files of the archive

[profiles.acme]
dirs = ["~/Developer/acme"]
```

The private file holds profiles, `[mcp]` servers, `default` and `files`;
`ai-profile` merges it with `config.toml`, and each profile, server or
`default` is defined in one of the two files only. `new` writes a new profile
into the private file; `mcp add`, `plugins` and `default` edit the file that
defines what they change.

`backup` packs the private file and `files` into one gzipped tar and pipes it
to the `backup` command; `restore` runs the `restore` command and unpacks what
it prints, paths relative to home. When the private file is missing, as on a
new Mac, any `ai-profile` command restores it first. The commands are the
user's: `ai-profile` knows neither 1Password nor the dotfiles manager.
`doctor` reports the private files changed since the last backup or restore.

`ai-secrets` does the same with its own `[private]` section in
`~/.config/ai-secrets/config.toml`, its own archive and `ai-secrets backup` /
`ai-secrets restore`; the agent restores the private file when it starts
without it. Bitwarden accounts, `ttl` and `stores.1password.read` can sit in
either file, each in one only.

## Switching to other accounts and agents

aimux's features (usage of all subscriptions, choice of the one with the most
headroom, switching when tokens run out) apply here too, but within the folder's
constraints: a switch never carries a client's data to an account that the
folder does not allow.

A running session cannot change account: the process has already read the
profile folder and its login. The work can, however, continue elsewhere:

- **same agent, another profile** (Claude Code): `handoff personal` copies the
  session file (`projects/<cartella-codificata>/<id>.jsonl`) into the
  destination profile and launches `claude --resume <id>` with its
  `CLAUDE_CONFIG_DIR`;
- **another agent, same profile**: `handoff codex` builds a summary from the
  Claude Code session file (first request, last exchanges, modified files),
  without spending tokens, saves it in `handoffs/` in the profile folder and
  launches the other agent in the same folder with the summary as the first
  message;
- **automatic choice**: `ai-profile --auto` launches, among the agents and
  profiles allowed in the folder, the one with the most headroom according to
  `usage`.

**A switch moves the client's data.** Resuming a client-d session in the personal
profile sends the whole conversation (code, documents, responses from the
client's MCPs) to the personal account, which has different terms of use from
those of a Team plan. For this reason each profile declares which profiles it can
switch to, and the default is none; the allowed agents are those in `agents`:

```toml
[profiles.client-d]
agents = ["claude", "codex"]
handoff_to = []          # no switching to other accounts

[profiles.personal]
agents = ["claude", "codex", "opencode", "pi"]
handoff_to = ["client-d"]  # from personal to a client is allowed
```

A folder used for several clients lists its profiles instead, and admits only
those, whatever their `handoff_to`:

```toml
[[folders]]
dir = "~/Developer/shared"
profiles = ["personal", "client-d", "client-b"]
```

There `claude` starts with the profile last started in the folder, recorded in
`~/.local/state/ai-profile/last-used.json`, and the first listed when none is
recorded; `ai-profile <profilo>` starts another and becomes the last used. Each
profile keeps its own sessions of the folder: `handoff` carries the work from
one to the other. A folder is either in `[[folders]]` or in the `dirs` of one
profile.

`ai-profile folder add [<cartella>] <profilo>…` writes the entry, for the
current folder when none is given: in the file that already lists the folder,
else in the private file (`config.toml` without one). A folder in the `dirs` of
a profile moves out of them into the entry, in that profile's file, with that
profile first, so `claude` keeps starting with it. `folder rm` removes the
profiles given, or the whole entry without any, and `folder list` shows each
folder with its profiles and the last used.

`--auto` and `handoff` choose only among the allowed combinations: the folder's
profile and those in its `handoff_to` (or the profiles of its `[[folders]]`
entry), each with its own agents. In a client-d
folder, that means Claude Code and Codex with the client-d accounts. `handoff`
takes the last Claude Code session in the current folder, or the one given with
`--session <id>`.

**From inside a session.** A Claude Code session has no terminal of its own for
another interactive agent, so `ai-profile handoff` run there (`CLAUDECODE=1`, or
standard input not a terminal) takes the current session from
`CLAUDE_CODE_SESSION_ID` and starts the next agent in a new terminal: a terminal
of the same Orca worktree (`orca terminal create --worktree active`), a
Terminal.app window outside Orca, or the clipboard when neither is available.
Another agent receives a short first message that points to the saved summary.
Inside Codex, `CODEX_SESSION_ID` names the current session, read from its
rollout file in `CODEX_HOME`, and the handoff goes to another agent (Claude
Code with a summary); Codex asks to run the command outside its sandbox, since
it writes the summary and opens a terminal. Inside OpenCode,
`OPENCODE_SESSION_ID` names the session, exported with `opencode session
export` and saved next to the summary. When the agent's folder (`CODEX_HOME`,
`OPENCODE_CONFIG_DIR`) belongs to no profile, as when Orca sets its own, or is
not set, the folder's profile applies. The `handoff` skill
and the OpenCode `/handoff` command come with `ai-profile` and are installed by
`sync`: the skill in `~/.agents/skills`, where Codex reads it, with a link in
`~/.claude/skills` for every Claude Code profile; the command in
`~/.config/opencode/commands`, since OpenCode does not invoke skills as
commands. Installed files carry a marker comment: `sync` updates them, and
leaves alone a file with the same name that lacks it.
The copy of the session is a snapshot: what happens in the old session after
the handoff does not reach the new one, so the old one is closed with `/exit`.

The Claude Code statusline
receives `rate_limits` (5 hours and 7 days): above a threshold it suggests
switching to the allowed combination with the most headroom.

## Several Claude accounts in one profile

A profile can have several Claude accounts for the same work, for example two
subscriptions, and switch from one to another without closing sessions:

```toml
[profiles.personal]
account = "me@example.com"
claude_accounts = ["me@client-d.example"]
```

| Command | What it does |
|---|---|
| `account save` | saves the profile's active account, credentials and the data shown by Claude, in a Keychain entry of its own (`ai-profile-account:<profilo>:<email>`) |
| `account use <email>` | puts the active account's token back in its own entry, loads the other into the profile's entry and updates `oauthAccount` in `.claude.json`; a part of the email that only one saved account contains is enough (`account use client-d`) |
| `account list` | saved accounts, with the active one marked, and those in `claude_accounts` not saved yet |

Inside a session the `account` skill, installed by `sync`, makes this
`/account` (list) and `/account <email or part>` (switch).

Claude Code rereads the credentials from the Keychain while it works, so open
sessions move to the new account without a restart. To add an account:
`account save`, `/login` with the other account in a session of the profile,
then `account save` again. Each profile thus has its own login for each account:
the same token in two Keychain entries breaks at the first rotation, because
when a session renews it the copy in the other entry is no longer valid. For the
same reason `account use` always saves the current token before switching. The
entries are written with `security`, as Claude Code does: written by another
program they change the access rules, and every read by Claude Code would ask for
the Keychain password. The command works on macOS.

`doctor` accepts as linked both `account` and the accounts in
`claude_accounts`. Switching account does not move the session from one profile
to another: MCP servers, settings and transcripts stay with the profile, only the
account that answers changes, so it is meant for accounts of the same client.

## Statusline

`ai-profile` offers profile and account; the statusline is the user's. For
Claude Code, `sync` sets `statusLine` in the shared `~/.claude/settings.json`
to `ai-profile statusline`, which finds the profile from `CLAUDE_CONFIG_DIR` and
the active account from `oauthAccount`, then runs the user's command with the
input Claude Code sends and `AI_PROFILE` and `AI_PROFILE_ACCOUNT` in its
environment:

```toml
[statusline]
claude = "sh ~/.claude/statusline-command.sh"
```

Without a command it prints `@<profile> <account>`. A `statusLine` that runs
something else while `config.toml` names no command is left as it is, and
`sync` says how to keep it. Codex and OpenCode follow when their
statuslines allow it.

## Usage

`usage` shows, for each profile and each agent, how much is left, reading with
each profile's credentials without ever writing them:

| Agent | What it shows | Source |
|---|---|---|
| Claude Code | percentage used in the 5 hours, in the 7 days and in the weekly per-model limits, with the reset time and the severity given by Anthropic (in yellow and red); one row for each saved account of the profile, with the active one marked | `GET https://api.anthropic.com/api/oauth/usage` with the profile's OAuth token, read from its Keychain entry (`Claude Code-credentials-` and the first 8 characters of the SHA-256 of the profile folder; outside macOS `.credentials.json`) |
| Codex | percentage used in the 5-hour window and the weekly one, as of the last session | the `token_count` events of the profile's session files, which contain `rate_limits`: no network and no credentials |
| Gemini CLI | remaining quota and reset | Code Assist's `retrieveUserQuota` with the profile's token, as `/stats` does |
| Cursor | — | no public source; usage only in the dashboard |
| OpenCode | cost and tokens of the last days; provider balance | `opencode stats --cost --json` on the profile's database; Kimi: `GET /v1/users/me/balance` with the profile's key; Mistral: no API, console only |
| Pi | tokens and cost per session | the profile's session files |

Windows exist for subscription agents; with API keys you pay per token, and
`usage` shows cost and balance. The Claude, Codex and Gemini endpoints are not
documented as public APIs: aimux uses the same ones for Claude and Codex, and
`usage` reports when a source stops answering. The same data can let `handoff`
suggest a switch, and let Orca and Claude Usage show the profiles.

## MCP servers and credentials

The agent's login is one per profile, but MCP servers use other accounts
(Google, Notion, budget…), and each credential must be reachable only from the
profile, or the repo, of the client it belongs to. `ai-profile` does not know
the individual servers: in `config.toml` each server is defined once, with
placeholders in the values, and each profile or repo declares which servers it
uses and with which values:

```toml
[mcp.google-workspace]
command = "~/.local/bin/google-workspace-mcp"

[mcp.notion-client-d]
url = "https://mcp.notion.com/mcp"

[mcp.notion-client-e]
url = "https://mcp.notion.com/mcp"

[mcp.esempio]
command = "uvx"
args = ["esempio-mcp"]
env.ESEMPIO_TOKEN_DIR = "~/.esempio/{profilo}"
dirs = [{ path = "~/.esempio/{profilo}", only = ["{email}.json"] }]

[profiles.client-d]
account = "me@client-d.example"
mcp = ["google-workspace", "notion-client-d", "notion-client-e"]

[profiles.personal]
account = "me@example.com"
mcp = ["budget", "google-workspace"]

[[profiles.personal.repos]]
dir = "~/Developer/client-c/app"
plugins = ["posthog", "cloudflare"]
```

The placeholders are `{profilo}` and the keys of `vars`; a repo's `vars` are
added to those of its profile. For each declared server `sync`:

1. replaces the placeholders and creates the `dirs` folders with permissions
   `700`; `only` lists the files a folder may contain;
2. writes the server into the configuration of each agent of the profile, in the
   agent's format: for all projects if declared in the profile; if declared in a
   repo, in local scope for Claude Code, in the repo's `.codex/config.toml` for
   Codex and in the repo's `opencode.json` for OpenCode, where it replaces the
   profile's server with the same name. Codex reads a project's configuration
   only if the profile considers it trusted, so `sync` marks it
   `trust_level = "trusted"`, unless the profile declares it untrusted;
3. updates an existing server whose configuration no longer matches the
   declared one.

The files that `sync` writes inside a repo go into `.git/info/exclude`, which
applies only to that clone, if git does not already ignore them; a file that the
repo already tracks in git belongs to the team, and `sync` does not touch it and
reports it. Servers that `config.toml` does not declare stay where they are, and
`sync` reports them. `sync --dry-run` lists the changes without making them, and
`doctor` warns when there are pending ones.

`sync` does not touch tokens: each service's login starts on first use, or the
token files are moved by hand.

`only` exists because a server can add credentials by itself: a login approved
by mistake puts another client's account in the profile folder, and `doctor`
reports it.

A server shared by all profiles, like the Google one in the README, appears in
every profile's `mcp` list; `doctor` checks that it answers.

### Claude Code plugins

Installed plugins are shared; `enabledPlugins` in the shared `settings.json`
enables those used everywhere. A repo's `plugins` go into its
`.claude/settings.local.json`, and a repo's `disabled_plugins` turn off there
plugins enabled everywhere; `sync` writes those keys without touching the rest
of the file.

`mcp … add` and `plugins … enable|disable` edit `config.toml` line by line, so
comments and order are kept; a list written over several lines must be edited
by hand. Then they run `sync`.

### Credentials outside MCP

- **GitHub**: `GH_CONFIG_DIR` per profile (see *Open questions*).
- **claude.ai connectors** (Gmail, Drive, Calendar…): they live on the Claude
  account and are connected from claude.ai; `doctor` lists them with
  `claude mcp list` because the account they point to can only be checked by
  hand.

## Skills

A Claude Code profile sees skills in two ways:

- **shared**: `skills/` is a link to `~/.claude/skills`, and the profile sees
  all skills;
- **per profile**, after `skills split <profilo>`: `skills/` is a real folder
  with a link for each shared skill, except those in `skills_exclude`, and the
  profile's own skills as real folders. `sync` adds the links to new shared
  skills and removes those to excluded or vanished skills; it does not touch the
  real folders.

```toml
[profiles.client-d]
skills_exclude = ["budget"]
```

With the per-profile folder, what installers write into
`$CLAUDE_CONFIG_DIR/skills` stays in the profile, and `skills share <profilo>
<skill>` moves it among the shared ones; skills synced from claude.ai
(`skills/synced/`) also stay with the profile's account. `skills link <skill>`
links into `~/.claude/skills` a skill that another tool has put in
`~/.agents/skills`, and `doctor` reports those without a link. `skills list`
shows what each profile sees and where it comes from (`skills` CLI,
`~/.agents/skills`, profile). A `skills` link that a dotfiles manager creates
must be removed from it before `split`, or the manager puts the link back.

## Claude Desktop

Claude Desktop accepts only one account, but it is an Electron app: with
`--user-data-dir` each instance has its own login, chats, connectors and
extensions. A profile with `desktop = { hue = <gradi> }` in `config.toml` has:

- a **copy of Claude.app** in
  `~/Library/Application Support/ai-profile/desktop/Claude <Profilo>.app`,
  with the Claude icon tinted by `hue` degrees (a custom Finder icon, which
  the Dock shows too);
- a **data folder** `~/Library/Application Support/Claude-<Profilo>`;
- a **launcher** `~/Applications/Claude <Profilo>.app`, written by
  `ai-profile desktop install`: an AppleScript applet, signed ad hoc, that
  runs `ai-profile desktop <profilo>` through the mise shim with `do shell
  script`, waits for it and shows its error in a dialog. It is an applet
  because Spotlight lists only apps with a real executable; `install` rewrites
  it when the path of `ai-profile` changes.

`ai-profile desktop [<profilo>]` opens the instance of the profile, or of the
current folder's profile, in this order:

1. a copy open without `--user-data-dir` (started by Claude's own updater, a
   Dock icon of the open window or Spotlight) shows the default Claude's data
   and account: a dialog offers to quit it and reopen it on the profile;
2. a copy that is not open is realigned to `/Applications/Claude.app`, which
   keeps updating itself: when it is missing, older, or without its icon, a
   new APFS clone is tinted beside it and swapped in only once complete, and
   what an interrupted swap left behind (`.refreshing`, `.previous`) is
   cleared;
3. the copy starts with `open -a <copy> --env CLAUDE_CONFIG_DIR=~/.claude-<profilo>
   --args --user-data-dir=<data folder>`, so the Code tab inside Desktop also
   uses the profile; an open copy is brought to the front.

The icon goes on a copy that has never run because macOS keeps a launcher,
which lacks the App Management permission, from writing into an app that has
run; it can still move and delete it. Claude's own updater replaces the copy
in place and relaunches it without the profile: the next open from the
launcher notices both. A lock keeps two opens of the same profile from
rebuilding the copy at once.

`doctor` reports, and with `--fix` repairs, a missing launcher or one
`ai-profile` did not write (moved to the Trash), a missing copy, a copy
behind Claude.app, without its icon or with leftovers of a swap; it
reports a copy open without the profile, a launcher of a profile without
`desktop`, and the folder left by claude-multiprofile.

Graphical apps that launch an agent from `PATH` (editors, Orca, Claudian) go
through the shim, which reads the default from `config.toml`.
Claudian also reads sessions and credentials itself, inside Obsidian, from the
`CLAUDE_CONFIG_DIR` in its environment variables (Claudian settings, Claude):
without it, it looks in `~/.claude` and the conversations lose their history.
The variable is set by hand, and changing it detaches the conversations from
their sessions, so the profile changes on purpose. `doctor` reads the vaults
listed by Obsidian and checks that each Claudian passes the profile of its vault.
Claude.app opened from the Dock does not launch agents from `PATH`: its Code tab
uses `~/.claude`, and a profile needs the launcher described above.

macOS only: there is no Claude Desktop for Linux, and on Windows
`--user-data-dir` is to be checked.

## doctor

`doctor` checks that the configuration is still the intended one and says what
does not match; `doctor --fix` repairs what can be repaired safely, and every
repair can be undone.

| Check | Example problem |
|---|---|
| Account per profile | client-d is linked to me@example.com, in Claude Code or in Codex; two profiles on the same account. |
| Shared parts | A link broken or replaced by a real file (changes no longer reach the other profiles); an agent without a link to `~/.agents/AGENTS.md`; a `~/.claude/CLAUDE.md` that has reappeared, for example rewritten by Set Up Your Mac. |
| Secrets | A reference that does not resolve (missing item, locked vault); the same key in two profiles; a key written in clear text in a configuration instead of as a reference; a `bw` that runs on Node (the npm package), hence with the `node` of the folder it starts in. |
| MCP | A personal server (budget, personal Google) present in a client profile; non-empty `mcpServers` in `~/.claude.json`, hence visible to sessions without a profile. |
| Declared servers | A `config.toml` server missing from an agent's configuration or with a different configuration; a present server that `config.toml` does not declare. |
| Shared credentials | Two profiles with a server pointing to the same folder or the same environment variable value; a `dirs` folder that is missing, with permissions wider than `700` or with files that `only` does not allow (a token of another account). |
| Plugins | A plugin in `plugins` enabled in the shared `settings.json` instead of only in the repo; a declared repo without the keys in `settings.local.json`. |
| claude.ai connectors | List of the Google connectors connected per profile, to be checked by hand. |
| Shared state | OpenCode service started by one profile and reachable from the others; Codex with MCP OAuth tokens in the Keychain; Gemini CLI with encrypted storage in the Keychain; a client's skills in `~/.agents/skills`. |
| Folders | A client folder with no associated profile; a folder associated with two profiles; a `[[folders]]` entry with one profile; a `mise.toml`, `mise.local.toml` or `.envrc` in a configured folder or one of its projects that sets `CLAUDE_CONFIG_DIR`, `CODEX_HOME` or another agent's folder, overriding the profile. |
| Agents in `PATH` | A shim that does not come before the real agent; two installations of the same agent. |
| Desktop | Copy started without `--user-data-dir` (icon pinned to the Dock from the open window, or relaunched by Claude's updater); copy behind in version or without its icon; leftovers of an interrupted update; launcher missing or not written by ai-profile. |
| Reading across profiles | Rules that keep a profile from reading the other profiles' folders are missing or incomplete. |
| Orphaned logins | Keychain entries of removed or renamed profiles (only reported, never deleted). |

### How it detects the wrong account

Each profile declares the expected account in `config.toml`
(`account = "me@client-d.example"`). `doctor` compares it with what the apps have
recorded:

- **Claude Code**: the profile's `.claude.json`, field `oauthAccount`
  (`emailAddress`, `accountUuid`, `organizationName`, `organizationUuid`).
- **Codex**: the profile's `auth.json`, field `email` of the `id_token` (a JWT,
  read without printing it), or `codex login status`.
- **Gemini CLI**: the profile's `google_accounts.json`, field `active`.
- **Claude Desktop**: `config.json` in the instance's data folder, field
  `lastKnownAccountUuid`. It is the same `accountUuid` as Claude Code, so the
  email is found by comparing it with the Code profiles.
- **Desktop copy started without a data folder**: `ps` lists the processes of
  the Claude.app copies; one without `--user-data-dir` runs on the default data
  and account.
- **Wrongly pinned icon**: `defaults read com.apple.dock persistent-apps` tells
  whether the Dock has the Claude.app copy pinned (which starts without a data
  folder) instead of the launcher.

## Platforms

| | macOS | Linux | Windows |
|---|---|---|---|
| Claude Code login | Keychain, one entry per profile folder | `.credentials.json` in the profile | to be checked |
| Codex and OpenCode login | files and database in the profile folder | as macOS | as macOS |
| Shared parts | symlinks | symlinks | junctions for folders, hardlinks or copies for files |
| Shims | scripts in `PATH` | scripts in `PATH` | `.cmd` in `PATH` |

The language is Go or Rust, to have a single executable per platform.

## Open questions

- **Reading across profiles.** Isolating logins and MCP does not prevent a client-d
  session from reading `~/.claude-personal/projects/` or `history.jsonl` with
  Read, Grep or Glob, and the same holds for the Codex and OpenCode folders.
  claude-multiprofile writes `permissions.deny` rules in each `settings.json`
  toward the other profiles' folders; with a shared `settings.json` this is not
  possible, because the rules would be the same for everyone. Possible routes: a
  `settings.json` per profile generated from a common base, or shared rules that
  deny the sessions and history of all profiles.
- **Cursor MCP and login.** `~/.cursor/mcp.json` and the CLI login are unique
  per macOS user: with Cursor in several profiles, one client's MCP servers are
  visible to the others. Possible routes: Cursor for one profile only, only MCP
  servers without credentials in `~/.cursor/mcp.json`, or `.cursor/mcp.json` in
  the client's repos, ignored by git.
- **Common instructions in Cursor.** User rules are set only in the interface:
  `~/.agents/AGENTS.md` reaches Cursor only as a project rule.
- **Plugins.** Claude Code wants a marketplace's install path to be inside the
  profile folder. With `plugins/` as a symlink to `~/.claude/plugins` the
  already installed plugins load, but installing or updating a plugin from a
  profile is to be checked. aimux uses a real `plugins/` per profile, with
  symlinks to `marketplaces/` and `cache/` and copies of
  `known_marketplaces.json` and `installed_plugins.json` with rewritten paths.
- **Skills per agent.** The `skills` CLI installs Claude Code skills in the
  profile and those of the other agents in `~/.agents/skills`. To be
  decided: the common folder, and whether OpenCode also loads `.claude/skills`
  (`OPENCODE_DISABLE_CLAUDE_CODE*`), with duplicates.
- **GitHub login per profile.** With a single `gh` login, a client-d session also
  reaches the personal repos and those of other clients. `new --gh` sets
  `GH_CONFIG_DIR=~/.claude-<profilo>/gh` (like claude-multiprofile) to use in the
  profile a fine-grained token limited to the client's organization; `doctor`
  reports a client profile's token that sees other clients' organizations. It
  concerns only `gh` (PRs, issues, API): `git` over SSH uses the 1Password key,
  the same for all profiles.
- **Orca.** Orca's worktrees are in the workspace folder, set to `.worktrees`
  (relative to the repo), so inside the associated folders; with an external
  folder, going up to the main repo still brings them back to the client's
  profile. In its terminals Orca wraps `claude` in a shell function that
  respects an already set `CLAUDE_CONFIG_DIR` and looks for the executable in
  `PATH`, so a `claude` typed by hand also goes through the shim.
  To be checked: which folder Orca reads usage and limits from, and whether its
  status line hook replaces the one that shows the profile.

## Discarded directions

- **Adopting claude-multiprofile for everything**: it does not share skills and
  instructions, does not associate folders with profiles, does not work on
  Windows and skips read protection when `settings.json` is shared. It remains
  the source of the per-profile Claude Desktop instances that `ai-profile
  desktop` now manages (see `existing-tools.md`).
- **Blocking Claude's updater in the copies**: Claude Desktop's
  `disableAutoUpdates` policy is read from the MDM profile of the bundle
  identifier the copies share with `/Applications/Claude.app`, which must keep
  updating; the copy is realigned at every open instead.
- **A launcher that starts the update in the background**: claude-multiprofile's
  launcher did, and the update was cut short when the launcher exited, leaving
  copies half swapped and the app unopened.
- **Adopting aimux**: it manages Claude Code, Codex and Gemini CLI and the usage
  of all subscriptions, but it is designed to spread work across subscriptions,
  not to separate clients: by default it shares transcripts and memory and does
  not associate folders with profiles. Its features (`usage`, `--auto`, `handoff`) are taken up here within the folder's constraints.
- **Changing account inside the running session**: no agent allows it.
- **LaunchAgent with `launchctl setenv CLAUDE_CONFIG_DIR`** for the default in
  graphical apps: every app opened from the Dock would inherit the variable, and
  the shim, which respects an already set variable, would no
  longer pick the profile from the folder in Claudian, Orca and editors.
- **MCP server secrets read by `ai-profile` at launch** and passed to the agent
  as environment variables: they apply only to launches that go through
  `ai-profile`, and a `claude` launched directly or from Claudian starts the
  server without a key.
- **`op run` around each MCP server**: it works however the agent is started,
  but for 1Password every process without a terminal is a separate request, and
  with many Claude sessions open it asks for dozens of approvals. `ai-secrets`
  asks for one per period.
- **Bitwarden Secrets Manager (`bws run`)**: it is a separate product to be
  enabled in the organization and requires an access token, itself a secret to
  be kept elsewhere; with the master password in 1Password the Password Manager
  is enough.
- **Summary requested from the current agent** for `handoff` to another agent:
  the switch is needed mostly when tokens have run out, and then the agent
  cannot answer.
- **Commands dedicated to workspace-mcp** (`google <profilo> <email>`, moving
  the tokens): they tie the program to one server and to the format of its
  folders, to save a step that happens once per account. Definitions with
  placeholders cover Google like any other server.
- **Only `OPENCODE_DB` per profile**: a separate database, but logs, prompt
  history and the background service registration stay shared between profiles.
- **API keys in the agents' configurations**: they would end up on disk in clear
  text, in files that the agents themselves can read and rewrite.
