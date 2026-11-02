#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go test -race ./...
# Node仅用于开发时验证网页脚本；Go程序运行不依赖Node。
if command -v node >/dev/null 2>&1; then
  node --test web/test/*.test.mjs
fi
