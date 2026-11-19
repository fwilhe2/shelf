# shelf container image: a static Go binary on distroless, running as non-root.
#
#   podman build -t localhost/shelf:latest .
#
# Cross-build for a Raspberry Pi:  podman build --platform linux/arm64 -t localhost/shelf:latest .
#
# The image includes grind's web page, to open office documents in the browser
# (docs/office.md). Pick another build of it with --build-arg GRIND_WEB=IMAGE.

ARG GRIND_WEB=ghcr.io/fwilhe2/grind-web:latest

# grind's page is the same for every architecture. Keep only the precompressed
# copy of each file (~10 MB instead of ~42 MB); shelf decompresses it for the
# rare browser that does not accept gzip.
FROM --platform=$BUILDPLATFORM ${GRIND_WEB} AS grind
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS office
COPY --from=grind /usr/share/grind/web /office
RUN find /office -type f ! -name '*.gz' -exec sh -c 'test -f "$1.gz" && rm "$1" || true' _ {} \; \
 && test -f /office/index.html.gz

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY web/ web/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w -buildid=' -o /shelf .

# ---- The image with search (docs/search.md), distroless too:
#
#   podman build --target search -t localhost/shelf:search .
#
# shelf, clerk, and tesseract and poppler's pdftoppm for clerk to read scans and
# photos, with only the Debian packages they load. deploy/third-party.sh
# collects them and their licences, and fails the build when they differ from
# THIRD_PARTY.md.

# clerk, at a fixed commit, so its crates (and their licences) change only here.
FROM docker.io/library/rust:1-slim-trixie AS clerk
ARG CLERK_REV=5ec47eec0739d68fae22954513377a7e705f5dc3
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates
RUN git init -q /src && cd /src && git fetch -q --depth 1 https://github.com/fwilhe2/clerk "$CLERK_REV" \
 && git checkout -q FETCH_HEAD
WORKDIR /src
RUN cargo build --release --locked -p clerk-cli \
 && install -D target/release/clerk /out/usr/local/bin/clerk
COPY THIRD_PARTY.md deploy/third-party.sh /tmp/
RUN /tmp/third-party.sh crates /out /tmp/THIRD_PARTY.md

FROM gcr.io/distroless/cc-debian13:nonroot AS cc

# tesseract (English and German) and pdftoppm, from the same Debian release as
# the distroless base, so they run on its glibc.
FROM docker.io/library/debian:trixie-slim AS ocr
RUN apt-get update && apt-get install -y --no-install-recommends \
    tesseract-ocr tesseract-ocr-eng tesseract-ocr-deu poppler-utils fontconfig-config fonts-dejavu-core
COPY --from=cc /var/lib/dpkg/status.d /tmp/base-status.d
COPY THIRD_PARTY.md deploy/third-party.sh /tmp/
RUN /tmp/third-party.sh debs /out /tmp/base-status.d /tmp/THIRD_PARTY.md /usr/bin/tesseract /usr/bin/pdftoppm \
    -- tesseract-ocr-eng tesseract-ocr-deu fontconfig-config fonts-dejavu-core

# cc-debian13: distroless with glibc and libstdc++, no shell, no package manager.
FROM gcr.io/distroless/cc-debian13:nonroot AS search
COPY --from=ocr /out /
COPY --from=clerk /out /
COPY --from=build /shelf /shelf
COPY --from=office /office /usr/share/grind/web
# Each of them runs, with every library and its data found.
RUN ["/usr/bin/tesseract", "--list-langs"]
RUN ["/usr/bin/pdftoppm", "-v"]
RUN ["/usr/local/bin/clerk", "--version"]
ENV SHELF_CONFIG=/config/config.json
ENV SHELF_OFFICE_DIR=/usr/share/grind/web
# The index is the only thing kept besides the config; `shelf index` fills it.
ENV SHELF_SEARCH_INDEX=/config/search/index.sqlite
EXPOSE 8443
ENTRYPOINT ["/shelf"]
CMD ["serve"]

# ---- The default image, without search.
# static-debian13: no shell, no package manager, no libc; ~2 MB plus the binary.
# :nonroot runs as uid/gid 65532.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /shelf /shelf
COPY --from=office /office /usr/share/grind/web
ENV SHELF_CONFIG=/config/config.json
# Set it to empty (e.g. Environment=SHELF_OFFICE_DIR= in the Quadlet unit) to turn office documents off.
ENV SHELF_OFFICE_DIR=/usr/share/grind/web
# No VOLUME instruction on purpose: it would silently create anonymous volumes
# when a mount is forgotten, and files would end up somewhere unexpected.
# Mount /config and /data explicitly (see deploy/shelf.container).
EXPOSE 8443
ENTRYPOINT ["/shelf"]
CMD ["serve"]
