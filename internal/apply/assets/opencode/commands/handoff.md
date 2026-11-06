---
description: Hand this session over to another agent (claude, codex) with ai-profile
---

<!-- Installed by ai-profile sync from the ai-profiles repository; changes here are overwritten. -->

Run this command with the shell tool, from the current working folder, and
show its output as it is:

```sh
ai-profile handoff $ARGUMENTS
```

If no target was given, run `ai-profile which` instead and ask which agent to
hand off to. After the handoff, tell the user that the next agent started where
`ai-profile` says, and that this session should be closed so two agents do not
work on the same files. Do not keep working in this session.
