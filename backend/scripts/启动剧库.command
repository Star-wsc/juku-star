#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
case "$(uname -m)" in
  arm64) binary="./dist/juku_darwin_arm64" ;;
  x86_64) binary="./dist/juku_darwin_amd64" ;;
  *) echo "不支持的 macOS 架构，请使用 go run ."; exit 1 ;;
esac
if [ ! -x "$binary" ]; then
  echo "找不到可执行程序，请先在项目目录运行 ./scripts/build.sh。"
  exit 1
fi
exec "$binary" "$@"
