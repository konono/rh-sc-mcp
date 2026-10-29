# アンインストール

[使い始める](../../README.ja.md) · [ドキュメント一覧](README.md)

以下の `make uninstall-*` は、リポジトリを取得してそのルートディレクトリで実行します。

## アンインストール

**すべてを消す 1 コマンドはありません。** 4 つの動かし方はそれぞれ別の場所に別のものを残し、そのいくつかは他のソフトウェアと共有しています。どれを指しているのかを推測する代わりに、面ごとに分けてあります。

```bash
make uninstall-stdio                                      # Claude Code の "rh-sc" 登録
make uninstall-http          PORT=8080                    # "rh-sc-http" 登録 (+ 使用中ポートの報告)
make uninstall-binary        CONFIRM=<path>               # go install したバイナリ
make uninstall-podman        CONTAINER=rh-sc-mcp          # 指定したコンテナ / イメージ
make uninstall-openshift     NAMESPACE=rh-sc-mcp          # namespace 内のこのアプリの資源
make uninstall-credentials   CONFIRM=<dir>                # ~/.config/rh-sc-mcp の token
```

### 名指ししない限り消さないもの

| 対象 | 理由 |
|---|---|
| `~/.config/rh-sc-mcp` の token | offline token の再発行は Red Hat のポータルに行く作業です。アンインストールはそれを失う理由になりません |
| OpenShift の namespace | たいていこのアプリより古く、他のものが入っています |
| `go install` したバイナリ | stdio と HTTP が同じ 1 ファイルを指していることがあり、片方のために消すともう片方が壊れます |
| 動いているプロセス | ポートを掴んでいる相手には、このスクリプトからは見えない所有者と再起動ポリシーがあります |

### それぞれの境界

**stdio / HTTP は Claude Code の登録を外します。** stdio は `rh-sc`、HTTP は `rh-sc-http` です。scope は別々のレジストリなので、`SCOPE`(既定 `user`) が実際の scope と食い違えば**何もせずに停止**します。project / local scope の登録は今いるディレクトリに紐づくため、cwd も表示します。`URL` を渡した場合は登録済みの URL と完全一致した時だけ進みます。

登録の中身は**表示しません**。HTTP の登録は `Authorization` ヘッダを持てますし、URL はクエリに資格情報を含みえます。出すのは名前・scope・cwd だけです。中身を見たいときは `claude mcp get <name>` を自分で実行してください。

**HTTP のサーバープロセスは止めません。** `PORT` を渡すと誰が掴んでいるかを表示しますが、kill はしません。shell のジョブなのか launchd / systemd のユニットなのかでやめさせ方が違い、監視下のプロセスなら殺してもすぐ戻ってきます。所有者が分かってから自分で止めてください。`PORT` を省略しても登録解除は行われます — リモートの HTTP サーバーにはローカルのポートがないためです。

**Podman は名指ししたものだけです。** `CONTAINER` と `IMAGE` のどちらも推測しません。パターンも使いません。`podman rmi $(podman images -q rh-sc-mcp)` のような形は、名前の似た他人のイメージを巻き込みます。

**OpenShift は指定 namespace 内の、このアプリの資源だけです。** `deployment` / `service` / `route` / 生成された `configmap/rh-sc-mcp-<hash>` / `secret/rh-sc-mcp` を消します。生成 ConfigMap の一覧取得に失敗した場合は**何も消さずに停止**します — 権限エラーを「対象なし」と取り違えると、成功に見えるのに残る、という一番まずい結果になるためです。

`secret/rh-sc-mcp` は**既定で消します**。ワークロードだけ消えて資格情報が誰も見ていない namespace に残るほうが悪い既定だからです。原本は `~/.config/rh-sc-mcp` にあり、`make deploy` がそこから書き直します。この Secret が別の誰かのものである場合や、すぐ再デプロイする場合は `KEEP_SECRET=1` で残せます (残した場合は終了時に警告を出します)。

---
