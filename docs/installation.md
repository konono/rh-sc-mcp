# インストールと更新の詳細

[使い始める](../README.md) · [ドキュメント一覧](README.md)

## インストールと更新

公開版は `go install` でインストールします。

```bash
go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest

# GOBIN があればそこ、無ければ GOPATH/bin に入る
GO_BIN_PATH="$(go env GOBIN)"
# GOPATH は複数要素を持てる。go install が書くのは先頭要素なので、そこだけ取る
if [ -z "$GO_BIN_PATH" ]; then GO_BIN_PATH="$(go env GOPATH | cut -d: -f1)/bin"; fi
"$GO_BIN_PATH/rh-sc-mcp" --version

# 以下の例で rh-sc-mcp とだけ入力できるようにする
export PATH="$GO_BIN_PATH:$PATH"
```

`go install ...@latest` は公開済み Go モジュールの最新タグから取得します。保存先は `GOBIN` が設定されていればそこ、無ければ **`GOPATH` の先頭要素**の `bin/` です (`GOPATH` はリスト形式を取れますが、`go install` が書くのは先頭だけです)。Claude Code に登録するときは、後述のとおり**この絶対パス**を指定します。

**Go ツールチェーンが必要です。** インストールも更新も `go install` に統一しているため、Go 1.25 以降が入っていない環境では利用できません。旧 `INSTALL_DIR` は使えず、保存先を変える場合は `GOBIN` を指定してください。

### `install.sh` を使う場合

以前から公開している `install.sh` の URL も使えます。`go install` を実行したうえで、**Claude Code があれば MCP サーバーの登録まで行います**。

```bash
curl -fsSL https://raw.githubusercontent.com/konono/rh-sc-mcp/main/install.sh | sh
```

| 状況 | 振る舞い |
|---|---|
| Go が無い | インストールせず、入手先と `go install` コマンドを表示して終了 |
| Claude Code が無い | **インストールは成功**。登録は行わず、後から実行するコマンドを表示 |
| `rh-sc` が未登録 | `--scope user` で登録。登録内容と scope を表示 |
| `rh-sc` が登録済み | **scope を問わず一切変更しません。** 現在の Scope と Command を表示 |
| 登録済みでパスが違う | 上に加えて、インストール先と登録先の両方を示し、該当 scope での `remove` / `add` 手順を表示 (実行はしません) |

登録の scope は **`user`** です。`install.sh` は任意のディレクトリから実行されるため、`local` だとそのディレクトリにだけ登録されてしまいます。[Claude Code の手動登録手順](clients.md#stdio-で接続する)で案内している登録は `local` のままです — こちらは特定のプロジェクトで試すことを想定しているためです。

このスクリプトが自動登録するのは Claude Code だけです。Codex、Cursor、OpenCode を使う場合は、同じ `go install` 済みバイナリを[各クライアントの手順](clients.md)で登録してください。どのクライアントでもサーバー側の権限設定は共通です。

**offline token は Claude Code の設定に書き込みません。** 登録に含めるのはバイナリの絶対パスだけで、token は Claude Code を起動したプロセスの環境、または本サーバー自身の設定ファイルから渡します。

Claude Code の登録に失敗しても、`go install` の成否とは分けて報告します。インストールが済んでいれば、登録だけ後から手動で行えます。

以後の更新は、インストール済みのコマンドで行えます。

```bash
rh-sc-mcp update
rh-sc-mcp --version
```

`update` は `go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest` を実行します。インストールと更新を同じ経路に揃えるためです。Go の既定設定であれば、ダウンロードしたモジュールはチェックサムデータベース (`sum.golang.org`) に照合されます。ただしこれは**利用者側の設定に依存します** — `GOSUMDB=off` や、このモジュールのパスに一致する glob が `GOPRIVATE` / `GONOSUMDB` に設定されていれば照合は行われません。環境の設定は `go env GOSUMDB GOPRIVATE GONOSUMDB` で確認できます。Red Hat の認証情報や設定ファイルは不要です。更新後は Claude Code などの MCP クライアントを再起動してください。

**実行前に、`go install` の書き込み先と現在動いているバイナリが同じかを確認します。** `go install` は起動元に関係なく `GOBIN` / `GOPATH` 先頭の `bin/` へ書くので、別の場所にコピーしたバイナリから実行すると「更新成功」と出たのに動かしているファイルは古いまま、という状態になり得ます。一致しない場合は**何もインストールせずに停止し**、両方のパスと手動手順を表示します。

シンボリックリンク経由の実行は同じファイルを指すので通ります。一方**ハードリンクは拒否します** — `go install` は rename で置き換えるため、別名のハードリンクは古い内容のまま残るからです (実測確認済み)。

更新後は、インストールされたバイナリに `--version` を実行して結果を確かめます。版が上がっていれば更新として報告し、同じなら「最新」と報告します。**`@latest` が古い版を指した場合 (タグの取り下げ等) は、更新ではなくダウングレードとしてエラーで報告します** — この時点でバイナリは既に置き換わっているため、黙って成功と表示しません。

Go ツールチェーンが必要です。`go` が見つからない場合は、入手先と実行すべきコマンドを表示して終了します。

開発中のソースからビルド・検証する場合は次のコマンドを使います。`make install` も Go のインストール先へ入れますが、**チェックアウトしたソース**をビルドするため、公開版を使う人は上の `go install ...@latest` を使ってください。

```bash
# ビルド (bin/rh-sc-mcp が生成される)
make build

# このチェックアウトから Go のインストール先へ配置
make install

# テスト・整形・静的解析
make test
make fmt
make vet
make lint    # golangci-lint (.golangci.yml)
```

---
