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
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATIC="${PIXEL_HUB_STATIC:-$(cd "$here/../../.." && pwd)}"
PORT="${PIXEL_PORT:-3610}"
RUN="${PIXEL_RUN:-/tmp/pixel-run}"
ADMIN="${PIXEL_ADMIN:-maya}"
PASSWORD="${PIXEL_PASSWORD:-Pixel-Passw0rd-9137}"
HUBENV="env -i PATH=/usr/local/bin:/usr/bin:/bin HOME=$RUN/home TZ=UTC GITEA_WORK_DIR=$RUN GITEA_CUSTOM=$RUN/custom"
as_user() { if [ "$(id -u)" = 0 ]; then su -s /bin/bash nobody -c "$1"; else bash -c "$1"; fi; }

stop() {
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
  nohup setsid bash -c "$(declare -f as_user); as_user \"cd $RUN && exec $HUBENV $PIXEL_HUB_BIN web --config $RUN/custom/conf/app.ini\"" >"$RUN/log/hub.log" 2>&1 &
  for _ in $(seq 1 90); do
    curl -fs -o /dev/null "http://127.0.0.1:$PORT/api/healthz" && { echo "hub up on :$PORT"; return 0; }
    sleep 1
  done
  tail -30 "$RUN/log/hub.log"
  exit 1
}

up() {
  : "${PIXEL_HUB_BIN:?set PIXEL_HUB_BIN to the hub binary}"
  down
  mkdir -p "$RUN"/{custom/conf,data,repos,log,home}
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
SECRET_KEY = pixel-harness-secret-key-0123456789abcdef
INTERNAL_TOKEN = eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJuYmYiOjE3MDAwMDAwMDB9.pixelharnessinternaltoken0123456789ABC
[oauth2]
JWT_SECRET = $(printf 'pixel-harness-jwt-secret-0123456' | base64 | tr '+/' '-_' | tr -d '=')
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
  if [ "${PIXEL_ASC:-}" = 1 ]; then
    cat >> "$RUN/custom/conf/app.ini" <<INI
[asc]
ENABLED = true
SERVER_URL = http://127.0.0.1:7890
PUBLIC_URL = http://127.0.0.1:$PORT
FORWARDED_KEY = 00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff
SSH_ENABLED = false
INI
  fi
  if [ "$(id -u)" = 0 ]; then chown -R nobody "$RUN"; fi
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
