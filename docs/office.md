# Office documents in the web UI (grind)

Goal: view and edit office documents (`.ods`, `.odt`, `.fods`, `.fodt`, and
`.xlsx`/`.docx` for viewing) right in shelf's web UI, using the WebAssembly web
shell of [grind](https://github.com/fwilhe2/grind) (`ui_web/`).

grind already brings the two pieces needed:

- **`?doc=<relative-url>`**: the page fetches the document at that path
  (relative to the page, never another host) and opens it.
- **The `api/v1` store protocol** (grind `doc/server.md` §11): the page probes
  `GET api/v1/hello` next to itself; when a server answers, Save writes back
  with `PUT api/v1/docs/{id}` and `If-Match`, and a stale version is a 412.

## Step 1 — view (and edit locally)

No change to grind.

- `shelf serve` serves grind's built `dist/` under `/office/` when an office
  directory is configured (`office_dir` in the config, or `$SHELF_OFFICE_DIR`).
  Without one, nothing changes.
  - Precompressed `*.gz` siblings are served with `Content-Encoding: gzip` when
    the browser accepts it.
  - `/office/` requires a login, like the rest of the UI.
- `/api/me` reports `office: true`; the file list then offers an *Open* action
  for office files: `/office/?doc=../dav/<share>/<path>`.
- The page is same-origin, so its `fetch` carries shelf's session cookie and
  share permissions apply unchanged.
- Edits stay in the browser; Save downloads a copy.
- The container image copies the page from the published grind image
  (`ghcr.io/fwilhe2/grind-web`, `/usr/share/grind/web`) and sets
  `SHELF_OFFICE_DIR`. A plain `go build` keeps working without grind.

## Step 2 — save back (ODF only)

- shelf implements a subset of `api/v1` under `/office/api/v1/`, with
  id = `<share>/<path>`:
  - `GET hello` → `{protocol: 1, commit, offers: ["store"]}`
  - `GET docs/{id}` → bytes + `ETag` (hash of the bytes)
  - `PUT docs/{id}` → `If-Match` required (428 without, 412 when stale),
    403 on read-only shares, atomic write
  - optionally `POST docs?name=` (*Save to the server as…*) and `GET docs`
- Requires the `X-Grind-Request` header on writes (CSRF; shelf's session cookie
  is `SameSite=Strict` already).
- Two small changes in grind:
  1. **Open a server document from the URL**: today `?doc=` always opens as a
     local document. Something like `?open=<id>` that opens it as
     `Backend::Remote` (~10 lines in `ui_web/src/lib.rs`).
  2. **Expose the build commit**: `hello` must return the exact commit the page
     was built from, or the page refuses to talk to the server. `build.sh`
     writes `dist/build.json`, which shelf reads.
- `.xlsx`/`.docx`: grind saves ODF, so these stay view-only, or get
  *save as `.ods` next to it*.

## Costs

| | |
|---|---|
| Image size | `dist/` is ~42 MB, half of it `.gz` duplicates. Serving only the `.gz` files is ~10 MB, roughly doubling the ~11 MB image. Dropping dictionaries or the print module shrinks it further. |
| Browser | ~2.2 MB gzipped wasm on first open, then cached; fonts and dictionaries load on demand. |
| Build | Copy the page from the grind image at a pinned tag instead of building Rust/wasm in shelf's Containerfile. |
| Coupling | Step 2 pins each shelf release to one grind build (the commit check). |
| License | grind is AGPL-3.0-or-later; shelf is AGPL-3.0-or-later too. |
