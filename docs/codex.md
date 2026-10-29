# Using it with Codex

**English** · [日本語](ja/codex.md)

[Get started](../README.md) · [MCP clients](clients.md)

The Codex CLI and the IDE extension share the same MCP configuration. Below is how to connect from a local Codex. [Official MCP guide](https://developers.openai.com/codex/mcp)

## stdio

Register the absolute path of the `go install`-ed binary.

```bash
GO_BIN_PATH="$(go env GOBIN)"
if [ -z "$GO_BIN_PATH" ]; then GO_BIN_PATH="$(go env GOPATH | cut -d: -f1)/bin"; fi
codex mcp add rh-sc -- "$GO_BIN_PATH/rh-sc-mcp"
codex mcp get rh-sc
```

Do not write the real offline token into Codex's configuration. If you want to pass `RH_SC_OFFLINE_TOKEN` from the environment that starts Codex, add the following single line to `~/.codex/config.toml`, **inside the existing `[mcp_servers.rh-sc]` table** that the CLI created, if needed. Do not add a duplicate table heading. This setting forwards that environment variable name to the MCP process Codex launches.

```toml
env_vars = ["RH_SC_OFFLINE_TOKEN"]
```

In environments where Codex does not inherit environment variables, you can set `offlineToken` in the server's `~/.config/rh-sc-mcp/config.yaml` instead. In the Codex TUI, check the connection state with `/mcp`.

## Streamable HTTP

Register this after [starting the server over HTTP](http.md). Codex sends the value of the named environment variable as the bearer token. **Only the variable name is saved in the configuration, never the value.**

```bash
codex mcp add rh-sc-http --url https://mcp.example.com/mcp \
  --bearer-token-env-var RH_SC_MCP_HTTP_TOKEN
codex mcp get rh-sc-http
```

Pass the same shared `RH_SC_MCP_HTTP_TOKEN` the server uses to the environment that starts Codex. Use HTTPS for remote servers. Check the connection with Codex's `/mcp`. The offline token stays on the server side only.
