#!/bin/bash
# Runs shelf for the browser tests (playwright.config.js starts it): a fresh
# build, a fresh config with user alice (password "pw") and one share "files"
# holding the fixtures, on 127.0.0.1:$PORT.
#
# GRIND_WEB: grind's built web page (its dist/). The office tests need it and
# are skipped without it, except in CI, where it is required.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'kill $pid 2>/dev/null; rm -rf "$work"' EXIT
trap 'exit 0' INT TERM

if [[ -z ${GRIND_WEB:-} && -n ${CI:-} ]]; then
    echo "server.sh: GRIND_WEB must be set in CI" >&2
    exit 1
fi

(cd "$here/../.." && go build -o "$work/shelf" .)
node "$here/fixtures.js" "$work/files"
printf 'alice\npw\n' | "$work/shelf" init -c "$work/config.json" -data "$work/files" \
    -listen "127.0.0.1:${PORT:-18090}" >/dev/null 2>&1
SHELF_OFFICE_DIR=${GRIND_WEB:-} "$work/shelf" serve -c "$work/config.json" &
pid=$!
wait $pid
