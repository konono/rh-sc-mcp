# Configuration and permissions

**English** · [日本語](ja/configuration.md)

[Get started](../README.md) · [Documentation index](README.md)

## Configuration

Configuration comes from a YAML file and from environment variables. The default path of the configuration file is `~/.config/rh-sc-mcp/config.yaml`. The file is chosen in the order `--config`, `RH_SC_MCP_CONFIG`, default path. The file and the environment are merged per key: **a key present in both is taken from the environment**, and keys that exist only in the file are used as written.

On the first run without `--config` or `RH_SC_MCP_CONFIG`, the directory at the default path (mode `0700`) and a minimal configuration file (mode `0600`) are created. An existing file is never overwritten. What is generated is just `readOnly: true` plus explanatory comments; it contains no credentials. If no offline token is configured, the server creates the file and then stops with an error.

```bash
# The first run generates the configuration file. Without a token it prints guidance and exits
rh-sc-mcp --print-scope
$EDITOR "$HOME/.config/rh-sc-mcp/config.yaml"
```

Every key is documented in [the example configuration](../config.example.yaml). **The offline token is the only required setting**; every other key is optional and falls back to its default. Add the offline token to the generated file as `offlineToken:`, or pass it through the environment variable `$RH_SC_OFFLINE_TOKEN` — the latter keeps credentials out of the file entirely. If a file explicitly named by `--config` or `RH_SC_MCP_CONFIG` does not exist, that is an error; the default file is not created in that case.

The minimal setup — token in the environment, generated configuration file — gives you a read-only server pointed at the public endpoint.

```bash
RH_SC_OFFLINE_TOKEN=... rh-sc-mcp --print-scope
```

### Main keys

| Key | Default | Description |
|---|---|---|
| `offlineToken` | (required) | Red Hat API offline token. The environment variable is recommended |
| `baseURL` | `https://api.access.redhat.com/support` | Root of the case API (including basePath) |
| `apiVersion` | `v3` | Major version of the case API (`v3` / `v1`) |
| `ssoTokenURL` | `https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token` | Token exchange endpoint |
| `ssoClientID` | `rhsm-api` | client_id for the token exchange |
| `readOnly` | `true` | Global kill switch. While true, everything is reduced to read |
| `defaultPermission` | (per-resource default) | Default level for resources not named in `resources` |
| `resources.<name>.permission` | `case`/`comment`=`read`, `kcs`=`none` | Per-resource `none` / `read` / `write` |
| `caseViewURL` | (none) | Prefix for human-facing links. When unset, `caseUrl` is not emitted |
| `maxResults` | `50` | Maximum cases per search (the API's own cap is 200) |
| `http.addr` | (none) | HTTP listen address. Empty means HTTP is disabled (off by default) |
| `http.token` | (none) | Shared bearer token. Required when `http.addr` is set, and must differ from the offline token |
| `http.stateless` | `true` | Hold no session. The default for running multiple replicas |
| `http.sessionTimeoutSeconds` | `0` | Only meaningful when stateful |
| `http.trustedOrigins` | `[]` | Extra origins allowed by the cross-origin check |
| `maxComments` | `50` | Maximum comments in a listing (truncated client-side) |
| `timeoutSeconds` | `60` | Time limit for a single upstream HTTP call |

### Environment variables

| Environment variable | Corresponding key |
|---|---|
| `RH_SC_OFFLINE_TOKEN` | `offlineToken` |
| `RH_SC_BASE_URL` | `baseURL` |
| `RH_SC_API_VERSION` | `apiVersion` |
| `RH_SC_SSO_TOKEN_URL` | `ssoTokenURL` |
| `RH_SC_SSO_CLIENT_ID` | `ssoClientID` |
| `RH_SC_READ_ONLY` | `readOnly` (`true`/`false`) |
| `RH_SC_MCP_CONFIG` | Path to the configuration file (`--config` wins; otherwise `~/.config/rh-sc-mcp/config.yaml`) |
| `RH_SC_MCP_HTTP_ADDR` | `http.addr` |
| `RH_SC_MCP_HTTP_TOKEN` | `http.token` |

### Permission model

There are three resources and three levels, ordered `none` < `read` < `write`.

| Resource | Default | Tools it covers |
|---|---|---|
| `case` | `read` | `rhcase_search` / `rhcase_get` (read), `rhcase_create` / `rhcase_update` (write) |
| `comment` | `read` | `rhcase_list_comments` / `rhcase_get_comment` (read), `rhcase_add_comment` (write) |
| `kcs` | `none` | `rhkcs_search` (read) |

Three things are worth knowing.

- **`case` and `comment` are independent.** Writing `comment: write` + `case: read` gives you "let the agent reply, but don't let it change status or severity".
- **`kcs` is never enabled unless you say so explicitly.** Even `defaultPermission: write` leaves it at `none`. It is a different API surface from case management, so "I want to read cases" must not implicitly turn on knowledge base search.
- **While `readOnly: true`, any granted write is ignored.** That way you can tighten things temporarily without going around deleting individual permission lines.

### Checking the configuration

You can see what is actually in effect without starting the server.

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

This output contains no credentials at all (not even a fingerprint). You can paste it straight into a bug report.

### Connectivity check (`--check-auth`)

You can find out whether the offline token works without starting a session.

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

The two stages are **reported separately**, because their causes and their fixes differ.

- `tokenExchange` — whether SSO accepted the offline token. On failure, SSO returns `invalid_grant` without distinguishing the cause, so you have to check the value itself.
- `apiProbe` — whether the API accepted the issued access token. SSO succeeding while the API returns `403` points to the account's visibility, not the token.

**`apiProbe: ok` does not mean cases can be fetched.** All it establishes is that a search request is accepted with these credentials. In fact, with `apiVersion: v1` both report `ok` while fetching individual cases returns `404` ([limitation 4](limitations.md#4-with-apiversion-v1-the-same-case-could-not-be-fetched)). To confirm fetching works, pass a known case number to `rhcase_get`.

The API-side probe is a single search with a fixed keyword (`maxResults: 1`). A keyword is needed because the API **rejects a filter with no search criteria** ([limitation 6](limitations.md#6-filter-search-is-rejected-without-criteria-not-documented-in-the-specification)). When `case` is `none`, the probe is skipped entirely and `apiProbe` says so.

**No case information appears in this diagnostic output.** The probe asks for at most one result and **discards the response body** — all it looks at is whether there was an error. Whether or not the search term matched anything, case data never reaches the output.

The keyword itself was chosen to be distinctive enough that matching is unlikely (a run on 2026-10-25 returned zero results). But that is **an observation about the data at that time**, not a property of the word, and a case containing this string in the future cannot be ruled out. The safety of the output rests on the "cap at one result and discard the body" behaviour above.

Result counts are not reported either, because the count for a particular search term does not mean "the number of cases you can see". To learn your visible scope, use `rhcase_search`.

On failure the exit code is 1, so this also works as a health check. **The output contains no credentials** (even if the upstream reflects the request headers back, they are scrubbed).

If you keep the token in a file, pass it without putting the value on the command line, and strip the trailing newline.

```bash
chmod 600 token   # the bare minimum if you keep one. token / *.token are already in .gitignore
RH_SC_OFFLINE_TOKEN="$(tr -d '\r\n' < token)" rh-sc-mcp --check-auth
```

Stripping the trailing newline is mandatory. With the newline attached you are sending a different string, which SSO will not accept — and the resulting error does not distinguish causes, so you lose the ability to tell it apart from a problem with the token itself.

---
