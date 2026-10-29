# Design and implementation

**English** · [日本語](ja/architecture.md)

[Get started](../README.md) · [Documentation index](README.md)

## Design principles

1. **The published specification is the source of truth.** What is implemented is what the bundled `CaseManagement-API_v1.json` (Swagger 2.0, `host: api.access.redhat.com`, `basePath: /support`) describes. Anything not in the specification is either made configurable or left out.
2. **Permissions are checked twice: at registration and at call time.** A tool outside your permissions is never even listed, so the model cannot propose it.
3. **Read-only by default.** A write becomes possible only when **both** `readOnly: false` and a per-resource `permission: write` are set explicitly.
4. **Never emit a value without a basis.** Case numbers and console URLs are not assembled from guesses (see [Limitations](limitations.md#limitations-and-unverified-areas)).

---
## The API surface

Based on the bundled `CaseManagement-API_v1.json`.

```
swagger:  2.0
title:    Support Services (version: v3)
host:     api.access.redhat.com
basePath: /support
schemes:  https
security: Bearer (apiKey, in: header, name: Authorization)
```

Only **the v3 case API and KCS search** are implemented. The specification also covers attachments, SFTP tokens, accounts, business hours, notified users, external tracker updates and more, but those are out of the first scope and not implemented.

| Implemented | Endpoint |
|---|---|
| ✅ | `POST /v3/cases` |
| ✅ | `POST /v3/cases/filter` |
| ✅ | `GET /v3/cases/{caseNumber}` |
| ✅ | `PUT /v3/cases/{caseNumber}` |
| ✅ | `GET /v3/cases/{caseNumber}/comments` |
| ✅ | `POST /v3/cases/{caseNumber}/comments` |
| ✅ | `GET /v3/cases/{caseNumber}/comments/{commentId}` |
| ✅ | `POST /search/v2/kcs` (optional, off by default) |
| ❌ | attachments / SFTP / accounts / businesshours / notifiedusers / externaltrackerupdates / v1-specific paths |

### Coping with variation in 2xx responses

The same success can come back in different shapes, so all of the following are accepted.

| Shape | Handling |
|---|---|
| `201` + a `location` array in the body | Taken as the location |
| `201` + only a `Location` header | Same |
| Both (identical values) | De-duplicated to one |
| `200` with an empty body | Treated as success, with a message supplied |
| `204` | Same |
| JSON whose `Content-Type` is not `application/json` | Parsed as JSON if it starts with `{` or `[` |
| `200` but HTML (a proxy sign-in page, say) | **Reported as an error** (not an empty struct, and not "succeeded but location unknown") |
| Zero comments with an empty body | Treated as an empty array |

The last row applies to creation too. `POST /v3/cases` can succeed with only a `Location` header, so an empty body is tolerated — but **if the body is not empty, it is validated as JSON**. Skipping that would let a gateway's `200` sign-in page be read as "the case was created but the number could not be read", which sends the caller looking for a case that does not exist, or creating the same case twice.

The error raised in that situation quotes the start of the body for diagnosis, but **the quoted part is scrubbed before it is truncated**. Upstreams reflecting the `Authorization` header back is exactly what tends to happen in this "200 + HTML" case (see [Security handling](#security-handling)).

---
## How authentication works

1. Present the offline token to the SSO realm with `grant_type=refresh_token` and `client_id=rhsm-api`
2. Cache the returned access token until **60 seconds before** the time implied by `expires_in`
3. Attach `Authorization: Bearer <access token>` to subsequent API calls
4. On a `401`, invalidate **only the token that request used** and retry exactly once

Notes:

- Concurrent calls serialise the token exchange into one, so the offline token is not sent more often than necessary.
- The `401` retry happens once. Against a server that always returns `401`, there is no loop.
- A `403` is not retried. That is an authorisation decision, not an expired token.
- A response with no `expires_in`, or an absurdly short one, is not read as "never expires"; it is treated as a short finite lifetime.

**Note:** the published specification only goes as far as "put a Bearer token in the `Authorization` header". The procedure for obtaining an access token from an offline token (the SSO realm URL, the `rhsm-api` client_id) is not in the specification; it comes from Red Hat's published API token instructions and reference implementations. That is why `ssoTokenURL` and `ssoClientID` are configurable.

---
## Security handling

### Credentials never reach the output

Error messages and logs are scrubbed in two stages.

1. **Removing the real values this process holds** — the offline token and the current access token are substituted out. The access token changes on every refresh, so the current value is consulted each time rather than a snapshot.
2. **Removing by pattern** — `Bearer <...>`, keyed values such as `access_token` / `refresh_token` / `client_secret`, and JWTs starting with `eyJ` are removed even if this process has never seen them. This catches upstream errors that quote the request headers back, and SSO error responses that carry a new token along with them.

Truncation happens **after** scrubbing. Cutting first would split a token in half and leave a prefix the patterns no longer recognise.

Scrubbing applies **not just to error responses, but to errors built from successful responses**. The "expected JSON but got …" diagnostic produced when `200` returns HTML quotes the start of the body, and a proxy's sign-in page sometimes contains the request's `Authorization` header verbatim; without scrubbing here, the 2xx path would remain as the one leak. `Response` in `internal/redhat` carries its own scrubber and cleans this quotation before truncating it.

### Path safety

Case numbers are validated against a regular expression (1–32 digits) and rejected before a request is sent. On top of that, every path segment is passed through `url.PathEscape` when the path is built. Values like `../../v3/accounts/current` or `04337263/comments` cannot rewrite the path. Comment IDs are similarly restricted to 1–64 alphanumeric characters.

### Other

- Response reading stops at 8 MiB.
- Error bodies are truncated to 2000 bytes after scrubbing, and the truncation is stated.
- `--print-scope` output contains no credentials.
- Starting with a write-capable configuration prints a warning to stderr.
- Write tools carry `destructiveHint` (for the MCP client's confirmation prompt). Their descriptions also state plainly that the action cannot be undone and is visible to everyone on the account.

---
## Differences from the earlier reference implementation (hydra-mcp-server)

[fnarel/hydra-mcp-server](https://github.com/fnarel/hydra-mcp-server) (Python / FastMCP) was used as a reference, but several decisions differ.

| Aspect | hydra-mcp-server | This implementation |
|---|---|---|
| Language / SDK | Python + FastMCP | Go + MCP Go SDK |
| Transport | stdio only | stdio (default) + Streamable HTTP (opt-in, bearer auth required) |
| Endpoint | `access.redhat.com/hydra/rest/v1` (a path not in the published specification) | `api.access.redhat.com/support` + `v3` (as published). Configurable |
| Operations | Read only (get case, search cases, KCS search) | Reads plus create, update and comment |
| Permission control | None (all tools always exposed) | Per-resource `none`/`read`/`write` from operator configuration. Read-only by default |
| Case search | Solr (`/search/v2/cases`) | v3 structured filter (`POST /v3/cases/filter`) |
| Error handling | Propagates `raise_for_status()` as-is | Interprets the specification's `LegacyApiErrorResponse` and returns it with credentials scrubbed |
| Tokens | Fetch and cache | Fetch and cache, plus a single retry on `401` and invalidation scoped to the token used |

`/hydra/rest` paths are not in the published specification, so this documentation gives no configuration example for them. `baseURL` and `apiVersion` are configurable, so you can technically point there, but what comes back is outside this implementation's guarantees (pointing at a non-default endpoint produces a startup warning).

---
