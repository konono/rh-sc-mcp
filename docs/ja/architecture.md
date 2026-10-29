# 設計と実装

[使い始める](../../README.ja.md) · [ドキュメント一覧](README.md)

## 設計方針

1. **公開仕様を正とする。** 同梱の `CaseManagement-API_v1.json` (Swagger 2.0 / `host: api.access.redhat.com` / `basePath: /support`) に書かれている範囲を実装します。仕様に書かれていないことは、設定可能にするか、やらないかのどちらかです。
2. **権限は登録時と呼び出し時の二重チェック。** 許可外のツールはそもそも列挙されないので、モデルが提案することすらありません。
3. **既定 read-only。** 書き込みは `readOnly: false` と個別の `permission: write` の **両方** を明示したときだけ有効になります。
4. **根拠のない値を出力しない。** ケース番号やコンソール URL を推測で組み立てて返すことはしません ([制限事項](limitations.md#制限事項と未確認事項) 参照)。

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

`/hydra/rest` 系のパスは公開仕様書に含まれていないため、このドキュメントでは設定例を示しません。`baseURL` / `apiVersion` は設定可能なので技術的には向けられますが、そのとき何が返るかは本実装の保証外です (既定以外のエンドポイントを指定すると起動時に警告が出ます)。

---
