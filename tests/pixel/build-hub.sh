#!/usr/bin/env bash
# Builds what the pixel harness photographs: the hub's web assets (webpack) and the hub binary, into tests/pixel/.bin/hub.
# Needs node_modules (npm ci) and a Go toolchain with cgo. In WSL, set the usual GOCACHE, GOFLAGS and GOTMPDIR first.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$root"
if ! ls public/assets/css/index.*.css >/dev/null 2>&1; then
  BROWSERSLIST_IGNORE_OLD_DATA=true npx webpack
fi
mkdir -p tests/pixel/.bin
# the version the boards show (v1.4.2 in the admin banner, 1.4.2 in the footer); a build without it says "development"
go build -tags 'sqlite sqlite_unlock_notify' -ldflags '-X "main.Version=1.4.2"' -o tests/pixel/.bin/hub .
echo "built tests/pixel/.bin/hub"
