#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
if [ ! -x "./dist/juku_linux_amd64" ]; then
  echo "找不到 Linux x64 程序，请先运行 ./scripts/build.sh。"
  exit 1
fi
exec ./dist/juku_linux_amd64 "$@"
