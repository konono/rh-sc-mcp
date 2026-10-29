# rh-sc-mcp

**English** · [日本語](README.ja.md)

An MCP server that lets Claude Code, Codex, Cursor and OpenCode work with Red Hat support cases. It can search, read and inspect comments on cases, and — if you allow it in the configuration — create and update cases and post comments. **Read-only by default.** Both stdio and Streamable HTTP are supported.

## Get started (stdio)

You need Go 1.25 or later, a supported MCP client, a [Red Hat offline token](https://access.redhat.com/management/api), and a Red Hat account that can see the cases you care about.

### 1. Install and register

Run the installer. It uses `go install` internally, so Go 1.25 or later is required.

```bash
curl -fsSL https://raw.githubusercontent.com/konono/rh-sc-mcp/main/install.sh | sh
```

If Claude Code is found, the installer registers the MCP server `rh-sc` at user scope. It never modifies an existing entry of the same name. For Codex, Cursor and OpenCode, install the binary first and then register it by following the per-client guide.

| Client | How to register for stdio |
|---|---|
| Claude Code | [Claude Code](docs/clients.md#connecting-over-stdio) |
| Codex (CLI / IDE) | [Codex](docs/codex.md) |
| Cursor | [Cursor](docs/cursor.md) |
| OpenCode | [OpenCode](docs/opencode.md) |

The installer does not touch the configuration of any other client. For installing manually with `go install`, where the binary lands, and how to update it, see [Installing and updating](docs/installation.md).

### 2. Pass the token and start

Set the offline token in the environment that starts your MCP client.

```bash
export RH_SC_OFFLINE_TOKEN='xxxxxxxxx'
```

Replace `xxxxxxxxx` with your actual token. Note that typing a real value directly into a command can leave it in your shell history.

Start the client you registered from this shell and confirm that the MCP server `rh-sc` is visible. A good first prompt is "use `rhcase_scope` and tell me what operations are available". To look for cases, pass `rhcase_search` a keyword or other criteria. **A search with no criteria is rejected by the API, so this server stops it before sending.** GUI applications do not always inherit environment variables from your shell, depending on how they are launched; in that case, put the token in [the server's configuration file](docs/configuration.md) instead.

### Configuration file

The default location is `~/.config/rh-sc-mcp/config.yaml`. On first run, the directory (`0700`) and a minimal file (`0600`) are created automatically. For example, to keep the token in the file and also enable KCS search:

```yaml
offlineToken: "your Red Hat offline token here"
readOnly: true
resources:
  kcs:
    permission: read
```

`offlineToken` can be omitted if you pass it through the environment variable `RH_SC_OFFLINE_TOKEN`. **When the same key is set in both the environment and the file, the environment wins**; everything else is merged. To use a different file, pass `--config /path/to/config.yaml`. Every key, along with permission examples, is documented in [Configuration and permissions](docs/configuration.md).

## What it can do

| Operation | Tools | Default |
|---|---|---|
| Search cases, show case details | `rhcase_search`, `rhcase_get` | Enabled |
| List comments, show one comment | `rhcase_list_comments`, `rhcase_get_comment` | Enabled |
| Create and update cases, post comments | `rhcase_create`, `rhcase_update`, `rhcase_add_comment` | Disabled |
| KCS knowledge base search | `rhkcs_search` | Disabled |

`rhcase_scope` is always available. To use KCS, set `resources.kcs.permission: read` in the configuration file. Writes require **both** `readOnly: false` and `permission: write` on the resource in question. Check what is currently granted with `rh-sc-mcp --print-scope`. [All tools and their I/O](docs/tools.md) / [Permission details](docs/configuration.md#permission-model)

## Using HTTP and containers

Over HTTP the server runs as a long-lived process and clients connect to `/mcp`. The connection requires a **shared bearer token, separate from the offline token**. Per-client HTTP registration examples are in [MCP clients](docs/clients.md); startup and hardening are covered in [Running the HTTP server](docs/http.md).

A published image is available as `ghcr.io/konono/rh-sc-mcp:latest` (linux/amd64 and linux/arm64). For Kubernetes / OpenShift, follow the [deployment guide](docs/deployment.md) to look up the latest release tag and pin to that version. The Red Hat offline token and the HTTP token are supplied separately at startup.

## Updating and removing

If `rh-sc-mcp` is not on your `PATH`, run it with the absolute path the installer printed.

```bash
rh-sc-mcp update
rh-sc-mcp --version
```

Restart your MCP client after updating. Removing the client registration, deleting the binary, tearing down Podman or OpenShift, and deleting credentials are four separate operations. [Uninstall instructions](docs/uninstall.md)

## Read more

- [Documentation index](docs/README.md) — find the page for what you want to do
- [Configuration and permissions](docs/configuration.md) / [Tools](docs/tools.md)
- [Limitations and what has been verified](docs/limitations.md)
- [Design, API and security](docs/architecture.md) / [Testing and releasing](docs/development.md)

## License

[MIT License](LICENSE) — Copyright (c) 2026 konono
