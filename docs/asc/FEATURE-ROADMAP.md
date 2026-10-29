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

## Definition of done (user directive: do not stop until all of it holds)

1. Every row in `feature-gaps/*.md` is `done`, or carries a reason the user accepted; counts in this file match.
2. `grep` finds no TODO, FIXME, `unimplemented!`, `todo!`, "not supported yet", placeholder or lorem text in user-reachable code of the hub and asc.
3. Every one of the 191 boards has its page implemented and matches its board at 390, 768, 1024 and 1440 pixels, light and dark (screenshot pairs checked), mobile twins included; axe has no critical or serious findings; no console errors on any page.
4. Every control on every page works against a real hub and a real asc serve (end-to-end journeys re-run: sign up, import, push, stack submit, review, queue, agent run, settings, admin, download).
5. Permissions and row-level visibility equal the platform's (parity matrix green); every security review finding is fixed.
6. CI green on both repositories (fmt, clippy -D warnings, all tests, image builds), signed release cut, staging deployed and healthy, docs updated.

## Language support: TSRX (user requirement)

TSRX (https://tsrx.dev/) is a TypeScript extension for declarative UIs, files `.tsrx`: statement-container component bodies `@{ ... }`, `@for` and `@if` control flow in the template, scoped `<style>` blocks, JSX-compatible expressions and events. The product must support it as a first-class language wherever code is shown, searched or understood.

| Area | Requirement | Where | Phase |
|---|---|---|---|
| Hub | **done** `.tsrx` detected as TSRX (language stats, linguist overrides, `.gitattributes` `linguist-language`) | modules/analyze, modules/highlight | 1 |
| Hub | **done** (chroma lexer `modules/highlight/tsrx`, golden tests; review and search results use the same highlighter) Syntax highlighting in file view, blame, diffs, review, search results, markdown fences (```tsrx) with the `@{}` container, `@for`/`@if`, scoped style (CSS) and TS/JSX inside | server-side highlighter and the web editor | 1 |
| Hub | **done** (CodeMirror 6 stream language `codemirror-tsrx.ts`; the hub editor is CodeMirror) Web editor (Monaco or equivalent): TSRX language registration, tokenizer, bracket and indent rules, comments | web_src/js/features/codeeditor | 1 |
| Hub | **done** (chip colour and statistics type; the facet lists whatever language the indexer reports) Search facet "language: tsrx" and the language chip | repo search, explore | 1 |
| asc | Code intelligence: parse `.tsrx` (components, functions, props types, imports/exports, `@for`/`@if` scopes), symbols and references, outline, blame hover and "used in" | crates/asc-codeintel | 3 |
| asc | Semantic/text search chunking for `.tsrx`; dependency scanning treats `.tsrx` imports like TS imports | crates/asc-serve codeintel, deps | 3 |
| asc | Diff and review: syntax-aware hunks and the "where to look" block for `.tsrx` | review | 3 |
| Tooling | `asc` formatter/lint hooks: allow Prettier and ESLint TSRX plugins in workflow templates; document language server use | docs | 3 |

## CLI authentication model (user requirement)

Authentication is per repository (per host and path), not a separate up-front step. `asc clone <url>` of a public repository needs no sign-in. When the server answers that the repository needs credentials, the client asks right there, the way git does: it tries a stored credential for that host and path (credential store / git credential helpers), else starts the device sign-in inline (shows the code and link, waits for approval) or prompts for a token when there is no browser, then retries the clone and stores the credential scoped to that host (and path when the user chooses). Push, fetch and submit behave the same on a 401. `asc auth login` remains for scripted setup but is never required before a first clone. Non-interactive runs (no TTY, CI) never prompt: they fail with a clear message naming `ASC_TOKEN` / credential helper options.

## Release cadence

- When Phase 1 (UI) is complete: cut a release (tag, signed artifacts, images, downloads published) and deploy it to staging.
- After that, every full feature (a finished feature family with its backend, UI, tests and docs) gets its own release and deploy, in the phase order above.
- A release needs: green CI on the tag, the signing key (`ASC_MINISIGN_KEY` secret, `ASC_RELEASE_PUBKEY` variable), GitHub Actions able to run (billing), and the deploy script (`scratchpad/deploy-asc.sh` pattern: wait for the asc image, pin `ASC_SERVE_IMAGE`, rebuild the hub image, pin the tag in Coolify, restart, wait for 200).

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
