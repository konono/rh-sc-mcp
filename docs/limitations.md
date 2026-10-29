# Limitations and verified scope

**English** · [日本語](ja/limitations.md)

[Get started](../README.md) · [Documentation index](README.md)

## Limitations and unverified areas

What is written here is what has *not* been confirmed. It is kept separate so that it is never presented as if it had been.

### 1. An inconsistency in the specification itself: `exp` in `SolrSearchRequest`

The request body for `/search/v2/kcs`, `SolrSearchRequest`, declares `required: ["q", "exp"]`, but `exp` does not exist in `properties`; what exists is `expression`.

```json
"SolrSearchRequest": {
  "required": ["q", "exp"],
  "properties": { "q": {...}, "expression": {...}, "start": {...}, "rows": {...}, "clientName": {...} }
}
```

**This inconsistency was settled against the real API** (2026-10-25, reads only, with `rows: 1`).

| Body sent | Result |
|---|---|
| `{q, rows}` | `200` — **`exp` is not required** |
| `{q, rows, expression}` | `200` — identical structure to the above. `expression` is accepted |
| `{q, rows, exp}` | **`500`** — sending `exp` breaks it |
| `{q, rows, clientName}` (what this implementation sends) | `200` |

So the specification's `required: ["q", "exp"]` is wrong twice over. Not only is `exp` not required — **following the specification literally produces a `500`**. This implementation sends only `expression` and never `exp`, which is correct, and an `exp` field must not be added to `SolrSearchRequest`. A test pins the absence of `exp` in the request body.

Incidentally, the error body for the `500` was `{message, detailMessage}`, decodable as the same `LegacyApiErrorResponse` the case API uses.

### 2. The shape of the KCS search response is not specified

The `200` for `/search/v2/kcs` carries only `"description": "Solr Response"` with no schema defined. **Since the specification does not say, the following is what was measured** (2026-10-25).

```
responseHeader: {status, QTime, zkConnected, params}
response:       {docs[], numFound, start, maxScore, numFoundExact}
spellcheck:     {}
```

One entry in `docs[]` had 26 fields. The breakdown is the problem: **`abstract` alone accounted for 9,401 B of 10,076 B (93%)**, alongside four copies of the same title (`publishedTitle` / `allTitle` / `title` / `sortTitle`), three URIs, and internal vector-index flags (`*_vector_field_set` and similar). Passed through raw, the API's default `rows=10` makes one call 62 KB.

So `rhkcs_search` **returns a projection** — `id` / `title` (one of the four) / `documentKind` / `language` / `lastModifiedDate` / `viewUri` / `snippet`, plus `numFound` / `numFoundExact` / `start` / the number returned. Measured against real responses, **`rows=1` goes from 13,435 B to 684 B and `rows=10` from 62,436 B to 11,364 B**.

- `snippet` is `abstract` truncated to **300 Unicode characters** (characters, not bytes, so the count is the same for English and Japanese). When truncation happens, `snippetTruncated` is set and it is also mentioned in `notes`.
- `viewUri` is the API's `view_uri` passed through **verbatim**. Whether it is an absolute URL, and whether it opens without a session, is not checked by this server.
- `numFoundExact` is returned too, because the API itself declares when `numFound` is approximate.

**Handling of unknown shapes**: since the shape is unspecified, it changing is expected rather than exceptional. If `response.docs` is missing or cannot be read as an array, no projection is attempted and an `unrecognizedShape` is returned containing **only `topLevelKeys` / `sizeBytes` / a description**. **None of the content is included** — telling you precisely that the shape changed, so a human can go look, is more useful than pouring an unknown payload into the model's context. This comes back as a normal result, not an error.

`rhkcs_search` remains off by default. It is a different API surface from case management, and the unspecified response shape is one more reason to require an explicit opt-in.

### 3. The console URL has no default

The human-facing case URL (something like `https://.../support/cases/#/case/<number>`) appears nowhere in the published API specification. This form was assembled at first, but **the default was removed because there is no basis for it**.

- Unset: `caseUrl` does not appear in tool output. Instead you get the `uri` (for a case) and `location` (on creation) that the API itself returned.
- Set (`caseViewURL`): a URL is returned as `caseUrl`, formed by appending the case number as the final path segment. The correctness of that value is the configuring operator's responsibility.

### 4. With `apiVersion: v1`, the same case could not be fetched

The specification also contains `/v1/cases` paths, and the version is configurable, but what this implementation assumes and tests is `v3`. Specifying `v1` produces a startup warning.

Here is what measuring the read paths on 2026-10-26 produced (a one-off observation with this environment's credentials).

| Call | Result |
|---|---|
| `POST /v1/cases/filter` (short `keyword`) | `400`, `{message, detailMessage}`, "Search string too short". **v3 accepts the same word with a 200** |
| `POST /v1/cases/filter` (ordinary word) | `200`, but only `totalCount: 0` with no `cases` key at all |
| `POST /v1/cases/filter` (a known number in `caseNumbers`) | `200` / `totalCount: 0` / zero results. **v3 returns `totalCount: 1` and one result for the same query** |
| `GET /v1/cases/{caseNumber}` (a number v3 returns 200 for) | `404`, "Unable to find case by number" |
| `GET /v1/cases/{n}/comments` (same) | `404` |

**With these credentials, cases retrievable through v3 could not be retrieved through v1.** With the same token and the same case number, v3 returns `200` (the case body with 73 keys, comments as an array).

**The reason has not been established.** v1's data scope could differ, this account might lack v1 access, or those paths might be effectively unsupported — the responses do not let you tell these apart. Nor is there material for generalising to "v1's read path is broken".

The error responses have two keys, `message` and `detailMessage`, a subset of this implementation's `LegacyApiErrorResponse`, so they decode. The `200` filter response also decodes as `FilterCasesResponse`, just without `cases`, and is treated as zero results. **Whether a successful v1 case body decodes as the `Case` type is unverified**, because none was ever obtained.

#### The same behaviour appears through this server

The table above came from hitting HTTP directly, but the binary with `RH_SC_API_VERSION=v1` behaved identically.

- `--print-scope` reports `apiVersion: v1` and a startup warning appears
- `--check-auth` shows `endpoint` as `.../support/v1`
- `rhcase_search` over MCP returns **`totalCount: 0` and zero results, without an error**

**This is the operational trap.** `--check-auth` returns `tokenExchange: ok` / `apiProbe: ok` even on `v1`, because the probe calls filter and v1's filter returns `200`. In other words, **`ok` from `--check-auth` means only "the credentials were accepted", not "cases can be retrieved at this `apiVersion`"**. It says `ok` even while `GET /v1/cases/{n}` returns `404`. For the same reason, zero results from `rhcase_search` cannot be distinguished between "no matches" and "not retrievable at this version".

Use `v3` (the default) in production.

### 5. What has been checked against the real API — reads verified, writes not

Every automated test runs against `httptest` stubs. The stubs were written from the published specification, so **by themselves they cannot detect a divergence between the specification and the real API** (one was in fact found; see [limitation 6](#6-filter-search-is-rejected-without-criteria-not-documented-in-the-specification)).

Against the real endpoints, the following was confirmed with a valid offline token on 2026-10-24 and 25.

| What was checked | Result |
|---|---|
| SSO token exchange | Succeeded. A Bearer token was issued with `expires_in` of `900` |
| Shape of SSO error responses | `{"error":"invalid_grant","error_description":"..."}` with `400`. Matches this implementation's `ssoError` |
| Routing of the v3 paths | `/support/v3/...` is actually reached (an authenticated request returned `200`) |
| `POST /v3/cases/filter` | `200`. Shape `{totalCount, cases}`, with DTO keys consistent with the specification |
| `GET /v3/cases/{caseNumber}` | `200`. An object with 73 keys, with a matching case number |
| `GET /v3/cases/{n}/comments` | `200`. An array (empty for a case with no comments) |
| `GET /v3/cases/{n}/comments/{id}` | `200`. An object with a matching id |
| Real behaviour over MCP | Connected to a real process with the Go SDK's `CommandTransport`. `tools/list` returned 5 tools with no write tools; `rhcase_search` / `rhcase_get` / `rhcase_list_comments` succeeded |
| `--check-auth` (success path) | With the fixed keyword added, exit 0, `tokenExchange: ok`, `apiProbe: ok`, no `error` |
| `--check-auth` (failure path) | With an unacceptable value, exit 1, `tokenExchange: failed`, `apiProbe: skipped`. No credential exposure on either path |
| `POST /search/v2/kcs` | `200`. Solr shape `{responseHeader, response, spellcheck}`. `500` only when `exp` is sent ([limitation 1](#1-an-inconsistency-in-the-specification-itself-exp-in-solrsearchrequest) / [2](#2-the-shape-of-the-kcs-search-response-is-not-specified)) |

Against the 73 keys returned by `GET /v3/cases/{caseNumber}`, this implementation's `Case` type is a projection that takes only the meaningful fields (see [The API surface](architecture.md#the-api-surface)). Unknown keys are ignored, so decoding does not break when fields not in the specification appear.

**What remains unverified in the v3 case API is the write paths** — `POST /v3/cases` (create), `PUT /v3/cases/{n}` (update) and `POST /v3/cases/{n}/comments` (post a comment). Running those leaves irreversible changes on a support case, so they need to be done deliberately against a chosen target, separately from read verification. The create, update and comment response shapes described in [Tools](tools.md) currently rest **on the published specification alone**.

`POST /search/v2/kcs` has been verified for reads (see the table above and [limitation 1](#1-an-inconsistency-in-the-specification-itself-exp-in-solrsearchrequest) / [2](#2-the-shape-of-the-kcs-search-response-is-not-specified)). That said, only one path was checked — an ordinary search term with `rows` kept small — so the behaviour of an elaborate Solr expression in `expression`, and the full set of fields that can come back, are not known. The `v1` read path was also measured, but with these credentials cases retrievable through v3 could not be retrieved ([limitation 4](#4-with-apiversion-v1-the-same-case-could-not-be-fetched)).

#### When `invalid_grant` comes back

During development, passing a value from a 41-byte file (40 characters plus a newline) made SSO return `400 invalid_grant: Invalid refresh token`. **That is the entire measurement** — the response does not tell you what form the value had, whether it had expired or been revoked, or whether it was an offline token at all. SSO returns the same `invalid_grant` in every one of those cases.

If `--check-auth` returns `invalid_grant`, replace the token with one issued from [Red Hat's official API Tokens page](https://access.redhat.com/management/api) and check that no trailing newline crept in.

### 6. Filter search is rejected without criteria (not documented in the specification)

Sending only `{"maxResults": 1}` to `POST /v3/cases/filter` makes the API return `400 No valid external filter criteria provided`. **Paging and sorting do not count as search criteria.** This behaviour appears nowhere in `FilterCasesV3Request`, which declares no required fields either (confirmed against the real API on 2026-10-25).

This implementation handles it in two ways.

- `rhcase_search` **rejects a call with no criteria before sending** and says what to add. Returning the API's `400` as-is would not tell you which argument was missing.
- The `--check-auth` probe attaches a fixed keyword. Without it, the probe **always fails** against the real API. The version before the keyword was added did return `400` against the real API, which is how the divergence was found. The version with the keyword has been confirmed to return `apiProbe: ok` with a real token.

Which fields count as "criteria" is documented neither in the specification nor in the API's responses; all that is confirmed is that `keyword` counts. So this implementation's check is lenient: "send it if there is at least one thing that is not paging or sorting". The API may still reject what is sent, in which case its error is returned as-is.

Note that this divergence could not be found by the stub-based tests. After finding it, the same rejection was implemented in the stub, so removing the criteria from the probe now fails that test.

### 7. Containers verified on Podman, OpenShift and the published image

On 2026-10-26, a build and run were confirmed with Podman 5.7.1, and on the same day an overlay was applied to a real OpenShift cluster. The v0.5.0 GitHub Actions run published linux/amd64 and linux/arm64 with Docker Buildx, and both were started with `docker run --version`. Locally, the arm64 image was pulled without authentication and confirmed to report `rh-sc-mcp 0.5.0`. The published image was also deployed to OpenShift and exercised over HTTP, within the scope below.

**It works in a configuration equivalent to the manifests.** Under the same conditions as `deploy/base/deployment.yaml` — read-only root filesystem, all capabilities dropped, no pinned `runAsUser` but an arbitrarily chosen UID, the ConfigMap equivalent mounted at `/etc/rh-sc-mcp/config.yaml` and passed with `--config` — the following was confirmed.

| Check | Result |
|---|---|
| `podman build` | Succeeded. Distroless base (no `/bin/sh`), `USER 65532:65532`, about 11.8 MB |
| An arbitrary UID (`--user 1000650000`) + `--read-only` + `--cap-drop=ALL` | Starts |
| `--print-scope` | Prints normally |
| `/healthz` / `/readyz` | Both `200` |
| `/mcp` (without / with token) | `401` / a JSON-RPC result |
| stdio `initialize` inside the container | A JSON-RPC result |
| Startup logs | The HTTP-enabled warning appears. No token in the output |
| Shutdown on `SIGTERM` | Logs `shutting down` and exits with code `0`. `podman stop` behaves the same and does not use up the default 10-second wait |

That UID is one arbitrarily chosen value. Verification with a UID actually assigned by a cluster follows below.

**This idle-state shutdown completed on SIGTERM alone.** Sending `podman kill --signal TERM` makes the process log `shutting down` and exit with code `0`. `podman stop` also exits `0`, and the command returned in 0.373 seconds — well inside the default 10-second wait, never escalating to SIGKILL.

That said, **there were no in-flight requests at measurement time**. The HTTP server is designed to wait up to 20 seconds for in-flight HTTP requests after SIGTERM, but that wait itself was not measured under load. The numbers above describe a shutdown with nothing to drain.

**It also worked on a real OpenShift cluster.** A dedicated namespace was created on a cluster with a single amd64 node (Ready) and `deploy/overlays/openshift` was applied. The only change from the manifests was swapping `image` for a verification build (linux/amd64) in the cluster-internal registry. Credentials were a dummy offline token and a randomly generated shared HTTP token; **the real Red Hat API was never called** (and naturally nothing was written).

| Check | Result |
|---|---|
| Pods | Both of `replicas: 2` Ready |
| SCC | `restricted-v2` |
| Assigned UID | `1000980000` — chosen by the cluster, not specified from our side |
| `readOnlyRootFilesystem` | `true` |
| `/healthz` / `/readyz` (port-forward) | Both `200` |
| `POST /mcp` without token (port-forward) | `401` |
| `initialize` with token (port-forward) | `200`, `serverInfo.name` is `rh-sc-mcp` |
| `tools/list` with token (port-forward) | `200`. The 5 read-only tools (`rhcase_scope` / `rhcase_search` / `rhcase_get` / `rhcase_list_comments` / `rhcase_get_comment`) |
| Through the `Route` (without / with token) | `401` / `initialize` returns `200` |

The "independent of the assigned UID" design described in [Containers and Kubernetes / OpenShift](deployment.md#containers-and-kubernetes--openshift) was **confirmed to behave as intended at two values: 1000650000 under Podman and 1000980000 as assigned by the cluster**. Two samples do not prove it works at any UID — the range `restricted-v2` can assign differs per namespace, and only a tiny part of one was touched. The basis is still the design, which avoids depending on a specific UID or on file ownership; the measurements merely failed to contradict it.

**The `Route`'s TLS was not verified.** The Route's cluster certificate could not be validated against a local trust store, so `curl -k` was used. The `Route` row above shows only that HTTP exchanges work through the Route; **TLS trustworthiness from a user's point of view is not guaranteed**. Check the certificate path yourself before exposing this publicly.

**State the node architecture explicitly.** The first attempt put an arm64 image on an amd64 node, and the Pod failed to start with `Exec format error`. The `Dockerfile` cross-compiles based on `TARGETOS` / `TARGETARCH`, but without those it produces the build host's architecture. Rebuilding from an arm64 machine for an amd64 node worked with these flags:

```bash
podman build --platform linux/amd64 \
  --build-arg BUILDPLATFORM=linux/arm64 \
  --build-arg TARGETOS=linux \
  --build-arg TARGETARCH=amd64 \
  -t <registry>/<namespace>/rh-sc-mcp:<tag> .
```

`--platform` decides the final image's architecture, and `TARGETOS` / `TARGETARCH` are passed to `go build` in the build stage. **Omitting `--platform` can leave the final image as arm64, reproducing `Exec format error`.** `BUILDPLATFORM` is the value for the machine doing the build. Check the result afterwards with the following — it was confirmed to return `linux/amd64`:

```bash
podman image inspect <image> --format '{{.Os}}/{{.Architecture}}'
```

**Shutdown and drain on a real cluster are unverified.** The SIGTERM measurements above were Podman-only; whether Pod deletion or a rolling update behaves the same has not been checked.

**There is one constraint.** The image's `CMD` points at `--config /etc/rh-sc-mcp/config.yaml`. Replace it and omit `--config`, and generating the default configuration file will try to write into `$HOME` and fail — `mkdir ... read-only file system` on a read-only root filesystem, and `permission denied` under an assigned UID because `$HOME` belongs to a different user. If you use the bare image without its `CMD`, either pass `--config` or point `HOME` somewhere writable. Using it as the manifests do avoids this entirely.

The Podman and OpenShift HTTP and stdio measurements above were made against a locally built image.

**Additional verification with the published v0.5.0 image:** the base manifests were applied in a dedicated namespace with `ghcr.io/konono/rh-sc-mcp:v0.5.0`, and 2 Pods reached Ready. In the first run a production offline token was supplied from a Secret, and `--check-auth` inside a Pod returned `tokenExchange: ok` / `apiProbe: ok`. That probe only sends one read-only filter request and does not establish that cases can be retrieved. No case was created, updated or commented on.

The second run swapped in a dummy offline token and a random shared HTTP token, created no Route, and used port-forwarding to the Service to confirm `200` on `/healthz` and `/readyz`, `401` on unauthenticated `/mcp`, and `200` with a JSON-RPC result for an authenticated `initialize`. Both verification namespaces and their Secrets were deleted afterwards. **Connecting through a Route with the published image, retrieving a real case, and shutdown/drain on a real cluster remain unverified.**

### 8. severity / status / product values are not validated

The specification defines no enumerations for these, so values such as `"4 (Low)"` are not validated server-side. A wrong value comes back as an upstream error. Where the specification does state the values (`contentType`, `createdByType`, comment `sortField` / `sortOrder`, case search `sortOrder`), they are validated and normalised.

---
