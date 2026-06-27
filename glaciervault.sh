#!/usr/bin/env bash
# Convenience wrapper — run from the repo root.
set -euo pipefail

cd "$(dirname "$0")"

case "${1:-help}" in
  build)
    docker build -t glaciervault:latest -f docker/Dockerfile .
    ;;
  up)
    cd docker && docker compose up -d
    ;;
  down)
    cd docker && docker compose down
    ;;
  logs)
    cd docker && docker compose logs -f
    ;;
  *)
    echo "Usage: $0 {build|up|down|logs}"
    exit 1
    ;;
esac
