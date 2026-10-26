# 設定と権限

[使い始める](../README.md) · [ドキュメント一覧](README.md)

## 設定

設定は YAML ファイルと環境変数で行います。設定ファイルの既定パスは `~/.config/rh-sc-mcp/config.yaml` です。`--config`、`RH_SC_MCP_CONFIG`、既定パスの順にファイルを選びます。設定ファイルと環境変数はキー単位でマージし、**両方にあるキーは環境変数が優先**、ファイルにしかないキーはそのまま使います。

`--config` と `RH_SC_MCP_CONFIG` を指定せずに初めて起動すると、既定パスのディレクトリ (権限 `0700`) と最小構成の設定ファイル (権限 `0600`) を作ります。既存ファイルは上書きしません。生成内容は `readOnly: true` と説明コメントだけで、認証情報は含みません。offline token が未設定なら、ファイルを作った後に起動はエラーで止まります。

```bash
# 初回は設定ファイルを生成。token 未設定なら案内を表示して終了
rh-sc-mcp --print-scope
$EDITOR "$HOME/.config/rh-sc-mcp/config.yaml"
```

[設定例](../config.example.yaml)に全キーの説明があります。**必須なのは offline token だけ** で、それ以外のキーはすべて省略可能です (省略時は既定値)。offline token は生成したファイルに `offlineToken:` として追加するか、環境変数 `$RH_SC_OFFLINE_TOKEN` で渡します。後者なら認証情報をファイルに保存せずに済みます。明示した `--config` または `RH_SC_MCP_CONFIG` のファイルが無い場合はエラーになり、既定ファイルは作りません。

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

**`apiProbe: ok` はケースを取得できることまでは示しません。** 確かめているのは「この資格情報で検索リクエストが受理される」ことだけです。実際に `apiVersion: v1` では、両方 `ok` になる一方で個々のケースの取得は `404` になります ([制限事項 4](limitations.md#4-apiversion-v1-では同じケースを取得できませんでした))。取得まで確認したい場合は `rhcase_get` に既知のケース番号を渡してください。

API 側のプローブは固定キーワードでの検索 1 回 (`maxResults: 1`) だけです。キーワードが必要なのは、API が**検索条件のないフィルタを拒否する**ためです ([制限事項 6](limitations.md#6-フィルタ検索は条件なしでは拒否される-仕様書に記載なし))。`case` が `none` の構成ではプローブ自体を省略し、その旨が `apiProbe` に入ります。

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
