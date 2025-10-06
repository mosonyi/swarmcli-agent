#!/bin/bash
set -euo pipefail

CERTS_DIR=certs
mkdir -p $CERTS_DIR

echo "[*] Generating CA key and certificate..."
openssl genrsa -out $CERTS_DIR/ca.key 4096
openssl req -x509 -new -nodes -key $CERTS_DIR/ca.key -sha256 -days 3650 \
  -subj "/CN=swarmcli-ca" -out $CERTS_DIR/ca.crt

echo "[*] Generating Agent cert and key..."
openssl genrsa -out $CERTS_DIR/agent.key 2048
openssl req -new -key $CERTS_DIR/agent.key -subj "/CN=swarmcli-agent" -out $CERTS_DIR/agent.csr

# Add SANs for localhost and 127.0.0.1
openssl x509 -req -in $CERTS_DIR/agent.csr -CA $CERTS_DIR/ca.crt -CAkey $CERTS_DIR/ca.key -CAcreateserial \
  -out $CERTS_DIR/agent.crt -days 365 -sha256 \
  -extfile <(printf "subjectAltName=DNS:localhost,IP:127.0.0.1")

echo "[*] Generating Proxy cert and key..."
openssl genrsa -out $CERTS_DIR/proxy.key 2048
openssl req -new -key $CERTS_DIR/proxy.key -subj "/CN=swarmcli-proxy" -out $CERTS_DIR/proxy.csr

# Add SANs for localhost, host.docker.internal, and 127.0.0.1
openssl x509 -req -in $CERTS_DIR/proxy.csr -CA $CERTS_DIR/ca.crt -CAkey $CERTS_DIR/ca.key -CAcreateserial \
  -out $CERTS_DIR/proxy.crt -days 365 -sha256 \
  -extfile <(printf "subjectAltName=DNS:localhost,DNS:host.docker.internal,IP:127.0.0.1")

echo "[*] Done. Certificates are in $CERTS_DIR/:"
ls -l $CERTS_DIR
