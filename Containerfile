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
