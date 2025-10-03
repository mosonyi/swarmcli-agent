#!/bin/bash
set -euo pipefail

CERTS_DIR=certs

if [ ! -d "$CERTS_DIR" ]; then
  echo "Error: $CERTS_DIR directory not found. Run ./gen-certs.sh first."
  exit 1
fi

echo "[*] Creating Docker Swarm secrets from $CERTS_DIR..."

docker secret create agent_cert $CERTS_DIR/agent.crt || true
docker secret create agent_key $CERTS_DIR/agent.key || true
docker secret create agent_ca $CERTS_DIR/ca.crt || true
docker secret create proxy_client_cert $CERTS_DIR/proxy.crt || true
docker secret create proxy_client_key $CERTS_DIR/proxy.key || true

echo "[*] Secrets created:"
docker secret ls | grep -E 'agent_|proxy_'
