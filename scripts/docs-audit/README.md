# Design Doc Surface Audit

Keeps `docs/design/` in sync with code surfaces (API mounts, `internal/` packages,
SQL migrations, FE routes). Docs live in this same repo.

## What is verified vs not

| Piece | Status |
|-------|--------|
| `audit_design_docs.py` + unit tests | Verified locally and on PR CI |
| `design-doc-audit.yml` soft CI | Verified (ran on PR #32) |
| Deterministic PR audit comment (`@duckpr docs` / workflow_dispatch) | Implemented; no LLM required |
| DuckPR / Pullfrog LLM docs-sync (`push: restricted`) | Wiring fixed to match pullfrog-private capabilities; **end-to-end agent run still needs a live dispatch with secrets** |

## Commands

```bash
python3 scripts/docs-audit/audit_design_docs.py
python3 scripts/docs-audit/audit_design_docs.py --diff
python3 scripts/docs-audit/audit_design_docs.py --update-snapshot
python3 scripts/docs-audit/audit_design_docs.py --list-extracted
python3 scripts/docs-audit/test_audit_design_docs.py
```

Or: `just docs-audit` / `just docs-audit-diff` / `just docs-audit-test`.

Docs agent (after merge / with secrets). Model must match DuckPR Review
(successful runs use e.g. `anthropic/glm-5.2` + `LLM_BASE_URL=https://api.kimi.com/coding/`).
Do **not** default to Claude models — this repo's DuckPR wiring is Kimi/OpenCode.

```bash
# audit comment only
gh workflow run "DuckPR Docs Sync" -f pr_number=<N> -f skip_agent=true

# audit + LLM agent (same-repo PR branch; pass the same model DuckPR Review uses)
gh workflow run "DuckPR Docs Sync" -f pr_number=<N> -f model=anthropic/glm-5.2
# or comment on the PR: @duckpr docs
```

## Surface map

Edit `surface_map.md`:

```
SurfaceID -> docs/design/path.md
SurfaceID -> internal
SurfaceID -> gated:<reason>
```

## Writing design docs (agent + humans)

Doc sync must follow `AGENTS.md` §「设计文档同步」. The Pullfrog skill
`.agents/skills/docs-sync/SKILL.md` operationalizes that contract: when to
write, no padding, truth-first, pick an existing `docs/design/` exemplar, and
keep compatibility + test/acceptance notes aligned with the code.

## Exit codes

| Exit | Meaning |
|------|---------|
| 0 | Clean |
| 1 | Coverage / map hygiene findings (CI soft-fails initially) |
| 2 | Extraction floor or completeness accounting failed |
