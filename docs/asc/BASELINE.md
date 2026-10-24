# ASC Hub (ASC fork) baseline

Fork base: upstream tag `v16.0.6` (`1aa139441`). Patch-stack branch: `asc/v16`.

## What the baseline is

The baseline is the set of passing backend tests on the unmodified upstream
tag plus the K01 changes only (CI workflow and `DEFAULT_BRANCH = master` in the
sqlite and pgsql integration templates). Every later K unit must keep this set
passing; a test leaves the set only with a note in the unit that removed it.

## How it is recorded

`.forgejo/workflows/asc-fork.yml` runs on every push to `asc/**`:

1. `go build ./...`
2. `make test-backend` with `-v`, the log tee'd to `test-backend.log`
3. the passing list (`--- PASS:` lines, sorted, unique) uploaded as the
   `baseline-pass` artifact (`baseline-pass.txt`)
4. `make test-sqlite-migration test-sqlite` and `make test-pgsql-migration test-pgsql`

To compare after a change, download `baseline-pass.txt` from the K01 run and
from the new run, then `comm -23 baseline-pass.txt new-pass.txt` must be empty.

## Environment facts

- The tests need Linux (`modules/util` and `modules/process` use `unix.Umask`
  and `Setpgid`), `make`, and CGO for sqlite. A Windows host cannot run
  `go build ./...` or `make test-backend`; `GOOS=linux go build ./...`
  succeeds on the K01 tree and is the local compile check.
- The first CI run on `asc/v16` is the authoritative pass list. Commit its
  count below once it has run.

| Run | Commit | Passing tests | Notes |
|---|---|---|---|
| first `asc-fork` run on `asc/v16` | (K01 commit) | pending CI | none |

## Default branch

`tests/{sqlite,pgsql}.ini.tmpl` set `[repository] DEFAULT_BRANCH = master`
(ASC never uses `main`). Integration tests that assume `main` surface here as
baseline differences; they are fixed in the test, not by reverting the setting.

## Known failures on the unmodified tag

These fail on `~/fj-base` (`32a533d11`, K01 only) as well, so they are not caused by an ASC unit.
Run them with `TZ=UTC` to separate a real failure from the timezone one.

| Test | Package | Cause |
|---|---|---|
| `TestFlexibleClaimsUnmarshal` | `services/auth/method` | The upstream test builds its expected times with `time.Local` and `time.Date`, which only matches when the host timezone is UTC. It passes with `TZ=UTC` and fails with a local zone such as `Europe/London` in summer. |
