# ── Stage 1: Build ────────────────────────────────────────────────────────────
FROM golang:1.26-alpine AS builder

# ca-certificates needed at build time for go modules over HTTPS
RUN apk add --no-cache ca-certificates git

WORKDIR /app

# Copy dependency files first — Docker layer cache avoids re-downloading
# modules when only source files change.
COPY go.mod go.sum ./

# Download modules (skipped if vendor/ directory is present and up-to-date).
RUN go mod download

# Copy the full source tree.
COPY . .

# Build a statically-linked binary.
# CGO_ENABLED=0  → pure Go, no libc dependency
# -trimpath      → removes local build paths from the binary (reproducible builds)
# -ldflags       → strip debug info to reduce binary size
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /app/reviewer \
    ./cmd/server

# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.21

# ca-certificates: needed for HTTPS calls to GitHub API and AI providers
# tzdata: needed if you ever add time-zone aware logging
RUN apk add --no-cache ca-certificates tzdata

# Run as a non-root user — defence in depth.
RUN addgroup -S reviewer && adduser -S reviewer -G reviewer

WORKDIR /app

# Copy only the compiled binary from the builder stage.
COPY --from=builder /app/reviewer .

# Ensure the binary is owned and executable by the app user.
RUN chown reviewer:reviewer /app/reviewer

USER reviewer

EXPOSE 3000

# HEALTHCHECK uses $PORT so it works with Railway's dynamic port assignment.
# Railway overrides PORT at runtime; default 3000 is used locally.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
    CMD wget -qO- http://localhost:${PORT:-3000}/health || exit 1

CMD ["./reviewer"]
