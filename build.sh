#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ge 1 && "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  exec go run ./cmd/rel "$@"
fi

exec go run ./cmd/build "$@"
