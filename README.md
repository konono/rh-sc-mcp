# rh-sc-mcp

Red Hat のサポートケースを Claude Code、Codex、Cursor、OpenCode から扱う MCP サーバーです。ケースの検索・参照・コメント確認ができ、設定で許可した場合は作成・更新・コメント投稿もできます。**既定は読み取り専用**です。stdio と Streamable HTTP に対応します。

## まず使う (stdio)

Go 1.25 以降、対応する MCP クライアント、[Red Hat の offline token](https://access.redhat.com/management/api)、ケースを参照できる Red Hat アカウントが必要です。

### 1. インストールと登録

まずバイナリをインストールします。

```bash
go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest
```

保存先は `GOBIN`、未設定なら `GOPATH` の先頭要素の `bin/` です。次に、使うクライアントへこのバイナリを MCP サーバーとして登録します。

| クライアント | stdio の登録手順 |
|---|---|
| Claude Code | [Claude Code](docs/clients.md#stdio-で接続する) |
| Codex (CLI / IDE) | [Codex](docs/codex.md) |
| Cursor | [Cursor](docs/cursor.md) |
| OpenCode | [OpenCode](docs/opencode.md) |

Claude Code では `install.sh` によるインストールと自動登録もできます。他のクライアントの設定は変更しません。[インストールと更新の詳細](docs/installation.md)

### 2. token を渡して起動

MCP クライアントを起動する環境で offline token を入力します。入力は画面に表示されず、コマンド行にも値を残しません。

```bash
printf 'Red Hat offline token: '
read -rs RH_SC_OFFLINE_TOKEN
printf '\n'
export RH_SC_OFFLINE_TOKEN
```

このシェルから登録したクライアントを起動し、MCP サーバー `rh-sc` が見えることを確認します。最初は「`rhcase_scope` を使って、利用可能な操作を教えて」と依頼できます。ケースを探すときは `rhcase_search` に検索語などの条件を渡してください。**条件なしの検索は API に拒否されるため、本サーバーも送信前に止めます。** GUI アプリでは起動方法によってシェルの環境変数を受け継がないことがあるため、その場合は [サーバーの設定ファイル](docs/configuration.md)に token を設定してください。

token を毎回入力したくない場合は、既定の `~/.config/rh-sc-mcp/config.yaml` に `offlineToken` を設定する方法もあります。ファイルは初回起動時に `0600` で作られます。環境変数と設定ファイルを併用した場合は、環境変数が優先されます。詳しくは [設定と権限](docs/configuration.md) を参照してください。

## 使える機能

| 操作 | ツール | 既定 |
|---|---|---|
| ケースの検索・詳細表示 | `rhcase_search`, `rhcase_get` | 有効 |
| コメントの一覧・詳細表示 | `rhcase_list_comments`, `rhcase_get_comment` | 有効 |
| ケースの作成・更新、コメント投稿 | `rhcase_create`, `rhcase_update`, `rhcase_add_comment` | 無効 |
| KCS ナレッジ検索 | `rhkcs_search` | 無効 |

`rhcase_scope` は常に使えます。KCS を使うには、設定ファイルに `resources.kcs.permission: read` を指定します。書き込みには `readOnly: false` と対象リソースの `permission: write` の両方が必要です。現在の権限は `rh-sc-mcp --print-scope` で確認できます。[全ツールと入出力](docs/tools.md) / [権限の詳細](docs/configuration.md#権限モデル)

## HTTP・コンテナで使う

HTTP ではサーバーを常駐させ、クライアントは `/mcp` に接続します。接続には **offline token とは別の共有 bearer token** が必要です。各クライアントの HTTP 登録例は [MCP クライアント一覧](docs/clients.md)、起動と保護の設定は [HTTP サーバーの運用](docs/http.md) を参照してください。

コンテナ、Kubernetes、OpenShift の手順と `make deploy-*` は [デプロイガイド](docs/deployment.md) にまとめています。**コンテナイメージは配布していません**。利用するレジストリ向けにビルドしてください。

## 更新と削除

```bash
rh-sc-mcp update
rh-sc-mcp --version
```

更新後は MCP クライアントを再起動してください。登録だけを外す、バイナリを消す、Podman や OpenShift から撤去する、資格情報を消す操作はそれぞれ別です。[アンインストール手順](docs/uninstall.md)

## 詳しく読む

- [ドキュメント一覧](docs/README.md) — やりたいことから各ページへ
- [設定と権限](docs/configuration.md) / [提供ツール](docs/tools.md)
- [制限事項と検証済みの範囲](docs/limitations.md)
- [設計・API・セキュリティ](docs/architecture.md) / [テストとリリース](docs/development.md)

## ライセンス

[MIT License](LICENSE) — Copyright (c) 2026 konono
