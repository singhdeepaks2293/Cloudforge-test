#!/usr/bin/env bash
# Generate a self-signed TLS certificate for CloudForge (Phase 7, R12).
#
# Usage:
#   ./scripts/gen-tls-cert.sh [hostname]
#
# Writes:
#   deployments/tls/fullchain.pem  (certificate)
#   deployments/tls/privkey.pem    (private key, 0600)
#
# The certificate is SELF-SIGNED for local learning deployments:
# browsers will warn about it until it is trusted — that is
# expected, and the warning is the honest signal that no public CA
# vouches for a local hostname. For a shared host, replace these
# files with a certificate from a real CA (e.g. Let's Encrypt) and
# keep the same filenames.
#
# After generating, start the stack with the TLS override:
#   docker compose --env-file .env \
#     -f deployments/compose/docker-compose.yml \
#     -f deployments/compose/docker-compose.tls.yml up -d --build
# and set CLOUDFORGE_TLS=1 in .env (HSTS + Secure session cookies).
# UNVERIFIED in this sandbox (no Docker/nginx here): the override
# and the nginx TLS config are reviewed, not run.
set -euo pipefail

HOST="${1:-localhost}"
OUT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/deployments/tls"
mkdir -p "$OUT_DIR"
chmod 700 "$OUT_DIR"

if [ -f "$OUT_DIR/fullchain.pem" ] && [ -f "$OUT_DIR/privkey.pem" ]; then
  echo "TLS files already exist in $OUT_DIR — refusing to overwrite."
  echo "Delete them first if you want a fresh certificate."
  exit 1
fi

# Self-signed, 365 days, RSA 2048. SAN covers the hostname, localhost
# and the LAN name operators usually reach the host by.
openssl req -x509 -newkey rsa:2048 -nodes \
  -keyout "$OUT_DIR/privkey.pem" \
  -out "$OUT_DIR/fullchain.pem" \
  -days 365 \
  -subj "/CN=$HOST" \
  -addext "subjectAltName=DNS:$HOST,DNS:localhost,IP:127.0.0.1"
chmod 600 "$OUT_DIR/privkey.pem"
chmod 644 "$OUT_DIR/fullchain.pem"

echo "Wrote:"
echo "  $OUT_DIR/fullchain.pem"
echo "  $OUT_DIR/privkey.pem"
openssl x509 -in "$OUT_DIR/fullchain.pem" -noout -subject -dates
echo
echo "Start with the TLS override (see header comment) and set CLOUDFORGE_TLS=1 in .env."
