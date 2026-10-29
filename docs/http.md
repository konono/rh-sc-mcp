# Running the HTTP server

**English** · [日本語](ja/http.md)

[Get started](../README.md) · [Documentation index](README.md)

## Exposing the server over HTTP

With the default stdio transport, the client starts the server and the OS decides who can talk to it. For shared deployments, MCP's [Streamable HTTP][streamable] transport is also available.

```bash
export RH_SC_MCP_HTTP_TOKEN="$(openssl rand -hex 32)"
rh-sc-mcp --http :8080
```

[streamable]: https://modelcontextprotocol.io/specification/2025-06-18/basic/transports

| Path | Authentication | Purpose |
|---|---|---|
| `/mcp` | Bearer token | MCP endpoint |
| `/healthz` | none | Liveness |
| `/readyz` | none | Readiness |

### What a token holder can reach

A client that can present the shared bearer token reaches **everything this configuration's scope allows**. Within that scope there is no filtering of cases at all — meaning **every case the offline token's owner can see is visible**. This server has no feature for restricting access to particular cases or accounts.

The only things you can narrow are the resource kind (`case` / `comment` / `kcs`) and read versus write. To narrow the visible set itself, your only option is to **use an offline token from a Red Hat account with less access**. And since the token is shared by all clients, you cannot vary what each client sees.

### The shared bearer token is mandatory, and distinct from the offline token

If you enable HTTP without a token, the server **does not start**. Over stdio the OS controls access; an open port does not, and this process can reach your organisation's support cases.

Further, **the shared token may not be the same string as the offline token**. An identical value is also a startup error. The shared token is handed out to every MCP client, so making it identical would give each client the Red Hat credential itself, letting them bypass this server's scope and hit the support API directly.

```bash
# Generate one
openssl rand -hex 32
```

Error messages never include either value, because this kind of output ends up pasted into bug reports.

### Probes

`/healthz` and `/readyz` are deliberately unauthenticated: the kubelet has no token to present.

Both report only "this process is responding" and **touch neither SSO nor the Red Hat API**. An implementation that did touch them would consume a token exchange on every poll, and a transient upstream problem would fail the probes and cause a restart loop. The tests confirm that after hitting the probes, the API stub has received zero requests.

### Configuration

```yaml
http:
  addr: ":8080"                 # can also come from --http / $RH_SC_MCP_HTTP_ADDR
  token: xxxxx                  # $RH_SC_MCP_HTTP_TOKEN is recommended
  stateless: true               # default
  sessionTimeoutSeconds: 300    # only when stateful (stateless: false)
  trustedOrigins: []            # extra origins allowed by the cross-origin check
```

Precedence for `addr` is `--http` flag > `$RH_SC_MCP_HTTP_ADDR` > `http.addr`. If all are empty, HTTP is disabled and the server stays on stdio (**off by default**).

Because `--http` is a way to enable HTTP after the fact from the command line, **the token requirement, the offline-token-equality check and the address format are all re-validated after the flag is applied**. A configuration with no token in the file is perfectly valid until that flag shows up.

`stateless` defaults to true so that multiple replicas can sit behind one Service without sticky routing. Set it to false only for a single instance that needs server-initiated messages.

Cross-origin protection and DNS rebinding protection are on by default. They prevent a page in a browser from driving this endpoint with the user's credentials, and prevent reaching a localhost-bound server through a rebound hostname.

### The permission model does not depend on the transport

Exactly the same scope applies over HTTP as over stdio. With the read-only default, write tools do not appear in `tools/list` and are refused if called. Changing the transport changes the route, not the policy (confirmed with real traffic over HTTP).

`--print-scope` includes the HTTP settings, but **never the token — not its value, its length, or a prefix**. Only `tokenConfigured: true/false`.

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

An example configuration lives in [`examples/http-server.yaml`](../examples/http-server.yaml).

---
