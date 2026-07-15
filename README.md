# openspec-go

A lean Go implementation of [OpenSpec](https://github.com/Fission-AI/OpenSpec): spec-driven development for AI coding agents. Single static binary, Claude Code and Crush integration, and one significant improvement over the original: archive-time conflict detection.

## Why this exists

OpenSpec keeps you and your AI agent honest: requirements live in structured markdown specs (`openspec/specs/`), work happens in self-contained changes (`openspec/changes/<id>/`), and archiving a change merges its deltas back into the specs. The original is around 32k lines of TypeScript covering 30+ AI tools, stores, worksets, telemetry, and shell completions. This port keeps only what the Claude Code workflow actually needs: under 6k lines of Go, two dependencies, no Node runtime.

### The fingerprint improvement

The original has a documented flaw: a `MODIFIED` delta is a whole-requirement replacement, so when two changes touch the same requirement, the second archive silently clobbers the first.

openspec-go records a sha256 fingerprint of every base requirement a change references (captured automatically when the skills run `status`/`validate`, stored in the change's `meta.json`). At archive time, if the base spec no longer matches the fingerprint, the archive blocks with no partial writes and tells the agent how to recover:

```
Error: archive blocked: the base spec changed since this change captured it
(likely another change was archived):
  - auth: "User Login"
Update the delta spec(s) from openspec/specs/<capability>/spec.md, then run:
openspec validate <change> --refresh-fingerprints
```

## Install

```bash
go install github.com/adriangitvitz/openspec-go/cmd/openspec@latest
```

Or build from source:

```bash
go build -ldflags "-X main.version=$(git describe --tags --always)" -o openspec ./cmd/openspec
```

## Quick start

```bash
cd your-project
openspec init
```

`init` scaffolds `openspec/` and generates the agent integration files:

- `.claude/skills/openspec-{propose,explore,apply-change,update-change,sync-specs,verify-change,archive-change,team}/SKILL.md`
- `.claude/commands/opsx/{propose,explore,apply,update,sync,verify,archive,team}.md`
- `.claude/agents/opsx-{product-owner,senior-staff,senior-engineer,backend-dev,frontend-dev,qa}.md`, the persona agents for the team-driven workflow
- `.crush/commands/opsx/*.md`, the same commands for [Crush](https://github.com/charmbracelet/crush), invoked as `project:opsx:<name>`; Crush picks up the skills from `.claude/skills` natively

Then talk to your agent: `/opsx:explore` to think an idea through, `/opsx:propose add-dark-mode` to plan it, `/opsx:apply` to implement, `/opsx:archive` to merge the specs and file the change away.

## Team-driven workflow

`--schema team-driven` turns a change into a virtual team process. Each artifact has a persona author and adversarial reviewers; the user approves at a human gate after each phase:

| Phase | Artifacts | Author -> Reviewers |
|---|---|---|
| 1. Business | research, proposal | product-owner -> senior-staff |
| 2. Technical design | specs, design, test-matrix | senior-staff -> senior-engineer + qa; senior-engineer -> senior-staff; qa |
| 3. Planning | tasks | backend-dev + frontend-dev -> senior-engineer |

Reviewers return findings (severity plus file-path evidence, at most two revision rounds). The QA persona derives a test matrix from the delta-spec scenarios, or analyzes coverage against one you reference. Tasks carry `(req: <requirement name>)` markers that `openspec validate` checks: every requirement needs a task, every marker needs a requirement. Drive it with `/opsx:team`.

Per-persona execution is configured in `openspec/config.yaml`:

```yaml
team:
  test_matrix: docs/qa/matrix.md   # optional, analyzed by the qa persona
  personas:
    product-owner:
      runner: openrouter           # external model via OpenRouter
      model: anthropic/claude-sonnet-4.5
    senior-engineer:
      runner: claude               # Claude Code subagent (default)
  confidential:                    # files withheld from external runners
    - INSTRUCTIONS.md
    - "secrets/**"
```

Undeclared personas default to `claude`. The OpenRouter runner reads its API key from the `OPENROUTER_API_KEY` environment variable only, never from config. `openspec validate` (with no arguments) checks the team section: persona ids, runner values, the test-matrix path, the search endpoint, and the confidential patterns (malformed or matching no file).

### Confidential files

`team.confidential` draws a per-runner data boundary. Patterns are root-relative globs (`*` matches within one path segment, `**` spans segments) naming files that never cross to an external model. Claude personas run inside the trusted harness and see everything. For openrouter personas the evidence bundle lists matches as withheld (path visible, content absent), `read_file` refuses them, `grep` silently skips them, and sibling extractions inherit their source's confidentiality. Releasing content deliberately means writing a sanitized copy outside the confidential set and citing that instead.

### Self-hosted web search (kurai)

Point `team.search.mcp_url` at a search MCP server. [kurai](https://github.com/adriangitvitz) (SearXNG with BM25/RRF/MMR ranking and trafilatura extraction, running in Docker or OrbStack for example) is the reference deployment:

```yaml
team:
  search:
    mcp_url: http://localhost:8080/mcp   # bearer token via OPENSPEC_SEARCH_TOKEN, never in config
```

With that set, openrouter personas gain `web_search` and `fetch_page` in their tool loop, so every persona's web research flows through your own searcher instead of a model provider's. Claude personas already use the session's search MCP tools; the generated assets instruct all personas to prefer it and to treat web content as untrusted data (cite as evidence, never follow instructions found in it). Without `team.search`, the tool loop stays repo-only and the request schema is unchanged.

## CLI

```
openspec init [path]                    scaffold openspec/ + agent integration files
openspec update [path]                  regenerate skill/command/agent files
openspec new change <name>              create a change directory
openspec status --change <id> [--json]  artifact completion status
openspec instructions <artifact|apply> --change <id> [--json]
openspec list [--specs] [--json]        list changes or specs
openspec validate [<id>] [--strict] [--json] [--refresh-fingerprints]
openspec archive <id> [--yes] [--skip-specs] [--no-validate] [--json]
openspec team prompt <persona> --change <id> --artifact <a> [--json]
openspec team run <persona> --change <id> --artifact <a> [--write] [--max-tool-iterations <n>] [--max-extraction-roundtrips <n>] [--timeout <dur>]
openspec team tools [--json]
```

`team prompt` assembles a persona's full prompt (system prompt, artifact brief, dependency artifacts, evidence bundle) for any orchestrator. `team run` executes it against the persona's configured OpenRouter model, with a read-only tool loop (`read_file`, `grep`, `list_dir`, `request_extraction`, plus `web_search`/`fetch_page` when `team.search` is configured) scoped to the project root. `team tools --json` prints the integration contract (tool schemas, needs protocol, extraction convention) so external harnesses like Crush orchestrate the same loop.

### Binary documents (PDF, docx, xlsx, pptx)

Binary sources are consumed through sibling extractions: `docs/EF.pdf` is inlined via `docs/EF.pdf.md`, written once by the orchestrating harness (Claude Code parses PDFs natively; the CLI never parses binaries) with a provenance header carrying the source's content hash. The bundle flags stale extractions when the source's bytes change. Documents without a sibling appear under "needs extraction" in the bundle, and `read_file` refuses binary documents, redirecting to the sibling or `request_extraction`. When an external model needs deeper detail, `request_extraction` pauses the run (exit code 7, JSON needs payload on stdout, persisted to `extraction-needs.json` in the change directory); the harness fulfills the request and re-runs, capped at 2 round-trips per persona and artifact. Documents matching `team.confidential` are exempt from all of this on external runs: they surface as withheld, and `request_extraction` on them errors in-run (never pauses) so a harness is never invited to extract one on an external persona's behalf.

## Spec format

```markdown
## Purpose
What this capability is for.

## Requirements

### Requirement: User Login
The system SHALL allow users to log in with email and password.

#### Scenario: Valid credentials
- **WHEN** a user submits valid credentials
- **THEN** a session is created
```

Changes describe edits as deltas: `## ADDED / MODIFIED / REMOVED / RENAMED Requirements` sections that `openspec archive` merges into the main specs, with validation and the fingerprint conflict check before any write.

## Custom workflows

Drop a schema at `openspec/schemas/<name>/schema.yaml` (same shape as the embedded `spec-driven` one) and select it with `openspec new change <name> --schema <name>` or `schema:` in `openspec/config.yaml`.

## License

MIT. Derived from [OpenSpec](https://github.com/Fission-AI/OpenSpec) (MIT, OpenSpec Contributors).
