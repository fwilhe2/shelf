# Third-party components

shelf is licensed under AGPL-3.0-or-later (see [LICENSE](LICENSE)). It ships
these components by others, each under its own licence. The web UI lists them
too: the ⓘ button in its header opens *About shelf*, which links to every
licence text below. The binary serves those texts itself, under `/vendor/` and
`/licenses/`.

Only permissive licences go into the binary. Each vendored component keeps its
upstream file names and licence files, unchanged.

## In the binary

| Component | Version | Licence | Where | Copyright |
|---|---|---|---|---|
| Go standard library | the one it is built with | BSD-3-Clause | compiled in | The Go Authors |
| golang.org/x/crypto, net, sys, term | see `go.mod` | BSD-3-Clause | compiled in | The Go Authors |
| [PDF.js](https://github.com/mozilla/pdf.js) | 6.5.438 | Apache-2.0 | `web/vendor/pdfjs-6.5.438/` | Mozilla Foundation and contributors |

The Go licence text is `web/licenses/go.txt` (identical for the standard
library and all four `golang.org/x` modules).

PDF.js brings its own parts, with their licence files beside them:

| Part | Licence | Licence file (under `web/vendor/pdfjs-6.5.438/`) |
|---|---|---|
| PDF.js itself | Apache-2.0 | `LICENSE` |
| OpenJPEG (JPEG 2000 decoder, `wasm/openjpeg.wasm`) | BSD-2-Clause | `wasm/LICENSE_OPENJPEG`, PDF.js's build of it: `wasm/LICENSE_PDFJS_OPENJPEG` (BSD-2-Clause) |
| PDFium's JBIG2 decoder (`wasm/jbig2.wasm`) | BSD-3-Clause | `wasm/LICENSE_JBIG2`, PDF.js's build of it: `wasm/LICENSE_PDFJS_JBIG2` (Apache-2.0) |
| qcms (colour management, `wasm/qcms_bg.wasm`) | MIT | `wasm/LICENSE_QCMS`, PDF.js's build of it: `wasm/LICENSE_PDFJS_QCMS` (MIT) |
| Foxit standard fonts (`standard_fonts/`) | BSD-3-Clause | `standard_fonts/LICENSE_FOXIT` |
| Adobe CMaps (`cmaps/`) | BSD-3-Clause | `cmaps/LICENSE` |
| CGATS001Compat ICC profile (`iccs/`) | CC0-1.0 | `iccs/LICENSE` |

Left out of the PDF.js package on purpose:

- The Liberation fonts from `standard_fonts/`. They are GPL-2.0 with a font
  exception, not permissive. PDF.js uses them only for PDFs that name Arial or
  Helvetica without embedding them, and falls back to the browser's sans-serif
  font without them.
- `wasm/quickjs-eval.*`, PDF.js's sandbox for scripts inside PDFs. shelf
  doesn't run them.
- The `*_nowasm_fallback.js` decoders, for browsers without WebAssembly.
- The modern `build/` and `web/`. shelf uses `legacy/`, which also works in
  browsers that are a few years old (tablets, older phones).
- Source maps, type definitions and `image_decoders/`.

## In the container image only

| Component | Licence | Where |
|---|---|---|
| [grind](https://github.com/fwilhe2/grind)'s web page | AGPL-3.0-or-later | `/usr/share/grind/web`, copied from `ghcr.io/fwilhe2/grind-web`. Its own third-party components are listed in the page: command palette (Ctrl+K) ▸ *About this build*. |
| [distroless](https://github.com/GoogleContainerTools/distroless) `static-debian13` | Apache-2.0, plus the licences of the Debian packages it contains | the base image, see `/usr/share/doc` in it |

## Updating PDF.js

```sh
v=6.5.438                                   # the new version
npm pack pdfjs-dist@$v && mkdir p && tar xzf pdfjs-dist-$v.tgz -C p && P=p/package
D=web/vendor/pdfjs-$v
mkdir -p $D/legacy/build $D/legacy/web $D/wasm
cp $P/LICENSE $D/
cp $P/legacy/build/pdf.min.mjs $P/legacy/build/pdf.worker.min.mjs $D/legacy/build/
cp -r $P/legacy/web/pdf_viewer.mjs $P/legacy/web/pdf_viewer.css $P/legacy/web/images $D/legacy/web/
cp $P/wasm/*.wasm $P/wasm/LICENSE_* $D/wasm/ && rm -f $D/wasm/*QUICKJS* $D/wasm/quickjs*
cp -r $P/standard_fonts $P/cmaps $P/iccs $D/
rm $D/standard_fonts/Liberation* $D/standard_fonts/LICENSE_LIBERATION
```

Then remove the old `web/vendor/pdfjs-*` directory, set `PDFJS_VERSION` in
`web/index.html`, check that every licence file is still the one listed here,
and update this file. `go test` fails when `web/index.html` names a vendored
file that is missing. The version is part of the path, so browsers fetch the
new files at once and can cache them for good.
