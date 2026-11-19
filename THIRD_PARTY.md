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

The container image is [distroless](https://github.com/GoogleContainerTools/distroless)
`cc-debian13` (Apache-2.0, plus the licences of the Debian packages it
contains). It adds grind's web page for office documents, and for full-text
search clerk, and tesseract and poppler's `pdftoppm` with the Debian packages
they load (see [docs/search.md](docs/search.md)).

| Component | Licence | Where |
|---|---|---|
| [grind](https://github.com/fwilhe2/grind)'s web page | AGPL-3.0-or-later | `/usr/share/grind/web`, copied from `ghcr.io/fwilhe2/grind-web`. Its own third-party components are listed in the page: command palette (Ctrl+K) ▸ *About this build*. |

`deploy/third-party.sh` collects these while the image is built, and **the
build fails when they differ from the lists below**: a new package, a package
gone, a new licence among clerk's crates. Then update this file. The image
itself carries, for every part:

- Debian packages: `/usr/share/doc/<package>/copyright` (the full licence
  texts) and `/var/lib/dpkg/status.d/<package>` (the exact version, as
  distroless keeps it, so image scanners see them).
- clerk's crates: `/usr/share/doc/clerk/crates.txt` (name, version, licence
  of each) and each crate's licence files in `/usr/share/doc/clerk/crates/`.

### clerk

| Component | Licence | Where |
|---|---|---|
| [clerk](https://github.com/fwilhe2/clerk) | AGPL-3.0-or-later | `/usr/local/bin/clerk`, built from the commit `CLERK_REV` names in the `Containerfile` |
| [grind](https://github.com/fwilhe2/grind)'s readers (`grind-*` crates) | AGPL-3.0-or-later | compiled into clerk |
| SQLite (bundled by the `libsqlite3-sys` crate) | public domain | compiled into clerk |
| clerk's other crates | permissive, see below | compiled into clerk |

Licences of the crates compiled into clerk (from `cargo tree`; the crate
`libsqlite3-sys` is MIT, the SQLite inside it public domain):

<!-- crate-licences -->
| Licence expression, as the crates state it |
|---|
| `0BSD OR MIT OR Apache-2.0` |
| `AGPL-3.0-or-later` |
| `Apache-2.0` |
| `Apache-2.0/MIT` |
| `Apache-2.0 OR MIT` |
| `(Apache-2.0 OR MIT) AND BSD-3-Clause` |
| `Apache-2.0 WITH LLVM-exception OR Apache-2.0 OR MIT` |
| `CC0-1.0` |
| `CC0-1.0 OR Apache-2.0 OR Apache-2.0 WITH LLVM-exception` |
| `CC0-1.0 OR MIT-0 OR Apache-2.0` |
| `ISC` |
| `MIT` |
| `MIT / Apache-2.0` |
| `MIT/Apache-2.0` |
| `MIT OR Apache-2.0` |
| `(MIT OR Apache-2.0) AND Unicode-3.0` |
| `MIT OR Zlib OR Apache-2.0` |
| `Unlicense/MIT` |
| `Unlicense OR MIT` |
| `Zlib` |
| `Zlib OR Apache-2.0 OR MIT` |
<!-- /crate-licences -->

These crates have no licence file of their own, so the image has the MIT
licence their `Cargo.toml` names, with their authors as the copyright holders:

<!-- crates-without-licence-file -->
| Crate | Licence |
|---|---|
| `adobe-cmap-parser` | MIT |
| `pdf-extract` | MIT |
| `type1-encoding-parser` | MIT |
<!-- /crates-without-licence-file -->

### Debian packages

From Debian 13 (trixie), the release the distroless base is built from. The
licence column is the main licence of each; parts of some are under others,
all in the package's `copyright` file. Versions change with Debian's security
updates; the image lists the exact ones.

**GPL**: poppler (`libpoppler147`, `poppler-utils`) and jbigkit (`libjbig0`)
are GPL only. Whoever passes the image on must offer their source code; it is
Debian's, at the versions in `/var/lib/dpkg/status.d/`, from
[sources.debian.org](https://sources.debian.org) or
[snapshot.debian.org](https://snapshot.debian.org). shelf and clerk only run
`pdftoppm` as a separate program.

<!-- debian-packages -->
| Package | Source package | Licence |
|---|---|---|
| `fontconfig-config` | fontconfig | MIT-like (fontconfig) |
| `fonts-dejavu-core` | fonts-dejavu | Bitstream Vera and public domain (fonts) |
| `libacl1` | acl | LGPL-2.1+ |
| `libarchive13t64` | libarchive | BSD-2-Clause and others |
| `libassuan9` | libassuan | LGPL-2.1+ |
| `libbrotli1` | brotli | MIT |
| `libbz2-1.0` | bzip2 | bzip2 (BSD-like) |
| `libcom-err2` | e2fsprogs | MIT-like (com_err) |
| `libcurl3t64-gnutls` | curl | curl (MIT-like) |
| `libcurl4t64` | curl | curl (MIT-like) |
| `libdeflate0` | libdeflate | MIT |
| `libexpat1` | expat | MIT |
| `libffi8` | libffi | MIT |
| `libfontconfig1` | fontconfig | MIT-like (fontconfig) |
| `libfreetype6` | freetype | FTL or GPL-2+ |
| `libgif7` | giflib | MIT |
| `libgmp10` | gmp | LGPL-3+ or GPL-2+ |
| `libgnutls30t64` | gnutls28 | LGPL-2.1+ |
| `libgpg-error0` | libgpg-error | LGPL-2.1+ |
| `libgpgme11t64` | gpgme1.0 | LGPL-2.1+ |
| `libgpgmepp6t64` | gpgme1.0 | LGPL-2.1+ |
| `libgssapi-krb5-2` | krb5 | MIT (krb5) |
| `libhogweed6t64` | nettle | LGPL-3+ or GPL-2+ |
| `libidn2-0` | libidn2 | LGPL-3+ or GPL-2+ |
| `libjbig0` | jbigkit | **GPL-2+** |
| `libjpeg62-turbo` | libjpeg-turbo | IJG, BSD-3-Clause, Zlib |
| `libk5crypto3` | krb5 | MIT (krb5) |
| `libkeyutils1` | keyutils | LGPL-2+ |
| `libkrb5-3` | krb5 | MIT (krb5) |
| `libkrb5support0` | krb5 | MIT (krb5) |
| `liblcms2-2` | lcms2 | MIT |
| `libldap2` | openldap | OpenLDAP-2.8 |
| `libleptonica6` | leptonlib | BSD-2-Clause (Leptonica) |
| `liblerc4` | lerc | Apache-2.0 |
| `liblz4-1` | lz4 | BSD-2-Clause |
| `liblzma5` | xz-utils | 0BSD |
| `libnettle8t64` | nettle | LGPL-3+ or GPL-2+ |
| `libnghttp2-14` | nghttp2 | MIT |
| `libnghttp3-9` | nghttp3 | MIT |
| `libngtcp2-16` | ngtcp2 | MIT |
| `libngtcp2-crypto-gnutls8` | ngtcp2 | MIT |
| `libnspr4` | nspr | MPL-2.0 |
| `libnss3` | nss | MPL-2.0 |
| `libopenjp2-7` | openjpeg2 | BSD-2-Clause |
| `libp11-kit0` | p11-kit | BSD-3-Clause |
| `libpng16-16t64` | libpng1.6 | libpng |
| `libpoppler147` | poppler | **GPL-2 or GPL-3** |
| `libpsl5t64` | libpsl | MIT |
| `librtmp1` | rtmpdump | LGPL-2.1+ |
| `libsasl2-2` | cyrus-sasl2 | BSD-style (Cyrus SASL) |
| `libsharpyuv0` | libwebp | BSD-3-Clause |
| `libssh2-1t64` | libssh2 | BSD-3-Clause |
| `libtasn1-6` | libtasn1-6 | LGPL-2.1+ |
| `libtesseract5` | tesseract | Apache-2.0 |
| `libtiff6` | tiff | libtiff (BSD-like) |
| `libunistring5` | libunistring | LGPL-3+ or GPL-2+ |
| `libwebp7` | libwebp | BSD-3-Clause |
| `libwebpmux3` | libwebp | BSD-3-Clause |
| `libxml2` | libxml2 | MIT |
| `libxxhash0` | xxhash | BSD-2-Clause |
| `poppler-utils` | poppler | **GPL-2 or GPL-3** |
| `tesseract-ocr` | tesseract | Apache-2.0 |
| `tesseract-ocr-deu` | tesseract-lang | Apache-2.0 |
| `tesseract-ocr-eng` | tesseract-lang | Apache-2.0 |
<!-- /debian-packages -->

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
