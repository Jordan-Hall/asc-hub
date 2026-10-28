# Feature roadmap: making the product exactly the design

Source of truth for what is left between the product and the design boards (`project-v3`, 191 boards). Per-feature rows live in `feature-gaps/<group>.md` (board, feature, status, where to implement, size). This page holds the plan and the counts; update the counts when the gap files change.

Status words: **done**, **ui-only-missing** (data and routes exist, markup or script missing), **needs-hub-backend**, **needs-asc-backend**, **needs-both**.

## Counts (audit of 2026-10-28)

| Group | done | ui-only-missing | needs-hub-backend | needs-asc-backend | needs-both | total |
|---|---|---|---|---|---|---|
| shell | 0 | 0 | 0 | 0 | 0 | 0 |
| settings | 0 | 0 | 0 | 0 | 0 | 0 |
| repo | 0 | 0 | 0 | 0 | 0 | 0 |
| review | 0 | 0 | 0 | 0 | 0 | 0 |
| **all** | 0 | 0 | 0 | 0 | 0 | 0 |

## Order of work

1. **Phase 1, UI only.** Every `ui-only-missing` row (data and routes exist; markup, script, states, mobile twin missing). Sequence: shell and public, repository, review and agents, settings and admin. Each page is checked against its board at 390, 768, 1024 and 1440 pixels, light and dark.
2. **Phase 2, hub backend.** Every `needs-hub-backend` row (the asc RPC exists).
3. **Phase 3, asc server and the hub pages that depend on it.** Every `needs-asc-backend` and `needs-both` row. Every call authorized by the hub's permission model, audited, with permission-denied and other-tenant tests.
4. **Phase 4, closure.** Re-run the audit, mark rows, list any row that cannot be built with its reason.

Rules: permissions and row-level visibility exactly the platform's; tokens are the platform's personal access tokens (extend them with expiry and new scopes where a board shows them, never a second token system); no TODO, stub, fake data or hidden control.

## Phase 1 rows (ui-only-missing)

### shell

| Board | Feature | Where | Size |
|---|---|---|---|

### settings

| Board | Feature | Where | Size |
|---|---|---|---|

### repo

| Board | Feature | Where | Size |
|---|---|---|---|

### review

| Board | Feature | Where | Size |
|---|---|---|---|
