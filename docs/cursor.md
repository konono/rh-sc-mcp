# Using it with Cursor

**English** · [日本語](ja/cursor.md)

[Get started](../README.md) · [MCP clients](clients.md)

Cursor reads both the project's `.cursor/mcp.json` and the user-wide `~/.cursor/mcp.json`. If the same name appears in both, the project one wins. The examples below add `rh-sc` to an existing `mcpServers` object. [Official Cursor MCP guide](https://prod.cursor.com/docs/mcp)

## stdio

Replace `/absolute/path/to/rh-sc-mcp` with where `go install` actually put the binary.

```json
{
  "mcpServers": {
    "rh-sc": {
      "type": "stdio",
      "command": "/absolute/path/to/rh-sc-mcp",
      "args": []
    }
  }
}
```

The path is `bin/rh-sc-mcp` under `go env GOBIN`, or under the first element of `go env GOPATH` if `GOBIN` is unset. Do not put the real offline token in `mcp.json`; either pass `RH_SC_OFFLINE_TOKEN` through Cursor's launch environment, or set it in the server's `~/.config/rh-sc-mcp/config.yaml`. A Cursor started from a GUI does not necessarily inherit your shell's environment variables, so the latter is more reliable.

## Streamable HTTP

[Start the server over HTTP](http.md) and register it like this. Cursor expands `${env:NAME}` inside `headers` from environment variables.

```json
{
  "mcpServers": {
    "rh-sc-http": {
      "url": "https://mcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${env:RH_SC_MCP_HTTP_TOKEN}"
      }
    }
  }
}
```

Do not store the real shared token in `mcp.json`; pass it through **an environment the Cursor process can read**. Use HTTPS for remote servers. After registering, restart Cursor and confirm the connection under **Customize → MCPs**. Cursor Agent may ask for approval before using a tool. The offline token stays on the HTTP server side only.
