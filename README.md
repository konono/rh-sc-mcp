# rh-sc-mcp

Red Hat サポートケース管理 API を MCP (Model Context Protocol) ツールとして公開する Go 製サーバーです。Claude Code などの MCP クライアントから Red Hat のサポートケースを検索・参照・作成・更新・コメントできます。トランスポートは **stdio (既定)** と **Streamable HTTP (選択式、既定 OFF)** の 2 つに対応します。

**何を許可するかは operator が設定で決めます。** モデル側からは変更できません。許可レベルを超えるツールは `tools/list` に現れず、ハンドラ側でも呼び出し時に再チェックします。既定は read-only です。

---

## 目次

- [設計方針](#設計方針)
- [必要なもの](#必要なもの)
- [最短で使う (Claude Code)](#最短で使う-claude-code)
- [インストールと更新](#インストールと更新)
- [設定](#設定)
- [MCP クライアントからの接続](#mcp-クライアントからの接続)
  - [stdio で接続する](#stdio-で接続する)
  - [HTTP で接続する](#http-で接続する)
- [HTTP で公開する](#http-で公開する)
- [コンテナと Kubernetes / OpenShift](#コンテナと-kubernetes--openshift)
- [提供ツール](#提供ツール)
- [対象 API](#対象-api)
- [認証の仕組み](#認証の仕組み)
- [セキュリティ上の扱い](#セキュリティ上の扱い)
- [制限事項と未確認事項](#制限事項と未確認事項)
- [旧参照実装 (hydra-mcp-server) との差分](#旧参照実装-hydra-mcp-server-との差分)
- [テスト](#テスト)
- [リリース](#リリース)
- [ライセンス](#ライセンス)

---

## 設計方針

1. **公開仕様を正とする。** 同梱の `CaseManagement-API_v1.json` (Swagger 2.0 / `host: api.access.redhat.com` / `basePath: /support`) に書かれている範囲を実装します。仕様に書かれていないことは、設定可能にするか、やらないかのどちらかです。
2. **権限は登録時と呼び出し時の二重チェック。** 許可外のツールはそもそも列挙されないので、モデルが提案することすらありません。
3. **既定 read-only。** 書き込みは `readOnly: false` と個別の `permission: write` の **両方** を明示したときだけ有効になります。
4. **根拠のない値を出力しない。** ケース番号やコンソール URL を推測で組み立てて返すことはしません ([制限事項](#制限事項と未確認事項) 参照)。

---

## 必要なもの

- Go 1.25 以降
- Red Hat カスタマーポータルの **offline token** (https://access.redhat.com/management/api で発行)
- サポートケースを参照できる Red Hat アカウント

実 API 資格情報がなくてもビルドとテストは通ります (テストは httptest のスタブに対して実行されます)。

---

## 最短で使う (Claude Code)

MCP を使いたいプロジェクトのディレクトリで実行します。`--scope local` の登録はそのプロジェクトだけで有効です。

```bash
go install github.com/konono/rh-sc-mcp/cmd/rh-sc-mcp@latest
GO_BIN_PATH="$(go env GOBIN)"
# GOPATH は複数要素を持てる。go install が書くのは先頭要素なので、そこだけ取る
if [ -z "$GO_BIN_PATH" ]; then GO_BIN_PATH="$(go env GOPATH | cut -d: -f1)/bin"; fi
claude mcp add --scope local rh-sc -- "$GO_BIN_PATH/rh-sc-mcp"

# token は画面に出さずに入力する。Claude Code と、その子プロセスの MCP に渡る
printf 'Red Hat offline token: '
read -rs RH_SC_OFFLINE_TOKEN
printf '\n'
export RH_SC_OFFLINE_TOKEN
claude
```

Claude Code 内で `/mcp` を開き、`rh-sc` が接続済みか確認します。最初は「`rhcase_scope` を使って、利用可能な操作を教えて」と依頼してください。既定は読み取り専用で、KCS 検索も既定では無効です。KCS は [権限モデル](#権限モデル) に従って明示的に有効にできます。

設定を追加したい場合は、[設定](#設定) と [MCP クライアントからの接続](#mcp-クライアントからの接続) を参照してください。

---

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

登録の scope は **`user`** です。`install.sh` は任意のディレクトリから実行されるため、`local` だとそのディレクトリにだけ登録されてしまいます。[上の最短手順](#最短で使う-claude-code)で案内している手動登録は `local` のままです — こちらは特定のプロジェクトで試すことを想定しているためです。

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

## 設定

設定は YAML ファイルと環境変数で行います。設定ファイルの既定パスは `~/.config/rh-sc-mcp/config.yaml` です。`--config`、`RH_SC_MCP_CONFIG`、既定パスの順にファイルを選びます。設定ファイルと環境変数はキー単位でマージし、**両方にあるキーは環境変数が優先**、ファイルにしかないキーはそのまま使います。

`--config` と `RH_SC_MCP_CONFIG` を指定せずに初めて起動すると、既定パスのディレクトリ (権限 `0700`) と最小構成の設定ファイル (権限 `0600`) を作ります。既存ファイルは上書きしません。生成内容は `readOnly: true` と説明コメントだけで、認証情報は含みません。offline token が未設定なら、ファイルを作った後に起動はエラーで止まります。

```bash
# 初回は設定ファイルを生成。token 未設定なら案内を表示して終了
rh-sc-mcp --print-scope
$EDITOR "$HOME/.config/rh-sc-mcp/config.yaml"
```

`config.example.yaml` に全キーの説明があります。**必須なのは offline token だけ** で、それ以外のキーはすべて省略可能です (省略時は既定値)。offline token は生成したファイルに `offlineToken:` として追加するか、環境変数 `$RH_SC_OFFLINE_TOKEN` で渡します。後者なら認証情報をファイルに保存せずに済みます。明示した `--config` または `RH_SC_MCP_CONFIG` のファイルが無い場合はエラーになり、既定ファイルは作りません。

最小構成 (環境変数で token を渡し、生成した設定ファイルを使う) では、公開エンドポイントに対する read-only サーバーになります。

```bash
RH_SC_OFFLINE_TOKEN=... rh-sc-mcp --print-scope
```

### 主要なキー

| キー | 既定値 | 説明 |
|---|---|---|
| `offlineToken` | (必須) | Red Hat API offline token。環境変数推奨 |
| `baseURL` | `https://api.access.redhat.com/support` | ケース API のルート (basePath 込み) |
| `apiVersion` | `v3` | ケース API のメジャーバージョン (`v3` / `v1`) |
| `ssoTokenURL` | `https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token` | トークン交換先 |
| `ssoClientID` | `rhsm-api` | トークン交換の client_id |
| `readOnly` | `true` | 全体キルスイッチ。true の間はすべて read に丸められる |
| `defaultPermission` | (リソース既定) | `resources` に明示がないリソースの既定レベル |
| `resources.<name>.permission` | `case`/`comment`=`read`, `kcs`=`none` | リソースごとの `none` / `read` / `write` |
| `caseViewURL` | (なし) | 人間向けリンクの接頭辞。未設定なら `caseUrl` を出力しない |
| `maxResults` | `50` | 検索 1 回あたりのケース数上限 (API 側上限は 200) |
| `http.addr` | (なし) | HTTP の待受アドレス。空なら HTTP 無効 (既定 OFF) |
| `http.token` | (なし) | 共有 bearer token。`http.addr` 設定時は必須、offline token と同値は不可 |
| `http.stateless` | `true` | セッションを持たない。複数レプリカ運用の既定 |
| `http.sessionTimeoutSeconds` | `0` | stateful 時のみ有効 |
| `http.trustedOrigins` | `[]` | cross-origin チェックで追加許可する Origin |
| `maxComments` | `50` | コメント一覧の件数上限 (クライアント側で切り詰め) |
| `timeoutSeconds` | `60` | 上流 HTTP 1 回あたりの制限時間 |

### 環境変数

| 環境変数 | 対応するキー |
|---|---|
| `RH_SC_OFFLINE_TOKEN` | `offlineToken` |
| `RH_SC_BASE_URL` | `baseURL` |
| `RH_SC_API_VERSION` | `apiVersion` |
| `RH_SC_SSO_TOKEN_URL` | `ssoTokenURL` |
| `RH_SC_SSO_CLIENT_ID` | `ssoClientID` |
| `RH_SC_READ_ONLY` | `readOnly` (`true`/`false`) |
| `RH_SC_MCP_CONFIG` | 設定ファイルのパス (`--config` が優先。未指定なら `~/.config/rh-sc-mcp/config.yaml`) |
| `RH_SC_MCP_HTTP_ADDR` | `http.addr` |
| `RH_SC_MCP_HTTP_TOKEN` | `http.token` |

### 権限モデル

リソースは 3 つ、レベルは `none` < `read` < `write` の 3 段階です。

| リソース | 既定 | 対象ツール |
|---|---|---|
| `case` | `read` | `rhcase_search` / `rhcase_get` (read)、`rhcase_create` / `rhcase_update` (write) |
| `comment` | `read` | `rhcase_list_comments` / `rhcase_get_comment` (read)、`rhcase_add_comment` (write) |
| `kcs` | `none` | `rhkcs_search` (read) |

ポイントが 3 つあります。

- **`case` と `comment` は独立しています。** `comment: write` + `case: read` と書けば、「エージェントに返信はさせるが、ステータスや severity は変えさせない」構成になります。
- **`kcs` は明示しない限り有効になりません。** `defaultPermission: write` にしても `none` のままです。ケース管理とは別の API 面なので、「ケースを読みたい」という設定が暗黙にナレッジベース検索を有効化しないようにしています。
- **`readOnly: true` の間は write 付与が無視されます。** 設定を一時的に絞るときに、個別の権限行を消して回る必要がありません。

### 設定の確認

実際に何が有効になっているかは、起動せずに確認できます。

```bash
rh-sc-mcp --print-scope
```

```json
{
  "apiVersion": "v3",
  "endpoint": "https://api.access.redhat.com/support",
  "maxComments": 50,
  "maxResults": 50,
  "readOnly": true,
  "resources": { "case": "read", "comment": "read", "kcs": "none" },
  "ssoClientID": "rhsm-api",
  "ssoTokenURL": "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token",
  "timeoutSec": 60,
  "tools": ["rhcase_scope", "rhcase_search", "rhcase_get", "rhcase_list_comments", "rhcase_get_comment"],
  "warnings": []
}
```

この出力に認証情報は一切含まれません (fingerprint も出しません)。バグ報告にそのまま貼れます。

### 疎通確認 (`--check-auth`)

offline token が通るかどうかは、セッションを開始せずに確認できます。

```bash
rh-sc-mcp --check-auth
```

```json
{
  "ssoTokenURL": "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token",
  "ssoClientID": "rhsm-api",
  "endpoint": "https://api.access.redhat.com/support/v3",
  "tokenExchange": "ok",
  "apiProbe": "ok"
}
```

2 段階を**別々に報告します**。原因も対処も違うためです。

- `tokenExchange` — SSO が offline token を受理したか。失敗した場合、SSO は原因を区別せず `invalid_grant` を返すため、値そのものを確認する必要があります。
- `apiProbe` — 発行された access token を API が受理したか。SSO は通るのに API が `403` を返すのは、アカウントの可視範囲の問題。

API 側のプローブは固定キーワードでの検索 1 回 (`maxResults: 1`) だけです。キーワードが必要なのは、API が**検索条件のないフィルタを拒否する**ためです ([制限事項 6](#6-フィルタ検索は条件なしでは拒否される-仕様書に記載なし))。`case` が `none` の構成ではプローブ自体を省略し、その旨が `apiProbe` に入ります。

**この診断出力にケースの情報は入りません。** プローブは最大 1 件しか要求せず、かつ**応答本文を破棄します** — 見ているのはエラーの有無だけです。検索語が何かにマッチしたかどうかに関わらず、ケースのデータが出力に乗ることはありません。

キーワード自体は、マッチする見込みが低くなるよう十分に固有な語を選んでいます (2026-10-25 の実行では 0 件でした)。ただしこれは**その時点のデータについての観測**であって語の性質ではなく、将来ケースにこの文字列が含まれる可能性は排除できません。出力の安全性が依存しているのは上記の「1 件上限 + 応答破棄」のほうです。

ケースの件数も報告しません。特定の検索語での件数は「見えるケース数」を意味しないためです。可視範囲を知りたい場合は `rhcase_search` を使ってください。

失敗時は exit code 1 を返すのでヘルスチェックにも使えます。**出力に認証情報は含まれません** (上流がリクエストヘッダを反射してきた場合も洗浄されます)。

token をファイルに置いている場合は、値をコマンドラインに出さず、末尾改行を落として渡してください。

```bash
chmod 600 token   # 置くなら最低限これ。token / *.token は .gitignore 済み
RH_SC_OFFLINE_TOKEN="$(tr -d '\r\n' < token)" rh-sc-mcp --check-auth
```

末尾改行の除去は必須です。改行が付いたままだと別の文字列を送ることになり、SSO は受理しません。そのときのエラーは原因を区別しないので、token 自体の問題との切り分けができなくなります。

---

## MCP クライアントからの接続

登録方法はトランスポートで変わります。

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
claude mcp add --scope local rh-sc -- "$GO_BIN_PATH/rh-sc-mcp"
```

以前に `bin/rh-sc-mcp` など別のパスで `rh-sc` を登録した場合は、先に `claude mcp remove rh-sc -s local` で古い登録を削除してから上のコマンドを実行します。登録後は `claude mcp get rh-sc` で `Command` が Go のインストール先を指すことを確認できます (接続状態の確認には offline token が必要です)。`--scope local` は現在のプロジェクトにだけ登録します。

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

サーバー側の立て方は [HTTP で公開する](#http-で公開する) を参照してください。ここはクライアント側の登録です。

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

## HTTP で公開する

既定の stdio では、クライアントがサーバーを起動し、誰が話しかけられるかは OS が決めます。共有デプロイ向けに MCP の [Streamable HTTP][streamable] トランスポートも使えます。

```bash
export RH_SC_MCP_HTTP_TOKEN="$(openssl rand -hex 32)"
rh-sc-mcp --http :8080
```

[streamable]: https://modelcontextprotocol.io/specification/2025-06-18/basic/transports

| パス | 認証 | 用途 |
|---|---|---|
| `/mcp` | Bearer token | MCP エンドポイント |
| `/healthz` | なし | liveness |
| `/readyz` | なし | readiness |

### token を持つ相手が到達できる範囲

共有 bearer token を提示できたクライアントは、**この設定の scope が許す範囲すべて**に到達します。scope の中では、ケースの絞り込みは一切ありません — つまり **offline token の持ち主から見えるケースはすべて見えます**。特定のケースやアカウントに限定する機能は本サーバーにはありません。

絞り込めるのはリソース種別 (`case` / `comment` / `kcs`) と読み書きの別だけです。閲覧範囲そのものを狭めたい場合は、**より権限の狭い Red Hat アカウントの offline token を使う**しかありません。token は全クライアントで共有なので、クライアントごとに見える範囲を変えることもできません。

### 共有 bearer token は必須、かつ offline token とは別物

token が無いまま HTTP を有効にするとサーバーは**起動しません**。stdio では OS がアクセスを制御しますが、開いたポートはそうではなく、このプロセスは組織のサポートケースに到達できます。

さらに、**共有 token に offline token と同じ文字列は使えません**。同値の場合も起動時にエラーになります。共有 token は全 MCP クライアントに配るものなので、同じにすると各クライアントが Red Hat の認証情報そのものを持つことになり、本サーバーの scope を迂回してサポート API を直接叩けてしまいます。

```bash
# 生成
openssl rand -hex 32
```

エラーメッセージにはどちらの値も載せません。この種の出力はバグ報告に貼られるためです。

### プローブ

`/healthz` と `/readyz` は意図的に無認証です。kubelet は提示できる token を持ちません。

両者は「このプロセスが応答している」ことだけを返し、**SSO にも Red Hat API にも触れません**。触れる実装にすると、ポーリングのたびに token 交換を消費し、上流の一時的な不調でプローブが落ちて再起動ループになります。テストでは、プローブを叩いたあと API スタブへのリクエストが 0 件であることを確認しています。

### 設定

```yaml
http:
  addr: ":8080"                 # --http / $RH_SC_MCP_HTTP_ADDR でも指定可
  token: xxxxx                  # $RH_SC_MCP_HTTP_TOKEN を推奨
  stateless: true               # 既定
  sessionTimeoutSeconds: 300    # stateful (stateless: false) のときのみ
  trustedOrigins: []            # cross-origin チェックで追加許可する Origin
```

`addr` の優先順は `--http` フラグ > `$RH_SC_MCP_HTTP_ADDR` > `http.addr` です。いずれも空なら HTTP は無効で、stdio のままです (**既定 OFF**)。

`--http` はコマンドラインから後付けで有効化する経路なので、**フラグ適用後にも token 必須・offline token 同値・アドレス形式の検証をやり直します**。設定ファイルに token が無い構成は、フラグが付くまでは正当だからです。

`stateless` が既定 true なのは、複数レプリカを 1 つの Service の背後に sticky routing なしで置けるようにするためです。サーバー発のメッセージが必要な単一インスタンスでのみ false にしてください。

cross-origin 保護と DNS rebinding 保護は既定で有効です。ブラウザ上のページがユーザーの資格情報を使ってこのエンドポイントを操作すること、および localhost にバインドしたサーバーへ rebind されたホスト名で到達することを防ぎます。

### 権限モデルはトランスポートに依存しません

HTTP でも stdio とまったく同じ scope が適用されます。read-only 既定なら `tools/list` に write ツールは現れず、呼び出しても拒否されます。トランスポートが変わるのは経路だけで、ポリシーは変わりません (HTTP 経由の実通信テストで確認しています)。

`--print-scope` には HTTP の設定も出ますが、**token は値も長さも接頭辞も出しません**。`tokenConfigured: true/false` だけです。

```bash
rh-sc-mcp --print-scope | jq .http
```

```json
{
  "enabled": true,
  "tokenConfigured": true,
  "addr": ":8080",
  "path": "/mcp",
  "stateless": true,
  "trustedOrigins": []
}
```

設定例は [`examples/http-server.yaml`](examples/http-server.yaml) にあります。

---

## コンテナと Kubernetes / OpenShift

`Dockerfile` と `deploy/` は、参照実装と同じ利用形態を再現するための**例**です。**イメージは公開していません** — `ghcr.io/konono/rh-sc-mcp` は例示値です。自分で build して自分が管理するレジストリに push してください。

```bash
docker build -t registry.example.com/your-org/rh-sc-mcp:v0.1.0 .
docker push registry.example.com/your-org/rh-sc-mcp:v0.1.0
```

### イメージの差し替え方

`deploy/base/kustomization.yaml` の `images:` を書き換えます。ここで**`name` は変更しません**。`name` は「どのイメージを置換するか」の照合キーで、`deployment.yaml` に書かれた値と一致している必要があります。ここを自分のレジストリに書き換えると照合が外れ、**kustomize は何も置換せず元の値のまま出力します** (エラーも警告も出ません)。

変えるのは `newName` と `newTag` です。

```yaml
images:
  - name: ghcr.io/konono/rh-sc-mcp              # 照合キー。触らない
    newName: registry.example.com/your-org/rh-sc-mcp
    newTag: v0.1.0
```

適用前に、置換後のイメージを目で確認してください。

```bash
kubectl kustomize deploy/overlays/kubernetes | grep 'image:'
kubectl kustomize deploy/overlays/openshift  | grep 'image:'
# → image: registry.example.com/your-org/rh-sc-mcp:v0.1.0
```

`ghcr.io/konono/...` のままなら、`name` を書き換えてしまっているか、`newName` を設定していません。

### 認証情報と scope

```bash
# 1. 認証情報。offline token と共有 HTTP token は別の値にすること。
kubectl create secret generic rh-sc-mcp \
  --from-literal=offlineToken="$RH_SC_OFFLINE_TOKEN" \
  --from-literal=httpToken="$(openssl rand -hex 32)"

# 2. scope。readOnly と resources を確認する。
$EDITOR deploy/base/config.yaml
```

`deploy/base/secret.example.yaml` は `REPLACE_ME` 入りのテンプレートです。実値を入れたファイルは `.gitignore` 済み (`deploy/**/secret.yaml` 等) なので、サンプルだけが追跡されます。

### 適用前の確認

```bash
kubectl kustomize deploy/overlays/kubernetes
kubectl kustomize deploy/overlays/openshift
```

### OpenShift

`Route` は `host` を省いているのでクラスタが生成し、`termination: edge` がルーターの既定証明書を使います。追加の編集は不要です。

```bash
oc apply -k deploy/overlays/openshift
```

### Kubernetes

`Ingress` は**そのままでは適用できません**。あなたにしか用意できない 2 つの値がプレースホルダになっています。

```bash
# 3. ホスト名 (rh-sc-mcp.example.com を 2 箇所)
$EDITOR deploy/overlays/kubernetes/ingress.yaml

# 4. そのホスト名の TLS 証明書 (cert-manager 等が発行するなら不要)
kubectl create secret tls rh-sc-mcp-tls --cert=tls.crt --key=tls.key

kubectl apply -k deploy/overlays/kubernetes
```

**TLS は必須です。** bearer token は毎リクエストのヘッダに乗るので、平文 HTTP は経路上の誰にでも token を渡します。

### マニフェストの中身

`deploy/base` に Deployment / Service と、`deploy/base/config.yaml` から生成される ConfigMap があります。認証情報は Secret に置くので、scope は git でレビューしつつ token は入れない構成です。kustomize が ConfigMap 名をハッシュ化するため、scope を変えると Pod が入れ替わります (scope は起動時に 1 度しか読まないので、これが無いと編集が反映されません)。

マニフェストは、コンテナを非 root・read-only root filesystem・全 capability drop・`RuntimeDefault` seccomp で動かす設定にしています。イメージは `USER 65532:65532` を宣言しています (マニフェストの `runAsNonRoot` が数値の UID を必要とするため)。一方 OpenShift の `restricted-v2` SCC は namespace のレンジから任意の UID を割り当て、イメージの `USER` を無視します。

**任意の UID でも動くよう、ファイルの書き込みと特定 UID への依存を避けた設計です** — プロセスはファイルを書かず、バイナリは誰でも読める配置に置いています。マニフェスト側で `runAsUser` を固定していないのも、SCC の割り当てを妨げないためです。**ただしコンテナ実行自体は未検証です** ([制限事項 7](#7-コンテナイメージの-build-は未検証))。これは設計上の意図であって、動作を確認した結果ではありません。

OpenShift overlay の `Route` には 300 秒の HAProxy タイムアウトを付けています。streamed な MCP 応答は開いたままになるため、ルーターの既定 30 秒では切られます。Kubernetes overlay は代わりに `Ingress` を置きます。どちらも公開するのは `/mcp` だけで、プローブはクラスタ内に留めます。

---

## 提供ツール

許可レベルを満たすものだけが登録されます。

| ツール | 必要権限 | 対応 API | 説明 |
|---|---|---|---|
| `rhcase_scope` | なし (常時) | — | 有効なエンドポイント・権限・ツール一覧・上限値を返す |
| `rhcase_search` | `case: read` | `POST /v3/cases/filter` | キーワード・ステータス・severity・製品・担当・アカウント・期間で検索。[条件を最低 1 つ要求する](#6-フィルタ検索は条件なしでは拒否される-仕様書に記載なし) |
| `rhcase_get` | `case: read` | `GET /v3/cases/{caseNumber}` | ケース 1 件の詳細。コメント履歴を含む (`includeComments: false` で省略可、既定は `maxComments` 件で打ち切り) |
| `rhcase_create` | `case: write` | `POST /v3/cases` | ケース新規作成 |
| `rhcase_update` | `case: write` | `PUT /v3/cases/{caseNumber}` | ケースの部分更新 |
| `rhcase_list_comments` | `comment: read` | `GET /v3/cases/{caseNumber}/comments` | コメント一覧 (日付範囲・ソート指定可) |
| `rhcase_get_comment` | `comment: read` | `GET /v3/cases/{caseNumber}/comments/{commentId}` | コメント 1 件 |
| `rhcase_add_comment` | `comment: write` | `POST /v3/cases/{caseNumber}/comments` | コメント投稿。`contentType` は `plaintext` (既定) / `html`、`createdByType` は `Customer` / `Associate` / `Internal` |
| `rhkcs_search` | `kcs: read` | `POST /search/v2/kcs` | ナレッジベース検索。タイトル・種別・`viewUri`・短い抜粋に射影して返す (任意機能、既定 OFF) |

### 入出力の設計

- **ケース番号は文字列です。** `04337263` のように先頭のゼロが意味を持つため、数値にすると壊れます。入力は数字 1〜32 桁に制限しており、それ以外は **ネットワークに出る前に** 拒否されます。
- **検索結果は要約行です。** `POST /v3/cases/filter` は 1 件ごとに `GetCaseResponse` 全体 (description とコメント履歴を含む) を返すため、そのまま 50 件返すとモデルのコンテキストを食い潰します。検索では「どのケースを開くか決めるのに必要な項目」だけに絞り、本文は `rhcase_get` で取得します。
- **更新は差分のみ送ります。** 省略したフィールドはペイロードに含めない (= 変更しない)、空文字を渡すとクリアする、という区別をします。`summary` の空文字だけは拒否します (件名のないケースは backend 側で弾かれ、その理由が伝わらないため)。
- **長い応答は上限で打ち切ります。** ケース検索は `maxResults` (既定 50)、コメント一覧とケース詳細に含まれるコメントは `maxComments` (既定 50)、KCS 検索は `rows` (上限 50) です。コメント API には件数パラメータが無いため、こちらは応答受信後に切り詰めます。
- **応答の注意点は `notes` に書きます。** 件数を切り詰めた、`maxResults` を丸めた、ページがまだある、API が一部フィールドを反映しなかった、などを明示します。いずれも黙って捨てることはしません。

### `rhcase_create` の戻り値

作成 API は `201` と `{"location": ["..."]}` を返します (環境によっては `Location` ヘッダのみ、あるいは両方)。本サーバーは **ヘッダとボディの両方を見て、重複を除いたうえで、API が返した location をすべて返します**。API がどちらも返さなかった場合 (`200` + 空ボディなど) は `location` フィールド自体が出力から省かれ、代わりに `notes` で「作成は受理されたが location が無いので、再作成の前に `rhcase_search` で確認すること」を伝えます。

ケース番号は `location` の末尾セグメントから読めたときだけ `caseNumber` に入ります。仕様は location の形式を保証していないため、読めない場合は空のままにし、`notes` でその旨と次の手順を伝えます。**存在しない番号を推測して返すことはしません。**

```json
{
  "caseNumber": "04400001",
  "location": ["https://api.access.redhat.com/support/v3/cases/04400001"],
  "httpStatus": 201,
  "notes": ["The case was created. Use this caseNumber for follow-up calls."]
}
```

### `rhcase_update` の戻り値

更新 API は `200` と `{"message": ..., "fieldsNotSentToSalesforce": ...}` を返します。`fieldsNotSentToSalesforce` は「ペイロードとしては受理したが backend には送らなかったフィールド」です。ここが空でない場合、更新は部分的にしか適用されていないので、`notes` で警告します。

### `rhkcs_search` の戻り値

検索バックエンドの応答をそのまま返さず、**射影します**。素通しすると 1 件あたり約 10 KB (うち `abstract` が 9.4 KB) で、既定 `rows` では 1 回 62 KB になるためです。詳細と実測値は[制限事項 2](#2-kcs-検索のレスポンス形状は仕様化されていない) にあります。

```json
{
  "numFound": 37134,
  "numFoundExact": true,
  "start": 0,
  "returned": 1,
  "documents": [
    {
      "id": "7890",
      "title": "...",
      "documentKind": "Solution",
      "language": "en",
      "lastModifiedDate": "2026-09-01T00:00:00Z",
      "viewUri": "https://access.redhat.com/solutions/7890",
      "snippet": "...",
      "snippetTruncated": true
    }
  ],
  "notes": ["..."]
}
```

- `snippet` は `abstract` を **Unicode 文字数で 300 文字**に切り詰めたものです。切り詰めた場合のみ `snippetTruncated` が付きます。
- `viewUri` は API の `view_uri` を**そのまま**通した値です。開けるかどうかは本サーバーでは確認していません。
- `numFoundExact` はバックエンドの申告です。`false` の場合、`numFound` は概数なので `notes` でもその旨を伝えます。
- 応答が既知の Solr 形でなかった場合は `documents` が空になり、代わりに `unrecognizedShape` が入ります。中身は `topLevelKeys` / `sizeBytes` / 説明だけで、**本文は一切含めません**。

---

## 対象 API

同梱の `CaseManagement-API_v1.json` に基づきます。

```
swagger:  2.0
title:    Support Services (version: v3)
host:     api.access.redhat.com
basePath: /support
schemes:  https
security: Bearer (apiKey, in: header, name: Authorization)
```

実装したのは **v3 ケース API と KCS 検索** のみです。仕様書には他にも attachments、SFTP token、accounts、business hours、notified users、external tracker updates などが含まれますが、第一スコープ外として実装していません。

| 実装 | エンドポイント |
|---|---|
| ✅ | `POST /v3/cases` |
| ✅ | `POST /v3/cases/filter` |
| ✅ | `GET /v3/cases/{caseNumber}` |
| ✅ | `PUT /v3/cases/{caseNumber}` |
| ✅ | `GET /v3/cases/{caseNumber}/comments` |
| ✅ | `POST /v3/cases/{caseNumber}/comments` |
| ✅ | `GET /v3/cases/{caseNumber}/comments/{commentId}` |
| ✅ | `POST /search/v2/kcs` (任意、既定 OFF) |
| ❌ | attachments 系 / SFTP 系 / accounts 系 / businesshours / notifiedusers / externaltrackerupdates / v1 系の固有パス |

### 2xx の揺れへの対応

同じ成功でも応答の形が一定しないため、以下をすべて許容します。

| 形 | 扱い |
|---|---|
| `201` + ボディの `location` 配列 | location として採用 |
| `201` + `Location` ヘッダのみ | 同上 |
| 両方 (同一値) | 重複排除して 1 件 |
| `200` でボディが空 | 成功として扱い、メッセージを補う |
| `204` | 同上 |
| JSON だが `Content-Type` が `application/json` でない | 先頭が `{` / `[` なら JSON として解釈する |
| `200` だが HTML (プロキシのサインイン画面など) | **エラーとして報告する** (空の構造体や「成功だが location 不明」を返さない) |
| コメント 0 件でボディが空 | 空配列として扱う |

最後の行は作成系でも同じです。`POST /v3/cases` は `Location` ヘッダだけでも成功しうるため「ボディが空なら許容」ですが、**ボディが空でないなら JSON であることを検証します**。これを省くと、ゲートウェイが返した 200 のサインイン画面が「ケースは作成されたが番号が読めなかった」として扱われ、呼び出し側が存在しないケースを探すか、同じケースをもう一度作ってしまいます。

なお、このとき出すエラーには診断のためボディの冒頭を引用しますが、**引用部分は洗浄してから切り詰めます**。上流が `Authorization` ヘッダを反射してくるのは、まさにこの「200 + HTML」のケースが多いためです ([セキュリティ上の扱い](#セキュリティ上の扱い) 参照)。

---

## 認証の仕組み

1. offline token を SSO realm に `grant_type=refresh_token` / `client_id=rhsm-api` で提示する
2. 返ってきた access token を、`expires_in` から **60 秒引いた時刻** までキャッシュする
3. 以降の API 呼び出しに `Authorization: Bearer <access token>` を付ける
4. `401` が返ったら、**そのリクエストが使ったトークンだけ** を破棄して 1 回だけ再試行する

補足:

- 同時に複数の呼び出しが走ってもトークン交換は 1 回に直列化されます (offline token の送信回数を増やさないため)。
- `401` の再試行は 1 回だけです。常に `401` を返すサーバー相手でもループしません。
- `403` は再試行しません。これは認可の判断であってトークンの失効ではないためです。
- `expires_in` が無い、あるいは極端に短い応答でも「無期限」とは解釈せず、短い有限の寿命として扱います。

**注意:** 公開仕様が規定しているのは「`Authorization` ヘッダに Bearer トークンを載せる」ところまでです。offline token から access token を得る手順 (SSO realm の URL、`rhsm-api` という client_id) は仕様書には書かれておらず、Red Hat が公開している API トークンの手順および参照実装に基づいています。そのため `ssoTokenURL` / `ssoClientID` は設定で変更できるようにしてあります。

---

## セキュリティ上の扱い

### 認証情報を出力に出さない

エラーメッセージとログは 2 段階で洗浄します。

1. **プロセスが保持している実値の除去** — offline token と現在の access token の文字列を置換します。access token は更新のたびに変わるため、スナップショットではなく常にその時点の値を参照します。
2. **パターンによる除去** — `Bearer <...>`、`access_token` / `refresh_token` / `client_secret` などのキー付きの値、`eyJ` で始まる JWT を、このプロセスが見たことのないものでも除去します。上流のエラーがリクエストヘッダをそのまま引用してくる場合や、SSO のエラー応答に新しいトークンが同梱されている場合に効きます。

切り詰めは **洗浄のあと** に行います。先に切るとトークンが途中で分断され、残った前半をパターンが検出できなくなるためです。

洗浄は **エラー応答だけでなく、成功応答から作るエラーにも** かかります。`200` で HTML が返ってきたときの「JSON を期待したが〜だった」という診断にはボディの冒頭が引用されますが、プロキシのサインイン画面はリクエストの `Authorization` ヘッダをそのまま含んでいることがあり、ここが抜けていると 2xx 経路だけが漏洩経路として残ります。`internal/redhat` の `Response` は自前の洗浄器を持ち、この引用を洗浄してから切り詰めます。

### パスの安全性

ケース番号は正規表現 (数字 1〜32 桁) で検証し、不正な値はリクエスト送信前に拒否します。加えてパス構築時に全セグメントを `url.PathEscape` します。`../../v3/accounts/current` や `04337263/comments` のような値でパスを書き換えることはできません。コメント ID も同様に英数字 1〜64 文字に制限しています。

### その他

- レスポンスの読み込みは 8 MiB で打ち切ります。
- エラー本文は洗浄後 2000 バイトで切り詰め、切り詰めたことを明示します。
- `--print-scope` の出力に認証情報は含めません。
- 書き込み可能な設定で起動すると stderr に警告が出ます。
- 書き込み系ツールには `destructiveHint` を付けています (MCP クライアント側の確認プロンプト用)。ツールの説明文にも「取り消せない」「アカウント上の全員に見える」ことを明記しています。

---

## 制限事項と未確認事項

ここに書いてあるのは「確認できていないこと」です。確認済みのように書かないために分けてあります。

### 1. 仕様書自身の不整合: `SolrSearchRequest` の `exp`

`/search/v2/kcs` のリクエストボディ `SolrSearchRequest` は `required: ["q", "exp"]` と宣言していますが、`properties` に `exp` は存在せず、あるのは `expression` です。

```json
"SolrSearchRequest": {
  "required": ["q", "exp"],
  "properties": { "q": {...}, "expression": {...}, "start": {...}, "rows": {...}, "clientName": {...} }
}
```

**この不整合は実 API で決着しました** (2026-10-25、`rows: 1` の読み取りのみ)。

| 送ったボディ | 結果 |
|---|---|
| `{q, rows}` | `200` — **`exp` は必須ではない** |
| `{q, rows, expression}` | `200` — 構造は上と同一。`expression` は受理される |
| `{q, rows, exp}` | **`500`** — `exp` を送ると壊れる |
| `{q, rows, clientName}` (本実装が送る形) | `200` |

つまり仕様書の `required: ["q", "exp"]` は二重に誤っています。`exp` は必須でないばかりか、**仕様書どおりに送ると `500` になります**。本実装は `expression` のみを送り `exp` を送らないので正しく、`SolrSearchRequest` に `exp` フィールドを足してはいけません。テストで送信ボディに `exp` が現れないことを固定しています。

なお `500` 時のエラー本文は `{message, detailMessage}` で、ケース API と同じ `LegacyApiErrorResponse` としてデコードできました。

### 2. KCS 検索のレスポンス形状は仕様化されていない

`/search/v2/kcs` の `200` は `"description": "Solr Response"` のみで、スキーマが定義されていません。**仕様書に書かれていないので、以下は実測した形です** (2026-10-25)。

```
responseHeader: {status, QTime, zkConnected, params}
response:       {docs[], numFound, start, maxScore, numFoundExact}
spellcheck:     {}
```

`docs[]` の 1 件は 26 フィールドありました。内訳が問題で、**`abstract` だけで 10,076 B 中 9,401 B (93%)** を占め、ほかに同内容のタイトルが 4 種 (`publishedTitle` / `allTitle` / `title` / `sortTitle`)、URI が 3 種、ベクトル索引の内部フラグ (`*_vector_field_set` 等) が並びます。素通しすると API の既定 `rows=10` で 1 回 62 KB になります。

そこで `rhkcs_search` は**射影して返します** — `id` / `title` (4 種から 1 つ) / `documentKind` / `language` / `lastModifiedDate` / `viewUri` / `snippet` と、`numFound` / `numFoundExact` / `start` / 返却件数。実レスポンスに対する実測で **`rows=1` が 13,435 B → 684 B、`rows=10` が 62,436 B → 11,364 B** になります。

- `snippet` は `abstract` を **Unicode 文字数で 300 文字**に切り詰めたものです (バイト数ではないので、日本語でも英語でも同じ文字数になります)。切り詰めた場合は `snippetTruncated` が立ち、`notes` にも出ます。
- `viewUri` は API の `view_uri` を**そのまま**通した値です。絶対 URL かどうか、セッションなしで開けるかどうかは本サーバーでは確認していません。
- `numFoundExact` も返します。`numFound` が概数の場合、API 自身がそう申告するためです。

**未知形状時の扱い**: 仕様化されていない以上、形が変わることは例外ではなく想定内です。`response.docs` が無い、あるいは配列として読めない場合は射影を行わず、`unrecognizedShape` として **`topLevelKeys` / `sizeBytes` / 説明だけ**を返します。**本文は一切含めません** — 未知のペイロードをモデルのコンテキストに流し込むより、形が変わったことを正確に伝えて人が見にいくほうが有用なためです。これはエラーではなく通常の結果として返ります。

`rhkcs_search` が既定 OFF なのは変わりません。ケース管理とは別の API 面であり、レスポンスが仕様化されていない点も含めて明示的な opt-in を求めます。

### 3. コンソール URL は既定値を持たない

人間向けのケース閲覧 URL (`https://.../support/cases/#/case/<番号>` のような形) は、公開 API 仕様のどこにも書かれていません。当初はこの形を組み立てていましたが、**根拠がないため既定値を削除しました**。

- 未設定時: ツール出力に `caseUrl` は現れません。代わりに API 自身が返した `uri` (ケース) と `location` (作成時) が入ります。
- 設定時 (`caseViewURL`): ケース番号を最後のパスセグメントとして連結した URL を `caseUrl` として返します。この値の正しさは設定した operator の責任です。

### 4. `apiVersion: v1` は未検証

仕様書には `/v1/cases` 系のパスも存在し、設定で切り替えられるようにしてありますが、本実装が想定・テストしているのは `v3` です。`v1` を指定すると起動時に警告が出ます。

### 5. 実 API の確認範囲 — 読み取りは確認済み、書き込みは未確認

自動テストはすべて `httptest` のスタブに対するものです。スタブは公開仕様から起こしたものなので、**それ自体では仕様と実 API の食い違いを検出できません** (実際に 1 件見つかりました。[制限事項 6](#6-フィルタ検索は条件なしでは拒否される-仕様書に記載なし))。

実エンドポイントに対しては、2026-10-24〜25 に有効な offline token で以下を確認しました。

| 確認したこと | 結果 |
|---|---|
| SSO のトークン交換 | 成功。Bearer token が発行され `expires_in` は `900` |
| SSO のエラー応答の形 | `{"error":"invalid_grant","error_description":"..."}` + `400`。本実装の `ssoError` と一致 |
| v3 パスのルーティング | `/support/v3/...` が実際に到達する (認証済みリクエストが `200` を返した) |
| `POST /v3/cases/filter` | `200`。`{totalCount, cases}` の形で、DTO のキーが仕様書と整合 |
| `GET /v3/cases/{caseNumber}` | `200`。73 キーのオブジェクトが返り、ケース番号が一致 |
| `GET /v3/cases/{n}/comments` | `200`。配列 (コメントなしのケースでは空配列) |
| `GET /v3/cases/{n}/comments/{id}` | `200`。id が一致するオブジェクト |
| MCP 経由の実動作 | Go SDK の `CommandTransport` で実プロセスに接続。`tools/list` は 5 件で write ツールは非表示、`rhcase_search` / `rhcase_get` / `rhcase_list_comments` が成功 |
| `--check-auth` (成功経路) | 固定キーワードを付けた修正版が exit 0 / `tokenExchange: ok` / `apiProbe: ok` / `error` なし |
| `--check-auth` (失敗経路) | 受理されない値では exit 1 / `tokenExchange: failed` / `apiProbe: skipped`。いずれの経路でも認証情報の露出なし |
| `POST /search/v2/kcs` | `200`。`{responseHeader, response, spellcheck}` の Solr 形。`exp` を送った場合のみ `500` ([制限事項 1](#1-仕様書自身の不整合-solrsearchrequest-の-exp) / [2](#2-kcs-検索のレスポンス形状は仕様化されていない)) |

`GET /v3/cases/{caseNumber}` が返す 73 キーに対し、本実装の `Case` 型は意味のあるフィールドだけを取る射影です ([対象 API](#対象-api) 参照)。未知のキーは無視されるので、仕様書に無いフィールドが増えてもデコードは壊れません。

**v3 ケース API のうち未確認なのは書き込み経路です** — `POST /v3/cases` (作成)、`PUT /v3/cases/{n}` (更新)、`POST /v3/cases/{n}/comments` (コメント投稿)。これらは実行するとサポートケースに取り消せない変更が残るため、読み取りの確認とは別に、対象を決めた上で明示的に実施する必要があります。作成・更新・コメントの応答形状に関する本 README の記述は、現時点では**公開仕様書のみを根拠としています**。

`POST /search/v2/kcs` は読み取りで確認済みです (上表、および[制限事項 1](#1-仕様書自身の不整合-solrsearchrequest-の-exp) / [2](#2-kcs-検索のレスポンス形状は仕様化されていない))。ただし確認したのは `rows` を絞った一般的な検索語での 1 系統だけで、`expression` に凝った Solr 式を渡した場合の挙動や、返りうるフィールドの全体像は把握していません。`apiVersion: v1` の経路は**未検証のまま**です ([制限事項 4](#4-apiversion-v1-は未検証))。

#### `invalid_grant` が返るとき

開発中に 41 バイトのファイル (40 文字 + 改行) に入った値を渡したところ、SSO は `400 invalid_grant: Invalid refresh token` を返しました。**実測として言えるのはこれだけです** — この応答から、その値がどういう形式だったか、失効していたのか取り消されていたのか、そもそも offline token だったのかは区別できません。SSO はいずれの場合も同じ `invalid_grant` を返します。

`--check-auth` が `invalid_grant` を返す場合は、[Red Hat 公式の API Tokens ページ](https://access.redhat.com/management/api) で発行した offline token に差し替えたうえで、末尾改行が混入していないかを確認してください。

### 6. フィルタ検索は条件なしでは拒否される (仕様書に記載なし)

`POST /v3/cases/filter` に `{"maxResults": 1}` だけを送ると、API は `400 No valid external filter criteria provided` を返します。**ページングやソートは検索条件として数えられません。** この挙動は `FilterCasesV3Request` のどこにも書かれておらず、必須フィールドの宣言もありません (2026-10-25 に実 API で確認)。

本実装はこれに 2 つの形で対応しています。

- `rhcase_search` は、条件が 1 つもない呼び出しを**送信前に拒否**し、何を足せばよいかを述べます。API の `400` をそのまま返すと、どの引数が足りないのか分からないためです。
- `--check-auth` のプローブは固定キーワードを添えます。これが無いと、プローブは実 API に対して**必ず失敗します**。実際、キーワードを付ける前の版は実 API で `400` になり、この乖離はそこで発見されました。キーワードを付けた版は実 token で `apiProbe: ok` になることを確認済みです。

どのフィールドが「条件」として数えられるかは仕様書にも API の応答にも書かれておらず、確認できているのは `keyword` が数えられることだけです。そのため本実装の判定は緩く、「ページング・ソート以外が 1 つでもあれば送る」としています。送った結果 API に拒否される可能性は残り、その場合は API のエラーがそのまま返ります。

なお、この乖離はスタブに対するテストでは発見できませんでした。発見後、スタブ側にも同じ拒否を実装してあるので、プローブから条件を外すと該当テストが落ちます。

### 7. コンテナイメージの build は未検証

`Dockerfile` は参照実装に合わせて用意しましたが、**この環境に Docker CLI が無いため build を実行していません**。確認したのは、同じビルド構成 (`CGO_ENABLED=0 GOOS=linux GOARCH=amd64` でのクロスコンパイル) が通ることと、`deploy/` の両オーバーレイが `kubectl kustomize` でレンダリングできることまでです。コンテナ内での実行、`distroless` ベースとの組み合わせ、`USER 65532` での動作は未確認です。

### 8. severity / status / product の値は検証しない

仕様書がこれらの列挙値を定義していないため、`"4 (Low)"` のような値をサーバー側で検証することはしていません。誤った値は上流のエラーとして返ります。一方、仕様書が値を明示している項目 (`contentType`、`createdByType`、コメントの `sortField` / `sortOrder`、ケース検索の `sortOrder`) は検証・正規化します。

---

## 旧参照実装 (hydra-mcp-server) との差分

[fnarel/hydra-mcp-server](https://github.com/fnarel/hydra-mcp-server) (Python / FastMCP) を参考にしていますが、方針が異なる点があります。

| 観点 | hydra-mcp-server | 本実装 |
|---|---|---|
| 言語 / SDK | Python + FastMCP | Go + MCP Go SDK |
| トランスポート | stdio のみ | stdio (既定) + Streamable HTTP (選択式、bearer 認証必須) |
| エンドポイント | `access.redhat.com/hydra/rest/v1` (公開仕様書に含まれないパス) | `api.access.redhat.com/support` + `v3` (公開仕様書どおり)。設定で変更可能 |
| 操作 | 参照のみ (case 取得、case 検索、KCS 検索) | 参照に加えて作成・更新・コメント投稿 |
| 権限制御 | なし (ツールは常に全部出る) | operator 設定でリソース別に `none`/`read`/`write`。既定 read-only |
| ケース検索 | Solr (`/search/v2/cases`) | v3 の構造化フィルタ (`POST /v3/cases/filter`) |
| エラー処理 | `raise_for_status()` をそのまま伝播 | 仕様の `LegacyApiErrorResponse` を解釈し、認証情報を洗浄して返す |
| トークン | 取得とキャッシュ | 取得・キャッシュに加えて `401` 時の 1 回再試行と、使用トークン単位の破棄 |

`/hydra/rest` 系のパスは公開仕様書に含まれていないため、本 README では設定例を示しません。`baseURL` / `apiVersion` は設定可能なので技術的には向けられますが、そのとき何が返るかは本実装の保証外です (既定以外のエンドポイントを指定すると起動時に警告が出ます)。

---

## テスト

```bash
make test        # go test -race ./...
```

実 API 資格情報は不要です。`httptest` で SSO realm とケース API のスタブを立て、以下を検証しています。

**認証 (`internal/redhat`)**
- `refresh_token` grant のフォーム内容 (grant_type / client_id / refresh_token)
- トークンのキャッシュ、期限切れ後の再取得 (時刻を注入して実時間を待たない)
- `expires_in` が無い / 極端に短い場合に無期限扱いしないこと
- 古いトークンによる `Invalidate` が最新のトークンを壊さないこと
- SSO 失敗時に `invalid_grant` などの情報を残しつつ offline token が漏れないこと

**API クライアント (`internal/redhat`)**
- 2xx の 6 パターン (201+body / 201+header / 両方 / 200 空 / 204 / 番号を読めない location)
- `Content-Type` が JSON でない JSON ボディの解釈、HTML ボディの拒否
- `LegacyApiErrorResponse` の分解、404 の判別、本文の切り詰め、空ボディ
- **`200` + HTML で認証情報が反射されたとき、診断メッセージに漏れないこと**
- **作成系で `200` + HTML が「成功」にならないこと**
- `401` → トークン再取得 → 再試行の成功、常時 `401` が 2 回で止まること、`403` を再試行しないこと
- timeout
- 不正なケース番号 11 種がネットワークに出る前に拒否されること
- パスセグメントのエスケープ、先頭ゼロの保持
- 更新で「省略したフィールドを送らない / 空文字は送る」こと

**設定 (`internal/config`)**
- 既定が read-only であること、`readOnly` が write 付与を上書きすること
- 書き込みには `readOnly: false` と `permission: write` の両方が要ること
- `kcs` が `defaultPermission` では有効にならないこと
- 綴り間違いキー・未知リソース・不正値の拒否、環境変数の優先、`maxResults` のクランプ
- `caseViewURL` 未設定時に `caseUrl` を作らないこと

**MCP (`internal/server`)**
- in-memory トランスポートで initialize → `tools/list` → `tools/call` まで通すこと
- read-only サーバーに write ツールが列挙されないこと、付与すると現れること
- `comment: write` が `case` に波及しないこと
- セッション中に権限を絞った場合、呼び出し時チェックが拒否し、API に到達しないこと
- 各ツールの正常系と、検索結果に本文が混入しないこと、件数の切り詰めと `notes`
- 引数検証 (不正なケース番号 / 日付 / ソート / enum) が API に到達しないこと
- 上流エラーがモデルに届き、かつ Bearer トークンが混ざらないこと
- **実際の経路 (SSO 交換 → クライアント → MCP 往復) で、`200` + HTML を返す上流に対し `rhcase_get` / `rhcase_create` / `rhcase_add_comment` がツールエラーになり、access token も offline token もモデル出力に現れないこと**
- 生成された入力スキーマに必須項目が立っていること、write ツールが `readOnlyHint` を名乗らないこと
**HTTP トランスポート (`internal/server/http_test.go`, `internal/config`)**
- Bearer 必須 (未提示 / 誤り / 正しい token の接頭辞 / offline token を提示 → すべて 401、正しい token のみ 200)、拒否時に API へ 1 件も到達しないこと
- 401 応答が token の値を body にも `WWW-Authenticate` にも載せないこと
- token 無し、および offline token と同値では `Handler()` が構築を拒否すること (設定検証に依存しない二重化)
- プローブ 2 本が無認証で 200 を返し、scope / ツール名 / token を漏らさず、**API スタブへのリクエストが 0 件**であること
- cross-origin: 許可外 Origin が拒否され、`trustedOrigins` に追加すると通り、他の Origin は依然拒否されること
- `stateless` 既定 true、`stateless: false` + `sessionTimeoutSeconds` が反映され handler が構築できること
- 実 HTTP (`httptest` + `StreamableClientTransport`) で initialize → `tools/list` → `tools/call`。read ツールが見え、write 3 ツールは列挙されず、呼んでも拒否されること
- `initialize` が JSON-RPC の result を返し `serverInfo.name` が一致すること
- ctx キャンセルで `ServeHTTP` が drain して nil を返すこと
- 起動失敗: `http.addr` だけで token 無し / token が offline token と同値 / ポートの無いアドレス / 負の `sessionTimeoutSeconds` / 空の `trustedOrigins` 要素
- `--http` 上書き後の再検証 (token 無し・offline token 同値・不正アドレスを弾き、正当な上書きは通し、空の上書きは設定を変えないこと)
- 環境変数が設定ファイルより優先されること、HTTP 有効化で警告が出ること
- `--print-scope` 相当の出力が HTTP 設定を伝えつつ token も接頭辞も含まないこと

- `--check-auth` が SSO 失敗と API 失敗を区別すること、`case: none` でプローブを省略すること、プローブが 1 件しか要求しないこと、出力に認証情報が現れないこと
- **条件のないフィルタをスタブが実 API と同じ `400` で拒否すること**、プローブがその拒否を踏まないこと、`rhcase_search` が条件なしの呼び出しを送信前に止めること
- **プローブがマッチした場合でも、応答本文が診断出力に入らないこと** (スタブは検索語に関わらずケースを返すので、マッチする側を直接検証している)。キーワード自体は形式 (長さ・英数字のみ) のみ検証
- **KCS**: 実測形状のスタブに対する射影 (タイトル 4 重複から 1 つ、`numFoundExact` の伝播、ページング note)、`abstract` の Unicode 300 文字での切り詰めと切断位置の妥当性、`docs_text_store` 等の内部フィールドが出力に残らないこと、送信ボディに `exp` が現れないこと (スタブも `exp` に `500` を返す)、未知形状で本文を返さず構造情報のみ返すこと、`rows` のクランプ

```
ok  github.com/konono/rh-sc-mcp/internal/config
ok  github.com/konono/rh-sc-mcp/internal/redhat
ok  github.com/konono/rh-sc-mcp/internal/server
```

---

## リリース

リリースは **release-please + GoReleaser** で自動化しています ([konono/aw](https://github.com/konono/aw) と同じ構成)。

```
main への push
  └─ release-please: conventional commits を読み、リリース PR を更新
       └─ その PR をマージ
            ├─ タグ・GitHub Release・CHANGELOG を作成
            └─ GoReleaser: バイナリを Release に添付
```

現在のバージョンは 2 箇所にあります — `.release-please-manifest.json` と `internal/version/version.go` です。前者が release-please にとっての記録で、後者は `release-please-config.json` の `extra-files` に登録された派生先です。リリースのたびに release-please が**両方をまとめて更新**するので、手で書き換える必要はありません。

`version.go` 側の目印は定数を囲む `x-release-please-start-version` / `x-release-please-end` のコメントです。release-please はこの範囲を探して置換します (このマーカー付きで実際に `0.1.0` → `0.2.0` へ書き換わることを確認済み)。**マーカーを消したり定数から引き離したりすると置換対象を見失います**。そうなったときに何が起きるか (エラーで止まるのか、`version.go` だけ古いまま進むのか) は確認していないので、**マーカーは動かさない**のが無難です。

コミットメッセージは [Conventional Commits](https://www.conventionalcommits.org/) が必須です。release-please はこれを読んでバージョンの上げ幅とリリースノートを決めるため、形式から外れたコミットは**変更履歴から黙って消えます**。PR では `commitlint` が検査します。

### 事前に必要なリポジトリ設定

**Actions secret の登録は不要です。** release-please は組み込みの `GITHUB_TOKEN` で動かします。

専用トークンを使わない判断は、手近なトークンを調べた結果です。この構成を組んだ環境では `gh auth token` が返す値が `gho_` で始まる OAuth トークンで、`repo` / `workflow` / `delete_repo` といったスコープを**そのユーザーが触れる全リポジトリに対して**持っていました。これを public リポジトリの Actions secret に置けば、このリポジトリを 1 つリリースするために必要な権限を大きく超えます。

`gh` は PAT でもログインできるので、手元の `gh auth token` が何を返すかは環境によります (`gh auth status` で確認できます)。ただし、スコープを絞った fine-grained PAT をわざわざ発行・更新し続けるより、下記の設定を 1 つ入れて `GITHUB_TOKEN` で済ませるほうが運用が単純です。

代わりに、**リポジトリ設定を 1 つ変更**してください。

> Settings → Actions → General → Workflow permissions →
> **Allow GitHub Actions to create and approve pull requests** を有効化

新規の個人リポジトリではこれが既定で無効です (API では `can_approve_pull_request_reviews: false`)。無効のままだと release-please は PR を作れずに失敗します。

**Workflow permissions の既定 (Read repository contents...) は変更不要です。** `release.yml` が job に必要な `contents: write` と `pull-requests: write` を自分で宣言しているためです。リポジトリ全体の既定を read のままにしておけば、他のワークフローが暗黙に書き込み権限を持つことはありません。

```bash
# 現在の設定を確認する
gh api repos/<owner>/<repo>/actions/permissions/workflow
# → {"default_workflow_permissions":"read","can_approve_pull_request_reviews":true}
```

### リリース PR の CI は手動承認が要ります

`GITHUB_TOKEN` を使うことの代償です。GitHub の [Trigger a workflow](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow) にあるとおり、`GITHUB_TOKEN` が起こしたイベントは原則として他のワークフローを起動しません。ただし `pull_request` の `opened` / `synchronize` には例外があり、**実行自体は作られたうえで承認待ち (approval required) の状態で止まります**。

したがって release-please が開いた PR では、CI と commitlint が止まったまま待機します。write 権限を持つ人が PR の Checks 画面で **Approve workflows to run** を押すと動きます。マージ前に必ず押してください — 押さないと、バージョンを変える唯一の PR だけが未検査のままマージされることになります。

なお後続の GoReleaser ジョブはこの影響を受けません。タグ push を契機に新しいワークフローが起動するのではなく、同一ワークフロー実行内で `needs` によって繋がっているためです。

**実測** (2026-10-25、このリポジトリでの 1 回目): `main` への push で Release ワークフローが成功し、release-please が PR #1 を作成しました。その PR の CI と Commitlint は `action_required` の状態で作られ、承認するまで開始されませんでした (run の `created_at` と `run_started_at` に 22 秒の差があります)。承認後は commitlint・lint・Go 1.25/1.26 の test がすべて pass しています。

同時に、バージョンが 2 箇所で揃って更新されることも確認できました。PR #1 の差分は `CHANGELOG.md` / `.release-please-manifest.json` / `internal/version/version.go` の 3 ファイルだけで、後者 2 つがどちらも `0.1.0` から `0.2.0` へ書き換わっています。

その `0.2.0` という値は、**この時点の観測であって初回リリースの一般則ではありません**。release-please は manifest の値を「現在のバージョン」として読み、そこから後のコミットを見て次を決めます。このときは `0.1.0` に `feat` 5 件が乗っていたので minor が上がりました。別のリポジトリ、別のコミット列なら別の値になります。

### 配布物

| 対象 | 配布 |
|---|---|
| linux / darwin × amd64 / arm64 | `rh-sc-mcp_<os>_<arch>.tar.gz` を Release に添付 |
| windows | **配布しません** |
| コンテナイメージ | **配布しません** ([制限事項 7](#7-コンテナイメージの-build-は未検証)) |

Windows を外しているのは、このサーバーを Windows で動かしたことが一度も無いためです。バイナリを配ると「動くはず」という暗黙の主張になります。Windows で使う場合はソースからビルドしてください。

macOS 向けバイナリについては、**ローカルで `go build` したものは本開発環境 (darwin/arm64) で stdio・HTTP とも動作確認済み**ですが、**GoReleaser が CI で生成する配布用 darwin バイナリは未検証**です。署名も notarization も行っていないため、ダウンロードしたバイナリは Gatekeeper に止められる可能性があります。

---

## ライセンス

[MIT License](LICENSE) — Copyright (c) 2026 konono
