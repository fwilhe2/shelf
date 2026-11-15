# mynas container image: a static Go binary on distroless, running as non-root.
#
#   podman build -t localhost/mynas:latest .
#
# Cross-build for a Raspberry Pi:  podman build --platform linux/arm64 -t localhost/mynas:latest .

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY web/ web/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags='-s -w -buildid=' -o /mynas .

# static-debian13: no shell, no package manager, no libc; ~2 MB plus the binary.
# :nonroot runs as uid/gid 65532.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /mynas /mynas
ENV MYNAS_CONFIG=/config/config.json
# No VOLUME instruction on purpose: it would silently create anonymous volumes
# when a mount is forgotten, and files would end up somewhere unexpected.
# Mount /config and /data explicitly (see deploy/mynas.container).
EXPOSE 8080
ENTRYPOINT ["/mynas"]
CMD ["serve"]
