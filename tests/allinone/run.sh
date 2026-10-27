#!/bin/bash
# Runs every test of the all-in-one image's scripts: the entrypoint and health check, the backup
# tool, scripts/smoke.sh, and the Compose checks. Linux only (flock, setpriv or a plain user).
#
#   tests/allinone/run.sh
#
# ASC_ALLINONE_COMPOSE_FILES (a colon separated list) adds Compose files of a deployment to the checks.

set -uo pipefail
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"

status=0
for t in entrypoint_test.sh backup_test.sh smoke_test.sh; do
	bash "$HERE/$t" || status=1
done
(cd "$REPO" && go test -count=1 ./tests/allinone/) || status=1
exit "$status"
