# HTTP サーバーの運用

[使い始める](../../README.ja.md) · [ドキュメント一覧](README.md)

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

設定例は [`examples/http-server.yaml`](../../examples/http-server.yaml) にあります。

---
