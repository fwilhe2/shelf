#!/bin/bash
# A throwaway shelf with full-text search, to try it out: the "search" image,
# with the demo files of test/demo-data.sh in a share that lives in memory
# only. Ctrl+C stops it, and nothing of it is left.
#
#   test/demo.sh            then open http://localhost:18080/, user demo, password demo
#
# The share and the config (with the search index) are in a directory in
# $XDG_RUNTIME_DIR (or /dev/shm), which is memory (tmpfs), and removed on exit.
# The demo files are made once and kept in ~/.cache/shelf-demo-data (or
# DEMO_DATA=DIR). Settings: PORT (default 18080), IMAGE (default
# localhost/shelf:search, built when missing; BUILD=1 builds it again).
set -euo pipefail

repo=$(cd "$(dirname "$0")/.." && pwd)
image=${IMAGE:-localhost/shelf:search}
port=${PORT:-18080}
demo=${DEMO_DATA:-${XDG_CACHE_HOME:-$HOME/.cache}/shelf-demo-data}
mem=${XDG_RUNTIME_DIR:-/dev/shm}

if [[ $(stat -f -c %T "$mem") != tmpfs ]]; then
	echo "$mem is not in memory (tmpfs); set XDG_RUNTIME_DIR to a directory that is" >&2
	exit 1
fi
if [[ ! -f $demo/SOURCES.md ]]; then
	"$repo/test/demo-data.sh" "$demo.tmp"
	mv "$demo.tmp" "$demo"
fi
if [[ -n ${BUILD:-} ]] || ! podman image exists "$image"; then
	podman build --target search -t "$image" "$repo"
fi

work=$(mktemp -d "$mem/shelf-demo.XXXXXX")
trap 'rm -rf -- "$work"' EXIT
mkdir "$work/config" "$work/data"
cp -r "$demo" "$work/data/demo"

# The image has no shell: each step is a container of its own, on the same
# directories. The container's user is you, so you can remove what it writes.
shelf() {
	podman run --rm --userns keep-id:uid=65532,gid=65532 \
		-v "$work/config:/config:Z" -v "$work/data:/data:Z" "$@"
}
printf 'demo\ndemo\n' | shelf -i "$image" init -data /data -listen :8080 >/dev/null 2>&1
shelf "$image" index

echo
echo "shelf demo: http://localhost:$port/  user demo, password demo  (Ctrl+C stops it)"
echo
tty=()
[[ -t 0 && -t 1 ]] && tty=(-it)
shelf "${tty[@]}" -p "127.0.0.1:$port:8080" "$image" serve || true
