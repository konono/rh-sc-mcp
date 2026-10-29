# MCP クライアントへの登録

[使い始める](../../README.ja.md) · [ドキュメント一覧](README.md)

## MCP クライアントからの接続

サーバーの MCP 実装はクライアントに依存しません。登録する設定形式だけが違います。

| クライアント | stdio / HTTP の登録例 | 接続確認 |
|---|---|---|
| Claude Code | [このページ](#stdio-で接続する) | `/mcp` |
| Codex (CLI / IDE) | [Codex](codex.md) | `/mcp`、`codex mcp get rh-sc` |
| Cursor | [Cursor](cursor.md) | Customize → MCPs |
| OpenCode | [OpenCode](opencode.md) | `opencode mcp list` |

登録方法はトランスポートでも変わります。

| | stdio (既定) | Streamable HTTP |
|---|---|---|
| サーバーの起動 | クライアントが起動する | 運用者が常駐させる |
| クライアント設定 | `command` + `args` | `type: http` + `url` + `headers` |
| 認証 | 不要 (OS が制御) | 共有 bearer token が必須 |
| offline token | クライアント側に必要 | **サーバー側のみ**。クライアントは持たない |

HTTP の場合、クライアントが持つのは共有 bearer token だけで、Red Hat の offline token はサーバー側に留まります。これが HTTP 構成の利点です。

### stdio で接続する

#### Claude Code

既定パスに設定した場合、`--config` は不要です。Claude Code に **go install で配置したバイナリの絶対パス**を登録します。

```bash
GO_BIN_PATH="$(go env GOBIN)"
# GOPATH は複数要素を持てる。go install が書くのは先頭要素なので、そこだけ取る
if [ -z "$GO_BIN_PATH" ]; then GO_BIN_PATH="$(go env GOPATH | cut -d: -f1)/bin"; fi
claude mcp add --scope user rh-sc -- "$GO_BIN_PATH/rh-sc-mcp"
```

`--scope user` は**すべてのプロジェクトから使える**登録です。`install.sh` が使う scope と同じで、これが既定の勧めです。**`claude mcp add` 自体の既定は `--scope local`** なので、`--scope` を省くと実行したディレクトリにしか登録されません。このプロジェクトでだけ試したい場合に限り `--scope local` に置き換えてください。

以前に `bin/rh-sc-mcp` など別のパスで `rh-sc` を登録した場合は、先に `claude mcp remove rh-sc -s user` で古い登録を削除してから上のコマンドを実行します。過去に local scope で登録していた場合は、**その登録を行ったディレクトリで** `claude mcp remove rh-sc -s local` を実行して外してください (scope ごとに別のレジストリなので、user に登録しても local の登録は残ります)。登録後は `claude mcp get rh-sc` で `Scope` が `User config` に、`Command` が Go のインストール先になっていることを確認できます (接続状態の確認には offline token が必要です)。

#### 設定ファイルで直接書く場合

```json
{
  "mcpServers": {
    "rh-sc": {
      "command": "/absolute/path/to/GOBIN-or-GOPATH/bin/rh-sc-mcp",
      "args": []
    }
  }
}
```

`command` は `"$GO_BIN_PATH/rh-sc-mcp"` の実際の絶対パスに置き換えます。シェル変数そのものを JSON に書いても、シェルのようには展開されません。`RH_SC_OFFLINE_TOKEN` は Claude Code を起動する環境から渡します。

offline token をサーバーの設定ファイル側 (`offlineToken:`) に書くこともできます。MCP クライアントの設定ファイルには token の実値を書かないでください (エージェントが読める場所であることが多いため)。

別の場所に設定ファイルを置く場合は `--config /path/to/config.yaml` を追加します。

ログは **stderr** に出ます。stdout は MCP のトランスポートなので、プロトコルフレーム以外は一切流しません。

### HTTP で接続する

サーバー側の立て方は [HTTP で公開する](http.md#http-で公開する) を参照してください。ここはクライアント側の登録です。

#### 推奨: `.mcp.json` に書き、token は環境変数で渡す

プロジェクトの `.mcp.json` に次を置きます。**token の実値はここに書きません** — `${VAR}` 形式で環境変数を参照します ([Claude Code の MCP ドキュメント](https://code.claude.com/docs/en/mcp) に記載された展開機能)。

```json
{
  "mcpServers": {
    "rh-sc-http": {
      "type": "http",
      "url": "https://mcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${RH_SC_MCP_HTTP_TOKEN}"
      }
    }
  }
}
```

クライアントを起動する環境に、**サーバー側と同じ共有 token** を入れておきます。値はシークレットストアから渡すのが最善です。

```bash
# 例: シークレットストアから取り出して渡す
RH_SC_MCP_HTTP_TOKEN="$(your-secret-tool read rh-sc-mcp/httpToken)" claude
```

手で入力する場合は、**エコーしない読み取り**を使ってください。

```bash
read -rs RH_SC_MCP_HTTP_TOKEN && export RH_SC_MCP_HTTP_TOKEN
claude
```

`export RH_SC_MCP_HTTP_TOKEN="<実値>"` と直接打つのは避けます。**そのコマンド自体がシェル履歴に残ります。**

この形で避けられるのは、**設定ファイルとコマンド引数への実値の記載**です。`.mcp.json` は実値を含まないのでリポジトリに追跡させられ、共有しても秘密は広がりません。一方で**値は Claude Code の環境変数としては存在します**。秘密が消えるわけではなく、置き場所を 1 箇所に絞っているだけだと理解してください。

#### URL の指定

| 用途 | URL |
|---|---|
| ローカルで動かしたサーバーに繋ぐ | `http://127.0.0.1:8080/mcp` |
| リモートのサーバーに繋ぐ | `https://mcp.example.com/mcp` |

パスは常に `/mcp` です。リモートでは **HTTPS が必須** です — bearer token は毎リクエストのヘッダに乗るので、平文 HTTP は経路上の誰にでも token を渡します。ローカルの `127.0.0.1` はループバックに閉じるため平文で構いません。

#### 接続の確認

project scope の `.mcp.json` は、**初回にワークスペースの信頼とサーバーの承認を求められることがあります**。承認前は接続されず、状態は「Pending approval」と表示されます。承認は対話的な `claude` セッションで行います。

**承認する前に、`url` と、`Authorization` ヘッダがどこへ送られるかを確認してください。** `.mcp.json` はリポジトリで共有されるファイルなので、自分が書いたとは限りません。承認するということは、その URL に毎リクエスト共有 token を送ることに同意するということです。

```bash
claude mcp get rh-sc-http
```

承認後は health check が走ります。なお `claude mcp get` は **ヘッダを設定ファイルに書かれたまま (`Bearer ${RH_SC_MCP_HTTP_TOKEN}`) 表示します** — 展開後の値は出ません。したがってこの出力は「環境変数が正しく設定されているか」の確認には使えません。実際に繋がっているかは、承認後に `claude` セッション内で `/mcp` を実行してサーバーとツールが見えることで確認してください。

#### `--header` を使わない理由

公式ドキュメントには CLI での追加例もあります。

```bash
# 推奨しません
claude mcp add --transport http rh-sc-http https://mcp.example.com/mcp \
  --header "Authorization: Bearer <token>"
```

この形だと **token の実値がコマンド引数に乗ります**。実行中は `ps` の出力やプロセス一覧から同じ機械の他プロセスに見え、さらに登録先の設定ファイルにも実値が書き込まれます。実値を直接打った場合は、そのコマンド行がシェル履歴にも残ります。

`.mcp.json` + 環境変数参照なら、**設定ファイルとコマンド引数への実値の記載を避けられます**。ただし値は Claude Code の環境変数としては存在します — 「どこにも残らない」わけではありません。その環境変数をどう用意するかは[前述](#推奨-mcpjson-に書きtoken-は環境変数で渡す)のとおり別途注意が要ります。

同じ理由で、**token を追跡対象のファイルに直接書かないでください**。`.mcp.json` に `${VAR}` ではなく実値を書くと、そのままコミットされます。

#### 検証状況

この環境 (Claude Code 2.1.310) で確認したのは次の範囲です。

- `.mcp.json` の `type: http` / `url` / `headers` という構文が受理され、HTTP サーバーとして認識されること
- `claude mcp get` が type / URL / ヘッダを表示し、ヘッダの `${VAR}` を未展開のまま出すこと
- `claude mcp add --transport http ... --header` が存在し、値が引数に乗ること

`${VAR}` が接続時に実際に展開されることは**公式ドキュメントの記載に依拠**しており、この環境では確認していません。project scope の承認は対話セッションを要し、非対話では接続まで到達できなかったためです。

---
