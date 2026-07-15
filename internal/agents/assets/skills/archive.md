---
name: openspec-archive-change
description: Archive a completed change. Use when the user wants to finalize and archive a change after implementation is complete.
license: MIT
compatibility: Requires openspec CLI (>= {{.Version}})
allowed-tools: Bash(openspec:*)
metadata:
  author: openspec-team
  version: "{{.Version}}"
  generatedBy: openspec-team
---

Archive a completed change.

**Input**: Optionally specify a change name. If omitted, check if it can be inferred from conversation context. If vague or ambiguous you MUST prompt for available changes.

**Steps**

1. **If no change name provided, prompt for selection**

   Run `openspec list --json` to get available changes. Use the **AskUserQuestion tool** to let the user select.

   Show only active changes (not already archived).
   Include the schema used for each change if available.

   **IMPORTANT**: Do NOT guess or auto-select a change. Always let the user choose.

2. **Check artifact completion status**

   Run `openspec status --change "<name>" --json` to check artifact completion.

   Parse the JSON to understand:
   - `schemaName`: The workflow being used
   - `changeRoot` and `artifactPaths`: where the change and its files live
   - `artifacts`: List of artifacts with their status (`done` or other)
   - `isComplete`: Whether all artifacts are complete

   **If any artifacts are not `done`:**
   - Display warning listing incomplete artifacts
   - Use **AskUserQuestion tool** to confirm user wants to proceed
   - Proceed if user confirms

3. **Check task completion status**

   Run:
   ```bash
   openspec instructions apply --change "<name>" --json
   ```
   Check the result:
   - `state: "all_done"` → all tasks complete, proceed
   - Otherwise, `progress.remaining` and the `tasks` array show what's unfinished (you can also read the tasks file and count `- [ ]` vs `- [x]`)

   **If incomplete tasks found:**
   - Display warning showing count of incomplete tasks
   - Use **AskUserQuestion tool** to confirm user wants to proceed
   - Proceed if user confirms

   **If no tasks file exists:** Proceed without task-related warning.

4. **Perform the archive**

   ```bash
   openspec archive "<name>" --yes
   ```
   Add `--json` when you want a machine-readable result to parse.

   The CLI handles the whole operation in one step:
   - Validates the change (skip with `--no-validate` - not recommended)
   - Applies the change's delta specs to the main specs under `openspec/specs/`, using per-requirement fingerprint conflict detection to catch base specs that changed since the delta was written
   - Moves the change directory to `openspec/changes/archive/YYYY-MM-DD-<name>/`

   Use `--skip-specs` only when main specs were already updated another way and you explicitly do not want the CLI to touch them.

   Do NOT move directories manually (`mkdir -p`/`mv`) - always archive through the CLI.

5. **Handle conflicts (archive blocked)**

   If the archive fails with a conflict error ("archive blocked"), a main spec changed since the change's delta was written. Reconcile, then retry:

   a. Read the current `openspec/specs/<capability>/spec.md` for each conflicting capability
   b. Update the change's delta spec to incorporate the new base content (re-express the delta against what the main spec says now)
   c. Refresh fingerprints:
      ```bash
      openspec validate "<name>" --refresh-fingerprints
      ```
   d. Retry the archive:
      ```bash
      openspec archive "<name>" --yes
      ```

6. **Display summary**

   Show archive completion summary including:
   - Change name
   - Schema that was used
   - Archive location
   - Whether delta specs were applied to main specs (or skipped / none existed)
   - Note about any warnings (incomplete artifacts/tasks)

**Output On Success**

```
## Archive Complete

**Change:** <change-name>
**Schema:** <schema-name>
**Archived to:** openspec/changes/archive/YYYY-MM-DD-<name>/
**Specs:** ✓ Deltas applied to main specs (or "No delta specs" or "Skipped (--skip-specs)")

All artifacts complete. All tasks complete.
```

**Output On Conflict**

```
## Archive Blocked

**Change:** <change-name>
**Conflict:** <capability> - main spec changed since the delta was written

Reconciling:
1. Read openspec/specs/<capability>/spec.md
2. Updated the change's delta spec against the new base
3. openspec validate "<name>" --refresh-fingerprints
4. Retrying archive...
```

**Guardrails**
- Always prompt for change selection if not provided
- Use artifact graph (openspec status --json) for completion checking
- Don't block archive on warnings - just inform and confirm
- Let the CLI move the change directory; never `mkdir`/`mv` it yourself (`.openspec.yaml` moves with the directory)
- Show clear summary of what happened
- On a conflict, reconcile the delta spec and refresh fingerprints - never force through with `--no-validate`
