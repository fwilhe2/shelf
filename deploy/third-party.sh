#!/bin/bash
# Collects the third-party parts of the search image (Containerfile, target
# "search") with their licences, and checks them against THIRD_PARTY.md, so
# the image cannot ship anything that is not listed there.
#
#   third-party.sh debs OUT BASE_STATUS_D THIRD_PARTY.md PROGRAM... -- PACKAGE...
#       Copies the Debian packages that own PROGRAMs and every library they
#       load, and the PACKAGEs (data such as tesseract's languages), into OUT:
#       their files, /usr/share/doc/<package>/copyright, and their entries in
#       /var/lib/dpkg/status.d/ as distroless keeps them, so image scanners see
#       them. Packages the distroless base has (BASE_STATUS_D) are left out.
#
#   third-party.sh crates OUT THIRD_PARTY.md
#       Run in clerk's source tree after building it: writes
#       OUT/usr/share/doc/clerk/crates.txt (every crate in the clerk binary, its
#       version and licence) and each crate's licence files beside it.
#
# Either fails when what it found differs from the lists in THIRD_PARTY.md.
set -euo pipefail

# listed SECTION FILE: the first `code` of each table row between the
# <!-- SECTION --> and <!-- /SECTION --> markers.
listed() {
	sed -n "/<!-- $1 -->/,/<!-- \/$1 -->/p" "$2" | sed -n 's/^| `\([^`]*\)`.*/\1/p' | sort -u
}

# same NAME FOUND_FILE LISTED_FILE: fails with a diff when the lists differ.
same() {
	if ! diff -u "$3" "$2" >/tmp/third-party.diff; then
		echo "THIRD_PARTY.md, $1, does not match what the image ships (+ ships, not listed; - listed, not shipped):" >&2
		tail -n +3 /tmp/third-party.diff >&2
		exit 1
	fi
}

debs() {
	local out=$1 base=$2 doc=$3
	shift 3
	local programs=() extra=()
	while [[ $# -gt 0 && $1 != -- ]]; do programs+=("$1"); shift; done
	[[ $# -gt 0 ]] && shift
	extra=("$@")

	local files=("${programs[@]}")
	mapfile -t -O "${#files[@]}" files < <(ldd "${programs[@]}" | awk '$2 == "=>" && $3 ~ /^\// {print $3}' | sort -u)
	local pkgs
	pkgs=$(
		for f in "${files[@]}"; do
			real=$(realpath "$f")
			dpkg -S "$real" 2>/dev/null || dpkg -S "${real/#\/usr\/lib\//\/lib\/}"
		done | cut -d: -f1 | tr ', ' '\n\n' | sed '/^$/d'
		printf '%s\n' "${extra[@]}"
	)
	pkgs=$(printf '%s\n' $pkgs | sort -u | while read -r p; do [[ -e $base/$p ]] || echo "$p"; done)

	mkdir -p "$out/var/lib/dpkg/status.d"
	for p in $pkgs; do
		dpkg-query -W -f='${Status}' "$p" | grep -q ' installed$'
		dpkg -L "$p" | while read -r f; do
			[[ -d $f && ! -L $f ]] && continue
			[[ -e $f || -L $f ]] || continue
			case $f in
			/usr/share/doc/*/copyright) ;;
			/usr/share/doc/* | /usr/share/man/* | /usr/share/lintian/* | /usr/share/bug/* | /usr/share/info/*) continue ;;
			esac
			# /lib is /usr/lib on Debian and in distroless; keep it a symlink there.
			dir=$(realpath "$(dirname "$f")")
			mkdir -p "$out$dir"
			cp -a "$f" "$out$dir/"
		done
		dpkg -s "$p" >"$out/var/lib/dpkg/status.d/$p"
		md5=$(ls /var/lib/dpkg/info/"$p".md5sums /var/lib/dpkg/info/"$p":*.md5sums 2>/dev/null | head -1 || true)
		[[ -n $md5 ]] && cp "$md5" "$out/var/lib/dpkg/status.d/$p.md5sums"
	done

	printf '%s\n' $pkgs >/tmp/found
	listed debian-packages "$doc" >/tmp/listed
	same "Debian packages" /tmp/found /tmp/listed
	echo "third-party.sh: $(wc -l </tmp/found) Debian packages, all listed in THIRD_PARTY.md"
}

crates() {
	local out=$1/usr/share/doc/clerk doc=$2
	local cargo_home=${CARGO_HOME:-$HOME/.cargo}
	mkdir -p "$out/crates"
	cargo tree -p clerk-cli -e normal --locked --prefix none --format '{p}|{l}' |
		sed 's/ (\*)$//' | awk -F'|' '{split($1, a, " "); print a[1] "\t" substr(a[2], 2) "\t" $2}' |
		sort -u >"$out/crates.txt"

	local missing=()
	while IFS=$'\t' read -r name version licence; do
		[[ -z $licence ]] && { echo "crate $name $version names no licence" >&2; exit 1; }
		dest=$out/crates/$name-$version
		mkdir -p "$dest"
		src=$(ls -d "$cargo_home"/registry/src/*/"$name-$version" 2>/dev/null | head -1 || true)
		if [[ -z $src ]]; then # clerk itself, or grind from git: the repository's licence
			case $name in
			clerk-*) src=. ;;
			grind-*) src=$(dirname "$(ls -d "$cargo_home"/git/checkouts/grind-*/*/LICENSE* | head -1)") ;;
			esac
		fi
		find "$src" -maxdepth 1 -type f \( -iname 'licen[cs]e*' -o -iname 'copying*' -o -iname 'unlicense*' -o -iname 'notice*' \) \
			-exec cp {} "$dest/" \;
		[[ -d $src/LICENSES ]] && cp -r "$src/LICENSES" "$dest/"
		if [[ -z $(ls "$dest") ]]; then
			missing+=("$name-$version")
			mit "$src/Cargo.toml" >"$dest/LICENSE"
		fi
	done <"$out/crates.txt"

	cut -f3 "$out/crates.txt" | sort -u >/tmp/found
	listed crate-licences "$doc" >/tmp/listed
	same "licences of clerk's crates" /tmp/found /tmp/listed
	listed crates-without-licence-file "$doc" >/tmp/listed
	printf '%s\n' "${missing[@]}" | sed '/^$/d' | sed 's/-[0-9][^-]*$//' | sort -u >/tmp/found
	same "crates without a licence file" /tmp/found /tmp/listed
	echo "third-party.sh: $(wc -l <"$out/crates.txt") crates, every licence listed in THIRD_PARTY.md"
}

# mit CARGO_TOML: the MIT licence, for a crate that says it is MIT and has no
# licence file, with its authors as the copyright holders.
mit() {
	grep -q '^license *= *"MIT"' "$1" || { echo "$1: no licence file, and not MIT" >&2; exit 1; }
	local authors
	authors=$(sed -n 's/^authors *= *\[\(.*\)\]/\1/p' "$1" | tr -d '"')
	cat <<EOF
This crate's Cargo.toml says: license = "MIT", and it has no licence file.
This is the MIT licence, with its authors as the copyright holders.

Copyright (c) ${authors:-the authors of this crate}

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
EOF
}

cmd=${1:?usage: see the top of $0}
shift
"$cmd" "$@"
