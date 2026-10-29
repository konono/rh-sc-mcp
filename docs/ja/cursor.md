# Cursor で使う

[使い始める](../../README.ja.md) · [クライアント一覧](clients.md)

Cursor はプロジェクトの `.cursor/mcp.json` とユーザー共通の `~/.cursor/mcp.json` を読みます。同名が両方にあるとプロジェクト側が優先されます。以下の例は既存の `mcpServers` に `rh-sc` を追加する形です。[Cursor 公式 MCP ガイド](https://prod.cursor.com/docs/mcp)

## stdio

`/absolute/path/to/rh-sc-mcp` を `go install` の実際の保存先に置き換えてください。

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

パスは `go env GOBIN`、未設定なら `go env GOPATH` の先頭要素の `bin/rh-sc-mcp` です。offline token の実値は `mcp.json` に書かず、Cursor の起動環境に `RH_SC_OFFLINE_TOKEN` を渡すか、サーバー側の `~/.config/rh-sc-mcp/config.yaml` に設定します。GUI から起動した Cursor がシェルの環境変数を受け継ぐとは限らないため、後者のほうが確実です。

## Streamable HTTP

サーバーを [HTTP で起動](http.md)し、次のように登録します。Cursor は `headers` 内の `${env:NAME}` を環境変数から展開します。

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

共有 token の実値は `mcp.json` に保存せず、**Cursor のプロセスが読める環境**に渡します。リモートでは HTTPS を使います。登録後は Cursor を再起動し、**Customize → MCPs** で接続を確認してください。Cursor Agent はツールを使う前に承認を求める場合があります。offline token は HTTP サーバー側だけに置きます。
