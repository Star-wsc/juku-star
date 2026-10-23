#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if ! command -v docker >/dev/null 2>&1; then
  echo "请先安装 Docker。" >&2
  exit 1
fi
exec docker build --pull -t "${JUKU_IMAGE:-juku:local}" "$@" "$project_dir"
