**Comment shape**

- Write every review comment in Simplified Technical English.

- Use the active voice. Name the actor. Keep one idea in one sentence. Do not use an internal type or field name as a noun. Use a real value when you can. Do not use = as a verb. Do not coin a term. Do not use em dashes.

- Write each finding in this shape:

  **Issue**
  What fails, in one or two sentences.

  **Context**
  What the user or operator does, and the state that leads to the failure.

  **Proposed Fix**
  What to change, in one sentence.

**Review instructions**

**Helpers**

- Before reviewing a new instruction or handler alone, read the nearest sibling. Flag a silent split in authorization, status transitions, counters, or account checks. Flag duplicated logic that a sibling already has as a helper.

- When the change adds a new helper or instruction path, read the nearest sibling. Check that the new path does not rebuild work a shared helper already does.

- When the change adds a method that only calls a shared helper and returns, flag the method. Callers can call the shared helper.

- When every caller of a shared helper repeats the same extra check, flag the copies. Put the check in the helper.

**Tests**

- In tests, assert the specific error and the exact log line at the expected index. Do not use `.is_err()` or `.contains()` to match a substring. Those keep passing after the failure moves or the message changes.

- When a one-shot test adds a helper to `tests/common`, flag the helper. Keep it in that test file.

**Onchain**

For a repository with onchain code:

- When an instruction takes a key, an epoch, or an id as an argument, and a passed account already holds that value or uses it as a seed, flag the argument. Read the value from the account.

- When the change skips, dedupes, or no-ops a duplicate, a zero, or the same account passed twice, flag that path. The instruction must revert.

- When two arguments must both be set or both be empty, flag a path that accepts one set and one empty. The instruction must revert.

- When a later instruction decides earlier work is done by reading a side effect, such as a balance or whether an account exists, flag that read. The earlier instruction must set a done flag on its own account.

- When the change ties two accounts that must share an epoch, a parent, or a seed, check that the instruction proves the link.

- When two accounts can share the same owner, flag a close or withdraw that only checks the owner. The instruction must also check the seeds of the account it means to touch.

- When a shared account-walk helper already verifies the owner, flag a second owner check in the handler. The helper already rejected a wrong owner.

- When the change puts a method on a shared type and only one instruction calls it, flag the method. Put the logic in that instruction.

- When a test writes account bytes or funds an account by hand to reach the start state, flag the setup. The test must call the real earlier instruction.
