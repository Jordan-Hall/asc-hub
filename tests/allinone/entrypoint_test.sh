#!/bin/bash
# Tests for docker/allinone/usr/local/bin/asc-hub-entrypoint and asc-hub-healthcheck, run against
# fake hub, asc and forgejo programs (tests/allinone/fake). Needs Linux, bash, flock, curl, python3.
# As root the entrypoint runs as uid 1000 (setpriv), like the image does; as another user it runs
# as that user.
#
#   tests/allinone/entrypoint_test.sh [name-filter]

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
ENTRY_SRC="$REPO/docker/allinone/usr/local/bin/asc-hub-entrypoint"
HEALTH_SRC="$REPO/docker/allinone/usr/local/bin/asc-hub-healthcheck"
FILTER="${1:-}"

PASS=0
FAILED=0
CASE_NAME=""
T=""
EP=""
RC=""
RUNAS=()
if [ "$(id -u)" = 0 ]; then
	RUNAS=(setpriv --reuid=1000 --regid=1000 --clear-groups)
fi
OWNER_UID="$([ "$(id -u)" = 0 ] && echo 1000 || id -u)"

ok() { PASS=$((PASS + 1)); }
fail() {
	FAILED=$((FAILED + 1))
	printf 'FAIL [%s] %s\n' "$CASE_NAME" "$*" >&2
	if [ -n "$T" ]; then
		for f in "$T"/out.*; do
			[ -e "$f" ] && { printf -- '--- %s\n' "$f" >&2; tail -n 15 "$f" >&2; }
		done
	fi
}
check() { # description, command...
	local d="$1"
	shift
	if "$@" >/dev/null 2>&1; then ok; else fail "$d"; fi
}
check_not() {
	local d="$1"
	shift
	if "$@" >/dev/null 2>&1; then fail "$d"; else ok; fi
}
eq() { # description, want, got
	if [ "$2" = "$3" ]; then ok; else fail "$1: want '$2', got '$3'"; fi
}
has() { # description, file, fixed string
	if grep -qF -- "$3" "$2" 2>/dev/null; then ok; else fail "$1: '$3' not in $2"; fi
}
hasnt() {
	if grep -qF -- "$3" "$2" 2>/dev/null; then fail "$1: '$3' is in $2"; else ok; fi
}
mode_of() { stat -c %a "$1" 2>/dev/null; }

free_port() { python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1])'; }

# new_case NAME: a fresh volume, fake programs and ports.
new_case() {
	CASE_NAME="$1"
	T="$(mktemp -d -p /tmp)"
	mkdir -p "$T/work" "$T/fake" "$T/bin" "$T/home"
	cp "$HERE"/fake/* "$T/bin/"
	cp "$ENTRY_SRC" "$HEALTH_SRC" "$T/bin/"
	chmod 755 "$T/bin/"*
	HP="$(free_port)"
	AP="$(free_port)"
	EP=""
	n_start=0
	own_all
	chmod 755 "$T"
}

# own_all: hand everything made by the test to the user that runs the entrypoint.
own_all() {
	if [ "$(id -u)" = 0 ]; then chown -R 1000:1000 "$T"; fi
}

# start [NAME=value ...]: run the entrypoint in the background; its pid is EP.
start() {
	n_start=$((n_start + 1))
	local -a envs=(
		"PATH=$PATH" "HOME=$T/home" "GITEA_WORK_DIR=$T/work"
		"ASC_BINARY=$T/bin/fake-asc" "ASC_HUB_TEST_HUB_COMMAND=$T/bin/fake-hub" "ASC_HUB_TEST_FORGEJO=$T/bin/fake-forgejo"
		"FAKE_DIR=$T/fake" "HTTP_PORT=$HP" "ASC_HUB_ASC_PORT=$AP"
	)
	if [ -z "${NO_ROOT_URL:-}" ]; then envs+=("ROOT_URL=https://hub.test/"); fi
	# A background job of a non-interactive shell starts with SIGINT ignored; docker does not do that,
	# so the wrapper restores the default before it replaces itself with the entrypoint.
	"${RUNAS[@]}" python3 -c 'import os, signal, sys; signal.signal(signal.SIGINT, signal.SIG_DFL); os.execvp(sys.argv[1], sys.argv[1:])' \
		env -i "${envs[@]}" "$@" bash "$T/bin/asc-hub-entrypoint" >"$T/out.$n_start" 2>&1 &
	EP=$!
}

run_args() { # run with command-line arguments, to completion
	"${RUNAS[@]}" env -i "PATH=$PATH" "GITEA_WORK_DIR=$T/work" "ASC_HUB_TEST_HUB_COMMAND=$T/bin/fake-hub" "FAKE_DIR=$T/fake" \
		bash "$T/bin/asc-hub-entrypoint" "$@" >"$T/out.args" 2>&1
	RC=$?
}

wait_for() { # seconds, command...
	local n="$1" i
	shift
	for ((i = 0; i < n * 10; i++)); do
		if "$@" >/dev/null 2>&1; then return 0; fi
		sleep 0.1
	done
	return 1
}

asc_up() { curl -fsS -m 2 -o /dev/null "http://127.0.0.1:$AP/readyz"; }
hub_up() { curl -fsS -m 2 -o /dev/null "http://127.0.0.1:$HP/api/healthz"; }
wait_ready() {
	if ! wait_for 30 asc_up; then fail "the container did not become ready"; return 1; fi
	return 0
}

# wait_exit SECONDS: wait for EP to end; sets RC.
wait_exit() {
	local n="$1" i
	for ((i = 0; i < n * 10; i++)); do
		if ! kill -0 "$EP" 2>/dev/null; then
			wait "$EP"
			RC=$?
			return 0
		fi
		sleep 0.1
	done
	RC=timeout
	return 1
}

alive() { kill -0 "$1" 2>/dev/null; }
last_pid() { tail -n 1 "$T/fake/$1" 2>/dev/null; }
count_events() {
	local n
	n="$(grep -c -x -- "$1" "$T/fake/events.log" 2>/dev/null)"
	echo "${n:-0}"
}
env_of() { # file, name: the value of an environment variable recorded by a fake
	sed -n "s/^$2=//p" "$T/fake/$1" | head -n 1
}

end_case() {
	if [ -n "$EP" ] && alive "$EP"; then
		kill -TERM "$EP" 2>/dev/null
		wait_exit 35 || kill -KILL "$EP" 2>/dev/null
	fi
	local f p
	for f in "$T"/fake/*.pids; do
		[ -e "$f" ] || continue
		while read -r p; do kill -KILL "$p" 2>/dev/null || true; done <"$f"
	done
	rm -rf "$T"
	T=""
}

selected() { [ -z "$FILTER" ] || [[ "$1" == *"$FILTER"* ]]; }

# --- cases ------------------------------------------------------------------------------------

t_first_boot() {
	new_case first_boot
	# A leftover from a crash: an empty secret and a stale temporary file.
	mkdir -p "$T/work/bootstrap"
	: >"$T/work/bootstrap/internal-token"
	echo stale >"$T/work/bootstrap/internal-token.tmp"
	own_all
	start DB_PASSWD=hunter2 FORGEJO__database__PASSWD=hunter3 "ASC_DATABASE_URL=sqlite://$T/work/asc/custom.db?mode=rwc"
	wait_ready || { end_case; return; }
	local b="$T/work/bootstrap"
	eq "internal-token length" 48 "$(wc -c <"$b/internal-token" | tr -d ' ')"
	eq "webhook-secret length" 48 "$(wc -c <"$b/webhook-secret" | tr -d ' ')"
	eq "secret-key length" 64 "$(wc -c <"$b/secret-key" | tr -d ' ')"
	eq "internal-token mode" 400 "$(mode_of "$b/internal-token")"
	eq "secret-key mode" 400 "$(mode_of "$b/secret-key")"
	eq "bootstrap mode" 700 "$(mode_of "$b")"
	eq "asc dir mode" 700 "$(mode_of "$T/work/asc")"
	eq "asc-serve.toml mode" 600 "$(mode_of "$T/work/asc/asc-serve.toml")"
	check_not "stale tmp removed" test -e "$b/internal-token.tmp"
	check "owned by the volume owner" test "$(stat -c %u "$b/secret-key")" = "$OWNER_UID"

	local toml="$T/work/asc/asc-serve.toml"
	has "toml listen" "$toml" "listen = \"127.0.0.1:$AP\""
	has "toml store" "$toml" "store = 'file://$T/work/asc/objects'"
	has "toml db" "$toml" "db = 'sqlite://$T/work/asc/custom.db?mode=rwc'"
	has "toml bridge" "$toml" "bridge_promote = true"
	has "toml hub url" "$toml" "url = \"http://127.0.0.1:$HP\""
	has "toml token file" "$toml" "internal_token_file = \"$b/internal-token\""
	has "toml git" "$toml" "enabled = true"
	hasnt "toml holds no secret" "$toml" "$(cat "$b/internal-token")"

	# order: migrate, then the hub, then asc serve
	eq "events" "asc-migrate hub-start asc-serve" "$(tr '\n' ' ' <"$T/fake/events.log" | sed 's/ $//')"

	local he="hub.env"
	eq "hub ENABLED" true "$(env_of $he FORGEJO__asc__ENABLED)"
	eq "hub SERVER_URL" "http://127.0.0.1:$AP" "$(env_of $he FORGEJO__asc__SERVER_URL)"
	eq "hub PUBLIC_URL" "https://hub.test" "$(env_of $he FORGEJO__asc__PUBLIC_URL)"
	eq "hub WRITER" asc "$(env_of $he FORGEJO__asc__WRITER)"
	eq "hub NEW_REPOSITORIES" asc "$(env_of $he FORGEJO__asc__NEW_REPOSITORIES)"
	eq "hub BRIDGE" eager "$(env_of $he FORGEJO__asc__BRIDGE)"
	eq "hub token uri" "file://$b/internal-token" "$(env_of $he FORGEJO__security__INTERNAL_TOKEN_URI)"
	eq "hub INSTALL_LOCK" true "$(env_of $he INSTALL_LOCK)"
	eq "hub DISABLE_REGISTRATION" true "$(env_of $he DISABLE_REGISTRATION)"
	eq "hub SSH_DOMAIN" hub.test "$(env_of $he SSH_DOMAIN)"
	eq "hub SSH_LISTEN_PORT" 2222 "$(env_of $he SSH_LISTEN_PORT)"
	eq "hub APP_NAME" "ASC Hub" "$(env_of $he APP_NAME)"
	eq "hub SECRET_KEY" "$(cat "$b/secret-key")" "$(env_of $he SECRET_KEY)"
	eq "hub gets no asc database url" "" "$(env_of $he ASC_DATABASE_URL)"
	eq "hub gets no loopback opt-in" "" "$(env_of $he ASC_ALLOW_LOOPBACK_HTTP)"
	eq "hub keeps its own db password" hunter2 "$(env_of $he DB_PASSWD)"

	local ae="asc.env"
	eq "asc loopback opt-in" 1 "$(env_of $ae ASC_ALLOW_LOOPBACK_HTTP)"
	eq "asc config" "$toml" "$(env_of $ae ASC_SERVE_CONFIG)"
	eq "asc home" "$T/work/asc/home" "$(env_of $ae HOME)"
	eq "asc gets no SECRET_KEY" "" "$(env_of $ae SECRET_KEY)"
	eq "asc gets no DB_PASSWD" "" "$(env_of $ae DB_PASSWD)"
	eq "asc gets no hub database password" "" "$(env_of $ae FORGEJO__database__PASSWD)"
	check_not "asc gets no FORGEJO__ keys" grep -q "^FORGEJO__" "$T/fake/$ae"
	check_not "asc gets no HTTP_PORT of the hub" grep -q "^HTTP_PORT=" "$T/fake/$ae"

	# nothing secret in the log
	for s in "$(cat "$b/internal-token")" "$(cat "$b/secret-key")" hunter2 hunter3; do
		hasnt "log holds no secret" "$T/out.1" "$s"
	done

	kill -TERM "$EP"
	wait_exit 35
	eq "exit status after SIGTERM" 0 "$RC"
	check_not "hub stopped" alive "$(last_pid hub.pids)"
	check_not "asc stopped" alive "$(last_pid asc.pids)"
	end_case
}

t_restart_idempotent() {
	new_case restart
	start
	wait_ready || { end_case; return; }
	local b="$T/work/bootstrap" sum1 sum2
	sum1="$(cat "$b/internal-token" "$b/webhook-secret" "$b/secret-key" "$T/work/asc/asc-serve.toml" | sha256sum)"
	kill -INT "$EP"
	wait_exit 35
	eq "exit status after SIGINT" 0 "$RC"
	check_not "asc stopped" alive "$(last_pid asc.pids)"
	start
	wait_ready || { end_case; return; }
	sum2="$(cat "$b/internal-token" "$b/webhook-secret" "$b/secret-key" "$T/work/asc/asc-serve.toml" | sha256sum)"
	eq "secrets and config unchanged by a restart" "$sum1" "$sum2"
	eq "migrate ran at each start" 2 "$(count_events asc-migrate)"
	eq "hub started twice" 2 "$(count_events hub-start)"
	# a changed variable reaches the configuration
	kill -TERM "$EP"
	wait_exit 35
	start ASC_CACHE_MEMORY=512MiB
	wait_ready || { end_case; return; }
	has "changed variable is written" "$T/work/asc/asc-serve.toml" 'memory = "512MiB"'
	end_case
}

t_admin_bootstrap() {
	new_case admin
	start ASC_HUB_ADMIN_EMAIL=boss@example.org ASC_HUB_ADMIN_USER=boss
	wait_ready || { end_case; return; }
	wait_for 10 test -e "$T/work/bootstrap/.admin-done" || fail "admin marker not written"
	local pw="$T/work/bootstrap/admin-password"
	eq "admin-password mode" 600 "$(mode_of "$pw")"
	has "admin-password content" "$pw" "generated random password"
	eq "one create call" 1 "$(grep -c 'admin user create' "$T/fake/forgejo.log")"
	has "create arguments" "$T/fake/forgejo.log" "admin user create --admin --username boss --email boss@example.org --random-password --must-change-password=true"
	hasnt "no password on the command line" "$T/fake/forgejo.log" "--password"
	eq "forgejo gets no asc database url" "" "$(env_of forgejo.env ASC_DATABASE_URL)"
	hasnt "log has no password" "$T/out.1" "Pw-0123456789"
	kill -TERM "$EP"
	wait_exit 35
	rm -f "$pw"
	start ASC_HUB_ADMIN_EMAIL=boss@example.org ASC_HUB_ADMIN_USER=boss
	wait_ready || { end_case; return; }
	sleep 1
	eq "no second create after a restart" 1 "$(grep -c 'admin user create' "$T/fake/forgejo.log")"
	check_not "no new password file" test -e "$pw"
	end_case

	new_case admin_exists
	echo boss >"$T/fake/users"
	own_all
	start ASC_HUB_ADMIN_EMAIL=boss@example.org ASC_HUB_ADMIN_USER=boss
	wait_ready || { end_case; return; }
	wait_for 10 test -e "$T/work/bootstrap/.admin-done" || fail "marker not written for an existing user"
	check_not "no create for an existing user" grep -q 'admin user create' "$T/fake/forgejo.log"
	check_not "no password file" test -e "$T/work/bootstrap/admin-password"
	end_case

	new_case admin_fails_then_retries
	touch "$T/fake/forgejo.fail"
	own_all
	start ASC_HUB_ADMIN_EMAIL=boss@example.org
	wait_ready || { end_case; return; }
	wait_for 10 grep -q 'warning: creating the admin user failed' "$T/out.1" || fail "no warning for a failed create"
	check_not "no marker after a failure" test -e "$T/work/bootstrap/.admin-done"
	check_not "no password file after a failure" test -e "$T/work/bootstrap/admin-password"
	check_not "no partial password file" test -e "$T/work/bootstrap/admin-password.tmp"
	check "container keeps running" alive "$EP"
	kill -TERM "$EP"
	wait_exit 35
	rm -f "$T/fake/forgejo.fail"
	start ASC_HUB_ADMIN_EMAIL=boss@example.org
	wait_ready || { end_case; return; }
	wait_for 10 test -e "$T/work/bootstrap/.admin-done" || fail "admin not created at the retry"
	has "default user name" "$T/fake/forgejo.log" "--username asc-admin"
	end_case
}

t_admin_invalid() {
	local email user
	for case_ in "email|bad email@example.org|" "email|nobody|" "user|ok@example.org|../x" "user|ok@example.org|a b" "user|ok@example.org|-lead"; do
		IFS='|' read -r kind email user <<<"$case_"
		new_case "admin_invalid_$kind"
		if [ -n "$user" ]; then
			start "ASC_HUB_ADMIN_EMAIL=$email" "ASC_HUB_ADMIN_USER=$user"
		else
			start "ASC_HUB_ADMIN_EMAIL=$email"
		fi
		wait_exit 10
		eq "invalid admin ($email/$user) is refused" 1 "$RC"
		eq "nothing was started" 0 "$(count_events hub-start)"
		check_not "no secrets written before validation" test -e "$T/work/bootstrap"
		end_case
	done
}

t_kill_either() {
	new_case kill_hub
	start
	wait_ready || { end_case; return; }
	kill -KILL "$(last_pid hub.pids)"
	wait_exit 40
	check "exits non-zero when the hub dies" test "$RC" != 0 -a "$RC" != timeout
	check_not "asc is stopped with it" alive "$(last_pid asc.pids)"
	has "says why" "$T/out.1" "a process exited"
	end_case

	new_case kill_asc
	start
	wait_ready || { end_case; return; }
	kill -KILL "$(last_pid asc.pids)"
	wait_exit 40
	check "exits non-zero when asc dies" test "$RC" != 0 -a "$RC" != timeout
	check_not "hub is stopped with it" alive "$(last_pid hub.pids)"
	end_case

	new_case asc_exits_cleanly
	touch "$T/fake/asc.exit-immediately"
	start
	wait_exit 40
	eq "a failing asc stops the container with its status" 4 "$RC"
	check_not "hub is stopped" alive "$(last_pid hub.pids)"
	end_case
}

t_sigterm_during_start() {
	new_case term_during_start
	echo 30 >"$T/fake/hub.delay"
	start
	wait_for 10 test -s "$T/fake/hub.pids" || fail "hub not started"
	kill -TERM "$EP"
	wait_exit 35
	eq "SIGTERM while the hub starts gives 0" 0 "$RC"
	eq "asc serve never started" 0 "$(count_events asc-serve)"
	check_not "hub stopped" alive "$(last_pid hub.pids)"
	end_case
}

t_start_failures() {
	new_case hub_exits_early
	touch "$T/fake/hub.exit-immediately"
	start
	wait_exit 20
	eq "hub exiting during start" 1 "$RC"
	has "says so" "$T/out.1" "the hub exited during start (status 3)"
	eq "asc serve never started" 0 "$(count_events asc-serve)"
	end_case

	new_case hub_timeout
	echo 60 >"$T/fake/hub.delay"
	start ASC_HUB_START_TIMEOUT=3
	wait_exit 30
	eq "hub that never answers" 1 "$RC"
	has "says so" "$T/out.1" "did not answer"
	check_not "hub stopped" alive "$(last_pid hub.pids)"
	eq "asc serve never started" 0 "$(count_events asc-serve)"
	end_case

	new_case migrate_fails
	touch "$T/fake/asc.migrate-fail"
	start
	wait_exit 20
	eq "failed migration" 1 "$RC"
	has "says so" "$T/out.1" "asc serve migrate failed (status 5)"
	eq "hub never started" 0 "$(count_events hub-start)"
	end_case
}

t_volume() {
	new_case volume_unwritable
	chmod 555 "$T/work"
	start
	wait_exit 10
	eq "read-only volume is refused" 1 "$RC"
	has "names the owner uid" "$T/out.1" "must be owned by uid 1000"
	has "names the volume" "$T/out.1" "$T/work"
	chmod 755 "$T/work"
	end_case

	new_case volume_missing
	rmdir "$T/work"
	start
	wait_exit 10
	eq "missing volume is refused" 1 "$RC"
	end_case

	if [ "$(id -u)" = 0 ]; then
		new_case volume_foreign_owner
		mkdir -p "$T/work/bootstrap"
		chown root:root "$T/work/bootstrap"
		start
		wait_exit 10
		eq "root-owned state is refused" 1 "$RC"
		has "names the file" "$T/out.1" "not owned by uid"
		end_case

		new_case refuses_root
		env -i "PATH=$PATH" "GITEA_WORK_DIR=$T/work" bash "$T/bin/asc-hub-entrypoint" >"$T/out.1" 2>&1
		eq "root is refused" 1 "$?"
		has "says so" "$T/out.1" "do not run as root"
		end_case
	fi

	new_case second_instance
	start
	wait_ready || { end_case; return; }
	local first="$EP"
	start
	local second="$EP"
	EP="$second"
	wait_exit 10
	eq "second hub on the same volume is refused" 1 "$RC"
	has "says so" "$T/out.2" "another ASC Hub is running"
	check "the first one is unharmed" alive "$first"
	check "and still ready" asc_up
	EP="$first"
	end_case
}

t_backup_lock() {
	new_case backup_lock
	cp "$REPO/docker/allinone/usr/local/bin/asc-hub-backup" "$T/bin/"
	chmod 755 "$T/bin/asc-hub-backup"
	mkdir -p "$T/out"
	own_all
	start
	wait_ready || { end_case; return; }
	"${RUNAS[@]}" bash "$T/bin/asc-hub-backup" create --data "$T/work" --out "$T/out/b.tar.gz" >"$T/out.backup" 2>&1
	eq "backup is refused while the container runs" 1 "$?"
	has "tells to stop it" "$T/out.backup" "stop it first"
	# the lock is held by the hub and asc, not only by the supervisor: kill the supervisor alone
	kill -KILL "$EP"
	wait "$EP" 2>/dev/null
	"${RUNAS[@]}" bash "$T/bin/asc-hub-backup" create --data "$T/work" --out "$T/out/b.tar.gz" >"$T/out.backup" 2>&1
	eq "backup is refused while an orphaned child still runs" 1 "$?"
	kill -KILL "$(last_pid hub.pids)" "$(last_pid asc.pids)" 2>/dev/null
	sleep 0.5
	"${RUNAS[@]}" bash "$T/bin/asc-hub-backup" create --data "$T/work" --out "$T/out/b.tar.gz" >"$T/out.backup" 2>&1
	eq "backup works when everything has stopped" 0 "$?"
	check "archive written" test -s "$T/out/b.tar.gz"
	end_case
}

t_command_passthrough() {
	new_case passthrough
	run_args forgejo admin user list
	eq "command exit status" 0 "$RC"
	has "the hub entrypoint gets the command" "$T/fake/hub.log" "args=forgejo admin user list"
	eq "no supervisor events" 0 "$(count_events asc-migrate)"
	check_not "no state written" test -e "$T/work/bootstrap"
	end_case
}

# bad CASE ENV...: the container refuses to start with that environment.
t_refused() {
	local name="$1" want="$2"
	shift 2
	new_case "refused_$name"
	start "$@"
	wait_exit 10
	eq "$name is refused" 1 "$RC"
	has "$name message" "$T/out.1" "$want"
	eq "$name: the hub was not started" 0 "$(count_events hub-start)"
	end_case
}

t_validation() {
	t_refused bridge "BRIDGE must be eager, lazy or off" FORGEJO__asc__BRIDGE=bogus
	t_refused s3_plain_http "ASC_S3_ENDPOINT is not valid" ASC_S3_ENDPOINT=http://s3.example.org ASC_S3_ACCESS_KEY_ID=k ASC_S3_SECRET_ACCESS_KEY=s
	t_refused s3_query_injection "ASC_S3_ENDPOINT is not valid" "ASC_S3_ENDPOINT=https://s3.example.org/x?path_style=false&y=1" ASC_S3_ACCESS_KEY_ID=k ASC_S3_SECRET_ACCESS_KEY=s
	t_refused s3_without_keys "needs ASC_S3_ACCESS_KEY_ID" ASC_S3_ENDPOINT=https://s3.example.org
	t_refused s3_bucket "ASC_S3_BUCKET is not valid" ASC_S3_BUCKET=Bad_Bucket
	t_refused git_flag "ASC_GIT_ENABLED is not valid" ASC_GIT_ENABLED=maybe
	t_refused db_scheme "ASC_DATABASE_URL must start with" ASC_DATABASE_URL=mysql://u:p@h/db
	t_refused quote "must not contain quotes" "ASC_CACHE_DISK=2GiB\"x"
	t_refused log_format "ASC_LOG_FORMAT is not valid" ASC_LOG_FORMAT=xml
	t_refused http_root "must use https unless the host is loopback" ROOT_URL=http://hub.example.org/
	t_refused ftp_root "ROOT_URL must be an absolute http or https URL" ROOT_URL=ftp://hub.example.org/
	t_refused http_port "HTTP port is not a number" HTTP_PORT=abc
	t_refused asc_port "ASC_HUB_ASC_PORT" ASC_HUB_ASC_PORT=0
	t_refused timeout "ASC_HUB_START_TIMEOUT is not valid" ASC_HUB_START_TIMEOUT=abc
}

t_variants() {
	new_case s3_postgres_lazy
	start ASC_S3_ENDPOINT=https://s3.example.org:3900 ASC_S3_BUCKET=my-bucket ASC_S3_REGION=eu-west-1 \
		ASC_S3_ACCESS_KEY_ID=AKIDTEST ASC_S3_SECRET_ACCESS_KEY=s3cr3tkey \
		"ASC_DATABASE_URL=postgres://asc:dbpass@db.internal:5432/asc?sslmode=require" \
		FORGEJO__asc__BRIDGE=LAZY ASC_GIT_ENABLED=true
	wait_ready || { end_case; return; }
	local toml="$T/work/asc/asc-serve.toml"
	has "s3 store" "$toml" "store = 's3://my-bucket?endpoint=https://s3.example.org:3900&region=eu-west-1&path_style=true'"
	has "postgres db" "$toml" "db = 'postgres://asc:dbpass@db.internal:5432/asc?sslmode=require'"
	has "lazy: no promote" "$toml" "bridge_promote = false"
	has "lazy: git layer from the variable" "$toml" "enabled = true"
	eq "asc has the S3 key id" AKIDTEST "$(env_of asc.env AWS_ACCESS_KEY_ID)"
	eq "asc has the S3 secret" s3cr3tkey "$(env_of asc.env AWS_SECRET_ACCESS_KEY)"
	eq "asc has the region" eu-west-1 "$(env_of asc.env AWS_REGION)"
	eq "hub has no S3 key id" "" "$(env_of hub.env ASC_S3_ACCESS_KEY_ID)"
	eq "hub has no S3 secret" "" "$(env_of hub.env ASC_S3_SECRET_ACCESS_KEY)"
	eq "hub has no asc database url" "" "$(env_of hub.env ASC_DATABASE_URL)"
	hasnt "log holds no S3 secret" "$T/out.1" s3cr3tkey
	hasnt "log holds no database password" "$T/out.1" dbpass
	eq "toml mode with credentials in it" 600 "$(mode_of "$toml")"
	end_case

	new_case lazy_default_git_off
	start ASC_HUB_BRIDGE=lazy
	wait_ready || { end_case; return; }
	has "lazy: no promote" "$T/work/asc/asc-serve.toml" "bridge_promote = false"
	has "lazy: git layer off by default" "$T/work/asc/asc-serve.toml" "enabled = false"
	end_case

	new_case no_root_url_warns
	NO_ROOT_URL=1 start
	wait_ready || { NO_ROOT_URL=; end_case; return; }
	has "warns" "$T/out.1" "ROOT_URL is not set"
	eq "falls back to a loopback address" "http://localhost:3000" "$(env_of hub.env FORGEJO__asc__PUBLIC_URL)"
	NO_ROOT_URL=
	end_case

	new_case ipv6_root_url
	start "ROOT_URL=https://user@[2001:db8::1]:8443/sub/"
	wait_ready || { end_case; return; }
	eq "ssh domain from an IPv6 root url" "2001:db8::1" "$(env_of hub.env SSH_DOMAIN)"
	end_case
}

t_healthcheck() {
	new_case healthcheck
	local -a envs=("PATH=$PATH" "HTTP_PORT=$HP" "ASC_HUB_ASC_PORT=$AP")
	local hub asc
	check_not "nothing runs" env -i "${envs[@]}" "$T/bin/asc-hub-healthcheck"
	python3 "$T/bin/fakeserve.py" "$HP" 0 &
	hub=$!
	wait_for 10 hub_up
	check_not "only the hub answers" env -i "${envs[@]}" "$T/bin/asc-hub-healthcheck"
	python3 "$T/bin/fakeserve.py" "$AP" 0 &
	asc=$!
	wait_for 10 asc_up
	check "both answer" env -i "${envs[@]}" "$T/bin/asc-hub-healthcheck"
	kill -TERM "$hub"
	wait "$hub" 2>/dev/null
	check_not "only asc answers" env -i "${envs[@]}" "$T/bin/asc-hub-healthcheck"
	kill -TERM "$asc"
	wait "$asc" 2>/dev/null
	end_case
}

cleanup_all() { [ -z "$T" ] || end_case; }
trap cleanup_all EXIT

for t in t_first_boot t_restart_idempotent t_admin_bootstrap t_admin_invalid t_kill_either t_sigterm_during_start t_start_failures t_volume t_backup_lock t_command_passthrough t_validation t_variants t_healthcheck; do
	if selected "$t"; then
		"$t"
	fi
done

printf 'entrypoint tests: %d passed, %d failed\n' "$PASS" "$FAILED"
[ "$FAILED" -eq 0 ]
