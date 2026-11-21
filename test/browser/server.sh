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
# Search, with a stand-in for clerk: "hello" finds three files, anything else
# none. scan notes the time, which status then tells.
f=$work/files
cat >"$work/clerk" <<EOF
#!/bin/sh
for word; do :; done
case "\$3:\$word" in find:hello) cat <<'HITS' ;; esac
{"path":"$f/testpage.pdf","hash":"00","size":1,"context":"…[Hello] from shelf Second page"}
{"path":"$f/blob.bin","hash":"00","size":4,"context":null}
{"path":"$f/notes.md","hash":"00","size":1,"context":"# Notes [Hello] from shelf."}
HITS
case "\$3" in
scan)
    sleep 1
    date -u '+%Y-%m-%d %H:%M UTC' >"$work/scanned"
    echo "$f: 12 files, 1 read; 1 added, 0 moved, 0 copied, 0 changed, 0 removed"
    echo "  9 with text" ;;
status)
    printf 'index\t$work/clerk.sqlite\nfiles\t12\ndocuments\t10\nwith text\t9\n'
    printf 'folder\t$f\t12 files\tscanned %s\n' "\$(cat "$work/scanned" 2>/dev/null || echo never)" ;;
esac
EOF
chmod +x "$work/clerk"
: >"$work/clerk.sqlite"
node -e 'const fs = require("fs"), [c, w] = process.argv.slice(1), cfg = JSON.parse(fs.readFileSync(c));
  cfg.search = { index: w + "/clerk.sqlite", clerk: w + "/clerk" };
  fs.writeFileSync(c, JSON.stringify(cfg))' "$work/config.json" "$work"

SHELF_OFFICE_DIR=${GRIND_WEB:-} "$work/shelf" serve -c "$work/config.json" &
pid=$!
wait $pid
