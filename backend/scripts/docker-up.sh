#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
  echo "请先安装 Docker 及 Compose v2。" >&2
  exit 1
fi
exec docker compose --project-directory "$project_dir" -f "$project_dir/compose.yaml" up -d --build "$@"
