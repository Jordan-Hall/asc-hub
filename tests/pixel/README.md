# Pixel comparison: design boards against the live hub

`npm run test:pixel` starts a throw-away hub, seeds it with the cast of the design boards, photographs every mapped board and its hub page at the same viewport and theme, and writes a ranked report (`index.html`, `results.json`, `shots/`, `detail/`).

| File | Job |
|---|---|
| `boards.json` | board -> route, viewport (`desktop` 1440 or `mobile` 390), themes, persona (`as`), `compareHeight` for composite boards, `needsAsc` for routes that need the asc server, `skip` with the reason for a board that has no page |
| `seed.mjs`, `backdate.py` | deterministic data through the REST API (fixed commit dates), then fixed ages for issues, comments, releases and repositories |
| `hub/hub.sh` | `up`, `stop`, `start`, `down` of the hub (WSL or Linux; drops to `nobody` under root) |
| `run.mjs` | orchestration: hub, logins, board server, screenshots, comparison |
| `compare.mjs`, `pixelmatch.mjs` | pixelmatch metric (vendored port) and the structural match by text, and the "largest differences" list |
| `probe.mjs` | code that runs in the page: hides the board's "States" and design notes, settles fonts and animation, measures headings, buttons, cards and chips |
| `build-hub.sh` | builds the web assets and `tests/pixel/.bin/hub` |

Determinism: both pages use the hub's own Inter and JetBrains Mono files, animations and transitions off, scrollbars hidden, reduced motion, UTC, `Date` frozen at 2026-10-30T14:00Z. Server-rendered text that depends on the wall clock (for example the greeting) cannot be frozen.

Settings come from environment variables or `tests/pixel/.env` (not committed): `PIXEL_BOARDS_DIR`, `PIXEL_REPORT_DIR`, `PIXEL_HUB_BIN`, `PIXEL_PORT` (default 3610), `PIXEL_HUB_URL` (use a hub that is already seeded), `PIXEL_THEMES`, `PIXEL_CONCURRENCY`.
Options: `--only=Dashboard,Repo`, `--themes=light`, `--concurrency=2`, `--keep-hub`.
