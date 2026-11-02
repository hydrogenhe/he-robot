#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p bin
go build -trimpath -o bin/gpt-robot ./cmd/gpt-robot
