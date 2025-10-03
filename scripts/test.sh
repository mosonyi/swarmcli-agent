#!/usr/bin/env bash
set -euo pipefail

PROXY=${PROXY:-wss://localhost:8443}

if [ $# -lt 1 ]; then
  echo "Usage: $0 <TASK_ID>"
  exit 1
fi

TASK_ID=$1

echo "==> Health check proxy"
curl -s ${PROXY/http/wss}/health || true

echo "==> Exec test (hostname)"
go run ./cmd/test -mode exec -task $TASK_ID -proxy $PROXY -cmd "hostname"

echo "==> Logs test (last 10 lines)"
go run ./cmd/test -mode logs -task $TASK_ID -proxy $PROXY -tail 10
