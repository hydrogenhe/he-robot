#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
go test -race ./...
# Node仅用于开发时验证网页脚本；Go程序运行不依赖Node。
if command -v node >/dev/null 2>&1; then
  node --test web/test/*.test.mjs
fi
# Python仅用于视频实验的离线回归；不调用模型或平台。
if command -v python3 >/dev/null 2>&1; then
  PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts/test -p 'test_video_sampling.py'
fi
