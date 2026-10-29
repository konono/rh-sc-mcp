# Tools and responses

**English** · [日本語](ja/tools.md)

[Get started](../README.md) · [Documentation index](README.md)

## Available tools

Only the tools whose permission level is satisfied get registered.

| Tool | Permission required | API | Description |
|---|---|---|---|
| `rhcase_scope` | none (always on) | — | Returns the active endpoint, permissions, tool list and result limits |
| `rhcase_search` | `case: read` | `POST /v3/cases/filter` | Search by keyword, status, severity, product, owner, account or date range. [Requires at least one criterion](limitations.md#6-filter-search-is-rejected-without-criteria-not-documented-in-the-specification) |
| `rhcase_get` | `case: read` | `GET /v3/cases/{caseNumber}` | Details of one case, including comment history (omit with `includeComments: false`; truncated at `maxComments` by default) |
| `rhcase_create` | `case: write` | `POST /v3/cases` | Open a new case |
| `rhcase_update` | `case: write` | `PUT /v3/cases/{caseNumber}` | Partial update of a case |
| `rhcase_list_comments` | `comment: read` | `GET /v3/cases/{caseNumber}/comments` | List comments (date range and sort can be specified) |
| `rhcase_get_comment` | `comment: read` | `GET /v3/cases/{caseNumber}/comments/{commentId}` | One comment |
| `rhcase_add_comment` | `comment: write` | `POST /v3/cases/{caseNumber}/comments` | Post a comment. `contentType` is `plaintext` (default) or `html`; `createdByType` is `Customer`, `Associate` or `Internal` |
| `rhkcs_search` | `kcs: read` | `POST /search/v2/kcs` | Knowledge base search. Projected down to title, kind, `viewUri` and a short snippet (optional, off by default) |

### How the inputs and outputs are designed

- **Case numbers are strings.** Leading zeros carry meaning, as in `04337263`, so turning them into numbers breaks them. Input is limited to 1–32 digits, and anything else is rejected **before it goes on the network**.
- **Search results are summary rows.** `POST /v3/cases/filter` returns an entire `GetCaseResponse` per case — description and comment history included — so returning 50 of those verbatim would eat the model's context. Search returns only the fields you need to decide which case to open; the body comes from `rhcase_get`.
- **Updates send only the delta.** Omitted fields are not included in the payload (i.e. left alone), while an empty string clears the field — the two are kept distinct. The one exception is an empty `summary`, which is rejected (a case without a subject is refused by the backend, and the reason does not make it back to you).
- **Long responses are truncated at a limit.** Case search uses `maxResults` (default 50); comment listings and the comments inside case details use `maxComments` (default 50); KCS search uses `rows` (capped at 50). The comments API has no count parameter, so that one is truncated after the response arrives.
- **Caveats about a response go in `notes`.** Results were truncated, `maxResults` was clamped, more pages remain, the API did not apply some field — all of these are stated explicitly. None of them are dropped silently.

### What `rhcase_create` returns

The create API returns `201` and `{"location": ["..."]}` (in some environments only a `Location` header, or both). This server **looks at both the header and the body, de-duplicates, and returns every location the API gave**. If the API returned neither (for example `200` with an empty body), the `location` field is omitted from the output entirely and `notes` instead says that the creation was accepted but there is no location, so you should check with `rhcase_search` before creating it again.

The case number is placed in `caseNumber` only when it could be read from the last path segment of `location`. The specification does not guarantee the shape of the location, so when it cannot be read the field is left empty and `notes` says so along with the next step. **A number that does not exist is never guessed.**

```json
{
  "caseNumber": "04400001",
  "location": ["https://api.access.redhat.com/support/v3/cases/04400001"],
  "httpStatus": 201,
  "notes": ["The case was created. Use this caseNumber for follow-up calls."]
}
```

### What `rhcase_update` returns

The update API returns `200` and `{"message": ..., "fieldsNotSentToSalesforce": ...}`. `fieldsNotSentToSalesforce` lists fields that were accepted as payload but not forwarded to the backend. When it is non-empty the update was only partially applied, so `notes` carries a warning.

### What `rhkcs_search` returns

Rather than passing the search backend's response through, it is **projected**. Passed through raw, one document is about 10 KB (9.4 KB of which is `abstract`), which makes a single call with the default `rows` 62 KB. Details and measurements are in [limitation 2](limitations.md#2-the-shape-of-the-kcs-search-response-is-not-specified).

```json
{
  "numFound": 37134,
  "numFoundExact": true,
  "start": 0,
  "returned": 1,
  "documents": [
    {
      "id": "7890",
      "title": "...",
      "documentKind": "Solution",
      "language": "en",
      "lastModifiedDate": "2026-09-01T00:00:00Z",
      "viewUri": "https://access.redhat.com/solutions/7890",
      "snippet": "...",
      "snippetTruncated": true
    }
  ],
  "notes": ["..."]
}
```

- `snippet` is `abstract` truncated to **300 Unicode characters**. `snippetTruncated` is only present when truncation happened.
- `viewUri` is the API's `view_uri` passed through **verbatim**. Whether it opens is not something this server checks.
- `numFoundExact` is the backend's own claim. When it is `false`, `numFound` is approximate and `notes` says so.
- If the response is not in the known Solr shape, `documents` comes back empty and `unrecognizedShape` is returned instead. That contains only `topLevelKeys` / `sizeBytes` / a description — **never any of the content**.

---
