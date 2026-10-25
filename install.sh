#!/bin/sh
set -eu

# Installs with `go install`, then registers the server with Claude Code
# if that is present.
#
# This script exists so the URL published earlier keeps working. Running
# `go install` directly does the same thing, minus the registration.

PACKAGE="github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp"
SPEC="$PACKAGE@latest"
BINARY="rh-sc-mcp"
SERVER_NAME="rh-sc"
# install.sh runs from whatever directory the user happens to be in, and
# the local scope would tie the registration to exactly that directory.
SCOPE="user"

if ! command -v go >/dev/null 2>&1; then
	echo "The Go toolchain is required, and \`go\` was not found on PATH." >&2
	echo "Earlier versions of this script downloaded a prebuilt binary; installation" >&2
	echo "now goes through go install, so Go 1.25 or newer has to be present." >&2
	echo "Install it from https://go.dev/dl/, then run:" >&2
	echo "  go install $SPEC" >&2
	exit 1
fi

if [ -n "${INSTALL_DIR:-}" ]; then
	echo "INSTALL_DIR is no longer supported. Set GOBIN instead, then run go install." >&2
	exit 1
fi

echo "Running go install $SPEC"
go install "$SPEC"

# Where go put it: GOBIN when set, otherwise bin/ under the *first*
# GOPATH entry. GOPATH may hold a list and go install writes to the head
# of it, so cutting at the separator matters.
BIN_DIR="$(go env GOBIN)"
if [ -z "$BIN_DIR" ]; then
	BIN_DIR="$(go env GOPATH | cut -d: -f1)/bin"
fi
BIN_PATH="$BIN_DIR/$BINARY"

if [ ! -x "$BIN_PATH" ]; then
	echo "go install reported success but $BIN_PATH is not an executable file." >&2
	echo "Check \`go env GOBIN GOPATH\` and install again." >&2
	exit 1
fi
echo "Installed $BIN_PATH"

# ---- Claude Code registration ---------------------------------------
#
# Everything below is a convenience. The install above has already
# succeeded, so nothing here may exit non-zero: a missing or unhappy
# Claude Code must not make a completed installation look failed.

# shq wraps a value in single quotes so the commands printed below can be
# pasted into a shell unchanged. GOBIN and GOPATH can contain spaces, and
# an unquoted path would break exactly where the user has least context to
# work out why. Embedded single quotes are closed, escaped and reopened.
shq() {
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

register_hint() {
	echo "  claude mcp add --scope $SCOPE $SERVER_NAME -- $(shq "$BIN_PATH")"
}

if ! command -v claude >/dev/null 2>&1; then
	echo
	echo "Claude Code was not found, so the MCP server was not registered."
	echo "The install itself succeeded. Register it once Claude Code is available:"
	register_hint
	exit 0
fi

# Ask before adding. `claude mcp add` refuses a name that already exists
# and leaves the existing entry alone, so this is not about avoiding
# damage -- it is so an existing registration in *any* scope is reported
# rather than silently shadowed by a new one.
if existing="$(claude mcp get "$SERVER_NAME" 2>/dev/null)"; then
	existing_scope="$(printf '%s\n' "$existing" | sed -n 's/^ *Scope: *//p' | head -1)"
	existing_command="$(printf '%s\n' "$existing" | sed -n 's/^ *Command: *//p' | head -1)"
	echo
	echo "Claude Code already has an MCP server named \"$SERVER_NAME\"; leaving it alone."
	echo "  Scope:   ${existing_scope:-unknown}"
	echo "  Command: ${existing_command:-unknown}"
	if [ -n "$existing_command" ] && [ "$existing_command" != "$BIN_PATH" ]; then
		echo
		echo "That is not the binary just installed:"
		echo "  installed: $BIN_PATH"
		echo "  registered: $existing_command"
		echo "To point Claude Code at the new one, remove and re-add it in the scope"
		echo "it currently lives in. For a user-scope entry:"
		echo "  claude mcp remove $SERVER_NAME --scope user"
		echo "  claude mcp add --scope user $SERVER_NAME -- $(shq "$BIN_PATH")"
		echo "For a project- or local-scope entry, use that scope instead, and run"
		echo "the commands from the directory the entry belongs to."
	fi
	exit 0
fi

echo
echo "Registering \"$SERVER_NAME\" with Claude Code (scope: $SCOPE)"
# No -e flags. The Red Hat offline token is not put into Claude Code's
# configuration: it reaches this server through the environment of the
# process that launches it.
if claude mcp add --scope "$SCOPE" "$SERVER_NAME" -- "$BIN_PATH" >/dev/null 2>&1; then
	echo "  Registered: $SERVER_NAME -> $BIN_PATH (scope: $SCOPE)"
	echo "  The Red Hat offline token is not stored in Claude Code's config."
	echo "  Export RH_SC_OFFLINE_TOKEN before starting Claude Code, or put it in"
	echo "  the server's own configuration file."
else
	echo "  Registration failed. The install itself succeeded; register manually:"
	register_hint
fi
