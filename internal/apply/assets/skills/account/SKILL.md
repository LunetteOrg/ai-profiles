---
name: account
description: Show or switch the Claude account of the current ai-profile profile (for example from a subscription that ran out of credits to another one). Use when the user types /account, /account <email or part of it>, or asks which Claude account is in use or to switch Claude account.
---

<!-- Installed by ai-profile sync from the ai-profiles repository; changes here are overwritten. -->

# Account

The Claude accounts of a profile are saved by `ai-profile account save`;
`ai-profile account use` switches between them for the whole profile: every
open session of the profile, this one included, moves to the other account
when it next reads its credentials.

## Steps

1. No argument: run `ai-profile account list` and show its output (the active
   account is marked with `*`). Also run `ai-profile usage` if the user wants to
   know which account has more headroom.
2. With an argument (an email, or a part of it such as `example` or
   `client-d`): run

   ```sh
   ai-profile account use <argument>
   ```

   and report its output. If it says the argument is ambiguous or matches no
   saved account, show the message and the saved accounts; do not guess.
3. An account that is not saved yet is added by the user: `ai-profile account
   save`, then `/login` with the other account in a session of the profile,
   then `ai-profile account save` again.
