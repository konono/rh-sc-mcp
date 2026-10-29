# Testing and releasing

**English** · [日本語](ja/development.md)

[Get started](../README.md) · [Documentation index](README.md)

The `make` and `gh` examples on this page are run from the root directory of the repository.

## Testing

```bash
make test        # go test -race ./...
```

No real API credentials are needed. `httptest` stands up stubs of the SSO realm and the case API, and the following is verified.

**Authentication (`internal/redhat`)**
- The form contents of the `refresh_token` grant (grant_type / client_id / refresh_token)
- Token caching and re-fetching after expiry (time is injected, so no real waiting)
- That a missing or absurdly short `expires_in` is not treated as "never expires"
- That `Invalidate` with an old token does not destroy a newer one
- That an SSO failure keeps information such as `invalid_grant` while never leaking the offline token

**API client (`internal/redhat`)**
- The six 2xx patterns (201+body / 201+header / both / empty 200 / 204 / a location whose number cannot be read)
- Parsing a JSON body whose `Content-Type` is not JSON, and rejecting an HTML body
- Decomposing `LegacyApiErrorResponse`, detecting 404, truncating the body, empty bodies
- **That credentials reflected back in a `200` + HTML response do not leak into the diagnostic message**
- **That `200` + HTML does not count as "success" on the create path**
- `401` → re-fetch token → successful retry; a permanent `401` stopping after two attempts; `403` not being retried
- Timeouts
- That eleven kinds of malformed case number are rejected before reaching the network
- Path segment escaping and preservation of leading zeros
- That updates "do not send omitted fields, but do send empty strings"

**Configuration (`internal/config`)**
- That the default is read-only, and that `readOnly` overrides a granted write
- That a write needs both `readOnly: false` and `permission: write`
- That `kcs` is not enabled by `defaultPermission`
- Rejection of misspelled keys, unknown resources and invalid values; environment precedence; clamping of `maxResults`
- That `caseUrl` is not synthesised when `caseViewURL` is unset

**MCP (`internal/server`)**
- initialize → `tools/list` → `tools/call` over an in-memory transport
- That write tools are not listed on a read-only server, and do appear once granted
- That `comment: write` does not spill over into `case`
- That tightening permissions mid-session makes the call-time check refuse, without reaching the API
- The happy path of each tool, that case bodies do not leak into search results, and truncation plus `notes`
- That argument validation (bad case number / date / sort / enum) stops before reaching the API
- That upstream errors reach the model without any Bearer token mixed in
- **That along the real path (SSO exchange → client → MCP round trip), `rhcase_get` / `rhcase_create` / `rhcase_add_comment` turn a `200` + HTML upstream into a tool error, and neither the access token nor the offline token appears in the model's output**
- That the generated input schemas mark required fields, and that write tools do not claim `readOnlyHint`

**HTTP transport (`internal/server/http_test.go`, `internal/config`)**
- Bearer required (absent / wrong / a prefix of the right token / presenting the offline token → all 401; only the correct token gets 200), and that a rejection reaches the API zero times
- That a 401 response carries the token value neither in the body nor in `WWW-Authenticate`
- That `Handler()` refuses to build with no token, or with a token equal to the offline token (a second layer independent of configuration validation)
- That both probes return 200 unauthenticated, leak neither scope nor tool names nor the token, and produce **zero requests to the API stub**
- Cross-origin: a disallowed Origin is refused, adding it to `trustedOrigins` lets it through, and other origins are still refused
- That `stateless` defaults to true, and that `stateless: false` + `sessionTimeoutSeconds` is applied and the handler builds
- Real HTTP (`httptest` + `StreamableClientTransport`) for initialize → `tools/list` → `tools/call`: read tools visible, the three write tools not listed and refused when called
- That `initialize` returns a JSON-RPC result with a matching `serverInfo.name`
- That cancelling the ctx makes `ServeHTTP` drain and return nil
- Startup failures: `http.addr` with no token / a token equal to the offline token / an address with no port / a negative `sessionTimeoutSeconds` / an empty element in `trustedOrigins`
- Re-validation after a `--http` override (rejecting no token, an offline-token-equal token and a malformed address; accepting a valid override; and an empty override leaving the configuration unchanged)
- That environment variables take precedence over the file, and that enabling HTTP produces a warning
- That the `--print-scope` output conveys the HTTP settings while containing neither the token nor a prefix of it

- That `--check-auth` distinguishes SSO failure from API failure, skips the probe when `case: none`, requests only one result, and emits no credentials
- **That the stub rejects an unfiltered filter with the same `400` as the real API**, that the probe does not trip that rejection, and that `rhcase_search` stops a criteria-less call before sending
- **That even when the probe matches, the response body does not enter the diagnostic output** (the stub returns a case regardless of the search term, so the matching side is tested directly). The keyword itself is only checked for shape (length, alphanumerics)
- **KCS**: projection against a stub with the measured shape (one title out of four duplicates, `numFoundExact` propagation, the paging note), truncation of `abstract` at 300 Unicode characters and the validity of the cut position, that internal fields such as `docs_text_store` do not survive into the output, that `exp` never appears in the request body (the stub also returns `500` for `exp`), that an unknown shape returns structural information only and no content, and clamping of `rows`

```
ok  github.com/konono/rh-sc-mcp/internal/config
ok  github.com/konono/rh-sc-mcp/internal/redhat
ok  github.com/konono/rh-sc-mcp/internal/server
```

---
## Releasing

Releases are automated with **release-please + GoReleaser + a GHCR image publish** (modelled on how [konono/aw](https://github.com/konono/aw) publishes).

```
push to main
  └─ release-please: reads conventional commits and updates the release PR
       └─ merge that PR
            ├─ create the tag, the GitHub Release and the CHANGELOG
            ├─ GoReleaser: attach binaries to the Release
            └─ Buildx: push linux/amd64 and linux/arm64 images to GHCR
```

On a first publish, the GHCR package is private by default. To let ordinary users pull without logging in, change the visibility to **Public** on the package settings page and confirm with an unauthenticated `podman pull`. A published GitHub Release alone is not evidence that the image is public. To rebuild a past tag, the `Publish container image` workflow can be run manually; a manual run does not move `latest` by default.

The current version lives in two places — `.release-please-manifest.json` and `internal/version/version.go`. The former is release-please's record; the latter is a derived copy registered under `extra-files` in `release-please-config.json`. release-please **updates both together** on each release, so you never edit them by hand.

The marker on the `version.go` side is the pair of comments around the constant, `x-release-please-start-version` and `x-release-please-end`. release-please looks for that range and substitutes inside it (verified: with those markers in place, `0.1.0` was actually rewritten to `0.2.0`). **Deleting the markers or separating them from the constant loses the substitution target.** What happens then — an error, or `version.go` silently staying behind — has not been checked, so **leaving the markers alone** is the safe course.

Commit messages must follow [Conventional Commits](https://www.conventionalcommits.org/). release-please reads them to decide the version bump and the release notes, so a commit outside the format **vanishes silently from the changelog**. `commitlint` checks this on pull requests.

### Repository settings you need up front

**No Actions secret is required.** release-please runs with the built-in `GITHUB_TOKEN`.

Not using a dedicated token was a decision based on inspecting the token that was close to hand. In the environment where this was set up, `gh auth token` returned an OAuth token starting with `gho_`, holding scopes such as `repo`, `workflow` and `delete_repo` **across every repository that user can touch**. Putting that into a public repository's Actions secrets would far exceed what releasing this one repository needs.

`gh` can also log in with a PAT, so what your own `gh auth token` returns depends on your environment (`gh auth status` tells you). Still, applying the single setting below and using `GITHUB_TOKEN` is simpler to operate than issuing and continually rotating a scoped fine-grained PAT.

Instead, **change one repository setting**.

> Settings → Actions → General → Workflow permissions →
> enable **Allow GitHub Actions to create and approve pull requests**

This is disabled by default on new personal repositories (in the API, `can_approve_pull_request_reviews: false`). Left disabled, release-please cannot create the PR and fails.

**The Workflow permissions default (Read repository contents...) does not need changing.** `release.yml` declares the `contents: write` and `pull-requests: write` its job needs. Keeping the repository-wide default at read means no other workflow implicitly gains write access.

```bash
# Check the current setting
gh api repos/<owner>/<repo>/actions/permissions/workflow
# → {"default_workflow_permissions":"read","can_approve_pull_request_reviews":true}
```

### CI on the release PR needs manual approval

This is the price of using `GITHUB_TOKEN`. As GitHub's [Trigger a workflow](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/trigger-a-workflow) explains, events raised by `GITHUB_TOKEN` generally do not start other workflows. There is an exception for `pull_request` `opened` / `synchronize`: **the run is created, but stops in an approval-required state**.

So on a PR opened by release-please, CI and commitlint sit waiting. Someone with write access presses **Approve workflows to run** on the PR's Checks tab to start them. Always press it before merging — otherwise the one PR that changes the version is the one merged unchecked.

The GoReleaser job that follows is unaffected: it is not a new workflow triggered by the tag push, but a `needs` dependency inside the same workflow run.

**Measured** (2026-10-25, the first time in this repository): a push to `main` ran the Release workflow successfully and release-please created PR #1. CI and Commitlint on that PR were created in the `action_required` state and did not start until approved (the run's `created_at` and `run_started_at` differ by 22 seconds). After approval, commitlint, lint and the Go 1.25/1.26 tests all passed.

The same run also confirmed that the version is updated in both places at once. The diff of PR #1 was exactly three files — `CHANGELOG.md`, `.release-please-manifest.json` and `internal/version/version.go` — with the latter two both going from `0.1.0` to `0.2.0`.

That `0.2.0` is **an observation at that moment, not a general rule for first releases**. release-please reads the manifest value as "the current version" and decides the next one from the commits after it. At that point `0.1.0` had 5 `feat` commits on top, so the minor version went up. A different repository or a different commit series yields a different value.

### What gets distributed

| Target | Distribution |
|---|---|
| linux / darwin × amd64 / arm64 | `rh-sc-mcp_<os>_<arch>.tar.gz` attached to the Release |
| windows | **Not distributed** |
| Container image | linux/amd64 and linux/arm64 published to GHCR under the release tag and `latest` ([deployment guide](deployment.md)) |

Windows is excluded because this server has never been run on Windows. Shipping a binary is an implicit claim that it works. To use it on Windows, build from source.

As for macOS binaries, **locally `go build`-ed ones have been confirmed to work over both stdio and HTTP in the development environment (darwin/arm64)**, but **the distributable darwin binaries GoReleaser produces in CI are unverified**. They are neither signed nor notarized, so a downloaded binary may be stopped by Gatekeeper.

---
