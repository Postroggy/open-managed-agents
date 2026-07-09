# Design Doc Surface Audit

Keeps `docs/design/` in sync with code surfaces (API mounts, `internal/` packages,
SQL migrations, FE routes). Docs live in this same repo.

## Commands

```bash
python3 scripts/docs-audit/audit_design_docs.py
python3 scripts/docs-audit/audit_design_docs.py --diff
python3 scripts/docs-audit/audit_design_docs.py --update-snapshot
python3 scripts/docs-audit/audit_design_docs.py --list-extracted
python3 scripts/docs-audit/test_audit_design_docs.py
```

Or: `just docs-audit` / `just docs-audit-diff` / `just docs-audit-test`.

## Surface map

Edit `surface_map.md`:

```
SurfaceID -> docs/design/path.md
SurfaceID -> internal
SurfaceID -> gated:<reason>
```

## Exit codes

| Exit | Meaning |
|------|---------|
| 0 | Clean |
| 1 | Coverage / map hygiene findings (CI soft-fails initially) |
| 2 | Extraction floor or completeness accounting failed |
