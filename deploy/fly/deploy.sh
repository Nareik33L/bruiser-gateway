#!/usr/bin/env bash
# Run from anywhere. Requires `fly auth login` on the operator machine.
# Does not embed tokens.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"
exec fly deploy --config deploy/fly/fly.toml --dockerfile deploy/fly/Dockerfile "$@"
