---
name: openspec-verify-change
description: Adversarially verify a change before archiving using parallel review agents. Use when the user wants a change reviewed, verified, or checked for correctness, grounding, and completeness before implementation or archive.
license: MIT
compatibility: Requires openspec CLI (>= {{.Version}})
allowed-tools: Bash(openspec:*), Task
metadata:
  author: openspec-go
  version: "{{.Version}}"
  generatedBy: openspec-go
---

Adversarially verify a change's artifacts before implementation or archive.

**Input**: Optionally specify a change name. If omitted, check if it can be inferred from conversation context; if ambiguous, run `openspec list --json` and prompt for selection.

**When to use**: after `/opsx:propose` (verify the plan before implementing) or before `/opsx:archive` (verify the change survives scrutiny). The point is independent, skeptical review — not a re-read by the author.

**Steps**

1. **Gather the evidence**
   - Run `openspec status --change "<name>" --json` for artifact paths.
   - Run `openspec validate "<name>"` and keep every warning — especially `knowledge-coverage` warnings (a mapped topic looks related but its docs were never cited).
   - Run `openspec instructions research --change "<name>" --json` (any artifact works) and note the `knowledge` array: topics, notes, paths.

2. **Spawn parallel review subagents** (Task tool). Each reviewer is READ-ONLY, receives the change directory path and the knowledge map, and must return a list of findings — each with a severity (critical / major / minor), a one-line claim, and the file path(s) that prove it. Instruct each reviewer to actively try to REFUTE the change, not to summarize it.

   - **Grounding reviewer**: For each factual claim in proposal.md, design.md, and research.md (file paths, behavior descriptions, "X works like Y"), verify it against the actual code. Report claims that are contradicted by source, and claims that cite nothing and cannot be verified.
   - **Coverage reviewer**: For each knowledge-map topic, decide independently whether it plausibly relates to this change (match by meaning, not wording — domain terms and code terms differ). For every related topic, check research.md cites at least one of its paths with a takeaway consistent with the doc's actual content. Read the mapped docs to confirm — a citation that misstates the doc is a finding.
   - **Spec quality reviewer**: For each delta in specs/: MODIFIED blocks must carry every scenario of the current base requirement in openspec/specs/ (missing ones get silently dropped concerns flagged at archive); scenarios must be concrete WHEN/THEN, testable, not restatements; requirement text must use SHALL/MUST; header names must match the base spec exactly.
   - **Blast radius reviewer**: Search the codebase for consumers of the behaviors this change modifies. Report affected code paths, specs, or requirements that the proposal's Impact section does not mention.

   For a small change (single capability, few files), one combined reviewer covering all four lenses is fine — do not spawn four agents to review a 20-line delta.

3. **Consolidate**
   - Deduplicate findings; drop anything a reviewer could not anchor to a file path.
   - Rank: critical (plan is wrong or loses data), major (gap that will surface during implementation), minor (quality).

4. **Present and route**
   - Present the ranked findings with their evidence.
   - For findings the user accepts, fix the artifacts (follow the update-change workflow), then re-run `openspec validate "<name>"`.
   - If delta specs changed while fixing, run `openspec validate "<name>" --refresh-fingerprints` afterward so archive does not report false conflicts.

5. **Gate**
   - Recommend proceeding (implement or archive) only when there are no unresolved critical findings and `openspec validate "<name>"` passes.
   - Never archive as part of this workflow — verification and archiving are separate decisions.

**Guardrails**
- Reviewers are read-only; all fixes happen in the main conversation with the user's knowledge.
- Every finding cites file paths. A finding without evidence is not a finding.
- Do not soften results: if the plan skipped a critical path, say so plainly and name the docs that prove it.
- Do not re-verify what the CLI already guarantees (fingerprints, dependency gates); focus review effort on what only reading code and docs can catch.

**Knowledge map**: When an `openspec instructions ... --json` payload includes a `knowledge` array, read the files under every topic that plausibly relates to this change BEFORE writing anything. Domain terms and code terms often differ (a topic named for the business concept may be implemented under a different name) — do not skip a topic because its wording doesn't match the request.
