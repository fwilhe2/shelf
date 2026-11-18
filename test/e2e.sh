#!/bin/bash
# End-to-end test and demo of shelf: runs the container image the way the
# README describes (rootless Podman, bind mounts, HTTPS from shelf's own CA),
# uses it like a family would (web API and WebDAV), and checks on the host
# that every file ended up exactly where and how it should.
#
#   podman build -t localhost/shelf:latest .
#   test/e2e.sh
#
# Settings: IMAGE (default localhost/shelf:latest), PORT (default 18443),
# KEEP=1 keeps the container and the temporary directory for a look around.
# In GitHub Actions, a report is written to the job summary.
set -euo pipefail

IMAGE=${IMAGE:-localhost/shelf:latest}
PORT=${PORT:-18443}
NAME=shelf-e2e-$$
REPO=$(cd "$(dirname "$0")/.." && pwd)

work=$(mktemp -d)
cfg=$work/config            # ~/.config/shelf on a real installation
data=$work/data             # ~/nas-data
mkdir -p "$cfg" "$data" "$work/client"
client=$work/client
base=https://localhost:$PORT

report=$work/report.md
pass=0

cleanup() {
    local rc=$?
    if [[ $rc -ne 0 ]]; then
        echo "::group::container log"
        podman logs "$NAME" 2>&1 | tail -n 100 || true
        echo "::endgroup::"
    fi
    if [[ ${KEEP:-} == 1 ]]; then
        echo "kept container $NAME and $work"
    else
        podman rm -f "$NAME" >/dev/null 2>&1 || true
        # files written by the container may belong to a subordinate uid
        podman unshare rm -rf "$work" 2>/dev/null || rm -rf "$work"
    fi
    if [[ -n ${GITHUB_STEP_SUMMARY:-} && -f $report ]]; then
        cat "$report" >> "$GITHUB_STEP_SUMMARY"
    fi
    exit $rc
}
trap cleanup EXIT

# ---------------------------------------------------------------- helpers

section() {
    printf '\n\033[1m== %s\033[0m\n' "$*"
    printf '\n### %s\n\n' "$*" >> "$report"
}

ok() {
    pass=$((pass + 1))
    printf '  \033[32m✔\033[0m %s\n' "$*"
    printf -- '- ✅ %s\n' "$*" >> "$report"
}

fail() {
    printf '  \033[31m✘ %s\033[0m\n' "$*"
    printf -- '- ❌ **%s**\n' "$*" >> "$report"
    exit 1
}

# check DESCRIPTION COMMAND...: runs the command, fails the test if it fails.
check() {
    local what=$1
    shift
    if "$@"; then ok "$what"; else fail "$what"; fi
}

# shelf ARGS...: run the shelf CLI in a fresh container, like the README.
shelf() {
    podman run --rm -i --userns keep-id:uid=65532,gid=65532 \
        -v "$cfg:/config:z" -v "$data:/data:z" "$IMAGE" "$@"
}

# shelf_exec ARGS...: run the shelf CLI inside the running server container.
shelf_exec() { podman exec -i "$NAME" /shelf "$@"; }

c() { curl -sS --cacert "$cfg/ca.crt" "$@"; }

# status ARGS...: HTTP status code of a request.
status() { c -o /dev/null -w '%{http_code}' "$@"; }

alice=(-u 'alice:correct horse')
bob=(-u 'bob:battery staple')
dav=$base/dav

sha() { sha256sum "$1" | cut -d' ' -f1; }
same_file() { [[ $(sha "$1") == $(sha "$2") ]]; }
owned_by_me() { [[ $(stat -c %u "$1") == $(id -u) ]]; }

start() {
    # The same settings as deploy/shelf.container.
    podman run -d --name "$NAME" \
        --userns keep-id:uid=65532,gid=65532 \
        -p "$PORT:8443" \
        -v "$cfg:/config:z" -v "$data:/data:z" \
        --read-only --cap-drop ALL --security-opt no-new-privileges \
        "$IMAGE" >/dev/null
    for _ in $(seq 50); do
        [[ $(status "$base/" 2>/dev/null) == 200 ]] && return 0
        sleep 0.2
    done
    fail "server did not come up"
}

# wait_for DESCRIPTION COMMAND...: retry a command for a few seconds (config reloads).
wait_for() {
    local what=$1
    shift
    for _ in $(seq 40); do
        if "$@" 2>/dev/null; then ok "$what"; return; fi
        sleep 0.25
    done
    fail "$what"
}

cat > "$report" <<EOF
## shelf end-to-end demo

Image \`$IMAGE\`, run rootless with Podman like \`deploy/shelf.container\`.
Every file is uploaded over the network and then checked **on the host** in the
bind-mounted data directory.
EOF

# ---------------------------------------------------------------- setup

section "Setting up: config, first user, HTTPS"

printf 'alice\ncorrect horse\n' | shelf init -data /data/files -listen :8443 >/dev/null 2>&1
check "shelf init created the config with user alice" grep -q '"alice"' "$cfg/config.json"
check "the config is only readable by its owner (0600)" test "$(stat -c %a "$cfg/config.json")" = 600
check "the password is stored as a bcrypt hash, not in plain text" \
    bash -c "grep -qF '\"password_hash\": \"\$2a\$' '$cfg/config.json' && ! grep -qF 'correct horse' '$cfg/config.json'"

shelf cert -hosts localhost,127.0.0.1 >/dev/null
check "shelf cert created a CA and a server certificate" test -s "$cfg/ca.crt" -a -s "$cfg/tls.crt" -a -s "$cfg/tls.key"
check "the server certificate is signed by the CA" openssl verify -CAfile "$cfg/ca.crt" "$cfg/tls.crt" >/dev/null

if [[ -x /usr/libexec/podman/quadlet ]]; then
    unitdir=$work/quadlet && mkdir -p "$unitdir" && cp "$REPO/deploy/shelf.container" "$unitdir/"
    check "the Quadlet unit deploy/shelf.container is valid" \
        bash -c "QUADLET_UNIT_DIRS='$unitdir' /usr/libexec/podman/quadlet -dryrun -user >/dev/null 2>&1"
fi

start
check "the server answers over HTTPS with a certificate the CA vouches for" test "$(status "$base/")" = 200
check "the CA certificate is offered for download at /ca.crt" bash -c "curl -sS --cacert '$cfg/ca.crt' '$base/ca.crt' | cmp -s - '$cfg/ca.crt'"

# ---------------------------------------------------------------- users and shares

section "Users and shares, changed while the server runs"

printf 'battery staple\n' | shelf_exec user add bob >/dev/null
mkdir -p "$data/movies"
echo "a film" > "$data/movies/holiday.mp4"   # put there on the host, e.g. by copying from a USB disk
shelf_exec share add movies /data/movies -ro >/dev/null
shelf_exec share add taxes /data/taxes -users alice >/dev/null
check "the new share directory was created on the host" test -d "$data/taxes"
bob_can_log_in() { [[ $(status "${bob[@]}" -X PROPFIND -H 'Depth: 1' "$dav/") == 207 ]]; }
wait_for "the running server picked up user bob without a restart" bob_can_log_in

check "a wrong password is rejected" test "$(status -u 'alice:nope' "$dav/")" = 401
check "anonymous requests are rejected" test "$(status "$base/api/shares")" = 401

c -c "$client/alice.jar" -H 'Content-Type: application/json' \
    -d '{"username":"alice","password":"correct horse"}' "$base/api/login" >/dev/null
api() { c -b "$client/alice.jar" "$@"; }
check "alice logs in to the web UI and gets a session" grep -q shelf_session "$client/alice.jar"
check "alice sees the shares files, movies and taxes" \
    test "$(api "$base/api/shares" | jq -c '[.[].name] | sort')" = '["files","movies","taxes"]'
check "bob does not see taxes" \
    test "$(c "${bob[@]}" "$base/api/shares" | jq -c '[.[].name] | sort')" = '["files","movies"]'
check "movies is reported as read-only" \
    test "$(api "$base/api/shares" | jq '.[] | select(.name=="movies") | .read_only')" = true

# ---------------------------------------------------------------- uploads

section "Uploading files and checking them on the host"

head -c 20M /dev/urandom > "$client/video.bin"
printf 'Grüße aus der Küche 🍰\n' > "$client/recipe.txt"
printf 'office document stand-in\n' > "$client/budget.ods"
: > "$client/empty.txt"

check "alice creates the folder 'Photos 2026'" test "$(status "${alice[@]}" -X MKCOL "$dav/files/Photos%202026")" = 201
check "… and a subfolder Summer" test "$(status "${alice[@]}" -X MKCOL "$dav/files/Photos%202026/Summer")" = 201
check "alice uploads a 20 MB file" \
    test "$(status "${alice[@]}" -T "$client/video.bin" "$dav/files/Photos%202026/Summer/video.bin")" = 201
check "alice uploads a file with a non-ASCII name" \
    test "$(status "${alice[@]}" -T "$client/recipe.txt" "$dav/files/K%C3%BCchen-Rezept%20%F0%9F%8D%B0.txt")" = 201
check "alice uploads an empty file" test "$(status "${alice[@]}" -T "$client/empty.txt" "$dav/files/empty.txt")" = 201
check "bob uploads an office document" test "$(status "${bob[@]}" -T "$client/budget.ods" "$dav/files/budget.ods")" = 201
check "alice puts a document into her private share" \
    test "$(status "${alice[@]}" -T "$client/recipe.txt" "$dav/taxes/2026.txt")" = 201

f=$data/files
check "the 20 MB file is on the host, byte for byte" same_file "$client/video.bin" "$f/Photos 2026/Summer/video.bin"
check "the non-ASCII name and content arrived unchanged" same_file "$client/recipe.txt" "$f/Küchen-Rezept 🍰.txt"
check "the empty file exists and is empty" bash -c "[[ -f '$f/empty.txt' && ! -s '$f/empty.txt' ]]"
check "bob's document is on the host" same_file "$client/budget.ods" "$f/budget.ods"
check "the private document is in the taxes directory" same_file "$client/recipe.txt" "$data/taxes/2026.txt"
check "uploaded files belong to the user running the container" owned_by_me "$f/Photos 2026/Summer/video.bin"
check "created folders belong to that user, too" owned_by_me "$f/Photos 2026"
check "no temporary or stray files were left behind" \
    test "$(find "$data" -type f | wc -l)" = 6

# ---------------------------------------------------------------- downloads

section "Reading files back"

c "${alice[@]}" -o "$client/video.down" "$dav/files/Photos%202026/Summer/video.bin"
check "downloading over WebDAV returns the same 20 MB" same_file "$client/video.bin" "$client/video.down"
check "a range request returns the right part of the file" \
    bash -c "curl -sS --cacert '$cfg/ca.crt' -u 'alice:correct horse' -r 1000-1999 '$dav/files/Photos%202026/Summer/video.bin' | cmp -s - <(tail -c +1001 '$client/video.bin' | head -c 1000)"
check "the file list shows the new entries, folders first" \
    test "$(api "$base/api/list?path=/files" | jq -c '[.entries[].name]')" = '["Photos 2026","budget.ods","empty.txt","Küchen-Rezept 🍰.txt"]'
check "the file list reports the right size" \
    test "$(api --get --data-urlencode 'path=/files/Photos 2026/Summer' "$base/api/list" | jq '.entries[0].size')" = $((20 << 20))

api --get --data-urlencode 'path=/files/Photos 2026' -o "$client/photos.zip" "$base/api/zip"
mkdir "$client/unzipped" && (cd "$client/unzipped" && unzip -q ../photos.zip)
check "a folder downloaded as zip contains the same bytes" same_file "$client/video.bin" "$client/unzipped/Summer/video.bin"

c "${alice[@]}" -X PROPFIND -H 'Depth: 1' "$dav/files/" > "$client/propfind.xml"
check "PROPFIND (what file managers use) lists the files" grep -q 'budget.ods' "$client/propfind.xml"

# ---------------------------------------------------------------- changes

section "Renaming, copying and deleting"

check "rename (MOVE) budget.ods to Budget 2026.ods" \
    test "$(status "${bob[@]}" -X MOVE -H "Destination: $dav/files/Budget%202026.ods" "$dav/files/budget.ods")" = 201
check "… the file was renamed on the host" bash -c "[[ ! -e '$f/budget.ods' ]] && cmp -s '$client/budget.ods' '$f/Budget 2026.ods'"
check "copy a folder (COPY)" \
    test "$(status "${alice[@]}" -X COPY -H "Destination: $dav/files/Backup" "$dav/files/Photos%202026")" = 201
check "… the copy is on the host" same_file "$client/video.bin" "$f/Backup/Summer/video.bin"
check "delete the copy (DELETE)" test "$(status "${alice[@]}" -X DELETE "$dav/files/Backup")" = 204
check "… it is gone from the host" test ! -e "$f/Backup"
check "overwrite a file with new content" \
    bash -c "echo v2 | curl -sS -o /dev/null --cacert '$cfg/ca.crt' -u 'alice:correct horse' -T - '$dav/files/empty.txt' && test \"\$(cat '$f/empty.txt')\" = v2"

# ---------------------------------------------------------------- access rules

section "Access rules"

check "writing to the read-only share movies is refused" \
    test "$(status "${alice[@]}" -T "$client/recipe.txt" "$dav/movies/new.txt")" = 403
check "deleting from movies is refused" test "$(status "${alice[@]}" -X DELETE "$dav/movies/holiday.mp4")" = 403
check "moving a file into movies is refused" \
    test "$(status "${alice[@]}" -X MOVE -H "Destination: $dav/movies/x.txt" "$dav/files/empty.txt")" = 403
check "… and movies is unchanged on the host" bash -c "[[ \$(ls '$data/movies') == holiday.mp4 && -e '$f/empty.txt' ]]"
check "reading from movies works" test "$(c "${bob[@]}" "$dav/movies/holiday.mp4")" = "a film"
check "bob cannot read alice's private share" \
    bash -c "[[ \$(curl -sS -o /dev/null -w '%{http_code}' --cacert '$cfg/ca.crt' -u 'bob:battery staple' '$dav/taxes/2026.txt') == 404 ]]"
check "bob cannot write into it either" \
    bash -c "[[ \$(curl -sS -o /dev/null -w '%{http_code}' --cacert '$cfg/ca.crt' -u 'bob:battery staple' -T '$client/recipe.txt' '$dav/taxes/x.txt') == 40[34] && ! -e '$data/taxes/x.txt' ]]"
check "'..' does not lead out of a share" \
    bash -c "! curl -sS --path-as-is --cacert '$cfg/ca.crt' -u 'bob:battery staple' '$dav/files/../taxes/2026.txt' | grep -q Grüße"
ln -s ../taxes "$f/sneaky"
check "a symlink in a share does not lead out of it" \
    bash -c "! curl -sS --cacert '$cfg/ca.crt' -u 'bob:battery staple' '$dav/files/sneaky/2026.txt' | grep -q Grüße"
check "… and cannot be used to write outside" \
    bash -c "curl -sS -o /dev/null --cacert '$cfg/ca.crt' -u 'bob:battery staple' -T '$client/recipe.txt' '$dav/files/sneaky/evil.txt'; [[ ! -e '$data/taxes/evil.txt' ]]"
rm "$f/sneaky"
check "uploaded HTML cannot run scripts on shelf's origin" \
    bash -c "curl -sS -D - -o /dev/null --cacert '$cfg/ca.crt' -u 'alice:correct horse' '$dav/files/empty.txt' | grep -qi '^content-security-policy: sandbox'"

# ---------------------------------------------------------------- office

section "Office documents"

check "the web UI is told which files open in the office page" \
    test "$(api "$base/api/me" | jq '.office | index(".ods") != null')" = true
check "the office page is served at /office/" \
    bash -c "curl -sS --cacert '$cfg/ca.crt' '$base/office/' | grep -i '<html' >/dev/null"
check "… precompressed for browsers that accept gzip" \
    bash -c "curl -sS -D - -o /dev/null -H 'Accept-Encoding: gzip' --cacert '$cfg/ca.crt' '$base/office/' | grep -qi '^content-encoding: gzip'"

# ---------------------------------------------------------------- viewer

section "Viewing files"

pdfjs=$(sed -n "s/^const PDFJS_VERSION = '\(.*\)';$/\1/p" "$(dirname "$0")/../web/index.html")
check "PDF.js for the PDF viewer is built into the binary" \
    bash -c "curl -sS -D - -o /dev/null --cacert '$cfg/ca.crt' '$base/vendor/pdfjs-$pdfjs/legacy/build/pdf.worker.min.mjs' | grep -qi '^content-type: text/javascript'"
check "… and its licence is served with it" \
    bash -c "curl -sS --cacert '$cfg/ca.crt' '$base/vendor/pdfjs-$pdfjs/LICENSE' | grep -q 'Apache License'"
check "only shelf itself may show the office page in a frame" \
    bash -c "curl -sS -D - -o /dev/null --cacert '$cfg/ca.crt' '$base/office/' | grep -qi '^x-frame-options: sameorigin'"

# ---------------------------------------------------------------- persistence

section "Restarting and backing up"

before=$(cd "$data" && find . -type f -print0 | sort -z | xargs -0 sha256sum)
podman rm -f "$NAME" >/dev/null
start
check "after a restart, the server is back" test "$(status "${alice[@]}" -X PROPFIND -H 'Depth: 1' "$dav/")" = 207
c "${alice[@]}" -o "$client/video.again" "$dav/files/Photos%202026/Summer/video.bin"
check "after a restart, the files are still there and unchanged" same_file "$client/video.bin" "$client/video.again"
check "after a restart, users and shares are still there" \
    test "$(c "${bob[@]}" "$base/api/shares" | jq -c '[.[].name] | sort')" = '["files","movies"]'
check "the data directory was not touched by the restart" \
    test "$(cd "$data" && find . -type f -print0 | sort -z | xargs -0 sha256sum)" = "$before"

backup=$work/backup && mkdir "$backup"
run_backup() {
    BACKUP_DEST=$backup BACKUP_SOURCES="$cfg $data" XDG_CONFIG_HOME=$work/none \
        bash "$REPO/deploy/shelf-backup" >/dev/null
}
run_backup
check "the backup script takes a snapshot" test -L "$backup/latest"
check "the snapshot holds the files" same_file "$client/video.bin" "$backup/latest/data/files/Photos 2026/Summer/video.bin"
check "the snapshot holds the config" cmp -s "$cfg/config.json" "$backup/latest/config/config.json"
first=$(readlink "$backup/latest")
sleep 1
c "${alice[@]}" -o /dev/null -T "$client/recipe.txt" "$dav/files/new.txt"
run_backup
check "a second snapshot has the new file" test -e "$backup/latest/data/files/new.txt"
check "unchanged files are shared between snapshots (hard links)" test \
    "$(stat -c %i "$backup/$first/data/files/Photos 2026/Summer/video.bin")" = \
    "$(stat -c %i "$backup/latest/data/files/Photos 2026/Summer/video.bin")"

# ---------------------------------------------------------------- report

section "What ended up on the host"
{
    echo '```'
    echo "\$ tree data/"
    (cd "$work" && find data | sort | sed 's|[^/]*/|  |g')
    echo
    echo "\$ cat config/config.json   (hashes shortened)"
    sed -E 's/("password_hash": "\$2a\$10\$.{6})[^"]*/\1…/' "$cfg/config.json"
    echo '```'
} | tee -a "$report"

printf '\n**%d checks passed.**\n' "$pass" >> "$report"
printf '\n\033[32m%d checks passed.\033[0m\n' "$pass"
