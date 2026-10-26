#!/bin/bash
# Tests for docker/allinone/usr/local/bin/asc-hub-backup. Needs Linux, GNU tar, flock, python3.
# As root the tool runs as uid 1000 (setpriv), like the image does.
#
#   tests/allinone/backup_test.sh [name-filter]

set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
TOOL_SRC="$REPO/docker/allinone/usr/local/bin/asc-hub-backup"
FILTER="${1:-}"

PASS=0
FAILED=0
CASE_NAME=""
T=""
RUNAS=()
if [ "$(id -u)" = 0 ]; then
	RUNAS=(setpriv --reuid=1000 --regid=1000 --clear-groups)
fi

ok() { PASS=$((PASS + 1)); }
fail() {
	FAILED=$((FAILED + 1))
	printf 'FAIL [%s] %s\n' "$CASE_NAME" "$*" >&2
	[ ! -f "$T/last.out" ] || tail -n 8 "$T/last.out" >&2
}
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
eq() { if [ "$2" = "$3" ]; then ok; else fail "$1: want '$2', got '$3'"; fi; }
has() { if grep -qF -- "$3" "$2" 2>/dev/null; then ok; else fail "$1: '$3' not in $2"; fi; }
hasnt() { if grep -qF -- "$3" "$2" 2>/dev/null; then fail "$1: '$3' is in $2"; else ok; fi; }
mode_of() { stat -c %a "$1" 2>/dev/null; }

own() { if [ "$(id -u)" = 0 ]; then chown -R 1000:1000 "$T"; fi; }

new_case() {
	CASE_NAME="$1"
	T="$(mktemp -d /tmp/asc-hub-bk.XXXXXX)"
	mkdir -p "$T/bin" "$T/out"
	cp "$TOOL_SRC" "$T/bin/asc-hub-backup"
	chmod 755 "$T/bin/asc-hub-backup"
	chmod 755 "$T"
	make_volume "$T/vol"
	mkdir -p "$T/empty"
	own
}

end_case() {
	[ -z "$T" ] || { chmod -R u+rwX "$T" 2>/dev/null; rm -rf "$T"; }
	T=""
}

# make_volume DIR: a small volume with the layout of the image.
make_volume() {
	local v="$1"
	mkdir -p "$v/custom/conf" "$v/data" "$v/git/repositories/alice/repo.git/objects/aa" "$v/bootstrap" \
		"$v/asc/objects/ab" "$v/asc/cache" "$v/asc/home" "$v/emptydir" "$v/git/lfs"
	printf '[database]\nDB_TYPE = sqlite3\n' >"$v/custom/conf/app.ini"
	echo "hub db" >"$v/data/gitea.db"
	echo "ref: refs/heads/main" >"$v/git/repositories/alice/repo.git/HEAD"
	echo "object" >"$v/git/repositories/alice/repo.git/objects/aa/bbcc"
	echo "name with spaces" >"$v/data/a  b.txt"
	for s in internal-token webhook-secret secret-key; do
		echo "secret-$s" >"$v/bootstrap/$s"
		chmod 400 "$v/bootstrap/$s"
	done
	echo "pw" >"$v/bootstrap/admin-password"
	chmod 600 "$v/bootstrap/admin-password"
	chmod 700 "$v/bootstrap"
	echo "asc db" >"$v/asc/asc.db"
	echo "obj" >"$v/asc/objects/ab/cd"
	echo "cached" >"$v/asc/cache/junk"
	printf "listen = \"127.0.0.1:7890\"\nstore = 'file://%s/asc/objects'\ndb = 'sqlite://%s/asc/asc.db?mode=rwc'\n" "$v" "$v" >"$v/asc/asc-serve.toml"
	: >"$v/.asc-hub.lock"
}

tool() { # args: run the tool as the container user
	"${RUNAS[@]}" bash "$T/bin/asc-hub-backup" "$@" >"$T/last.out" 2>&1
	RC=$?
}

selected() { [[ "$1" == *"$FILTER"* ]] || [ -z "$FILTER" ]; }

# evil_archive NAME KIND: an archive that restore must refuse.
evil_archive() {
	python3 - "$T/out/$1.tar.gz" "$2" <<'PY'
import io, sys, tarfile
path, kind = sys.argv[1], sys.argv[2]
good = b"format=asc-hub-backup/1\ncreated=2026-01-01T00:00:00Z\nhub_db=sqlite\nasc_db=sqlite\nasc_store=file\n"
with tarfile.open(path, "w:gz") as t:
    def add(name, data=b"x", type_=tarfile.REGTYPE, link=""):
        i = tarfile.TarInfo(name)
        i.type = type_
        i.linkname = link
        i.mode = 0o600
        i.size = len(data) if type_ == tarfile.REGTYPE else 0
        t.addfile(i, io.BytesIO(data) if type_ == tarfile.REGTYPE else None)
    if kind != "no-manifest":
        add("ASC-HUB-BACKUP", b"format=other/9\n" if kind == "bad-format" else good)
    add("./ok.txt")
    if kind == "dotdot":
        add("./../escape.txt")
    elif kind == "dotdot-mid":
        add("./a/../../escape.txt")
    elif kind == "absolute":
        add("/tmp/escape.txt")
    elif kind == "symlink":
        add("./link", type_=tarfile.SYMTYPE, link="/etc")
    elif kind == "hardlink":
        add("./hard", type_=tarfile.LNKTYPE, link="./ok.txt")
    elif kind == "device":
        add("./dev", type_=tarfile.CHRTYPE)
    elif kind == "fifo":
        add("./fifo", type_=tarfile.FIFOTYPE)
    elif kind == "backslash":
        add("./a\\b")
PY
}

# --- cases ------------------------------------------------------------------------------------

t_roundtrip() {
	new_case roundtrip
	tool create --data "$T/vol" --out "$T/out/b.tar.gz"
	eq "create status" 0 "$RC"
	eq "archive mode" 600 "$(mode_of "$T/out/b.tar.gz")"
	check "checksum file" test -s "$T/out/b.tar.gz.sha256"
	check "checksum matches" bash -c "cd '$T/out' && sha256sum -c b.tar.gz.sha256"
	check_not "no partial file left" test -e "$T/out/.b.tar.gz.part"
	check_not "the cache is not archived" bash -c "tar -tzf '$T/out/b.tar.gz' | grep -q 'asc/cache/junk'"
	check_not "the lock file is not archived" bash -c "tar -tzf '$T/out/b.tar.gz' | grep -q 'asc-hub.lock'"
	eq "manifest first" ASC-HUB-BACKUP "$(tar -tzf "$T/out/b.tar.gz" | sed -n 1p)"

	tool verify --in "$T/out/b.tar.gz"
	eq "verify status" 0 "$RC"
	has "verify prints the manifest" "$T/last.out" "format=asc-hub-backup/1"
	has "verify prints hub db" "$T/last.out" "hub_db=sqlite"

	tool restore --data "$T/empty" --in "$T/out/b.tar.gz"
	eq "restore status" 0 "$RC"
	check "restored tree equals the original" diff -r --exclude=asc-hub.lock --exclude=.asc-hub.lock --exclude=cache "$T/vol" "$T/empty"
	eq "secret mode kept" 400 "$(mode_of "$T/empty/bootstrap/secret-key")"
	eq "admin password mode kept" 600 "$(mode_of "$T/empty/bootstrap/admin-password")"
	eq "bootstrap mode kept" 700 "$(mode_of "$T/empty/bootstrap")"
	check "empty directory kept" test -d "$T/empty/emptydir"
	check "name with spaces kept" test -f "$T/empty/data/a  b.txt"
	check "cache directory exists, empty" test -d "$T/empty/asc/cache" -a ! -e "$T/empty/asc/cache/junk"
	check_not "manifest is not restored" test -e "$T/empty/ASC-HUB-BACKUP"
	check "owner is the container user" test "$(stat -c %u "$T/empty/bootstrap/secret-key")" = "$([ "$(id -u)" = 0 ] && echo 1000 || id -u)"
	has "restore explains the next step" "$T/last.out" "Start the container"

	# the source volume is untouched
	check "source still has its cache" test -f "$T/vol/asc/cache/junk"
	end_case
}

t_streams() {
	new_case streams
	"${RUNAS[@]}" bash "$T/bin/asc-hub-backup" create --data "$T/vol" --out - >"$T/out/s.tar.gz" 2>"$T/last.out"
	eq "create to stdout" 0 "$?"
	check_not "no checksum for a stream" test -e "$T/out/s.tar.gz.sha256"
	"${RUNAS[@]}" bash "$T/bin/asc-hub-backup" restore --data "$T/empty" --in - <"$T/out/s.tar.gz" >"$T/last.out" 2>&1
	eq "restore from stdin" 0 "$?"
	check "tree equals" diff -r --exclude=.asc-hub.lock --exclude=cache "$T/vol" "$T/empty"
	check_not "spool removed" test -e "$T/empty/.restore-archive.tmp"
	"${RUNAS[@]}" bash "$T/bin/asc-hub-backup" verify --in - <"$T/out/s.tar.gz" >"$T/last.out" 2>&1
	eq "verify from stdin" 0 "$?"
	end_case
}

t_refusals() {
	new_case create_refusals
	tool create --data "$T/vol" --out "$T/out/b.tar.gz"
	tool create --data "$T/vol" --out "$T/out/b.tar.gz"
	eq "no overwrite of a backup" 1 "$RC"
	has "says so" "$T/last.out" "does not overwrite"
	tool create --data "$T/vol" --out "$T/vol/inside.tar.gz"
	eq "archive inside the volume" 1 "$RC"
	tool create --data "$T/empty" --out "$T/out/c.tar.gz"
	eq "not a hub volume" 1 "$RC"
	has "says so" "$T/last.out" "does not look like an ASC Hub volume"
	tool create --data "$T/missing" --out "$T/out/c.tar.gz"
	eq "missing directory" 1 "$RC"
	check_not "no archive after a refusal" test -e "$T/out/c.tar.gz"
	ln -s /etc "$T/vol/data/link"
	own
	tool create --data "$T/vol" --out "$T/out/d.tar.gz"
	eq "symlink in the volume" 1 "$RC"
	has "names the link" "$T/last.out" "data/link"
	check_not "no archive after a refusal" test -e "$T/out/d.tar.gz"
	end_case

	new_case running_hub
	# a running hub holds the lock for as long as it lives
	"${RUNAS[@]}" flock -n "$T/vol/.asc-hub.lock" sleep 60 &
	local holder=$!
	sleep 0.5
	tool create --data "$T/vol" --out "$T/out/b.tar.gz"
	eq "create while the hub runs" 1 "$RC"
	has "tells to stop it" "$T/last.out" "stop it first"
	"${RUNAS[@]}" flock -n "$T/empty/.asc-hub.lock" sleep 60 &
	local holder2=$!
	sleep 0.5
	tool restore --data "$T/empty" --in "$T/out/none.tar.gz"
	eq "restore while a hub runs" 1 "$RC"
	pkill -P "$holder" 2>/dev/null
	pkill -P "$holder2" 2>/dev/null
	kill "$holder" "$holder2" 2>/dev/null
	wait "$holder" "$holder2" 2>/dev/null
	end_case

	new_case restore_nonempty
	tool create --data "$T/vol" --out "$T/out/b.tar.gz"
	echo keep >"$T/empty/precious"
	own
	tool restore --data "$T/empty" --in "$T/out/b.tar.gz"
	eq "non-empty target" 1 "$RC"
	has "says so" "$T/last.out" "is not empty"
	eq "target untouched" "keep" "$(cat "$T/empty/precious")"
	check_not "nothing extracted" test -e "$T/empty/bootstrap"
	rm "$T/empty/precious"
	mkdir "$T/empty/lost+found"
	own
	tool restore --data "$T/empty" --in "$T/out/b.tar.gz"
	eq "lost+found alone does not block a restore" 0 "$RC"
	end_case

	new_case usage
	tool
	eq "no arguments" 2 "$RC"
	tool create --data
	eq "missing value" 2 "$RC"
	tool frobnicate
	eq "unknown command" 2 "$RC"
	tool create --data "$T/vol"
	eq "create needs --out" 2 "$RC"
	end_case
}

t_tamper() {
	new_case tamper
	tool create --data "$T/vol" --out "$T/out/b.tar.gz"

	cp "$T/out/b.tar.gz" "$T/out/flip.tar.gz"
	cp "$T/out/b.tar.gz.sha256" "$T/out/flip.tar.gz.sha256"
	printf '\x00' | dd of="$T/out/flip.tar.gz" bs=1 seek=100 count=1 conv=notrunc 2>/dev/null
	tool verify --in "$T/out/flip.tar.gz"
	eq "a flipped byte is caught" 1 "$RC"

	cp "$T/out/b.tar.gz" "$T/out/trunc.tar.gz"
	truncate -s -200 "$T/out/trunc.tar.gz"
	tool verify --in "$T/out/trunc.tar.gz"
	eq "a truncated archive is caught" 1 "$RC"
	tool restore --data "$T/empty" --in "$T/out/trunc.tar.gz"
	eq "restore of a truncated archive" 1 "$RC"
	check_not "nothing extracted from a truncated archive" test -e "$T/empty/bootstrap"

	# another valid archive under the old checksum
	evil_archive other no-evil
	cp "$T/out/other.tar.gz" "$T/out/swap.tar.gz"
	cp "$T/out/b.tar.gz.sha256" "$T/out/swap.tar.gz.sha256"
	own
	tool verify --in "$T/out/swap.tar.gz"
	eq "checksum mismatch is caught" 1 "$RC"
	has "says so" "$T/last.out" "checksum mismatch"

	echo "not gzip" >"$T/out/plain.tar.gz"
	tool verify --in "$T/out/plain.tar.gz"
	eq "not a gzip file" 1 "$RC"
	tool verify --in "$T/out/absent.tar.gz"
	eq "absent file" 1 "$RC"
	end_case
}

t_evil() {
	local kind
	for kind in dotdot dotdot-mid absolute symlink hardlink device fifo backslash no-manifest bad-format; do
		new_case "evil_$kind"
		evil_archive "$kind" "$kind"
		own
		tool verify --in "$T/out/$kind.tar.gz"
		eq "verify refuses $kind" 1 "$RC"
		tool restore --data "$T/empty" --in "$T/out/$kind.tar.gz"
		eq "restore refuses $kind" 1 "$RC"
		eq "target stays empty after $kind" "" "$(find "$T/empty" -mindepth 1 -maxdepth 1 ! -name .asc-hub.lock -printf '%f ')"
		check_not "nothing escaped ($kind)" test -e "$T/out/escape.txt" -o -e /tmp/escape.txt
		end_case
	done
	new_case evil_control
	evil_archive control no-evil
	own
	tool verify --in "$T/out/control.tar.gz"
	eq "the control archive passes" 0 "$RC"
	end_case
}

t_external() {
	new_case external
	printf '[database]\nDB_TYPE = postgres\nHOST = db:5432\n' >"$T/vol/custom/conf/app.ini"
	printf "listen = \"127.0.0.1:7890\"\nstore = 's3://b?endpoint=https://s3.example.org&region=eu&path_style=true'\ndb = 'postgres://u:p@db/asc'\n" >"$T/vol/asc/asc-serve.toml"
	own
	tool create --data "$T/vol" --out "$T/out/x.tar.gz"
	eq "create with external services" 0 "$RC"
	has "hub database named" "$T/last.out" "the hub database (postgres) is outside the volume"
	has "asc database named" "$T/last.out" "the asc serve database is outside the volume"
	has "bucket named" "$T/last.out" "objects are in an S3 bucket"
	hasnt "no database password in the output" "$T/last.out" "u:p@"
	tool verify --in "$T/out/x.tar.gz"
	has "manifest says postgres" "$T/last.out" "hub_db=postgres"
	has "manifest says external" "$T/last.out" "asc_db=external"
	has "manifest says s3" "$T/last.out" "asc_store=s3"
	tool restore --data "$T/empty" --in "$T/out/x.tar.gz"
	has "restore repeats the note" "$T/last.out" "NOT in this archive"
	end_case
}

t_root_restore() {
	[ "$(id -u)" = 0 ] || return 0
	new_case root_restore
	tool create --data "$T/vol" --out "$T/out/b.tar.gz"
	chown root:root "$T/empty"
	bash "$T/bin/asc-hub-backup" restore --data "$T/empty" --in "$T/out/b.tar.gz" >"$T/last.out" 2>&1
	eq "restore as root" 0 "$?"
	eq "files are handed to uid 1000" 1000 "$(stat -c %u "$T/empty/bootstrap/secret-key")"
	eq "directories too" 1000 "$(stat -c %u "$T/empty/data")"
	end_case
}

trap 'end_case' EXIT

for t in t_roundtrip t_streams t_refusals t_tamper t_evil t_external t_root_restore; do
	selected "$t" && "$t"
done

printf 'backup tests: %d passed, %d failed\n' "$PASS" "$FAILED"
[ "$FAILED" -eq 0 ]
