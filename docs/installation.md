# Installing and updating in detail

**English** · [日本語](ja/installation.md)

[Get started](../README.md) · [Documentation index](README.md)

## Installing and updating

Normally you use `install.sh`. The script places the binary with `go install` and, if Claude Code is found, also registers the MCP server.

```bash
curl -fsSL https://raw.githubusercontent.com/konono/rh-sc-mcp/main/install.sh | sh
```

**Go 1.25 or later is required.** Both the installer and the update command use `go install`. The old `INSTALL_DIR` is gone; to change where the binary lands, set `GOBIN`.

| Situation | Behaviour |
|---|---|
| Go is missing | Installs nothing; prints where to get Go and the `go install` command, then exits |
| Claude Code is missing | **Installation still succeeds.** No registration is performed; the command to run later is printed |
| `rh-sc` is not registered | Registers it with `--scope user`. Prints what was registered and at which scope |
| `rh-sc` is already registered | **Changes nothing, at any scope.** Prints the current Scope and Command |
| Already registered at a different path | As above, plus both the install path and the registered path |
| Already registered at `local` / `project` scope | As above, plus the `remove` / `add` steps to move it to `user` scope (it does not run them) |

The registration scope is **`user`**. `install.sh` can be run from any directory, so `local` would register the server only in that one directory. The [manual Claude Code registration steps](clients.md#connecting-over-stdio) use `user` as well. Note that **`claude mcp add` itself defaults to `local`**, so omitting `--scope` when registering by hand limits the server to the directory you ran it in.

If an existing entry is at `local` / `project` scope, `install.sh` does not rewrite it either. Even when the path matches, the "only usable in that directory" situation remains, so the script prints the commands to move it to `user` scope. Run the migration from the directory that owns the scope. Scope is detected from the first word of the `Scope` line that `claude mcp get` prints (`Local` / `Project`), and **when the wording is not recognised, nothing is printed** — staying silent is safer than printing the wrong migration steps.

This script only auto-registers Claude Code. For Codex, Cursor and OpenCode, register the same `go install`-ed binary using [the per-client instructions](clients.md). The server-side permission settings are the same for every client.

**The offline token is never written into Claude Code's configuration.** The registration contains only the absolute path to the binary; the token comes from the environment of the process that launched Claude Code, or from this server's own configuration file.

If Claude Code registration fails, it is reported separately from the success or failure of `go install`. Once the installation is done, you can perform the registration by hand later.

### Installing manually with `go install`

You can also install just the binary without the installer. This does not register the server with any MCP client.

```bash
go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest

# Goes to GOBIN if set, otherwise to bin/ under the first element of GOPATH
GO_BIN_PATH="$(go env GOBIN)"
if [ -z "$GO_BIN_PATH" ]; then GO_BIN_PATH="$(go env GOPATH | cut -d: -f1)/bin"; fi
"$GO_BIN_PATH/rh-sc-mcp" --version

# So that the examples below can just say rh-sc-mcp
export PATH="$GO_BIN_PATH:$PATH"
```

`go install ...@latest` fetches the latest tag of the published Go module. `GOPATH` can hold several elements, but `go install` writes to the first one. Register the absolute path of this binary with your MCP client.

From then on you can update using the installed command itself.

```bash
rh-sc-mcp update
rh-sc-mcp --version
```

`update` runs `go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest`, so that installing and updating take the same path. With Go's default settings, downloaded modules are checked against the checksum database (`sum.golang.org`). That however **depends on your own configuration** — no check happens if `GOSUMDB=off` is set, or if a glob matching this module path appears in `GOPRIVATE` / `GONOSUMDB`. Inspect your environment with `go env GOSUMDB GOPRIVATE GONOSUMDB`. No Red Hat credentials or configuration file are needed. Restart Claude Code or any other MCP client after updating.

**Before it runs, the command checks that `go install`'s destination and the currently running binary are the same file.** `go install` writes to `GOBIN` / the first `GOPATH`'s `bin/` regardless of where it is invoked from, so if you run a copy of the binary from somewhere else you can end up seeing "update succeeded" while the file you actually run stays old. On a mismatch the command **installs nothing and stops**, printing both paths and the manual steps.

Running through a symlink resolves to the same file and is accepted. **Hard links are rejected**, however — `go install` replaces the file with a rename, so a second hard-linked name keeps the old content (confirmed by measurement).

After updating, `--version` is run against the installed binary and the result is checked. If the version went up it is reported as an update; if it is the same, it is reported as already current. **If `@latest` resolves to an older version (a withdrawn tag, say), it is reported as an error — a downgrade, not an update** — because at that point the binary has already been replaced, and silently reporting success would be wrong.

The Go toolchain is required. If `go` is not found, the command prints where to get it and what to run, then exits.

To build and verify from a source checkout during development, use the following. `make install` also places the binary in Go's install location, but it builds **the source you have checked out**, so if you just want the published version use `go install ...@latest` above.

```bash
# Build (produces bin/rh-sc-mcp)
make build

# Install from this checkout into Go's install location
make install

# Test, format, static analysis
make test
make fmt
make vet
make lint    # golangci-lint (.golangci.yml)
```

---
