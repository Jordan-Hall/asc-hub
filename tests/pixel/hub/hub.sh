#!/usr/bin/env bash
# Starts or stops the throw-away hub that the pixel harness photographs.
#   hub.sh up     create a fresh state directory, create the admin user, start the hub, wait for it
#   hub.sh stop   stop it, keep the state (the harness edits the database between stop and start)
#   hub.sh start  start it again on the kept state
#   hub.sh down   stop it and delete the state directory
# Environment: PIXEL_HUB_BIN (required for up and start), PIXEL_HUB_STATIC (repository root with templates, options
# and public/assets; default: three directories above this script), PIXEL_PORT (default 3610), PIXEL_RUN (default
# /tmp/pixel-run; keep it on a Linux file system), PIXEL_ADMIN / PIXEL_PASSWORD.
# The hub refuses to run as root, so under root the script drops to the user "nobody".
# PIXEL_ASC=serve runs `asc serve` next to the hub (PIXEL_ASC_BIN is the asc binary, PIXEL_ASC_PORT its port, default 7891) with
# the hub configured as the all-in-one image configures it (docker/allinone/usr/local/bin/asc-hub-entrypoint): new repositories
# are stored in asc, the writer is asc, the bridge is eager. PIXEL_ASC=1 only turns [asc] on in the hub (no asc server).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATIC="${PIXEL_HUB_STATIC:-$(cd "$here/../../.." && pwd)}"
PORT="${PIXEL_PORT:-3610}"
RUN="${PIXEL_RUN:-/tmp/pixel-run}"
ADMIN="${PIXEL_ADMIN:-maya}"
ASC_PORT="${PIXEL_ASC_PORT:-7891}"
ASC_ADMIN_PORT="${PIXEL_ASC_ADMIN_PORT:-$((ASC_PORT + 2000))}"
ASC_MODE="${PIXEL_ASC:-}"
BOOT="$RUN/boot"
ASC_DIR="$RUN/asc"
SERVE_CFG="$ASC_DIR/asc-serve.toml"
PASSWORD="${PIXEL_PASSWORD:-Pixel-Passw0rd-9137}"
# the state directory is a Linux path: a Windows path that went through a shell's path conversion (D:/..., C:/...) is a relative path
# for bash, and the hub would write its tokens and configuration into the directory the script was started from
case "$RUN" in
  /*) ;;
  *) echo "PIXEL_RUN must be an absolute Linux path (got '$RUN'); in Git Bash set MSYS_NO_PATHCONV=1" >&2; exit 2 ;;
esac
# a random string of $1 characters (of the set $2), for the secrets of one run: nothing the hub of a run holds is the same twice
rand() { LC_ALL=C tr -dc "${2:-A-Za-z0-9}" </dev/urandom | head -c "$1" || true; }
HUBENV="env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=$RUN/home TZ=UTC GITEA_WORK_DIR=$RUN GITEA_CUSTOM=$RUN/custom"
as_user() { if [ "$(id -u)" = 0 ]; then su -s /bin/bash nobody -c "$1"; else bash -c "$1"; fi; }

stop() {
  # the first stop is the one after the seed: from then on people are added to organizations by invitation (as on a real hub), which
  # the seed could not use (it adds people to teams directly)
  [ -d "$RUN/custom/conf" ] && touch "$RUN/seeded"
  if [ -f "$ASC_DIR/serve.pid" ]; then # asc serve of this run only: the pid it wrote itself
    kill "$(cat "$ASC_DIR/serve.pid")" 2>/dev/null || true
    for _ in $(seq 1 20); do kill -0 "$(cat "$ASC_DIR/serve.pid")" 2>/dev/null || break; sleep 1; done
    rm -f "$ASC_DIR/serve.pid"
  fi
  pkill -f "web --config $RUN/custom/conf/app.ini" 2>/dev/null || true
  for _ in $(seq 1 30); do # wait until the old hub has let go of the port
    pgrep -f "web --config $RUN/custom/conf/app.ini" >/dev/null 2>&1 || break
    sleep 1
  done
}

down() {
  stop
  rm -rf "$RUN"
}

start() {
  : "${PIXEL_HUB_BIN:?set PIXEL_HUB_BIN to the hub binary}"
  if [ -f "$RUN/seeded" ] && [ -n "$ASC_MODE" ] && ! grep -q ADD_MEMBERS_BY_INVITATIONS "$RUN/custom/conf/app.ini"; then
    sed -i '/^\[service\]/a ADD_MEMBERS_BY_INVITATIONS = true' "$RUN/custom/conf/app.ini"
  fi
  nohup setsid bash -c "$(declare -f as_user); as_user \"cd $RUN && exec $HUBENV $PIXEL_HUB_BIN web --config $RUN/custom/conf/app.ini\"" >"$RUN/log/hub.log" 2>&1 &
  for _ in $(seq 1 90); do
    curl -fs -o /dev/null "http://127.0.0.1:$PORT/api/healthz" && { echo "hub up on :$PORT"; start_asc; return 0; }
    sleep 1
  done
  tail -30 "$RUN/log/hub.log"
  exit 1
}

# asc serve asks the hub for authorization, so it starts once the hub answers (as the all-in-one entrypoint does).
start_asc() {
  [ "$ASC_MODE" = serve ] || return 0
  : "${PIXEL_ASC_BIN:?set PIXEL_ASC_BIN to the asc binary}"
  # another asc serve on the port would answer for ours (the hub would talk to it), so refuse to start next to one
  for p in "$ASC_PORT" "$ASC_ADMIN_PORT"; do
    if (exec 3<>"/dev/tcp/127.0.0.1/$p") 2>/dev/null; then echo "port $p is in use: set PIXEL_ASC_PORT to a free one" >&2; exit 1; fi
  done
  nohup setsid bash -c "$(declare -f as_user); as_user \"cd $RUN && echo \\$\\$ >$ASC_DIR/serve.pid && exec env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=$ASC_DIR/home TZ=UTC ASC_SERVE_CONFIG=$SERVE_CFG ASC_ALLOW_LOOPBACK_HTTP=1 RUST_LOG=\${ASC_LOG:-info} ASC_LOG_FORMAT=text $PIXEL_ASC_BIN serve\"" >"$RUN/log/asc.log" 2>&1 &
  for _ in $(seq 1 90); do
    (exec 3<>"/dev/tcp/127.0.0.1/$ASC_PORT") 2>/dev/null && { echo "asc serve up on :$ASC_PORT"; return 0; }
    sleep 1
  done
  tail -30 "$RUN/log/asc.log"
  exit 1
}

# the secrets, the asc serve configuration and the migration (once, at up)
prepare_asc_secrets() {
  mkdir -p "$BOOT" "$ASC_DIR"/{objects,cache,gitview,home}
  chmod 0700 "$BOOT" "$ASC_DIR"
  secret() { [ -s "$1" ] || { LC_ALL=C tr -dc "${3:-A-Za-z0-9}" </dev/urandom | head -c "$2" >"$1" || true; }; chmod 0400 "$1"; }
  secret "$BOOT/internal-token" 48
  secret "$BOOT/webhook-secret" 48
  secret "$BOOT/forwarded-key" 64 '0-9a-f'
}

prepare_asc() {
  cat >"$SERVE_CFG" <<TOML
listen = "127.0.0.1:$ASC_PORT"
store = 'file://$ASC_DIR/objects'
db = 'sqlite://$ASC_DIR/asc.db?mode=rwc'
cache = { memory = "128MiB", disk_dir = "$ASC_DIR/cache", disk = "1GiB" }

[admin]
listen = "127.0.0.1:$ASC_ADMIN_PORT"

[auth]
provider = "forgejo"
forwarded_key_file = "$BOOT/forwarded-key"

[forgejo]
url = "http://127.0.0.1:$PORT"
internal_token_file = "$BOOT/internal-token"
webhook_secret_file = "$BOOT/webhook-secret"
repo_root = "$RUN/repos"
bridge_promote = true

[limits]
text_memory = "128MiB"
trusted_proxies = ["127.0.0.1/32"]

[git]
enabled = true
view_dir = "$ASC_DIR/gitview"

[ssh]
enabled = false

[agents]
enabled = true

[workflows]
enabled = true
TOML
  if [ "$(id -u)" = 0 ]; then chown -R nobody "$RUN"; fi
  as_user "cd $RUN && env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=$ASC_DIR/home ASC_SERVE_CONFIG=$SERVE_CFG ASC_ALLOW_LOOPBACK_HTTP=1 $PIXEL_ASC_BIN serve migrate" >"$RUN/log/asc-migrate.log" 2>&1 \
    || { cat "$RUN/log/asc-migrate.log"; exit 1; }
}

up() {
  : "${PIXEL_HUB_BIN:?set PIXEL_HUB_BIN to the hub binary}"
  down
  mkdir -p "$RUN"/{custom/conf,data,repos,log,home}
  if [ "$ASC_MODE" = serve ]; then : "${PIXEL_ASC_BIN:?set PIXEL_ASC_BIN to the asc binary}"; mkdir -p "$BOOT"; chmod 0700 "$BOOT"; fi
  INTERNAL_TOKEN_LINE="INTERNAL_TOKEN = $(rand 64)"
  if [ "$ASC_MODE" = serve ]; then
    # the secrets first: the hub reads the shared token from a file (it refuses a literal token next to a file)
    prepare_asc_secrets
    INTERNAL_TOKEN_LINE="INTERNAL_TOKEN_URI = file://$BOOT/internal-token"
  fi
  cat > "$RUN/custom/conf/app.ini" <<INI
APP_NAME = ASC Hub
RUN_MODE = prod
WORK_PATH = $RUN
[server]
HTTP_ADDR = 127.0.0.1
HTTP_PORT = $PORT
ROOT_URL = http://127.0.0.1:$PORT/
DOMAIN = 127.0.0.1
STATIC_ROOT_PATH = $STATIC
APP_DATA_PATH = $RUN/data
OFFLINE_MODE = true
DISABLE_SSH = true
LFS_START_SERVER = false
[database]
DB_TYPE = sqlite3
PATH = $RUN/data/hub.db
[repository]
ROOT = $RUN/repos
[security]
INSTALL_LOCK = true
SECRET_KEY = $(rand 40)
$INTERNAL_TOKEN_LINE
[oauth2]
JWT_SECRET = $(rand 43 'A-Za-z0-9_-')
[service]
DISABLE_REGISTRATION = false
REQUIRE_SIGNIN_VIEW = false
[mailer]
ENABLED = false
[log]
MODE = console
LEVEL = warn
[session]
PROVIDER = file
PROVIDER_CONFIG = $RUN/data/sessions
[ui]
DEFAULT_THEME = asc-auto
INI
  if [ "$ASC_MODE" = serve ]; then
    cat >> "$RUN/custom/conf/app.ini" <<INI
[asc]
ENABLED = true
SERVER_URL = http://127.0.0.1:$ASC_PORT
PUBLIC_URL = http://127.0.0.1:$PORT
FORWARDED_KEY_URI = file://$BOOT/forwarded-key
WRITER = asc
NEW_REPOSITORIES = asc
BRIDGE = eager
SHADOW_SAMPLE_PERCENT = 0
SSH_ENABLED = false
DOWNLOAD_DIR = $RUN/download
INI
  elif [ "$ASC_MODE" = 1 ]; then
    cat >> "$RUN/custom/conf/app.ini" <<INI
[asc]
ENABLED = true
SERVER_URL = http://127.0.0.1:7890
PUBLIC_URL = http://127.0.0.1:$PORT
FORWARDED_KEY = $(rand 64 '0-9a-f')
SSH_ENABLED = false
DOWNLOAD_DIR = $RUN/download
INI
  fi
  # the release the Download boards show: a manifest and the files it lists
  if [ -n "$ASC_MODE" ]; then python3 "$here/download-manifest.py" "$RUN/download"; fi
  if [ "$(id -u)" = 0 ]; then chown -R nobody "$RUN"; fi
  if [ "$ASC_MODE" = serve ]; then prepare_asc; fi
  start # the first start creates the tables; the admin user needs them
  as_user "cd $RUN && $HUBENV $PIXEL_HUB_BIN --config $RUN/custom/conf/app.ini admin user create --admin --username $ADMIN --password '$PASSWORD' --email $ADMIN@acme.test --must-change-password=false" >"$RUN/log/admin.log" 2>&1     || { cat "$RUN/log/admin.log"; exit 1; }
}

case "${1:-}" in
  up) up ;;
  start) start ;;
  stop) stop ;;
  down) down ;;
  *) echo "usage: hub.sh up|start|stop|down" >&2; exit 2 ;;
esac
