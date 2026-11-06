---
name: handoff
description: Hand the current session over to another Claude account/profile or to another agent (Codex, OpenCode) with ai-profile. Use when the user types /handoff <profile|agent>, or asks to continue this work in another profile, on another Claude account, or with Codex/OpenCode. Not for Orca worktree handoffs between Orca agents, which the orca-cli skill covers.
---

<!-- Installed by ai-profile sync from the ai-profiles repository; changes here are overwritten. -->

# Handoff

`ai-profile handoff` continues the current work elsewhere and opens the next
agent in a new terminal (an Orca terminal in the same worktree, a Terminal.app
window, or the clipboard as a last resort).

## Steps

1. Take the target from the arguments: a profile (e.g. `client-d`) or an agent
   (`codex`, `opencode`). If none was given, run `ai-profile which` and ask the
   user which profile or agent to hand off to.
2. Run, from the session's working folder (in Codex, approve running it
   outside the sandbox: it writes the summary and opens a terminal):

   ```sh
   ai-profile handoff <target>
   ```

   - **Profile** (from Claude Code only): the current session file is copied
     into that profile and the new terminal runs `claude --resume <id>` with it. The folder's
     `handoff_to` must allow the profile; if `ai-profile` refuses, report its
     message and stop: the handoff would move this client's data to an account
     the folder does not admit.
   - **Agent:** a summary of this session (first request, latest exchanges,
     files changed) is saved under the profile's `handoffs/` folder and the
     new agent starts with it.
3. Report where the next agent started, as `ai-profile` prints it.
4. Tell the user to close this session (`/exit` in Claude Code, `/quit` in
   Codex), so two agents do not work on the same files. Do not keep working in this session after the
   handoff: what happens here no longer reaches the copy.
