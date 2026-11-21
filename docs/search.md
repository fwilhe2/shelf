# Full-text search (clerk)

shelf finds files by the words in them, PDFs, office documents, text files and,
with OCR, scans and photos, using [clerk](https://github.com/fwilhe2/clerk).
clerk keeps an index of the shares outside of them; shelf runs it as a program
and never reads the index itself, so clerk's index format stays clerk's.

## Turning it on

The container image has everything and search is on; see [The
container](#the-container). Without it, install `clerk`, and for scans and photos `tesseract` (with the languages of
your documents, e.g. `tesseract-langpack-deu` on Fedora,
`tesseract-ocr-deu` on Debian) and poppler's `pdftoppm`. Then name the index in
the config:

```json
"search": { "index": "/var/lib/shelf/clerk.sqlite", "clerk": "/usr/local/bin/clerk" }
```

or set `$SHELF_SEARCH_INDEX` (and `$SHELF_CLERK`). `"clerk"` defaults to
`clerk` on `$PATH`. The index must not be inside a share, and the user shelf
runs as must be able to write to its directory.

## Indexing

Indexing has clerk read every share: the first time every file, and every text
in it, which can take a while for scans; after that, only what changed. Files
added since the last run are found once indexed.

`shelf serve` indexes by itself:

- a minute after it starts,
- 30 seconds after the last change made through it (an upload, a rename, a
  delete, from the web UI or WebDAV), so a scan from the phone is found soon,
- and every hour, for files changed on the disk directly (rsync, another
  program). Change that with `"every"` in `"search"` (e.g. `"15m"`, `"6h"`),
  or `$SHELF_INDEX_EVERY`; `"off"` leaves only the first two.

The web UI's *Search index* dialog (the button next to the search box) shows,
for each share, how many files are indexed, when clerk last read it, and what
the last run found, and *Update now* starts a run. Its API:
`GET /api/index` for the status, `POST /api/index` to start a run.

`shelf index` runs the same once, in the foreground, e.g. after a big import:

```sh
shelf index
```

Two runs at once are refused, whichever started them.

A share whose directory is missing (an unmounted disk) is left out, and its
files stay in the index as they were.

## Searching

The search box in the web UI's header (shown when search is on) asks
`GET /api/search?q=words`. shelf runs `clerk find --json -- words` and returns
the files where every word is in the path or in the text, with where in the
text, at most 200:

```json
{"results": [{"path": "/files/scans/letter.pdf", "size": 1809489,
              "context": "…die goldene [Kugel] der Königstochter…"}],
 "truncated": false}
```

Every result must be in a share the user may access, and is opened through
that share like any other file, so a symlink in a share does not lead out of
it and a deleted file is not shown. One index holds all shares; the shares are
the boundary.

## The container

The image has clerk, tesseract (English and German) and poppler's `pdftoppm`,
and search is on, with the index at `/config/search/index.sqlite`. It is
distroless (`cc-debian13`, with glibc; no shell, no package manager), about
120 MB. The server indexes by itself, as above; by hand:

```sh
podman exec shelf /shelf index
```

`deploy/third-party.sh` collects the Debian packages and clerk's crates with
their licences while the image is built, and fails the build when they differ
from [THIRD_PARTY.md](../THIRD_PARTY.md). clerk is built from the commit
`CLERK_REV` names in the `Containerfile`; raising it may need THIRD_PARTY.md
updated.

`test/demo.sh` runs it with the files of `test/demo-data.sh` (public domain or
made by the script; their `SOURCES.md` says which) in a share in a directory
in `$XDG_RUNTIME_DIR`, which is memory, removed when it stops.
