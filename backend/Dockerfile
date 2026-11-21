# syntax=docker/dockerfile:1
# CloudForge API — multi-stage build (Milestone 9, Part 3).
#
# Builder: Go 1.26, pinned by digest (resolved 2026-11-21 for
# golang:1.26.0-bookworm). Runtime: distroless static (no shell, no
# package manager, no wget/curl — the image's own healthcheck uses
# the api binary's --health-check flag instead).
#
# Security posture:
# - Runs as the distroless `nonroot` user (uid 65532), never root.
# - No added Linux capabilities; the api needs none (only the
#   network-engine service carries NET_ADMIN, via compose).
# - No secrets are baked in: configuration comes from the
#   environment at runtime (.env / compose `environment:`).
# - CA certificates are copied from the builder so outbound TLS
#   (e.g. MinIO with MINIO_USE_SSL=true) can verify.
# - The build is static (CGO_ENABLED=0) and reproducible-ish:
#   -trimpath and -ldflags="-s -w" strip paths/symbols.
#
# The same image also carries the worker binary (/app/worker) so a
# deployment can run `worker` from this image with a different
# compose command; the api service itself only runs /app/api.
#
# Build context is backend/ (see deployments/compose/docker-compose.yml).
FROM golang:1.26.0-bookworm@sha256:2a0ba12e116687098780d3ce700f9ce3cb340783779646aafbabed748fa6677c AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 GOTOOLCHAIN=local go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

# NOTE on the runtime base: distroless does not publish numeric
# version tags; `static-debian12:nonroot` is the stable codename tag
# for the Debian 12 static image with the nonroot user. The digest
# could not be resolved from this sandbox (registry proxy), so pin
# it in CI with e.g. `docker buildx imagetools inspect
# gcr.io/distroless/static-debian12:nonroot` and append
# @sha256:<digest> here before a production release.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /out/api /app/api
COPY --from=builder /out/worker /app/worker
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 \
  CMD ["/app/api", "--health-check"]
ENTRYPOINT ["/app/api"]
