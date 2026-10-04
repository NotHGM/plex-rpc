#!/usr/bin/env bash
# Builds the Windows release binaries into dist/.
#
#   DISCORD_CLIENT_ID=123456789012345678 scripts/build.sh [version]
#
# The version defaults to the latest git tag.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
CLIENT_ID="${DISCORD_CLIENT_ID:-}"

go generate ./cmd/plex-rpc

rm -rf dist
mkdir -p dist
for arch in amd64 arm64; do
	GOOS=windows GOARCH=$arch CGO_ENABLED=0 go build -trimpath \
		-ldflags "-H windowsgui -s -w -X main.version=$VERSION -X main.defaultClientID=$CLIENT_ID" \
		-o "dist/plex-rpc-windows-$arch.exe" ./cmd/plex-rpc
done
(cd dist && sha256sum ./*.exe | sed 's# \./# #' > SHA256SUMS.txt)

if [ -z "$CLIENT_ID" ]; then
	echo "warning: DISCORD_CLIENT_ID not set; users must add discord_client_id to config.json" >&2
fi
echo "built $VERSION:"
ls -l dist
