#!/bin/sh
set -eu

# Compatibility with the installation command published before go install
# became the only supported path. Go itself chooses GOBIN or GOPATH/bin.
if ! command -v go >/dev/null 2>&1; then
	echo "The Go toolchain is required, and \`go\` was not found on PATH." >&2
	echo "Earlier versions of this script downloaded a prebuilt binary; installation" >&2
	echo "now goes through go install, so Go 1.25 or newer has to be present." >&2
	echo "Install it from https://go.dev/dl/, then run:" >&2
	echo "  go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest" >&2
	exit 1
fi

if [ -n "${INSTALL_DIR:-}" ]; then
	echo "INSTALL_DIR is no longer supported. Set GOBIN instead, then run go install." >&2
	exit 1
fi

echo "install.sh now uses go install. Prefer running this command directly:" >&2
echo "  go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest" >&2
exec go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest
