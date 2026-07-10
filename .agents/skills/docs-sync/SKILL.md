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

## Hard rules

1. **Allowed writes only**
   - `docs/design/**`
   - `scripts/docs-audit/surface_map.md`
   - `scripts/docs-audit/surface_snapshot.json`
2. **Do not** modify Go/TS business code, tests, configs, or workflows.
3. **Truth first**: only document behavior present in the PR diff or linked
   design context. If unclear, mark `gated:<reason>` or leave a `<!-- TODO -->`
   and explain in the PR comment — do not invent APIs or state machines.
4. Prefer the lightest correct fix:
   - update an existing design doc
   - add a focused new `docs/design/...md`
   - map `-> internal` (infra / no design concern)
   - map `-> gated:<reason>` (deferred)
5. You are already on the PR head branch with push credentials for **this
   feature branch only** (`push: restricted`). Push commits to the current
   branch. Do **not** push to `main`/`master`, create tags, or delete branches.
6. After doc/map edits, run:
   ```bash
   python3 scripts/docs-audit/audit_design_docs.py --update-snapshot
   python3 scripts/docs-audit/audit_design_docs.py --diff --output /tmp/docs-audit.json
   ```
   Address remaining high findings that this PR owns; list deferred items.

## Workflow

### 1. Gather context

- Read PR title, body, and changed files (`gh pr view`, `gh pr diff`).
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

### 3. Apply fixes

| Finding | Action |
|---------|--------|
| unmapped surface that needs a design doc | write/update `docs/design/...` and map it |
| unmapped infra / chrome | map `-> internal` with a short comment in the map |
| not ready to document | map `-> gated:<reason>` |
| missing_doc | create the file or retarget the map |
| dead_entry / dead_doc_target | prune or fix the map |

Follow existing `docs/design/` style.

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

### Deferred
- `<surface>` — `gated:<reason>`

### Notes
- <blockers>
```

## Out of scope

- SDD / `specs/`
- Public user docs / OpenAPI / changelog
- Rewriting unrelated design docs for style
- Auto-running on every PR (manual / `@duckpr docs` only)
