#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
frontend_dir="$repo_root/internal/webui/frontend"
output_dir="$repo_root/bin"

if [[ "$(go env GOOS)" != "linux" ]]; then
  echo "Production binaries must be built on Linux." >&2
  exit 1
fi

source /etc/os-release
if [[ "$ID" != "ubuntu" || "$VERSION_ID" != "20.04" ]]; then
  echo "Production binaries must be built on Ubuntu 20.04." >&2
  exit 1
fi
if [[ "$(ldd --version | head -n 1 | awk '{print $NF}')" != "2.31" ]]; then
  echo "Production agent requires a glibc 2.31 build environment." >&2
  exit 1
fi

mkdir -p "$output_dir"
cd "$frontend_dir"
pnpm install --frozen-lockfile
pnpm run build

cd "$repo_root"
go mod download
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$output_dir/gpu-scheduler-controller" ./cmd/controller
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$output_dir/gpu-scheduler-agent" ./cmd/agent
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$output_dir/gpu-scheduler-admin" ./cmd/admin

echo "Binaries written to $output_dir"
