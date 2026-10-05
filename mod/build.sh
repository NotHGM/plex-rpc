#!/usr/bin/env bash
# Builds the Plex RPC in-app mod: version.dll (proxy) + plexrpc_core.dll (engine).
#
#   DISCORD_CLIENT_ID=... TOOLCHAIN=/path/to/llvm-mingw/bin mod/build.sh [version]
#
# Requires an llvm-mingw (or mingw-w64) cross toolchain on PATH, plus Go.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo dev)}"
CLIENT_ID="${DISCORD_CLIENT_ID:-}"
CC_WIN="${CC_WIN:-x86_64-w64-mingw32-clang}"

mkdir -p mod/dist

echo "building plexrpc_core.dll"
CGO_ENABLED=1 GOOS=windows GOARCH=amd64 CC="$CC_WIN" \
	go build -trimpath -buildmode=c-shared \
	-ldflags "-s -w -X main.version=$VERSION -X main.defaultClientID=$CLIENT_ID" \
	-o mod/dist/plexrpc_core.dll ./mod/core

echo "building version.dll"
"$CC_WIN" -O2 -shared -o mod/dist/version.dll mod/proxy/proxy.c mod/proxy/version.def

rm -f mod/dist/plexrpc_core.h
(cd mod/dist && sha256sum version.dll plexrpc_core.dll > SHA256SUMS.txt)
echo "done:"
ls -l mod/dist
