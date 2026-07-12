#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cd "$ROOT_DIR/console"
pnpm install --frozen-lockfile
pnpm lint
pnpm build

cd "$ROOT_DIR"
rm -rf internal/webui/dist
mkdir -p internal/webui/dist
cp -R console/dist/. internal/webui/dist/

go test ./...
mkdir -p bin
go build -o bin/tarisya-core ./cmd/core
go build -o bin/tarisya-agent ./cmd/agent