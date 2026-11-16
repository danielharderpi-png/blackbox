#!/usr/bin/env sh
# Installs `blackbox` and `bbox` (macOS/Linux). Requires Go 1.21+.
set -eu

cd "$(dirname "$0")"
command -v go >/dev/null 2>&1 || { echo "error: Go is not installed (https://go.dev/dl/)" >&2; exit 1; }

go install ./cmd/blackbox ./cmd/bbox

bindir="$(go env GOBIN)"
[ -n "$bindir" ] || bindir="$(go env GOPATH)/bin"
echo "installed blackbox and bbox to $bindir"

case ":$PATH:" in
  *":$bindir:"*) ;;
  *) echo "note: $bindir is not on your PATH. Add this to your shell profile:"
     echo "  export PATH=\"\$PATH:$bindir\"" ;;
esac
