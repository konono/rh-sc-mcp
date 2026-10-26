# テストとリリース

[使い始める](../README.md) · [ドキュメント一覧](README.md)

このページの `make` と `gh` の例は、リポジトリのルートディレクトリで実行します。

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

リリースは **release-please + GoReleaser + GHCR イメージ公開**で自動化しています ([konono/aw](https://github.com/konono/aw) の公開方式を参考)。

```
main への push
  └─ release-please: conventional commits を読み、リリース PR を更新
       └─ その PR をマージ
            ├─ タグ・GitHub Release・CHANGELOG を作成
            ├─ GoReleaser: バイナリを Release に添付
            └─ Buildx: GHCR に linux/amd64・linux/arm64 のイメージを push
```

初回公開時、GHCR の package は既定で private です。一般利用者がログインせず pull できるようにするには、package の設定画面で visibility を **Public** に変更し、認証なしの `podman pull` で確認します。GitHub Release が公開されたことだけでは、イメージの公開まで確認できたことにはなりません。過去のタグを再ビルドする場合は `Publish container image` ワークフローを手動実行できます。手動実行では `latest` を既定で動かしません。

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
| コンテナイメージ | GHCR に linux/amd64・linux/arm64 をリリースタグと `latest` で公開 ([デプロイ手順](deployment.md)) |

Windows を外しているのは、このサーバーを Windows で動かしたことが一度も無いためです。バイナリを配ると「動くはず」という暗黙の主張になります。Windows で使う場合はソースからビルドしてください。

macOS 向けバイナリについては、**ローカルで `go build` したものは本開発環境 (darwin/arm64) で stdio・HTTP とも動作確認済み**ですが、**GoReleaser が CI で生成する配布用 darwin バイナリは未検証**です。署名も notarization も行っていないため、ダウンロードしたバイナリは Gatekeeper に止められる可能性があります。

---
