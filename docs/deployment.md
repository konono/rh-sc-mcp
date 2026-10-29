# Containers and OpenShift / Kubernetes

**English** · [日本語](ja/deployment.md)

[Get started](../README.md) · [Documentation index](README.md)

The `make` and `oc apply -k` examples on this page are run from the root directory of a checkout of this repository.

## Containers and Kubernetes / OpenShift

The published image is `ghcr.io/konono/rh-sc-mcp:latest`. It contains both linux/amd64 and linux/arm64; an unauthenticated pull and `--version` on the arm64 image have been verified. **When deploying to a cluster, look up the latest release tag with a command and pin to that version.** That way you never forget to update a version number written into the documentation, and you never unintentionally follow a moving tag. Existing Pods are not replaced automatically when `latest` moves, so updating requires a redeploy. [The package page](https://github.com/users/konono/packages/container/package/rh-sc-mcp) is public.

```bash
podman pull ghcr.io/konono/rh-sc-mcp:latest
```

The image contains no credentials and no configuration file. At startup you mount a configuration file with `--config` and pass the Red Hat offline token and the separate shared HTTP token from a Secret as environment variables. On OpenShift, `make deploy` (below) creates that Secret for you.

### Changing the image

If you use kustomize by hand, pin the tag under `images:` in `deploy/base/kustomization.yaml`. **Do not change `name` there.** `name` is the match key for "which image to replace" and must equal the value written in `deployment.yaml`. Change it and the match fails, so **kustomize substitutes nothing and emits the original value** (with no error and no warning). The `v0.5.0` below is an example; check the real latest tag with `gh release view --repo konono/rh-sc-mcp --json tagName --jq .tagName`.

For the official image, change only `newTag`. If you mirror it to another registry, change `newName` as well.

```yaml
images:
  - name: ghcr.io/konono/rh-sc-mcp              # match key; leave it alone
    newTag: v0.5.0
```

Look at the substituted image before applying.

```bash
kubectl kustomize deploy/overlays/kubernetes | grep 'image:'
kubectl kustomize deploy/overlays/openshift  | grep 'image:'
# → image: ghcr.io/konono/rh-sc-mcp:v0.5.0
```

If it does not change to the tag you specified, check whether you edited `name` and broke the match.

### Credentials and scope

**On OpenShift, [`make deploy`](#deploying-with-make) creates the Secret too.** It reads the token files under `~/.config/rh-sc-mcp`, strips newlines and passes them with `--from-file`, so you do not need to do the following by hand.

If you use the Kubernetes overlay, or want to create just the Secret first, do this. **Do not use `--from-literal`** — the value rides in `kubectl`'s `argv`, where anyone on the same host can read it with `ps`.

```bash
# 1. Credentials. The offline token and the shared HTTP token must be different values.
#    Pass them via files. Strip trailing newlines, which would otherwise end up in the Secret.
mkdir -p ~/.config/rh-sc-mcp && chmod 700 ~/.config/rh-sc-mcp
install -m 600 /dev/null ~/.config/rh-sc-mcp/token
$EDITOR ~/.config/rh-sc-mcp/token          # paste the offline token
( umask 077; openssl rand -hex 32 | tr -d '\n' > ~/.config/rh-sc-mcp/http-token )

# 2. Create the Secret, going through a temp file with the trailing newline stripped.
#    It is wrapped in a subshell so the temp file is removed even if kubectl fails.
(
  umask 077                                  # dir 700 / file 600
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT                  # removed on success and on failure
  printf '%s' "$(cat ~/.config/rh-sc-mcp/token)" > "$tmp/offlineToken"
  kubectl create secret generic rh-sc-mcp \
    --from-file=offlineToken="$tmp/offlineToken" \
    --from-file=httpToken="$HOME/.config/rh-sc-mcp/http-token"
)

# 3. Scope. Review readOnly and resources.
$EDITOR deploy/base/config.yaml
```

The server does not trim environment variables, so **a leftover newline in a value** makes SSO return `invalid_grant` for the offline token, and makes the bearer token fail comparison so that every request gets a `401`. These failures are hard to diagnose, so always make the files you pass to `--from-file` newline-free.

`deploy/base/secret.example.yaml` is a template containing `REPLACE_ME`. Files with real values are already in `.gitignore` (`deploy/**/secret.yaml` and friends), so only the sample is tracked.

### Reviewing before applying

```bash
kubectl kustomize deploy/overlays/kubernetes
kubectl kustomize deploy/overlays/openshift
```

### OpenShift

The `Route` omits `host`, so the cluster generates one, and `termination: edge` uses the router's default certificate. No further editing is needed. Whether an MCP client can trust that default certificate is a separate question, and during testing it could not be validated against a local trust store ([limitation 7](limitations.md#7-containers-verified-on-podman-openshift-and-the-published-image)).

```bash
oc apply -k deploy/overlays/openshift
```

That is the step-by-step form. If you build, apply and check in the same sequence every time, use the `make` targets below instead. The stages are identical, with one command each.

### Deploying with `make`

The Makefile calls `scripts/deploy-openshift.sh`. `NAMESPACE` and `IMAGE` have no defaults: both name a write target, and picking a default would mean choosing one on your behalf.

```bash
# 1. Get the latest release tag (requires the gh CLI)
TAG="$(gh release view --repo konono/rh-sc-mcp --json tagName --jq .tagName)"
IMAGE="ghcr.io/konono/rh-sc-mcp:$TAG"

# 2. See what would be applied (does not change the cluster)
make deploy-plan NAMESPACE=rh-sc-mcp IMAGE="$IMAGE"

# 3. Apply (no build or push needed)
make deploy NAMESPACE=rh-sc-mcp IMAGE="$IMAGE"

# 4. Status and Route URL
make deploy-status NAMESPACE=rh-sc-mcp
```

Only if you use your own modified image do you need `make deploy-image IMAGE=<registry>/<name>:<tag> ARCH=<node-arch>` to build and push.

### Verifying the published image on OpenShift

On 2026-10-26, v0.5.0 was deployed to a dedicated namespace and checked. The verification used the base manifests, which create no Route, created the Secret with `--from-file`, and then applied a temporary overlay pinning the image to v0.5.0. `oc rollout status deployment/rh-sc-mcp` confirmed 2 Pods Ready, and `rh-sc-mcp --config /etc/rh-sc-mcp/config.yaml --check-auth` was run inside a Pod. With a production offline token, the SSO exchange and the read-only API probe both succeeded.

HTTP was checked in a separate deployment using a dummy token, through `oc port-forward svc/rh-sc-mcp 18080:80`. **The Service port is 80** (the container listens on 8080). `/healthz` and `/readyz` returned 200, unauthenticated `/mcp` returned 401, and an authenticated initialize returned 200. The namespaces and Secrets from both runs have been deleted. Access through a Route with the published image, and fetching a real case, remain unverified ([details of what was verified](limitations.md#7-containers-verified-on-podman-openshift-and-the-published-image)).

**Tokens are read from files.** Put the Red Hat offline token in `~/.config/rh-sc-mcp/token` with mode 600. If the shared bearer token (`http-token`) is absent it is generated and saved in the same place, then reused — regenerating it every time would give every configured client a `401`. To use a different directory for testing, override it with `RH_SC_MCP_CREDENTIAL_DIR`.

| Aspect | Handling |
|---|---|
| How values are passed | `--from-file`. With `--from-literal` the value rides in `argv` and is visible via `ps` |
| Trailing newlines | Stripped. The server does not trim environment variables, so leftovers make SSO return `invalid_grant` and make every bearer comparison fail with `401` |
| Whitespace inside a value | **Rejected.** Silently stripping it turns into an unexplainable `401` |
| Output | Neither the value, nor a hash, nor the length is printed |
| ConfigMap | If `deploy/base/config.yaml` contains `offlineToken` or `http.token`, the script stops before applying. Anything written there is visible to whoever can read that ConfigMap |

**Whatever can be checked locally is checked before touching the cluster.** A missing token file, wrong permissions, a newline, whitespace inside a value, and kustomize render errors all surface before the namespace is created.

**Failures after that point cannot be prevented, though.** If Secret creation or `oc apply` fails on RBAC or networking after the namespace exists, **an empty namespace is left behind**. Fix the cause and run it again — the script is built to be safe to repeat.

**Nothing is deleted.** `oc apply` runs without `--prune`. Resources removed from the overlay stay in the cluster, so cleanup is a deliberate manual step. The Secret is overwritten from `~/.config/rh-sc-mcp` on every run, followed by `oc rollout restart` — unlike ConfigMaps, Secret names carry no hash, so without a restart the Pods keep the old values.

### Kubernetes

The `Ingress` **cannot be applied as-is.** Two values only you can supply are placeholders.

```bash
# 3. The hostname (rh-sc-mcp.example.com, in two places)
$EDITOR deploy/overlays/kubernetes/ingress.yaml

# 4. A TLS certificate for that hostname (not needed if cert-manager or similar issues one)
kubectl create secret tls rh-sc-mcp-tls --cert=tls.crt --key=tls.key

kubectl apply -k deploy/overlays/kubernetes
```

**TLS is mandatory.** The bearer token rides in a header on every request, so plaintext HTTP hands the token to anyone on the path.

### What is in the manifests

`deploy/base` holds a Deployment and a Service, plus a ConfigMap generated from `deploy/base/config.yaml`. Credentials live in a Secret, so the scope can be reviewed in git while the token stays out of it. kustomize hashes the ConfigMap name, so changing the scope replaces the Pods (the scope is read once at startup, so without that the edit would not take effect).

The manifests run the container as non-root, with a read-only root filesystem, all capabilities dropped and the `RuntimeDefault` seccomp profile. The image declares `USER 65532:65532` (because `runAsNonRoot` in the manifests requires a numeric UID). OpenShift's `restricted-v2` SCC, meanwhile, assigns an arbitrary UID from the namespace's range and ignores the image's `USER`.

**To stay independent of the assigned UID, nothing depends on a specific UID or on file ownership** — the binary is placed where anyone can read it, and **in this configuration the server only reads the mounted configuration via `--config` and writes no files at startup**. The manifests also avoid pinning `runAsUser`, so as not to interfere with the SCC's assignment. This is not just design intent: **running under an arbitrarily chosen UID of 1000650000 with a read-only root filesystem and all capabilities dropped was verified with Podman**, and **on a real OpenShift cluster the Pod reached Ready under UID 1000980000 as assigned by `restricted-v2`** ([limitation 7](limitations.md#7-containers-verified-on-podman-openshift-and-the-published-image)). Those are the two UIDs that were checked, not every possible one.

Writes are absent because `--config` is passed. Omit it and the server tries to create the default configuration file, writing into `$HOME`. The manifests always pass it via `args`, so this does not apply there; if you replace `CMD`, see [limitation 7](limitations.md#7-containers-verified-on-podman-openshift-and-the-published-image).

The OpenShift overlay's `Route` carries a 300-second HAProxy timeout, because a streamed MCP response stays open and the router's 30-second default would cut it. The Kubernetes overlay uses an `Ingress` instead. Both expose only `/mcp`, keeping the probes inside the cluster.

---
