# Multi-stage build for contextd.
#
# CGo is required because sqlite-vec (mattn/go-sqlite3) loads the vec0
# extension via sqlite3_auto_extension. We build on Alpine (musl libc) and
# run on Alpine to avoid any glibc/musl mismatch.

# ── builder ──────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

# gcc + musl-dev are needed for CGo (mattn/go-sqlite3).
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Download dependencies before copying source so Docker can cache this layer.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=1 go build \
    -tags sqlite_fts5 \
    -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /contextd \
    ./cmd/contextd

# ── runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.20

RUN apk add --no-cache ca-certificates

# Non-root user/group.
RUN addgroup -S contextd && adduser -S contextd -G contextd

# Data directory (mount a volume here for persistence).
RUN mkdir -p /var/lib/contextd /etc/contextd && \
    chown contextd:contextd /var/lib/contextd

# Default container config: listen on all interfaces, store data in the volume.
# Mount your own config at /etc/contextd/contextd.yaml to override.
RUN printf 'server:\n  host: "0.0.0.0"\n  port: 8080\nstorage:\n  path: "/var/lib/contextd/contextd.db"\n' \
    > /etc/contextd/contextd.yaml

COPY --from=builder /contextd /usr/local/bin/contextd

USER contextd

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

VOLUME ["/var/lib/contextd"]

CMD ["contextd", "serve", "--config", "/etc/contextd/contextd.yaml"]
