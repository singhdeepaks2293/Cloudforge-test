# syntax=docker/dockerfile:1
# CloudForge Rust network engine — M1 image.
# Pinned base images; no `latest` tags.
#
# Builder needs protoc (tonic-build). It is installed here from Debian;
# the build.rs also falls back to a vendored protoc if none is on PATH.
FROM rust:1.100-bookworm AS builder
RUN apt-get update \
    && apt-get install -y --no-install-recommends protobuf-compiler \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
# Copy the workspace and the shared proto contracts (paths are relative
# to the CloudForge repo root as build context).
COPY rust-network-engine/ ./rust-network-engine/
COPY proto/ ./proto/
WORKDIR /build/rust-network-engine
RUN cargo build --release --locked

# Runtime: M1 needs NO privileges. Do not add CAP_NET_ADMIN here —
# the M1 backend is simulated. M4 (rtnetlink/nftables) will add the
# minimal capabilities and must run as a separate, reviewed image.
FROM debian:bookworm-slim
RUN groupadd -r cloudforge && useradd -r -g cloudforge cloudforge
COPY --from=builder /build/rust-network-engine/target/release/network-engine /usr/local/bin/network-engine
USER cloudforge
EXPOSE 50051
ENV NETWORK_ENGINE_ADDR=0.0.0.0:50051
ENV RUST_LOG=network_engine=info,grpc_server=info
# No HEALTHCHECK in M1: health is exposed via gRPC only
# (`NetworkEngine/Health`), and this image has no grpc_health_probe.
# M4 adds a probe-based healthcheck alongside the real backend.
ENTRYPOINT ["/usr/local/bin/network-engine"]
