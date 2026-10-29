# Registering with MCP clients

**English** · [日本語](ja/clients.md)

[Get started](../README.md) · [Documentation index](README.md)

## Connecting from an MCP client

The server's MCP implementation does not depend on the client. Only the configuration format differs.

| Client | Registration examples (stdio / HTTP) | How to check the connection |
|---|---|---|
| Claude Code | [This page](#connecting-over-stdio) | `/mcp` |
| Codex (CLI / IDE) | [Codex](codex.md) | `/mcp`, `codex mcp get rh-sc` |
| Cursor | [Cursor](cursor.md) | Customize → MCPs |
| OpenCode | [OpenCode](opencode.md) | `opencode mcp list` |

Registration also differs by transport.

| | stdio (default) | Streamable HTTP |
|---|---|---|
| Starting the server | The client starts it | An operator runs it continuously |
| Client configuration | `command` + `args` | `type: http` + `url` + `headers` |
| Authentication | None (the OS controls access) | A shared bearer token is required |
| Offline token | Needed on the client side | **Server side only.** The client never holds it |

Over HTTP the client only holds the shared bearer token, and the Red Hat offline token stays on the server. That is the advantage of the HTTP setup.

### Connecting over stdio

#### Claude Code

If you use the default configuration path, `--config` is not needed. Register **the absolute path of the binary placed by go install** with Claude Code.

```bash
GO_BIN_PATH="$(go env GOBIN)"
# GOPATH can hold several elements; go install writes to the first one, so take only that
if [ -z "$GO_BIN_PATH" ]; then GO_BIN_PATH="$(go env GOPATH | cut -d: -f1)/bin"; fi
claude mcp add --scope user rh-sc -- "$GO_BIN_PATH/rh-sc-mcp"
```

`--scope user` registers the server **for use from every project**. It is the same scope `install.sh` uses, and it is the recommended default. **`claude mcp add` itself defaults to `--scope local`**, so omitting `--scope` registers the server only in the directory you ran it in. Substitute `--scope local` only if you want to try it in this one project.

If you previously registered `rh-sc` at a different path such as `bin/rh-sc-mcp`, remove the old entry with `claude mcp remove rh-sc -s user` before running the command above. If you once registered it at local scope, run `claude mcp remove rh-sc -s local` **from the directory where you registered it** (each scope is a separate registry, so a user-scope entry does not replace the local one). After registering, `claude mcp get rh-sc` should show `Scope` as `User config` and `Command` as Go's install location (checking the connection state additionally requires the offline token).

#### Writing the configuration file directly

```json
{
  "mcpServers": {
    "rh-sc": {
      "command": "/absolute/path/to/GOBIN-or-GOPATH/bin/rh-sc-mcp",
      "args": []
    }
  }
}
```

Replace `command` with the real absolute path that `"$GO_BIN_PATH/rh-sc-mcp"` expands to. Writing the shell variable itself into JSON does not expand it the way a shell would. `RH_SC_OFFLINE_TOKEN` comes from the environment that starts Claude Code.

You can also put the offline token on the server side instead (`offlineToken:`). Do not write the real token value into an MCP client's configuration file — those files are usually readable by the agent.

To keep the configuration file elsewhere, add `--config /path/to/config.yaml`.

Logs go to **stderr**. stdout is the MCP transport, so nothing but protocol frames is ever written there.

### Connecting over HTTP

For how to stand up the server, see [Exposing the server over HTTP](http.md#exposing-the-server-over-http). This section is about the client side.

#### Recommended: write `.mcp.json` and pass the token via an environment variable

Put the following in the project's `.mcp.json`. **The real token value does not go here** — reference an environment variable with the `${VAR}` form (an expansion feature documented in the [Claude Code MCP docs](https://code.claude.com/docs/en/mcp)).

```json
{
  "mcpServers": {
    "rh-sc-http": {
      "type": "http",
      "url": "https://mcp.example.com/mcp",
      "headers": {
        "Authorization": "Bearer ${RH_SC_MCP_HTTP_TOKEN}"
      }
    }
  }
}
```

Put **the same shared token the server uses** into the environment that starts the client. Feeding it from a secret store is best.

```bash
# Example: read it out of a secret store and pass it in
RH_SC_MCP_HTTP_TOKEN="$(your-secret-tool read rh-sc-mcp/httpToken)" claude
```

If you type it by hand, use a **non-echoing read**.

```bash
read -rs RH_SC_MCP_HTTP_TOKEN && export RH_SC_MCP_HTTP_TOKEN
claude
```

Avoid typing `export RH_SC_MCP_HTTP_TOKEN="<real value>"` directly. **That command itself lands in your shell history.**

What this approach avoids is **the real value appearing in configuration files and in command arguments**. Because `.mcp.json` contains no real value, it can be tracked in the repository and shared without spreading the secret. That said, **the value does exist as an environment variable of Claude Code**. The secret does not disappear; you have only narrowed it down to one place.

#### Choosing the URL

| Purpose | URL |
|---|---|
| Connect to a server running locally | `http://127.0.0.1:8080/mcp` |
| Connect to a remote server | `https://mcp.example.com/mcp` |

The path is always `/mcp`. **HTTPS is mandatory for remote servers** — the bearer token rides in a header on every request, so plaintext HTTP hands the token to anyone on the path. Plaintext is fine for local `127.0.0.1`, which stays on the loopback interface.

#### Verifying the connection

A project-scope `.mcp.json` **may ask you to trust the workspace and approve the server the first time**. Until it is approved the server is not connected and the status reads "Pending approval". Approval happens in an interactive `claude` session.

**Before approving, check the `url` and where the `Authorization` header will be sent.** `.mcp.json` is a file shared through the repository, so you may not be the one who wrote it. Approving means agreeing to send the shared token to that URL on every request.

```bash
claude mcp get rh-sc-http
```

After approval a health check runs. Note that `claude mcp get` **shows the header exactly as written in the configuration file (`Bearer ${RH_SC_MCP_HTTP_TOKEN}`)** — the expanded value is not printed. This output therefore cannot tell you whether the environment variable is set correctly. To confirm the connection is actually working, run `/mcp` inside a `claude` session after approval and check that the server and its tools are visible.

#### Why not `--header`

The official documentation also shows a CLI form.

```bash
# Not recommended
claude mcp add --transport http rh-sc-http https://mcp.example.com/mcp \
  --header "Authorization: Bearer <token>"
```

In this form **the real token value rides in the command arguments**. While the command runs it is visible to other processes on the same machine through `ps` or a process listing, and the real value is also written into the target configuration file. If you typed the real value, the command line ends up in your shell history too.

With `.mcp.json` plus an environment variable reference, **you avoid the real value in both configuration files and command arguments**. The value still exists as an environment variable of Claude Code — it is not "gone from everywhere". How you populate that environment variable needs its own care, as [described above](#recommended-write-mcpjson-and-pass-the-token-via-an-environment-variable).

For the same reason, **do not write the token directly into a tracked file**. Putting a real value in `.mcp.json` instead of `${VAR}` means committing it.

#### What has been verified

In this environment (Claude Code 2.1.310) the following was confirmed:

- The `type: http` / `url` / `headers` syntax in `.mcp.json` is accepted and recognised as an HTTP server
- `claude mcp get` displays the type, URL and headers, and prints `${VAR}` in headers unexpanded
- `claude mcp add --transport http ... --header` exists, and the value rides in the arguments

That `${VAR}` is actually expanded at connection time **rests on the official documentation** and was not confirmed here. Project-scope approval requires an interactive session, and a non-interactive run could not reach the point of connecting.

---
