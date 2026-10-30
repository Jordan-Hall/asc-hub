#!/usr/bin/env bash
# Sourced by the seed (tests/pixel/seed.mjs) in PIXEL_ASC=serve mode: runs the real `asc` client against the harness hub as
# the people of the cast, the way they would. Needs HUB (the hub's URL), ASC_URL (asc serve), ASC_BIN, PASSWORD and WORK
# (an empty scratch directory).
set -euo pipefail
export GIT_TERMINAL_PROMPT=0 ASC_ALLOW_LOOPBACK_HTTP=1 GIT_CONFIG_SYSTEM=/dev/null

# person <login> <display name>: a home with the credentials of one hub user (a token made through the API)
person() {
  local u=$1 name=$2 home=$WORK/home-$1 tok
  mkdir -p "$home"
  printf '[user]\n\tname = %s\n\temail = %s@acme.test\n[safe]\n\tdirectory = *\n[credential]\n\thelper = store\n' "$name" "$u" >"$home/.gitconfig"
  tok=$(curl -fsS -u "$u:$PASSWORD" -H 'Content-Type: application/json' -X POST "$HUB/api/v1/users/$u/tokens" \
    -d "{\"name\":\"pixel-cli-$RANDOM$RANDOM\",\"scopes\":[\"all\"]}" | sed -n 's/.*"sha1":"\([^"]*\)".*/\1/p')
  [ -n "$tok" ] || { echo "no token for $u" >&2; return 1; }
  as "$u" auth login --host "$HUB" --token-stdin <<<"$tok" >/dev/null
  as "$u" auth login --host "$ASC_URL" --token-stdin <<<"$tok" >/dev/null
}

# as <login> <asc args...>: asc as that person (cwd stays where the caller is)
as() {
  local u=$1; shift
  local home=$WORK/home-$u
  HOME=$home ASC_HOME=$home/.asc GIT_CONFIG_GLOBAL=$home/.gitconfig "$ASC_BIN" "$@"
}

# when <iso time>: the time git records for the next commits
when() { export GIT_AUTHOR_DATE=$1 GIT_COMMITTER_DATE=$1; }

# clone <login> <owner/repo> <dir>
clone() { as "$1" clone "$HUB/$2" "$3" >/dev/null; }
