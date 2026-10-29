# Uninstalling

**English** · [日本語](ja/uninstall.md)

[Get started](../README.md) · [Documentation index](README.md)

The `make uninstall-*` commands below are run from the root directory of a checkout of this repository.

## Uninstalling

**There is no single command that removes everything.** The four ways of running this leave different things in different places, and some of those are shared with other software. Rather than guessing which one you mean, they are split per surface.

```bash
make uninstall-stdio                                      # the "rh-sc" registration in Claude Code
make uninstall-http          PORT=8080                    # the "rh-sc-http" registration (+ reports who holds the port)
make uninstall-binary        CONFIRM=<path>               # the go install-ed binary
make uninstall-podman        CONTAINER=rh-sc-mcp          # the named container / image
make uninstall-openshift     NAMESPACE=rh-sc-mcp          # this app's resources inside the namespace
make uninstall-credentials   CONFIRM=<dir>                # the tokens in ~/.config/rh-sc-mcp
```

### What is never removed unless you name it

| Target | Why |
|---|---|
| The tokens in `~/.config/rh-sc-mcp` | Reissuing an offline token means a trip to the Red Hat portal. Uninstalling is not a reason to lose it |
| The OpenShift namespace | It is usually older than this app and contains other things |
| The `go install`-ed binary | stdio and HTTP may point at the same single file, so removing it for one breaks the other |
| Running processes | Whoever holds a port has an owner and a restart policy this script cannot see |

### Where each one draws the line

**stdio / HTTP remove the Claude Code registration.** stdio is `rh-sc`, HTTP is `rh-sc-http`. Scopes are separate registries, so if `SCOPE` (default `user`) disagrees with the actual scope, the command **stops without doing anything**. Project and local scope registrations are tied to the directory you are in, so the cwd is printed too. If you pass `URL`, it only proceeds when the registered URL matches exactly.

The contents of a registration are **not displayed**. An HTTP registration can carry an `Authorization` header, and a URL can hold credentials in its query. Only the name, the scope and the cwd are printed. To look inside, run `claude mcp get <name>` yourself.

**The HTTP server process is not stopped.** Passing `PORT` prints who holds it, but nothing is killed. How you stop it differs depending on whether it is a shell job or a launchd / systemd unit, and a supervised process comes straight back if you kill it. Work out the owner and stop it yourself. Omitting `PORT` still deregisters the client entry — a remote HTTP server has no local port.

**Podman only touches what you name.** Neither `CONTAINER` nor `IMAGE` is guessed, and no patterns are used. A form like `podman rmi $(podman images -q rh-sc-mcp)` would sweep up someone else's similarly named image.

**OpenShift only touches this app's resources in the given namespace.** It removes the `deployment` / `service` / `route` / the generated `configmap/rh-sc-mcp-<hash>` / `secret/rh-sc-mcp`. If listing the generated ConfigMaps fails, it **stops without deleting anything** — mistaking a permissions error for "nothing to do" produces the worst outcome, which looks like success while leaving things behind.

`secret/rh-sc-mcp` **is deleted by default**, because the worse default is the workload disappearing while credentials linger in a namespace nobody is watching. The original lives in `~/.config/rh-sc-mcp`, and `make deploy` writes it back from there. If the Secret belongs to someone else, or you are about to redeploy, keep it with `KEEP_SECRET=1` (a warning is printed on exit when you do).

---
