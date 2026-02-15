#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"
echo "Building cc-top..."
go build -o cc-top ./cmd/cc-top/
echo "Starting cc-top..."
exec ./cc-top "$@"
