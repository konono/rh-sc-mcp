# rh-sc-mcp

Red Hat サポートケース管理 API を MCP (Model Context Protocol) ツールとして公開する Go 製サーバーです。stdio で動作し、Claude Code などの MCP クライアントから Red Hat のサポートケースを検索・参照・作成・更新・コメントできます。

**何を許可するかは operator が設定で決めます。** モデル側からは変更できません。許可レベルを超えるツールは `tools/list` に現れず、ハンドラ側でも呼び出し時に再チェックします。既定は read-only です。

---

## 目次

- [設計方針](#設計方針)
- [必要なもの](#必要なもの)
- [ビルド](#ビルド)
- [設定](#設定)
- [MCP クライアントからの接続](#mcp-クライアントからの接続)
- [提供ツール](#提供ツール)
- [対象 API](#対象-api)
- [認証の仕組み](#認証の仕組み)
- [セキュリティ上の扱い](#セキュリティ上の扱い)
- [制限事項と未確認事項](#制限事項と未確認事項)
- [旧参照実装 (hydra-mcp-server) との差分](#旧参照実装-hydra-mcp-server-との差分)
- [テスト](#テスト)

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

## ビルド

```bash
# ビルド (bin/rh-sc-mcp が生成される)
make build

# $GOPATH/bin にインストール
make install

# テスト・整形・静的解析
make test
make fmt
make vet
```

バージョン確認:

```bash
./bin/rh-sc-mcp --version
```

---

## 設定

設定は YAML ファイルと環境変数で行います。両方指定した場合は **環境変数が優先** します。

```bash
cp config.example.yaml config.yaml
$EDITOR config.yaml
```

`config.example.yaml` に全キーの説明があります。**必須なのは offline token だけ** で、それ以外のキーはすべて省略可能です (省略時は既定値)。offline token も環境変数 `$RH_SC_OFFLINE_TOKEN` で渡すなら設定ファイルに書く必要はなく、その場合は設定ファイルそのものを省略できます。どちらにも無い場合は起動時にエラーになります。

最小構成 (環境変数だけ、設定ファイルなし) では、公開エンドポイントに対する read-only サーバーになります。

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
| `RH_SC_MCP_CONFIG` | `--config` のパス |

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
./bin/rh-sc-mcp --config config.yaml --print-scope
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
./bin/rh-sc-mcp --config config.yaml --check-auth
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

API 側のプローブは固定キーワードでの検索 1 回 (`maxResults: 1`) だけです。キーワードが必要なのは、API が**検索条件のないフィルタを拒否する**ためです ([制限事項 6](#6-フィルタ検索は条件なしでは拒否される仕様書に記載なし))。`case` が `none` の構成ではプローブ自体を省略し、その旨が `apiProbe` に入ります。

**この診断出力にケースの情報は入りません。** プローブは最大 1 件しか要求せず、かつ**応答本文を破棄します** — 見ているのはエラーの有無だけです。検索語が何かにマッチしたかどうかに関わらず、ケースのデータが出力に乗ることはありません。

キーワード自体は、マッチする見込みが低くなるよう十分に固有な語を選んでいます (2026-10-25 の実行では 0 件でした)。ただしこれは**その時点のデータについての観測**であって語の性質ではなく、将来ケースにこの文字列が含まれる可能性は排除できません。出力の安全性が依存しているのは上記の「1 件上限 + 応答破棄」のほうです。

ケースの件数も報告しません。特定の検索語での件数は「見えるケース数」を意味しないためです。可視範囲を知りたい場合は `rhcase_search` を使ってください。

失敗時は exit code 1 を返すのでヘルスチェックにも使えます。**出力に認証情報は含まれません** (上流がリクエストヘッダを反射してきた場合も洗浄されます)。

token をファイルに置いている場合は、値をコマンドラインに出さず、末尾改行を落として渡してください。

```bash
chmod 600 token   # 置くなら最低限これ。token / *.token は .gitignore 済み
RH_SC_OFFLINE_TOKEN="$(tr -d '\r\n' < token)" ./bin/rh-sc-mcp --check-auth
```

末尾改行の除去は必須です。改行が付いたままだと別の文字列を送ることになり、SSO は受理しません。そのときのエラーは原因を区別しないので、token 自体の問題との切り分けができなくなります。

---

## MCP クライアントからの接続

stdio トランスポートで動作します。

### Claude Code

```bash
claude mcp add rh-sc -- /path/to/rh-sc-mcp --config /path/to/config.yaml
```

### 設定ファイルで直接書く場合

```json
{
  "mcpServers": {
    "rh-sc": {
      "command": "/path/to/rh-sc-mcp",
      "args": ["--config", "/path/to/config.yaml"],
      "env": {
        "RH_SC_OFFLINE_TOKEN": "..."
      }
    }
  }
}
```

offline token を設定ファイル側 (`offlineToken:`) に書くか、MCP クライアントの `env` で渡すかは運用次第です。`env` に書くとそのクライアントの設定ファイルにも秘密が広がる点に注意してください (エージェントが読める場所であることが多い)。

ログは **stderr** に出ます。stdout は MCP のトランスポートなので、プロトコルフレーム以外は一切流しません。

---

## 提供ツール

許可レベルを満たすものだけが登録されます。

| ツール | 必要権限 | 対応 API | 説明 |
|---|---|---|---|
| `rhcase_scope` | なし (常時) | — | 有効なエンドポイント・権限・ツール一覧・上限値を返す |
| `rhcase_search` | `case: read` | `POST /v3/cases/filter` | キーワード・ステータス・severity・製品・担当・アカウント・期間で検索。条件を最低 1 つ要求する |
| `rhcase_get` | `case: read` | `GET /v3/cases/{caseNumber}` | ケース 1 件の詳細 (description、コメント履歴を含む) |
| `rhcase_create` | `case: write` | `POST /v3/cases` | ケース新規作成 |
| `rhcase_update` | `case: write` | `PUT /v3/cases/{caseNumber}` | ケースの部分更新 |
| `rhcase_list_comments` | `comment: read` | `GET /v3/cases/{caseNumber}/comments` | コメント一覧 (日付範囲・ソート指定可) |
| `rhcase_get_comment` | `comment: read` | `GET /v3/cases/{caseNumber}/comments/{commentId}` | コメント 1 件 |
| `rhcase_add_comment` | `comment: write` | `POST /v3/cases/{caseNumber}/comments` | コメント投稿 |
| `rhkcs_search` | `kcs: read` | `POST /search/v2/kcs` | ナレッジベース検索。タイトル・種別・`viewUri`・短い抜粋に射影して返す (任意機能、既定 OFF) |

### 入出力の設計

- **ケース番号は文字列です。** `04337263` のように先頭のゼロが意味を持つため、数値にすると壊れます。入力は数字 1〜32 桁に制限しており、それ以外は **ネットワークに出る前に** 拒否されます。
- **検索結果は要約行です。** `POST /v3/cases/filter` は 1 件ごとに `GetCaseResponse` 全体 (description とコメント履歴を含む) を返すため、そのまま 50 件返すとモデルのコンテキストを食い潰します。検索では「どのケースを開くか決めるのに必要な項目」だけに絞り、本文は `rhcase_get` で取得します。
- **更新は差分のみ送ります。** 省略したフィールドはペイロードに含めない (= 変更しない)、空文字を渡すとクリアする、という区別をします。`summary` の空文字だけは拒否します (件名のないケースは backend 側で弾かれ、その理由が伝わらないため)。
- **応答の注意点は `notes` に書きます。** 件数を切り詰めた、`maxResults` を丸めた、ページがまだある、API が一部フィールドを反映しなかった、などを明示します。

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

自動テストはすべて `httptest` のスタブに対するものです。スタブは公開仕様から起こしたものなので、**それ自体では仕様と実 API の食い違いを検出できません** (実際に 1 件見つかりました。[制限事項 6](#6-フィルタ検索は条件なしでは拒否される仕様書に記載なし))。

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

### 7. severity / status / product の値は検証しない

仕様書がこれらの列挙値を定義していないため、`"4 (Low)"` のような値をサーバー側で検証することはしていません。誤った値は上流のエラーとして返ります。一方、仕様書が値を明示している項目 (`contentType`、`createdByType`、コメントの `sortField` / `sortOrder`、ケース検索の `sortOrder`) は検証・正規化します。

---

## 旧参照実装 (hydra-mcp-server) との差分

[fnarel/hydra-mcp-server](https://github.com/fnarel/hydra-mcp-server) (Python / FastMCP) を参考にしていますが、方針が異なる点があります。

| 観点 | hydra-mcp-server | 本実装 |
|---|---|---|
| 言語 / SDK | Python + FastMCP | Go + MCP Go SDK |
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

## ライセンス

未定 (リポジトリ所有者が決定)。
