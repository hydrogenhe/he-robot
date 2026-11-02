#!/bin/zsh
# 预编译Go程序运行无需Node；缺少程序时从源码构建。
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
cd "$(dirname "$0")/.." || exit 1
if [ ! -x bin/gpt-robot ]; then
  if ! command -v go >/dev/null; then
    echo '请使用匹配本机的预编译程序，或先安装 Go 1.25+ 后从源码构建。'
    read -r '?按回车退出'; exit 1
  fi
  bash scripts/build.sh || exit 1
fi
bin/gpt-robot serve --root "$PWD" --open
read -r '?服务已停止，按回车关闭'
