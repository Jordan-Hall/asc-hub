# Pixel comparison: design boards against the live hub

`npm run test:pixel` starts a throw-away hub, seeds it with the cast of the design boards, photographs every mapped board and its hub page at the same viewport and theme, and writes a ranked report (`index.html`, `results.json`, `shots/`, `detail/`).

| File | Job |
|---|---|
| `boards.json` | board -> route, viewport (`desktop` 1440 or `mobile` 390), themes, persona (`as`), `compareHeight` for composite boards, `needsAsc` for routes that need the asc server, `skip` with the reason for a board that has no page |
| `seed.mjs`, `seed-files.mjs`, `backdate.py` | deterministic data through the REST API (fixed commit dates, a small landed history on main), then fixed ages for issues, comments, releases and repositories |
| `seed-asc.mjs`, `hub/asc-cli.sh` | with `PIXEL_ASC=serve`: the stacks, their reviews, the releases with files and the workflow runs, made with the real `asc` client against the running hub |
| `hub/hub.sh` | `up`, `stop`, `start`, `down` of the hub (WSL or Linux; drops to `nobody` under root) |
| `run.mjs` | orchestration: hub, logins, board server, screenshots, comparison |
| `compare.mjs`, `pixelmatch.mjs` | pixelmatch metric (vendored port) and the structural match by text, and the "largest differences" list |
| `probe.mjs` | code that runs in the page: hides the board's "States" and design notes, settles fonts and animation, measures headings, buttons, cards and chips |
| `build-hub.sh` | builds the web assets and `tests/pixel/.bin/hub` |

Determinism: both pages use the hub's own Inter and JetBrains Mono files, animations and transitions off, scrollbars hidden, reduced motion, UTC, `Date` frozen at 2026-10-30T14:00Z. Server-rendered text that depends on the wall clock (for example the greeting) cannot be frozen.

Settings come from environment variables or `tests/pixel/.env` (not committed): `PIXEL_BOARDS_DIR`, `PIXEL_REPORT_DIR`, `PIXEL_HUB_BIN`, `PIXEL_PORT` (default 3610), `PIXEL_HUB_URL` (use a hub that is already seeded), `PIXEL_THEMES`, `PIXEL_CONCURRENCY`.
Options: `--only=Dashboard,Repo`, `--themes=light`, `--concurrency=2`, `--keep-hub`.

asc mode: `PIXEL_ASC=serve` also starts `asc serve` (`PIXEL_ASC_BIN`, a Linux binary built with the `serve` feature; `PIXEL_ASC_PORT`, default 7891, and the admin port 2000 above it, which must be free) and configures the hub as the all-in-one image does: new repositories live in asc, asc is the writer, the bridge is eager. Without it the routes under `asc/` answer 404 or 503. `PIXEL_ASC=1` only switches `[asc]` on in the hub (no server); an empty value is a plain hub. Agent runs are not seeded: a key is used only after its provider answered a test call, and the harness has no provider.
