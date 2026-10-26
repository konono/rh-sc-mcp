# OpenCode で使う

[使い始める](../README.md) · [クライアント一覧](clients.md)

OpenCode は `opencode.json` またはユーザー共通の `~/.config/opencode/opencode.json` で MCP を設定します。**1.x と 2.x は `mcp` の形が違います。** 先に `opencode --version` で確認してください。[1.x の公式ガイド](https://dev.opencode.ai/docs/mcp-servers/) / [2.x の公式ガイド](https://dev.opencode.ai/v2/docs/mcp-servers/)

## 1.x の設定

手元の OpenCode 1.18.23 と同じ形式です。`/absolute/path/to/rh-sc-mcp` は `go install` の保存先に置き換えます。

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

## 2.x の設定

2.x ではサーバー名を `mcp.servers` の下に置きます。1.x の `enabled` は使わず、無効化には `disabled: true` を使います。

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

HTTP の共有 token は OpenCode の起動環境に置きます。`{env:RH_SC_MCP_HTTP_TOKEN}` は OpenCode の設定構文であり、値をファイルに保存しません。リモートでは HTTPS を使い、offline token は HTTP サーバー側だけに置きます。stdio の offline token は OpenCode の起動環境から `RH_SC_OFFLINE_TOKEN` を渡すか、サーバー側の設定ファイルに置きます。

`opencode mcp list` で接続状態を確認してください。設定の既存キーを消さずに `mcp` の対象位置へ追記し、同じ名前の登録が既にある場合は内容を確認してから変更してください。
