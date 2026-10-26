# 制限事項と実測範囲

[使い始める](../README.md) · [ドキュメント一覧](README.md)

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

### 4. `apiVersion: v1` では同じケースを取得できませんでした

仕様書には `/v1/cases` 系のパスも存在し、設定で切り替えられるようにしてありますが、本実装が想定・テストしているのは `v3` です。`v1` を指定すると起動時に警告が出ます。

2026-10-26 に read 経路だけを実測した結果は次のとおりです (この環境の資格情報での 1 回限りの観測です)。

| 呼び出し | 結果 |
|---|---|
| `POST /v1/cases/filter` (`keyword` が短い) | `400`。`{message, detailMessage}` で「Search string too short」。**v3 は同じ語を 200 で受理**します |
| `POST /v1/cases/filter` (通常の語) | `200`。ただし `totalCount: 0` のみで `cases` キー自体がない |
| `POST /v1/cases/filter` (`caseNumbers` に既知の番号) | `200` / `totalCount: 0` / 0 件。**同じ条件で v3 は `totalCount: 1` / 1 件** |
| `GET /v1/cases/{caseNumber}` (v3 が 200 を返す番号) | `404`「Unable to find case by number」 |
| `GET /v1/cases/{n}/comments` (同上) | `404` |

**この資格情報では、v3 で取得できるケースを v1 からは取得できませんでした。** 同一トークン・同一ケース番号で v3 側は `200` (ケース本体 73 キー、comments は配列) を返します。

**理由は確定していません。** v1 のデータ範囲が異なる、このアカウントに v1 の権限がない、当該パスが実質的に非対応になっている、などが考えられますが、応答からは区別できません。「v1 の read 経路が壊れている」と一般化できる材料もありません。

エラー応答は `message` と `detailMessage` の 2 キーで、本実装の `LegacyApiErrorResponse` の部分集合なのでデコードできます。`200` の filter 応答も `cases` が無いだけで `FilterCasesResponse` としてデコードでき、0 件として扱われます。一方、**v1 の成功時のケース本体は取得できていないため、`Case` 型でデコードできるかは未確認**です。

#### この挙動は本サーバー経由でも同じです

上の表は HTTP を直接叩いた結果ですが、`RH_SC_API_VERSION=v1` を設定した本バイナリでも同じでした。

- `--print-scope` は `apiVersion: v1` を報告し、起動時に警告が出ます
- `--check-auth` の `endpoint` は `.../support/v1` になります
- MCP 経由の `rhcase_search` は**エラーにならず `totalCount: 0` / 0 件**を返します

**ここが運用上の落とし穴です。** `--check-auth` は `v1` でも `tokenExchange: ok` / `apiProbe: ok` を返します。プローブは filter を呼んでおり、v1 の filter は `200` を返すからです。つまり **`--check-auth` の `ok` は「資格情報が受理された」ことだけを意味し、その `apiVersion` でケースを取得できることは意味しません**。v1 では `GET /v1/cases/{n}` が `404` を返す状態でも `ok` になります。同じ理由で、`rhcase_search` が 0 件を返しても「該当なし」なのか「このバージョンでは取得できない」なのかは区別がつきません。

実運用では `v3` (既定) を使ってください。

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

`GET /v3/cases/{caseNumber}` が返す 73 キーに対し、本実装の `Case` 型は意味のあるフィールドだけを取る射影です ([対象 API](architecture.md#対象-api) 参照)。未知のキーは無視されるので、仕様書に無いフィールドが増えてもデコードは壊れません。

**v3 ケース API のうち未確認なのは書き込み経路です** — `POST /v3/cases` (作成)、`PUT /v3/cases/{n}` (更新)、`POST /v3/cases/{n}/comments` (コメント投稿)。これらは実行するとサポートケースに取り消せない変更が残るため、読み取りの確認とは別に、対象を決めた上で明示的に実施する必要があります。[ツールの詳細](tools.md)にある作成・更新・コメントの応答形状は、現時点では**公開仕様書のみを根拠としています**。

`POST /search/v2/kcs` は読み取りで確認済みです (上表、および[制限事項 1](#1-仕様書自身の不整合-solrsearchrequest-の-exp) / [2](#2-kcs-検索のレスポンス形状は仕様化されていない))。ただし確認したのは `rows` を絞った一般的な検索語での 1 系統だけで、`expression` に凝った Solr 式を渡した場合の挙動や、返りうるフィールドの全体像は把握していません。`apiVersion: v1` の read 経路も実測しましたが、この資格情報では v3 で取得できるケースを取得できませんでした ([制限事項 4](#4-apiversion-v1-では同じケースを取得できませんでした))。

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

### 7. コンテナは Podman・OpenShift・公開イメージで検証済み

2026-10-26 に Podman 5.7.1 で build と実行を、同日 OpenShift 実クラスタで overlay の適用を確認しました。v0.5.0 の GitHub Actions では Docker Buildx で linux/amd64・linux/arm64 を公開し、両方を `docker run --version` で起動確認しました。ローカルでも認証なしで arm64 イメージを pull し、`rh-sc-mcp 0.5.0` が返ることを確認しています。公開イメージの OpenShift 配置と HTTP 通信も、下記の範囲で確認しました。

**マニフェスト相当の構成で動きます。** `deploy/base/deployment.yaml` と同じ条件 — read-only root filesystem、全 capability drop、`runAsUser` を固定せず任意に選んだ UID を与える、ConfigMap 相当を `/etc/rh-sc-mcp/config.yaml` にマウントし `--config` で渡す — で次を確認しました。

| 確認項目 | 結果 |
|---|---|
| `podman build` | 成功。distroless ベース (`/bin/sh` 不在)、`USER 65532:65532`、約 11.8 MB |
| 任意に選んだ UID (`--user 1000650000`) + `--read-only` + `--cap-drop=ALL` | 起動する |
| `--print-scope` | 正常に出力 |
| `/healthz` / `/readyz` | どちらも `200` |
| `/mcp` (token なし / あり) | `401` / JSON-RPC の result |
| コンテナ内の stdio `initialize` | JSON-RPC の result |
| 起動ログ | HTTP 有効化の警告が出る。token の出力なし |
| `SIGTERM` での停止 | `shutting down` を記録して終了コード `0`。`podman stop` も同じで、既定の 10 秒待ちを使い切らない |

この UID は任意に選んだ 1 値です。クラスタが実際に割り当てた UID での確認は後述します。

**今回のアイドル状態での停止は SIGTERM で完結しました。** `podman kill --signal TERM` を送ると、プロセスは `shutting down` を記録して終了コード `0` で終わります。`podman stop` でも終了コードは `0` で、コマンドが戻るまで 0.373 秒 — 既定の 10 秒待ちを使い切らず、SIGKILL への昇格には至っていません。

ただし**計測時に処理中のリクエストはありませんでした**。HTTP サーバーは SIGTERM を受けたあと処理中の HTTP リクエストの終了を最大 20 秒待つ設計ですが、その待ち時間そのものを負荷をかけた状態で測ってはいません。上の数値は「捌くものが何も無いときの停止」です。

**OpenShift 実クラスタでも動きました。** 単一の amd64 ノード (Ready) を持つクラスタに専用 namespace を切り、`deploy/overlays/openshift` を適用しました。マニフェストから変えたのは `image` をクラスタ内部レジストリの検証ビルド (linux/amd64) に差し替えた 1 点だけです。資格情報は dummy の offline token とランダム生成の共有 HTTP token で、**実 Red Hat API は一度も呼んでいません** (当然、書き込みもしていません)。

| 確認項目 | 結果 |
|---|---|
| Pod | `replicas: 2` の両方が Ready |
| SCC | `restricted-v2` |
| 割り当て UID | `1000980000` — クラスタが選んだ値で、こちらからは指定していない |
| `readOnlyRootFilesystem` | `true` |
| `/healthz` / `/readyz` (port-forward) | どちらも `200` |
| `POST /mcp` token なし (port-forward) | `401` |
| `initialize` token あり (port-forward) | `200`、`serverInfo.name` は `rh-sc-mcp` |
| `tools/list` token あり (port-forward) | `200`。read-only の 5 ツール (`rhcase_scope` / `rhcase_search` / `rhcase_get` / `rhcase_list_comments` / `rhcase_get_comment`) |
| `Route` 経由 (token なし / あり) | `401` / `initialize` が `200` |

[コンテナと Kubernetes / OpenShift](deployment.md#コンテナと-kubernetes--openshift) に書いた「割り当てられた UID に依存しない」という設計は、**Podman の 1000650000 とクラスタが割り当てた 1000980000 の 2 値で、意図どおり動くことを確認**しました。2 サンプルなので「どの UID でも動く」ことの証明にはなりません — `restricted-v2` が割り当てうるレンジは namespace ごとに異なり、そのごく一部を踏んだだけです。根拠は依然として「特定 UID とファイル所有権に依存しない作りにしてある」という設計のほうで、実測はそれと矛盾しなかった、という位置づけです。

**`Route` の TLS は確認していません。** Route のクラスタ証明書をローカルの信頼ストアで検証できず、`curl -k` で確認しました。上の `Route` 行が示すのは「Route を通して HTTP のやりとりが成立する」ところまでで、**利用者から見た TLS の信頼性は保証していません**。公開する際は証明書の経路をご自身で確認してください。

**ノードのアーキテクチャを明示してください。** 最初に arm64 のイメージを amd64 ノードへ載せてしまい、Pod が `Exec format error` で起動しませんでした。`Dockerfile` は `TARGETOS` / `TARGETARCH` を見てクロスコンパイルしますが、これらが渡らないとビルドホストのアーキテクチャのまま出てきます。arm64 のマシンから amd64 ノード向けに作り直したときは、次のように明示して通りました。

```bash
podman build --platform linux/amd64 \
  --build-arg BUILDPLATFORM=linux/arm64 \
  --build-arg TARGETOS=linux \
  --build-arg TARGETARCH=amd64 \
  -t <registry>/<namespace>/rh-sc-mcp:<tag> .
```

`--platform` が最終イメージのアーキテクチャを決め、`TARGETOS` / `TARGETARCH` がビルドステージの `go build` に渡ります。**`--platform` を省くと最終イメージが arm64 のまま出ることがあり、`Exec format error` が再発します**。`BUILDPLATFORM` はビルドするマシン側の値です。ビルド後は次で確かめてください — 実際に `linux/amd64` が返ることを確認しています。

```bash
podman image inspect <image> --format '{{.Os}}/{{.Architecture}}'
```

**実クラスタでの停止と drain は未検証です。** 上の SIGTERM の計測は Podman 単体のもので、Pod の削除や rolling update で同じように振る舞うかは確認していません。

**制約が 1 つあります。** イメージの `CMD` は `--config /etc/rh-sc-mcp/config.yaml` を指しています。これを置き換えて `--config` を省くと、既定の設定ファイル生成が `$HOME` に書き込もうとして失敗します — read-only root filesystem では `mkdir ... read-only file system`、割り当て UID では `$HOME` が別ユーザーの所有なので `permission denied` です。素のイメージを `CMD` 抜きで使う場合は、`--config` を渡すか `HOME` を書き込み可能な場所に向けてください。マニフェストどおりに使う限りは該当しません。

上の Podman / OpenShift の HTTP と stdio の実測は、ローカルで作成したイメージに対するものです。

**公開イメージ v0.5.0 を使った追加検証:** 専用 namespace に `ghcr.io/konono/rh-sc-mcp:v0.5.0` を指定して base マニフェストを適用し、2 Pod が Ready になりました。1 回目は本番 offline token を Secret から渡し、Pod 内の `--check-auth` が `tokenExchange: ok` / `apiProbe: ok` を返しました。このプローブは読み取り専用の filter を 1 回送るだけで、ケースを取得できることまでは保証しません。ケースの作成・更新・コメント投稿は実行していません。

2 回目は dummy offline token とランダムな共有 HTTP token に替え、Route を作らず Service への port-forward で `/healthz`・`/readyz` が `200`、未認証の `/mcp` が `401`、認証付き `initialize` が `200` で JSON-RPC result を返すことを確認しました。両方の検証 namespace と Secret は終了後に削除済みです。**公開イメージでの Route 経由の接続、実ケースの取得、実クラスタでの停止・drain は未確認**です。

### 8. severity / status / product の値は検証しない

仕様書がこれらの列挙値を定義していないため、`"4 (Low)"` のような値をサーバー側で検証することはしていません。誤った値は上流のエラーとして返ります。一方、仕様書が値を明示している項目 (`contentType`、`createdByType`、コメントの `sortField` / `sortOrder`、ケース検索の `sortOrder`) は検証・正規化します。

---
