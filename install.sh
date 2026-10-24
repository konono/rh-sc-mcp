#!/bin/bash
set -euo pipefail

REPO="konono/rh-sc-mcp"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
COMMAND_NAME="rh-sc-mcp"

echo "Installing $COMMAND_NAME..."

# Detect OS. Only the targets goreleaser actually builds are accepted;
# windows is not published because the server has not been run there.
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux|darwin) ;;
  *)
    echo "Error: Unsupported OS: $OS" >&2
    echo "Released binaries cover linux and darwin only. Build from source with: go build ./cmd/$COMMAND_NAME" >&2
    exit 1
    ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

# Get latest release tag.
#
# The fetch is tested with `if` rather than assigned directly: under
# `set -e` a failing command substitution ends the script at the
# assignment, so a 404 -- which is exactly what a repository with no
# published release returns -- would exit silently, before any of the
# advice below could be printed.
if ! RELEASE_JSON=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest"); then
  echo "" >&2
  echo "Error: could not fetch the latest release of $REPO." >&2
  echo "If no release has been published yet, or the repository is private, build from source:" >&2
  echo "  git clone https://github.com/$REPO && cd ${REPO#*/}" >&2
  echo "  go build -o $COMMAND_NAME ./cmd/$COMMAND_NAME" >&2
  exit 1
fi

# `|| true` for the same reason: grep exits non-zero when it matches
# nothing, which would end the script before the check below.
LATEST=$(printf '%s' "$RELEASE_JSON" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/' || true)
if [ -z "$LATEST" ]; then
  echo "Error: the release API answered, but no tag_name was found in the response." >&2
  exit 1
fi

# Must match archives.name_template in .goreleaser.yml.
URL="https://github.com/$REPO/releases/download/$LATEST/${COMMAND_NAME}_${OS}_${ARCH}.tar.gz"

echo "Downloading $COMMAND_NAME $LATEST for ${OS}/${ARCH}..."

# Download and extract
mkdir -p "$INSTALL_DIR"
curl -fsSL "$URL" | tar xz -C "$INSTALL_DIR" "$COMMAND_NAME"
chmod +x "$INSTALL_DIR/$COMMAND_NAME"

# PATH check
if ! echo "$PATH" | tr ':' '\n' | grep -qx "$INSTALL_DIR"; then
  echo ""
  echo "Warning: $INSTALL_DIR is not in your PATH."
  echo "Add the following to your shell profile (~/.zshrc, ~/.bashrc, etc.):"
  echo ""
  echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
  echo ""
fi

echo "Installed $COMMAND_NAME $LATEST to $INSTALL_DIR/$COMMAND_NAME"
echo ""
echo "This server needs a Red Hat API offline token, in \$RH_SC_OFFLINE_TOKEN or"
echo "in a configuration file. The configuration file itself is optional: with"
echo "only the token set, it runs read-only against the published endpoint."
echo "Next steps:"
echo "  $COMMAND_NAME --help         # flags"
echo "  $COMMAND_NAME --print-scope  # what the current configuration permits"
echo "  $COMMAND_NAME --check-auth   # verify the offline token reaches the API"
echo ""
echo "Configuration and client setup: https://github.com/$REPO#readme"
