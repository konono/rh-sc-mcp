# 提供ツールと応答

[使い始める](../../README.ja.md) · [ドキュメント一覧](README.md)

## 提供ツール

許可レベルを満たすものだけが登録されます。

| ツール | 必要権限 | 対応 API | 説明 |
|---|---|---|---|
| `rhcase_scope` | なし (常時) | — | 有効なエンドポイント・権限・ツール一覧・上限値を返す |
| `rhcase_search` | `case: read` | `POST /v3/cases/filter` | キーワード・ステータス・severity・製品・担当・アカウント・期間で検索。[条件を最低 1 つ要求する](limitations.md#6-フィルタ検索は条件なしでは拒否される-仕様書に記載なし) |
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

検索バックエンドの応答をそのまま返さず、**射影します**。素通しすると 1 件あたり約 10 KB (うち `abstract` が 9.4 KB) で、既定 `rows` では 1 回 62 KB になるためです。詳細と実測値は[制限事項 2](limitations.md#2-kcs-検索のレスポンス形状は仕様化されていない) にあります。

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
