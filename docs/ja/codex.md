# Codex で使う

[使い始める](../../README.ja.md) · [クライアント一覧](clients.md)

Codex CLI と IDE 拡張は MCP 設定を共有します。以下はローカルの Codex から接続する例です。[公式 MCP ガイド](https://developers.openai.com/codex/mcp)

## stdio

`go install` したバイナリの絶対パスを登録します。

```bash
GO_BIN_PATH="$(go env GOBIN)"
if [ -z "$GO_BIN_PATH" ]; then GO_BIN_PATH="$(go env GOPATH | cut -d: -f1)/bin"; fi
codex mcp add rh-sc -- "$GO_BIN_PATH/rh-sc-mcp"
codex mcp get rh-sc
```

offline token を Codex の設定に実値で書かないでください。Codex を起動する環境から `RH_SC_OFFLINE_TOKEN` を渡す場合、必要に応じて `~/.codex/config.toml` に CLI が作った **既存の `[mcp_servers.rh-sc]` 表の中へ**次の 1 行を加えます。表の見出しを重複して追加しないでください。これは Codex が起動する MCP プロセスへ、その環境変数名を転送する設定です。

```toml
env_vars = ["RH_SC_OFFLINE_TOKEN"]
```

Codex が環境変数を受け継がない環境では、サーバー側の `~/.config/rh-sc-mcp/config.yaml` に `offlineToken` を設定できます。Codex の TUI では `/mcp` で接続状態を確認してください。

## Streamable HTTP

サーバーを [HTTP で起動](http.md)した後に登録します。Codex は指定した環境変数の値を bearer token として送ります。**変数名だけが設定に保存され、値は保存されません。**

```bash
codex mcp add rh-sc-http --url https://mcp.example.com/mcp \
  --bearer-token-env-var RH_SC_MCP_HTTP_TOKEN
codex mcp get rh-sc-http
```

Codex を起動する環境に、サーバー側と同じ共有 `RH_SC_MCP_HTTP_TOKEN` を渡してください。リモートでは HTTPS を使います。接続確認は Codex の `/mcp` で行います。offline token はサーバー側だけに置きます。
