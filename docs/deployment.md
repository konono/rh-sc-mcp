# コンテナと OpenShift / Kubernetes

[使い始める](../README.md) · [ドキュメント一覧](README.md)

このページの `make` と `oc apply -k` の例は、リポジトリを取得してそのルートディレクトリで実行します。

## コンテナと Kubernetes / OpenShift

リリース時に `ghcr.io/konono/rh-sc-mcp:<release-tag>` と `:latest` を linux/amd64・linux/arm64 の両方で作成します。**デプロイには版を固定したタグ** (例 `v0.5.0`) を指定してください。初回公開後に GitHub Packages 側で package の visibility を Public にする必要があります。利用前に[パッケージページ](https://github.com/users/konono/packages/container/package/rh-sc-mcp)で公開状態を確認してください。

```bash
podman pull ghcr.io/konono/rh-sc-mcp:<release-tag>
```

イメージには credential や設定ファイルを含めていません。起動時に `--config` で設定ファイルをマウントし、Red Hat の offline token と別の共有 HTTP token を Secret から環境変数で渡します。OpenShift では後述の `make deploy` がこの Secret を作ります。

### イメージの差し替え方

`deploy/base/kustomization.yaml` の `images:` でタグを固定します。ここで**`name` は変更しません**。`name` は「どのイメージを置換するか」の照合キーで、`deployment.yaml` に書かれた値と一致している必要があります。ここを変更すると照合が外れ、**kustomize は何も置換せず元の値のまま出力します** (エラーも警告も出ません)。

公式イメージなら `newTag` のみ変更します。別レジストリへ複製する場合は `newName` も変更します。

```yaml
images:
  - name: ghcr.io/konono/rh-sc-mcp              # 照合キー。触らない
    newTag: <release-tag>
```

適用前に、置換後のイメージを目で確認してください。

```bash
kubectl kustomize deploy/overlays/kubernetes | grep 'image:'
kubectl kustomize deploy/overlays/openshift  | grep 'image:'
# → image: ghcr.io/konono/rh-sc-mcp:<release-tag>
```

指定したタグに変わらない場合は、`name` を書き換えて照合が外れていないか確認してください。

### 認証情報と scope

**OpenShift なら [`make deploy`](#make-でデプロイする) が Secret も作ります。** `~/.config/rh-sc-mcp` の token ファイルから読み、改行を落として `--from-file` で渡すので、下の手順を自分で踏む必要はありません。

Kubernetes overlay を使う場合や、Secret だけ先に作りたい場合は次のようにします。**`--from-literal` は使わないでください** — 値が `kubectl` の `argv` に乗り、同じホストの誰でも `ps` で読めます。

```bash
# 1. 認証情報。offline token と共有 HTTP token は別の値にすること。
#    ファイル経由で渡す。末尾の改行は Secret の値に入ってしまうので落とす。
mkdir -p ~/.config/rh-sc-mcp && chmod 700 ~/.config/rh-sc-mcp
install -m 600 /dev/null ~/.config/rh-sc-mcp/token
$EDITOR ~/.config/rh-sc-mcp/token          # offline token を貼る
( umask 077; openssl rand -hex 32 | tr -d '\n' > ~/.config/rh-sc-mcp/http-token )

# 2. Secret を作る。末尾改行を落とした一時ファイルを経由する。
#    サブシェルにまとめてあるのは、kubectl が失敗しても必ず消すため。
(
  umask 077                                  # dir 700 / file 600
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT                  # 成功・失敗どちらでも消える
  printf '%s' "$(cat ~/.config/rh-sc-mcp/token)" > "$tmp/offlineToken"
  kubectl create secret generic rh-sc-mcp \
    --from-file=offlineToken="$tmp/offlineToken" \
    --from-file=httpToken="$HOME/.config/rh-sc-mcp/http-token"
)

# 3. scope。readOnly と resources を確認する。
$EDITOR deploy/base/config.yaml
```

サーバーは環境変数を trim しないので、**値に改行が残ると** offline token は SSO が `invalid_grant` を返し、bearer token は照合が外れて全リクエストが `401` になります。原因の分かりにくい失敗なので、`--from-file` に渡すファイルは必ず改行なしにしてください。

`deploy/base/secret.example.yaml` は `REPLACE_ME` 入りのテンプレートです。実値を入れたファイルは `.gitignore` 済み (`deploy/**/secret.yaml` 等) なので、サンプルだけが追跡されます。

### 適用前の確認

```bash
kubectl kustomize deploy/overlays/kubernetes
kubectl kustomize deploy/overlays/openshift
```

### OpenShift

`Route` は `host` を省いているのでクラスタが生成し、`termination: edge` がルーターの既定証明書を使います。追加の編集は不要です。ただしその既定証明書を MCP クライアント側が信頼できるかは別問題で、検証時はローカルの信頼ストアで検証できませんでした ([制限事項 7](limitations.md#7-コンテナは-podman-と-openshift-実クラスタで検証済み-docker-自体では未実行))。

```bash
oc apply -k deploy/overlays/openshift
```

これは手で順番に実行する形です。ビルド・適用・確認を毎回同じ手順で行うなら、次の `make` を使ってください。段階は同じで、各段を 1 コマンドにしてあります。

### `make` でデプロイする

`scripts/deploy-openshift.sh` を Makefile から呼びます。`NAMESPACE` と `IMAGE` に既定値はありません。どちらも書き込み先を指す値で、既定を決めるとこちらが勝手に選ぶことになるためです。

```bash
# 1. 何が適用されるかを見る (クラスタは変更しない)
make deploy-plan NAMESPACE=rh-sc-mcp IMAGE=ghcr.io/konono/rh-sc-mcp:<release-tag>

# 2. 適用 (ビルドと push は不要)
make deploy NAMESPACE=rh-sc-mcp IMAGE=ghcr.io/konono/rh-sc-mcp:<release-tag>

# 3. 状態と Route URL
make deploy-status NAMESPACE=rh-sc-mcp
```

自分で改変したイメージを使う場合だけ、`make deploy-image IMAGE=<registry>/<name>:<tag> ARCH=<node-arch>` でビルド・push できます。

**token はファイルから読みます。** `~/.config/rh-sc-mcp/token` に Red Hat の offline token を `chmod 600` で置いてください。共有 bearer token (`http-token`) は無ければ生成して同じ場所に保存し、以後使い回します — 毎回作り直すと、設定済みのクライアントが全部 `401` になるからです。検証用に別のディレクトリを使うときは `RH_SC_MCP_CREDENTIAL_DIR` で差し替えられます。

| 項目 | 扱い |
|---|---|
| 値の渡し方 | `--from-file`。`--from-literal` は値が `argv` に乗り `ps` で見えます |
| 末尾の改行 | 取り除きます。サーバーは環境変数を trim しないので、残ると SSO が `invalid_grant`、bearer 照合が全件 `401` になります |
| 値の中の空白 | **拒否します**。黙って削ると、原因の分からない `401` に化けます |
| 出力 | 値・ハッシュ・長さのいずれも出しません |
| ConfigMap | `deploy/base/config.yaml` に `offlineToken` / `http.token` があれば適用前に停止します。そこに書いた値は、その ConfigMap を読める権限を持つ相手に見えます |

**手元で確かめられることは、クラスタを触る前に確かめます。** token ファイルの不在・権限・改行・値の中の空白、そして kustomize のレンダエラーは、namespace を作る前に出ます。

ただし**それ以降の失敗までは防げません**。namespace を作ったあとの Secret 作成や `oc apply` が RBAC や通信で失敗すれば、**空の namespace は残ります**。その場合は原因を直して再実行してください — 同じコマンドを繰り返して問題ない作りです。

**消すものはありません。** `oc apply` は `--prune` なしで実行します。overlay から消したリソースはクラスタに残るので、片付けは意図的に手で行ってください。Secret は毎回 `~/.config/rh-sc-mcp` の内容で上書きされ、その後 `oc rollout restart` が走ります — ConfigMap と違って Secret の名前はハッシュを持たないため、再起動しないと Pod は古い値のままです。

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

**割り当てられた UID に依存しないよう、特定 UID やファイル所有権への依存を避けてあります** — バイナリは誰でも読める配置に置いてあり、**この構成ではマウントした設定を `--config` で読むだけで、起動時にファイルを書きません**。マニフェスト側で `runAsUser` を固定していないのも、SCC の割り当てを妨げないためです。これは設計意図にとどまりません。**任意に選んだ UID 1000650000 で、read-only root filesystem + 全 capability drop のもと実際に動くことを Podman で確認**し、さらに **OpenShift 実クラスタで `restricted-v2` が割り当てた UID 1000980000 でも Pod が Ready になることを確認**しました ([制限事項 7](limitations.md#7-コンテナは-podman-と-openshift-実クラスタで検証済み-docker-自体では未実行))。確認したのはこの 2 つの UID で、全 UID を試したわけではありません。

書き込みが起きないのは `--config` を渡しているからです。これを省くと既定の設定ファイルを作ろうとして `$HOME` に書き込みます。マニフェストは `args` で常に渡すので該当しませんが、`CMD` を置き換える場合は[制限事項 7](limitations.md#7-コンテナは-podman-と-openshift-実クラスタで検証済み-docker-自体では未実行) を参照してください。

OpenShift overlay の `Route` には 300 秒の HAProxy タイムアウトを付けています。streamed な MCP 応答は開いたままになるため、ルーターの既定 30 秒では切られます。Kubernetes overlay は代わりに `Ingress` を置きます。どちらも公開するのは `/mcp` だけで、プローブはクラスタ内に留めます。

---
