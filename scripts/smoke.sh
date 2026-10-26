#!/bin/bash
# Smoke test of a running ASC Hub. It checks, in this order: /api/healthz, /.well-known/asc, the
# sign-in page and a token sign-in, creating a repository, a push and a clone over https, the
# repository's web page, a clone and a push over ssh. It then deletes what it made.
#
#   ASC_SMOKE_URL=https://hub.example.org ASC_SMOKE_USER=alice ASC_SMOKE_TOKEN_FILE=~/.hub-token \
#     scripts/smoke.sh
#
# Settings (environment):
#   ASC_SMOKE_URL          required  public address of the hub. https, or http for a loopback host
#                                    when ASC_SMOKE_ALLOW_HTTP=1.
#   ASC_SMOKE_USER         required  user name that owns the token; the test repository is created
#                                    under it.
#   ASC_SMOKE_TOKEN_FILE   or        a file whose first line is an access token with the scopes
#   ASC_SMOKE_TOKEN                  write:repository and write:user. Prefer the file: an environment
#                                    variable is visible to other processes of the same user.
#   ASC_SMOKE_SSH_HOST     optional  host name the ssh clone address must have (default: the host of
#                                    ASC_SMOKE_URL). The test connects nowhere else.
#   ASC_SMOKE_SSH          optional  the ssh program (default ssh).
#   ASC_SMOKE_SKIP_SSH=1   optional  skip the ssh checks (reported as skipped, never as passed).
#   ASC_SMOKE_TIMEOUT      optional  seconds per request (default 20).
#
# Needs curl, git 2.31 or later, ssh, ssh-keygen, base64, sed, grep, mktemp. The token goes to
# curl through a file in a private temporary directory and to git through its environment, never
# onto a command line, into the log or to a host other than the one in ASC_SMOKE_URL. The ssh host
# key is accepted at its first sight (the run uses a new known_hosts file). Exit status 0 only if
# every check passed and the cleanup worked.

set -euo pipefail
umask 077

PASSED=0
SKIPPED=0
log() { printf '%s\n' "$*"; }
die() {
	printf 'FAIL %s\n' "$*" >&2
	exit 1
}
pass() {
	PASSED=$((PASSED + 1))
	log "ok   $*"
}
skip() {
	SKIPPED=$((SKIPPED + 1))
	log "skip $*"
}

# --- settings ---------------------------------------------------------------------------------

BASE="${ASC_SMOKE_URL:-}"
BASE="${BASE%/}"
USER_NAME="${ASC_SMOKE_USER:-}"
TIMEOUT="${ASC_SMOKE_TIMEOUT:-20}"
SSH_BIN="${ASC_SMOKE_SSH:-ssh}"

[ -n "$BASE" ] || die "ASC_SMOKE_URL is not set"
[ -n "$USER_NAME" ] || die "ASC_SMOKE_USER is not set"
[[ "$USER_NAME" =~ ^[A-Za-z0-9][A-Za-z0-9_.-]{0,38}$ ]] || die "ASC_SMOKE_USER is not a valid user name"
[[ "$TIMEOUT" =~ ^[0-9]{1,3}$ ]] && [ "$TIMEOUT" -ge 1 ] || die "ASC_SMOKE_TIMEOUT must be a number of seconds"
[[ "$BASE" =~ ^(https?)://([A-Za-z0-9.-]+)(:[0-9]{1,5})?(/[A-Za-z0-9._~/-]*)?$ ]] || die "ASC_SMOKE_URL must be an absolute http(s) URL without credentials, query or fragment"
SCHEME="${BASH_REMATCH[1]}"
HOST="${BASH_REMATCH[2]}"
PROTO='=https'
if [ "$SCHEME" = http ]; then
	[ "${ASC_SMOKE_ALLOW_HTTP:-}" = 1 ] || die "ASC_SMOKE_URL uses plain http: only a loopback host is allowed, and only with ASC_SMOKE_ALLOW_HTTP=1"
	case "$HOST" in
	localhost | 127.*) PROTO='=http,https' ;;
	*) die "ASC_SMOKE_URL uses plain http on a host that is not loopback" ;;
	esac
fi
SSH_HOST="${ASC_SMOKE_SSH_HOST:-$HOST}"
[[ "$SSH_HOST" =~ ^[A-Za-z0-9.-]+$ ]] || die "ASC_SMOKE_SSH_HOST is not a host name"

if [ -n "${ASC_SMOKE_TOKEN_FILE:-}" ]; then
	[ -r "$ASC_SMOKE_TOKEN_FILE" ] || die "cannot read ASC_SMOKE_TOKEN_FILE"
	IFS= read -r TOKEN <"$ASC_SMOKE_TOKEN_FILE" || true
else
	TOKEN="${ASC_SMOKE_TOKEN:-}"
fi
[ -n "${TOKEN:-}" ] || die "set ASC_SMOKE_TOKEN_FILE or ASC_SMOKE_TOKEN"
[[ "$TOKEN" =~ ^[A-Za-z0-9_.-]{8,200}$ ]] || die "the access token has characters that a token does not have"

for tool in curl git base64 sed grep mktemp; do
	command -v "$tool" >/dev/null 2>&1 || die "$tool is needed and was not found"
done
git_version="$(git --version | sed -n 's/^git version \([0-9]*\)\.\([0-9]*\).*/\1 \2/p')"
read -r git_major git_minor <<<"$git_version"
{ [ "${git_major:-0}" -gt 2 ] || { [ "${git_major:-0}" -eq 2 ] && [ "${git_minor:-0}" -ge 31 ]; }; } || die "git 2.31 or later is needed"
if [ "${ASC_SMOKE_SKIP_SSH:-}" != 1 ]; then
	command -v "$SSH_BIN" >/dev/null 2>&1 || die "$SSH_BIN is needed for the ssh checks (or set ASC_SMOKE_SKIP_SSH=1)"
	command -v ssh-keygen >/dev/null 2>&1 || die "ssh-keygen is needed for the ssh checks (or set ASC_SMOKE_SKIP_SSH=1)"
fi

TMP="$(mktemp -d "${TMPDIR:-/tmp}/asc-smoke.XXXXXX")"
RUN_ID="$(LC_ALL=C tr -dc 'a-z0-9' </dev/urandom | head -c 8 || true)"
[ "${#RUN_ID}" -eq 8 ] || die "could not make a run id"
REPO_NAME="asc-smoke-$RUN_ID"
REPO_OWNED=""
KEY_ID=""
CLEANUP_FAILED=0
CODE=""

# --- http -------------------------------------------------------------------------------------

printf 'header = "Authorization: token %s"\n' "$TOKEN" >"$TMP/auth.conf"

# fetch [-a] METHOD PATH [JSON]: the status code is in CODE and the body in $TMP/body. With -a the
# token is sent. Redirects are not followed, so the token cannot be forwarded to another host.
fetch() {
	local auth=0
	if [ "$1" = -a ]; then
		auth=1
		shift
	fi
	local method="$1" path="$2" body="${3:-}"
	local -a args=(-sS --proto "$PROTO" --max-redirs 0 --connect-timeout 10 -m "$TIMEOUT" -o "$TMP/body" -w '%{http_code}' -X "$method" -H 'Accept: application/json')
	if [ "$auth" = 1 ]; then args+=(-K "$TMP/auth.conf"); fi
	if [ -n "$body" ]; then args+=(-H 'Content-Type: application/json' --data-binary "$body"); fi
	CODE="$(curl "${args[@]}" "$BASE$path" 2>"$TMP/curl.err")" || CODE="000"
	: >>"$TMP/body"
}

# json_str KEY: the first string value of KEY in the last body.
json_str() {
	grep -o "\"$1\":\"[^\"]*\"" "$TMP/body" | head -n 1 | sed "s/^\"$1\":\"//; s/\"\$//"
}
json_num() {
	grep -o "\"$1\":[0-9]*" "$TMP/body" | head -n 1 | sed "s/^\"$1\"://"
}

expect() { # description, wanted code
	if [ "$CODE" != "$2" ]; then
		local err=""
		[ "$CODE" != 000 ] || err=" ($(head -c 200 "$TMP/curl.err"))"
		die "$1: expected status $2, got $CODE$err"
	fi
}

# --- cleanup ----------------------------------------------------------------------------------

cleanup() {
	local rc=$?
	trap - EXIT
	set +e
	if [ -n "$REPO_OWNED" ]; then
		fetch -a DELETE "/api/v1/repos/$USER_NAME/$REPO_NAME"
		if [ "$CODE" = 204 ]; then
			log "ok   cleanup: repository $USER_NAME/$REPO_NAME deleted"
		else
			printf 'FAIL cleanup: could not delete the repository %s/%s (status %s): delete it by hand\n' "$USER_NAME" "$REPO_NAME" "$CODE" >&2
			CLEANUP_FAILED=1
		fi
	fi
	if [ -n "$KEY_ID" ]; then
		fetch -a DELETE "/api/v1/user/keys/$KEY_ID"
		if [ "$CODE" = 204 ]; then
			log "ok   cleanup: ssh key $KEY_ID deleted"
		else
			printf 'FAIL cleanup: could not delete the ssh key %s (status %s): delete the key titled asc-smoke-%s by hand\n' "$KEY_ID" "$CODE" "$RUN_ID" >&2
			CLEANUP_FAILED=1
		fi
	fi
	rm -rf "$TMP"
	if [ "$rc" -eq 0 ] && [ "$CLEANUP_FAILED" -ne 0 ]; then rc=1; fi
	if [ "$rc" -eq 0 ]; then
		log "smoke: $PASSED checks passed, $SKIPPED skipped"
	else
		log "smoke: FAILED ($PASSED checks passed before the failure)"
	fi
	exit "$rc"
}
trap cleanup EXIT

# --- 1. health --------------------------------------------------------------------------------

fetch GET /api/healthz
expect "health check" 200
pass "GET /api/healthz answers 200"

# --- 2. discovery -----------------------------------------------------------------------------

fetch GET /.well-known/asc
[ "$CODE" != 404 ] || die "GET /.well-known/asc answers 404: [asc] ENABLED is false or SERVER_URL is empty"
expect "discovery" 200
ASC_SERVER="$(json_str server)"
[ -n "$ASC_SERVER" ] || die "/.well-known/asc has no \"server\" field"
case "$ASC_SERVER" in
https://*) ;;
http://localhost* | http://127.*) ;;
*) die "/.well-known/asc announces '$ASC_SERVER', which is neither https nor loopback" ;;
esac
pass "GET /.well-known/asc announces $ASC_SERVER"
if [ "$ASC_SERVER" != "$BASE" ] && [ "$ASC_SERVER" != "$BASE/" ]; then
	log "note /.well-known/asc announces a different address than ASC_SMOKE_URL; clients use the announced one"
fi

# --- 3. sign-in -------------------------------------------------------------------------------

fetch GET /user/login
expect "sign-in page" 200
grep -qi '<form' "$TMP/body" || die "the sign-in page has no form"
pass "the sign-in page loads"

fetch GET /api/v1/user
case "$CODE" in
401 | 403) pass "the API refuses a request without a token ($CODE)" ;;
*) die "the API answered $CODE to a request without a token; expected 401 or 403" ;;
esac

fetch -a GET /api/v1/user
[ "$CODE" != 401 ] || die "sign-in with the token failed (401): the token is wrong, expired or has no scope"
expect "token sign-in" 200
LOGIN="$(json_str login)"
[ "${LOGIN,,}" = "${USER_NAME,,}" ] || die "the token belongs to '$LOGIN', not to ASC_SMOKE_USER ($USER_NAME)"
pass "the token signs in as $LOGIN"

# --- 4. create a repository ---------------------------------------------------------------------

fetch -a POST /api/v1/user/repos "{\"name\":\"$REPO_NAME\",\"private\":false,\"auto_init\":false,\"description\":\"ASC Hub smoke test, safe to delete\"}"
if [ "$CODE" != 201 ]; then
	[ "$CODE" != 403 ] || die "creating a repository was refused (403): the token needs the write:repository scope"
	expect "create repository" 201
fi
CLONE_URL="$(json_str clone_url)"
WANT_CLONE="$BASE/$USER_NAME/$REPO_NAME.git"
# The answer must describe the repository that was asked for, under the user that was asked for.
if [ "${CLONE_URL,,}" != "${WANT_CLONE,,}" ]; then
	die "the new repository's clone address is '$CLONE_URL', expected '$WANT_CLONE' (is ROOT_URL the address you test?); the repository $USER_NAME/$REPO_NAME may exist: check by hand"
fi
REPO_OWNED=1
BRANCH="$(json_str default_branch)"
[ -n "$BRANCH" ] || BRANCH=main
[[ "$BRANCH" =~ ^[A-Za-z0-9._/-]{1,100}$ ]] || die "the default branch name '$BRANCH' is not usable"
pass "created $USER_NAME/$REPO_NAME (default branch $BRANCH)"

# --- 5. clone and push over https -------------------------------------------------------------------

BASIC="$(printf '%s:%s' "$USER_NAME" "$TOKEN" | base64 | tr -d '\n')"
mkdir -p "$TMP/home"
export HOME="$TMP/home" GIT_TERMINAL_PROMPT=0 GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL=/dev/null
export GIT_CONFIG_COUNT=1 "GIT_CONFIG_KEY_0=http.$BASE/.extraHeader" "GIT_CONFIG_VALUE_0=Authorization: Basic $BASIC"
unset GIT_SSH_COMMAND GIT_ASKPASS SSH_ASKPASS 2>/dev/null || true

GIT_ID=(-c user.name=asc-smoke -c user.email=asc-smoke@invalid -c commit.gpgsign=false)
CONTENT="asc smoke $RUN_ID"

git clone -q "$WANT_CLONE" "$TMP/a" 2>"$TMP/git.err" || die "git clone over https failed: $(head -c 300 "$TMP/git.err")"
pass "git clone over https (empty repository)"

git -C "$TMP/a" checkout -q -b "$BRANCH" 2>/dev/null || true
printf '%s\n' "$CONTENT" >"$TMP/a/smoke.txt"
git -C "$TMP/a" add smoke.txt
git -C "$TMP/a" "${GIT_ID[@]}" commit -q -m "smoke test $RUN_ID"
git -C "$TMP/a" push -q origin "HEAD:refs/heads/$BRANCH" 2>"$TMP/git.err" || die "git push over https failed: $(head -c 300 "$TMP/git.err")"
pass "git push over https"

# The hub imports a push into ASC asynchronously: give it a moment.
wait_api() { # description, path, wanted code
	local i
	for ((i = 0; i < 30; i++)); do
		fetch -a GET "$2"
		if [ "$CODE" = "$3" ]; then return 0; fi
		sleep 1
	done
	die "$1: expected status $3 for $2 within 30 s, last status $CODE"
}
wait_api "the pushed branch is visible through the API" "/api/v1/repos/$USER_NAME/$REPO_NAME/branches/$BRANCH" 200
pass "the API shows branch $BRANCH"
wait_api "the pushed file is readable through the API" "/api/v1/repos/$USER_NAME/$REPO_NAME/raw/smoke.txt?ref=$BRANCH" 200
[ "$(cat "$TMP/body")" = "$CONTENT" ] || die "the file smoke.txt read through the API differs from what was pushed"
pass "the API serves the pushed file unchanged"

git clone -q "$WANT_CLONE" "$TMP/b" 2>"$TMP/git.err" || die "second git clone over https failed: $(head -c 300 "$TMP/git.err")"
[ "$(cat "$TMP/b/smoke.txt" 2>/dev/null)" = "$CONTENT" ] || die "the clone over https does not hold the pushed file"
HEAD_HTTPS="$(git -C "$TMP/b" rev-parse HEAD)"
[ "$HEAD_HTTPS" = "$(git -C "$TMP/a" rev-parse HEAD)" ] || die "the clone over https has a different head than the push"
pass "git clone over https returns the pushed commit"

# --- 6. the web page --------------------------------------------------------------------------------

fetch GET "/$USER_NAME/$REPO_NAME"
case "$CODE" in
200)
	grep -qF "$REPO_NAME" "$TMP/body" || die "the repository page does not mention $REPO_NAME"
	pass "the repository page loads and names the repository"
	;;
302 | 303 | 307)
	skip "the repository page: the hub asks for sign-in to view (REQUIRE_SIGNIN_VIEW), so a public page cannot be checked without a session"
	;;
*) die "the repository page answered $CODE" ;;
esac

# --- 7. ssh -----------------------------------------------------------------------------------------

if [ "${ASC_SMOKE_SKIP_SSH:-}" = 1 ]; then
	skip "clone and push over ssh (ASC_SMOKE_SKIP_SSH=1)"
else
	ssh-keygen -q -t ed25519 -N '' -C "asc-smoke-$RUN_ID" -f "$TMP/id" >/dev/null
	PUB="$(cat "$TMP/id.pub")"
	fetch -a POST /api/v1/user/keys "{\"title\":\"asc-smoke-$RUN_ID\",\"key\":\"$PUB\",\"read_only\":false}"
	[ "$CODE" != 403 ] || die "adding an ssh key was refused (403): the token needs the write:user scope"
	expect "add ssh key" 201
	[ "$(json_str title)" = "asc-smoke-$RUN_ID" ] || die "the answer to adding the ssh key is not for the key that was sent"
	KEY_ID="$(json_num id)"
	[[ "$KEY_ID" =~ ^[0-9]+$ ]] || die "the answer to adding the ssh key has no id"
	pass "added a temporary ssh key"

	fetch -a GET "/api/v1/repos/$USER_NAME/$REPO_NAME"
	expect "repository" 200
	SSH_URL="$(json_str ssh_url)"
	[[ "$SSH_URL" =~ ^ssh://([A-Za-z0-9_]+)@([A-Za-z0-9.-]+)(:([0-9]{1,5}))?/([A-Za-z0-9._/-]+)$ ]] || die "the repository's ssh address '$SSH_URL' is not an ssh:// address that this test can use"
	[ "${BASH_REMATCH[2],,}" = "${SSH_HOST,,}" ] || die "the ssh address names host '${BASH_REMATCH[2]}', not '$SSH_HOST': set ASC_SMOKE_SSH_HOST if that is intended (the test connects to no other host)"
	[ "${BASH_REMATCH[5]}" = "$USER_NAME/$REPO_NAME.git" ] || die "the ssh address path '${BASH_REMATCH[5]}' is not $USER_NAME/$REPO_NAME.git"

	GIT_SSH_COMMAND="$(printf '%q' "$SSH_BIN") -F /dev/null -i $(printf '%q' "$TMP/id") -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=$(printf '%q' "$TMP/known_hosts") -o ConnectTimeout=15"
	export GIT_SSH_COMMAND
	git clone -q "$SSH_URL" "$TMP/c" 2>"$TMP/git.err" || die "git clone over ssh failed: $(head -c 300 "$TMP/git.err")"
	[ "$(git -C "$TMP/c" rev-parse HEAD)" = "$HEAD_HTTPS" ] || die "the clone over ssh has a different head than the clone over https"
	[ "$(cat "$TMP/c/smoke.txt")" = "$CONTENT" ] || die "the clone over ssh does not hold the pushed file"
	pass "git clone over ssh returns the same commit as https"

	printf '%s\n' "second $RUN_ID" >"$TMP/c/ssh.txt"
	git -C "$TMP/c" add ssh.txt
	git -C "$TMP/c" "${GIT_ID[@]}" commit -q -m "smoke test over ssh $RUN_ID"
	git -C "$TMP/c" push -q origin "HEAD:refs/heads/smoke-ssh" 2>"$TMP/git.err" || die "git push over ssh failed: $(head -c 300 "$TMP/git.err")"
	wait_api "the branch pushed over ssh is visible through the API" "/api/v1/repos/$USER_NAME/$REPO_NAME/branches/smoke-ssh" 200
	pass "git push over ssh, and the API shows the branch"
fi
