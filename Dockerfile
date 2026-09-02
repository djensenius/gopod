# syntax=docker/dockerfile:1.7

ARG ALPINE_VERSION=3.22.2
ARG ALPINE_DIGEST=sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412
ARG GO_VERSION=1.27.0

FROM scratch AS go-amd64
ADD --checksum=sha256:675c26c449cbb18fc24b74650de1eabbae6e16f64326fd85a283fb3b58280685 \
    https://go.dev/dl/go1.27.0.linux-amd64.tar.gz /go.tar.gz

FROM scratch AS go-arm64
ADD --checksum=sha256:51798d2c42d0e1c6ed7fd9f48728b4193abac9e8aad6dbac2fe96a81f5909bda \
    https://go.dev/dl/go1.27.0.linux-arm64.tar.gz /go.tar.gz

# hadolint ignore=DL3006
FROM go-${BUILDARCH} AS go-toolchain

FROM scratch AS supercronic-amd64
ADD --checksum=sha256:a53ae236602c7338aba3fbaff40bda6300eae3b9fedb8261eb06cfe3724430c1 \
    https://github.com/aptible/supercronic/releases/download/v0.2.49/supercronic-linux-amd64 /supercronic

FROM scratch AS supercronic-arm64
ADD --checksum=sha256:02aa0cb229ba09050cba6638059dadb9eedc2276632ea43d6a57a2f8c1629dd5 \
    https://github.com/aptible/supercronic/releases/download/v0.2.49/supercronic-linux-arm64 /supercronic

# hadolint ignore=DL3006
FROM supercronic-${TARGETARCH} AS supercronic

FROM --platform=${BUILDPLATFORM} alpine:${ALPINE_VERSION}@${ALPINE_DIGEST} AS build
ARG BUILDARCH
ARG GO_VERSION
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

COPY --from=go-toolchain /go.tar.gz /tmp/go.tar.gz
RUN tar -C /usr/local -xzf /tmp/go.tar.gz \
    && rm /tmp/go.tar.gz \
    && test "$(/usr/local/go/bin/go version)" = "go version go${GO_VERSION} linux/${BUILDARCH}"

ENV PATH=/usr/local/go/bin:${PATH}
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY *.go ./
COPY podcast ./podcast

RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH="${TARGETARCH}" \
    go build \
      -buildvcs=false \
      -trimpath \
      -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
      -o /out/gopod \
      .

FROM alpine:${ALPINE_VERSION}@${ALPINE_DIGEST} AS runtime
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

LABEL org.opencontainers.image.title="GoPod" \
      org.opencontainers.image.description="Record configured podcast streams on demand or on a schedule" \
      org.opencontainers.image.source="https://github.com/djensenius/gopod" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.created="${DATE}" \
      org.opencontainers.image.licenses="MIT"

RUN apk add --no-cache \
      ca-certificates=20260611-r0 \
      ffmpeg=6.1.2-r2 \
      tzdata=2026c-r0 \
    && addgroup -S -g 10001 gopod \
    && adduser -S -D -H -u 10001 -G gopod gopod \
    && install -d -o 10001 -g 10001 -m 0755 /config /data

COPY --from=build --chmod=0555 /out/gopod /usr/local/bin/gopod
COPY --from=supercronic --chmod=0555 /supercronic /usr/local/bin/supercronic
COPY --chmod=0555 docker/entrypoint.sh /usr/local/bin/gopod-entrypoint

ENV GOPOD_CONFIG=/config/config.json \
    GOPOD_CRONTAB=/config/crontab \
    TZ=Etc/UTC

WORKDIR /data
USER 10001:10001

ENTRYPOINT ["/usr/local/bin/gopod-entrypoint"]
