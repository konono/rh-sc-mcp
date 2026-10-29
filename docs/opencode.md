# Using it with OpenCode

**English** · [日本語](ja/opencode.md)

[Get started](../README.md) · [MCP clients](clients.md)

OpenCode configures MCP through `opencode.json` or the user-wide `~/.config/opencode/opencode.json`. **The shape of `mcp` differs between 1.x and 2.x.** Check with `opencode --version` first. [Official 1.x guide](https://dev.opencode.ai/docs/mcp-servers/) / [Official 2.x guide](https://dev.opencode.ai/v2/docs/mcp-servers/)

## Configuration for 1.x

This is the format used by OpenCode 1.18.23, the version tested here. Replace `/absolute/path/to/rh-sc-mcp` with where `go install` put the binary.

### stdio

```json
{
  "mcp": {
    "rh-sc": {
      "type": "local",
      "command": ["/absolute/path/to/rh-sc-mcp"],
      "enabled": true
    }
  }
}
```

### Streamable HTTP

```json
{
  "mcp": {
    "rh-sc-http": {
      "type": "remote",
      "url": "https://mcp.example.com/mcp",
      "oauth": false,
      "headers": {
        "Authorization": "Bearer {env:RH_SC_MCP_HTTP_TOKEN}"
      }
    }
  }
}
```

## Configuration for 2.x

In 2.x the server name goes under `mcp.servers`. The 1.x `enabled` key is gone; use `disabled: true` to turn a server off.

### stdio

```json
{
  "mcp": {
    "servers": {
      "rh-sc": {
        "type": "local",
        "command": ["/absolute/path/to/rh-sc-mcp"]
      }
    }
  }
}
```

### Streamable HTTP

```json
{
  "mcp": {
    "servers": {
      "rh-sc-http": {
        "type": "remote",
        "url": "https://mcp.example.com/mcp",
        "oauth": false,
        "headers": {
          "Authorization": "Bearer {env:RH_SC_MCP_HTTP_TOKEN}"
        }
      }
    }
  }
}
```

Keep the shared HTTP token in OpenCode's launch environment. `{env:RH_SC_MCP_HTTP_TOKEN}` is OpenCode's own configuration syntax and does not store the value in the file. Use HTTPS for remote servers, and keep the offline token on the HTTP server side only. For stdio, pass the offline token as `RH_SC_OFFLINE_TOKEN` from OpenCode's launch environment, or put it in the server's configuration file.

Check the connection state with `opencode mcp list`. Add to the right place under `mcp` without deleting existing keys, and if an entry of the same name already exists, look at it before changing anything.
