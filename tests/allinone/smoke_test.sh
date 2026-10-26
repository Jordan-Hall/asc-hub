#!/bin/bash
# Tests for scripts/smoke.sh against tests/allinone/fake/fakehub.py (real bare repositories behind
# `git http-backend`, and a fake ssh program). Needs Linux, bash, curl, git, ssh-keygen, python3.
#
#   tests/allinone/smoke_test.sh [name-filter]

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
SMOKE="$REPO/scripts/smoke.sh"
FILTER="${1:-}"

PASS=0
FAILED=0
CASE_NAME=""
T=""
HUB_PID=""
TOKEN="0123456789abcdef0123456789abcdef01234567"

ok() { PASS=$((PASS + 1)); }
fail() {
	FAILED=$((FAILED + 1))
	printf 'FAIL [%s] %s\n' "$CASE_NAME" "$*" >&2
	[ ! -f "$T/smoke.out" ] || tail -n 12 "$T/smoke.out" >&2
}
eq() { if [ "$2" = "$3" ]; then ok; else fail "$1: want '$2', got '$3'"; fi; }
has() { if grep -qF -- "$3" "$2" 2>/dev/null; then ok; else fail "$1: '$3' not in $2"; fi; }
hasnt() { if grep -qF -- "$3" "$2" 2>/dev/null; then fail "$1: '$3' is in $2"; else ok; fi; }
check() {
	local d="$1"
	shift
	if "$@" >/dev/null 2>&1; then ok; else fail "$d"; fi
}
check_not() {
	local d="$1"
	shift
	if "$@" >/dev/null 2>&1; then fail "$d"; else ok; fi
}

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

# new_case NAME [FAKE_X=1 ...]: a fresh fake hub.
new_case() {
	CASE_NAME="$1"
	shift
	T="$(mktemp -d /tmp/asc-smoke-t.XXXXXX)"
	mkdir -p "$T/state"
	PORT="$(free_port)"
	printf '%s\n' "$TOKEN" >"$T/token"
	chmod 600 "$T/token"
	env "$@" FAKE_X=0 python3 "$HERE/fake/fakehub.py" "$PORT" "$T/state" alice "$TOKEN" &
	HUB_PID=$!
	local i
	for ((i = 0; i < 50; i++)); do
		curl -fsS -m 1 -o /dev/null "http://127.0.0.1:$PORT/api/healthz" 2>/dev/null && break
		sleep 0.1
	done
	REQ="$T/state/requests.log"
}

end_case() {
	[ -z "$HUB_PID" ] || { kill "$HUB_PID" 2>/dev/null; wait "$HUB_PID" 2>/dev/null; }
	HUB_PID=""
	[ -z "$T" ] || rm -rf "$T"
	T=""
}

# smoke [NAME=value ...]: run the script against the fake hub; status in RC, output in smoke.out.
smoke() {
	env -i "PATH=$PATH" "HOME=$T" "TMPDIR=$T" \
		"ASC_SMOKE_URL=http://127.0.0.1:$PORT" ASC_SMOKE_ALLOW_HTTP=1 ASC_SMOKE_USER=alice "ASC_SMOKE_TOKEN_FILE=$T/token" \
		"ASC_SMOKE_SSH=$HERE/fake/fakessh" "FAKE_KEYS=$T/state/keys" "FAKE_REPOS=$T/state/repos" "FAKE_SSH_LOG=$T/ssh.log" \
		ASC_SMOKE_TIMEOUT=10 "$@" bash "$SMOKE" >"$T/smoke.out" 2>&1
	RC=$?
}

repos_left() { ls -A "$T/state/repos/alice" 2>/dev/null | wc -l | tr -d ' '; }
keys_left() { wc -l <"$T/state/keys" | tr -d ' '; }
selected() { [ -z "$FILTER" ] || [[ "$1" == *"$FILTER"* ]]; }

# --- cases ------------------------------------------------------------------------------------

t_success() {
	new_case success
	smoke
	eq "all checks pass" 0 "$RC"
	for line in "GET /api/healthz answers 200" "announces http://127.0.0.1:$PORT" "sign-in page loads" "refuses a request without a token" \
		"signs in as alice" "created alice/asc-smoke-" "git clone over https (empty" "git push over https" \
		"API shows branch main" "serves the pushed file unchanged" "returns the pushed commit" "repository page loads" \
		"added a temporary ssh key" "clone over ssh returns the same commit" "git push over ssh" "cleanup: repository" "cleanup: ssh key"; do
		has "step reported" "$T/smoke.out" "$line"
	done
	has "summary" "$T/smoke.out" "smoke: 15 checks passed, 0 skipped"
	eq "the repository is deleted" 0 "$(repos_left)"
	eq "the ssh key is deleted" 0 "$(keys_left)"
	hasnt "the token is never printed" "$T/smoke.out" "$TOKEN"
	check "ssh was used" test -s "$T/ssh.log"
	hasnt "the token never reached ssh" "$T/ssh.log" "$TOKEN"
	check_not "no temporary files left" bash -c "ls '$T' | grep -q '^asc-smoke\\.'"
	end_case
}

t_skip_ssh() {
	new_case skip_ssh
	smoke ASC_SMOKE_SKIP_SSH=1
	eq "passes" 0 "$RC"
	has "reported as skipped" "$T/smoke.out" "skip clone and push over ssh"
	has "summary counts the skip" "$T/smoke.out" "1 skipped"
	check_not "no ssh call" test -s "$T/ssh.log"
	eq "the repository is deleted" 0 "$(repos_left)"
	end_case
}

t_signin_required() {
	new_case signin_required FAKE_SIGNIN_REQUIRED=1
	smoke
	eq "passes" 0 "$RC"
	has "reported as skipped" "$T/smoke.out" "skip the repository page"
	end_case
}

t_failures() {
	new_case bad_token
	printf '%s\n' "ffffffffffffffffffffffffffffffffffffffff" >"$T/token"
	smoke
	eq "wrong token" 1 "$RC"
	has "says so" "$T/smoke.out" "sign-in with the token failed"
	eq "nothing created" 0 "$(repos_left)"
	check_not "no repository request was made" grep -q 'POST /api/v1/user/repos' "$REQ"
	hasnt "the token is never printed" "$T/smoke.out" "ffffffffffffffffffffffffffffffffffffffff"
	end_case

	new_case push_rejected FAKE_REJECT_PUSH=1
	smoke
	eq "push refused" 1 "$RC"
	has "says so" "$T/smoke.out" "git push over https failed"
	eq "the repository is still cleaned up" 0 "$(repos_left)"
	has "cleanup reported" "$T/smoke.out" "cleanup: repository"
	end_case

	new_case no_wellknown FAKE_NO_WELLKNOWN=1
	smoke
	eq "missing discovery" 1 "$RC"
	has "says why" "$T/smoke.out" "[asc] ENABLED is false"
	check_not "stops before creating anything" grep -q 'POST /api/v1/user/repos' "$REQ"
	end_case

	new_case http_wellknown FAKE_WELLKNOWN_HTTP=1
	smoke
	eq "plain http announced" 1 "$RC"
	has "says so" "$T/smoke.out" "neither https nor loopback"
	end_case

	new_case wrong_owner FAKE_WRONG_OWNER=1
	smoke
	eq "the answer names another owner" 1 "$RC"
	has "says so" "$T/smoke.out" "clone address is"
	check_not "nothing is deleted on an answer that does not match" grep -q 'DELETE /api/v1/repos' "$REQ"
	end_case

	new_case cleanup_fails FAKE_DELETE_FAILS=1
	smoke
	eq "a failed cleanup fails the run" 1 "$RC"
	has "says what to delete" "$T/smoke.out" "delete it by hand"
	end_case

	new_case ssh_host_mismatch FAKE_SSH_HOST=evil.example.org
	smoke
	eq "ssh address on another host" 1 "$RC"
	has "says so" "$T/smoke.out" "not '127.0.0.1'"
	check_not "ssh never connected" test -s "$T/ssh.log"
	eq "the repository is cleaned up" 0 "$(repos_left)"
	eq "the key is cleaned up" 0 "$(keys_left)"
	end_case

	new_case ssh_host_allowed FAKE_SSH_HOST=localhost
	smoke ASC_SMOKE_SSH_HOST=localhost
	eq "an explicit ssh host is accepted" 0 "$RC"
	end_case
}

t_settings() {
	new_case settings
	smoke ASC_SMOKE_URL=http://hub.example.org
	eq "plain http to a non-loopback host" 1 "$RC"
	has "says so" "$T/smoke.out" "plain http on a host that is not loopback"
	smoke ASC_SMOKE_ALLOW_HTTP= "ASC_SMOKE_URL=http://127.0.0.1:$PORT"
	eq "plain http needs the opt-in" 1 "$RC"
	has "says so" "$T/smoke.out" "ASC_SMOKE_ALLOW_HTTP=1"
	smoke "ASC_SMOKE_URL=https://user:pw@hub.example.org"
	eq "credentials in the address" 1 "$RC"
	hasnt "the password is not echoed" "$T/smoke.out" "pw@"
	smoke "ASC_SMOKE_URL=https://hub.example.org/?x=1"
	eq "query in the address" 1 "$RC"
	smoke ASC_SMOKE_URL=
	eq "no address" 1 "$RC"
	smoke ASC_SMOKE_USER=
	eq "no user" 1 "$RC"
	smoke "ASC_SMOKE_USER=../root"
	eq "bad user name" 1 "$RC"
	smoke ASC_SMOKE_TOKEN_FILE=
	eq "no token" 1 "$RC"
	smoke "ASC_SMOKE_TOKEN_FILE=$T/absent"
	eq "unreadable token file" 1 "$RC"
	smoke ASC_SMOKE_TOKEN_FILE= 'ASC_SMOKE_TOKEN=bad token!'
	eq "token with odd characters" 1 "$RC"
	smoke ASC_SMOKE_TIMEOUT=0
	eq "zero timeout" 1 "$RC"
	check_not "none of those reached the hub" grep -q 'POST' "$REQ"
	end_case

	new_case token_from_env
	smoke ASC_SMOKE_TOKEN_FILE= "ASC_SMOKE_TOKEN=$TOKEN" ASC_SMOKE_SKIP_SSH=1
	eq "token from the environment" 0 "$RC"
	end_case

	new_case unreachable
	kill "$HUB_PID"
	wait "$HUB_PID" 2>/dev/null
	HUB_PID=""
	smoke
	eq "hub down" 1 "$RC"
	has "says what failed" "$T/smoke.out" "health check: expected status 200, got 000"
	end_case
}

trap 'end_case' EXIT

for t in t_success t_skip_ssh t_signin_required t_failures t_settings; do
	selected "$t" && "$t"
done

printf 'smoke tests: %d passed, %d failed\n' "$PASS" "$FAILED"
[ "$FAILED" -eq 0 ]
