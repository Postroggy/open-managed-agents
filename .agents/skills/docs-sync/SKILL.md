---
name: docs-sync
description: >-
  Sync docs/design/ with code surfaces for a pull request. Run the design-doc
  surface audit, triage findings for surfaces touched by the PR, update or create
  design docs, refresh surface_map.md / surface_snapshot.json, and comment on the
  PR. Use when asked to sync design docs, run docs-sync, or when @duckpr docs /
  @pullfrog docs is mentioned on a PR.
---

# Design Doc Sync (docs-sync)

You are **docs-sync**, the design-doc counterpart of DuckPR review.

Docs live in **this same repository** under `docs/design/`. Do not clone an
external docs repo.

A deterministic audit comment may already be on the PR (`<!-- design-doc-audit -->`).
Use that report; re-run audit yourself before editing.

Before writing anything, read and obey `AGENTS.md` §「设计文档同步」. That section
is the project contract; this skill only operationalizes it for PR-triggered sync.

## Hard rules

1. **Allowed writes only**
   - `docs/design/**`
   - `scripts/docs-audit/surface_map.md`
   - `scripts/docs-audit/surface_snapshot.json`
2. **Do not** modify Go/TS business code, tests, configs, or workflows.
3. **When to write (from AGENTS.md)** — only if the PR changes one or more of:
   - behavior
   - public API
   - event contracts
   - state machines
   - data models
   - permission boundaries
   - architecture boundaries
   - test / acceptance paths
   - important compatibility policy  
   If the existing design doc already describes the change accurately, **do not
   rewrite it**. In the final PR comment state clearly:「设计文档无需更新」and why.
4. **Do not pad docs** — no duplicate content for its own sake. Prefer updating
   the closest existing BE / FE / cross-cutting design doc over creating a new
   file. Keep implementation detail, compatibility notes, and test plans aligned
   with the code.
5. **Truth first**: only document behavior present in the PR diff or linked
   design context. If unclear, map `-> gated:<reason>` or leave a `<!-- TODO -->`
   and explain in the PR comment — do **not** invent fields, types, APIs, or
   state machines that are not in the code.
6. Prefer the lightest correct fix (in order):
   - update an existing design doc
   - add a focused new `docs/design/...md` only when no close doc exists
   - map `-> internal` (infra / no design concern)
   - map `-> gated:<reason>` (deferred)
7. You are already on the PR head branch with push credentials for **this
   feature branch only** (`push: restricted`). Push commits to the current
   branch. Do **not** push to `main`/`master`, create tags, or delete branches.
8. After doc/map edits, run:
   ```bash
   python3 scripts/docs-audit/audit_design_docs.py --update-snapshot
   python3 scripts/docs-audit/audit_design_docs.py --diff --output /tmp/docs-audit.json
   ```
   Address remaining high findings that this PR owns; list deferred items.

## Document style (match existing `docs/design/`)

There is **no single template file**. Choose the closest exemplar by surface kind,
then mirror its section depth and tone — do not default to a generic OpenAPI page.

| Surface kind | Prefer updating / mirroring |
|---|---|
| HTTP resource / request–response contract | `docs/design/be/ccrv2/worker-get-api.md`, `docs/design/be/ccrv2/otlp-metrics-api.md` |
| Package / HTTP / platform boundaries | `docs/design/be/http-platform-workbench-boundaries.md`, `docs/design/be/db-platform-auth-boundaries.md` |
| Permissions / policy | `docs/design/be/permission-policies.md`, `docs/design/be/managed-agent-claude-code-permission-bridge.md` |
| FE behavior / UI contracts | `docs/design/fe/sessions/session-tool-call-display.md`, `docs/design/fe/sessions/session-detail-lane-timeline-design.md` |

### Required content when you *do* write or update a design doc

Keep it proportional to the change. For non-trivial design docs, include:

1. **Scope** — what this doc covers and what it does not.
2. **Behavior / contract** — only what the code actually does (paths, events,
   states, fields that exist in the diff).
3. **Boundaries** — package, auth, or API boundary notes when relevant
   (align with backend design rules in `AGENTS.md`).
4. **Compatibility** — Anthropic-compatible API semantics, migration, or
   intentional non-goals when relevant.
5. **Test / acceptance** — how to verify (commands, scenarios, or pointers to
   existing tests). Empty “测试计划” fluff is worse than a short concrete list.

### Anti-patterns

- Inventing TypeScript/Go types or response fields not present in the PR.
- Turning a one-line stub into a long product API manual.
- Copying unrelated design docs for style padding.
- Creating a new doc when an existing mapped doc for the same area can absorb
  a short section.
- Rewriting docs that already match the code (“无需更新” instead).

## Workflow

### 1. Gather context

- Read `AGENTS.md` §「设计文档同步」 (and backend/FE rules if the PR touches those).
- Read PR title, body, and changed files (`gh pr view`, `gh pr diff`).
- Skim the closest existing design doc(s) for surfaces the PR touches.
- Run:
  ```bash
  python3 scripts/docs-audit/audit_design_docs.py --diff --output /tmp/docs-audit.json
  ```
- If exit code is `2`, **STOP**. Comment that extraction/integrity failed and do
  not invent docs.

### 2. Triage

From `/tmp/docs-audit.json` and the PR diff, select findings that this PR
actually touches (changed packages, mounts, migrations, FE routes).

Ignore unrelated standing `gated:needs-design-doc` noise unless the PR clearly
owns that surface.

Decide per finding: **update existing doc** / **new focused doc** /
`internal` / `gated` / **无需更新**.

### 3. Apply fixes

| Finding | Action |
|---------|--------|
| unmapped surface that needs a design doc | write/update closest `docs/design/...` (see style table) and map it |
| unmapped infra / chrome | map `-> internal` with a short comment in the map |
| not ready to document | map `-> gated:<reason>` |
| missing_doc | create the file or retarget the map |
| dead_entry / dead_doc_target | prune or fix the map |
| code changed but doc already accurate | no file edit; say「设计文档无需更新」in the summary |

### 4. Commit + push

- Commit on the current PR branch: `docs: sync design docs for <area>`
- Push the current branch (restricted push is enough for feature branches).
- If the change is large/unrelated, open companion branch `docs/sync-<slug>`
  from current HEAD and open a PR; still comment on the original PR.

### 5. Final PR comment (one summary)

```markdown
## Design docs sync

**Audit:** exit `<code>` — `<N>` findings addressed, `<M>` deferred

### Updated
- `docs/design/...` — <one-line why>
- `scripts/docs-audit/surface_map.md` — <mappings>

### 设计文档无需更新
- <surface or area> — <why existing doc already matches>

### Deferred
- `<surface>` — `gated:<reason>`

### Notes
- <blockers>
```

## Out of scope

- SDD / `specs/`
- Public user docs / OpenAPI / changelog as a substitute for `docs/design/`
- Rewriting unrelated design docs for style only
- Auto-running on every PR (manual / `@duckpr docs` only)
