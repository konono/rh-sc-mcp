#!/usr/bin/env bash
#
# Deploy rh-sc-mcp to an OpenShift cluster.
#
# Subcommands: image | plan | apply | status
#
# Credentials are read from files, never from arguments or the
# environment, and nothing derived from them — value, hash or length —
# is printed. `oc create secret --from-literal` is deliberately not used
# because the value would appear in this process's argv.
#
# Nothing here deletes. `oc apply` without --prune upserts, so a
# resource dropped from the overlay stays in the cluster until somebody
# removes it on purpose.

set -euo pipefail

readonly APP=rh-sc-mcp
readonly MATCH_IMAGE=ghcr.io/konono/rh-sc-mcp   # the `name` key in deploy/base/kustomization.yaml
readonly ROLLOUT_TIMEOUT=180s

repo_root() { cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd; }
# Assigned before `readonly`, so a failing cd is seen. Combined, the
# exit status would be readonly's and REPO would quietly be empty —
# which turns every path below into one rooted at /.
REPO="$(repo_root)" || {
  printf 'rh-sc-mcp-deploy: cannot locate the repository root\n' >&2
  exit 1
}
readonly REPO

# Overridable so tests never touch the operator's real tokens.
CRED_DIR="${RH_SC_MCP_CREDENTIAL_DIR:-$HOME/.config/$APP}"

die() { printf '%s: %s\n' "$APP-deploy" "$*" >&2; exit 1; }
note() { printf '  %s\n' "$*"; }

need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not on PATH"; }

require_var() {
  local name="$1"
  [ -n "${!name:-}" ] || die "$name is required. Example: make deploy NAMESPACE=rh-sc-mcp IMAGE=registry/ns/$APP:v0.3.1"
}

# --- image -----------------------------------------------------------

# split_image sets IMAGE_NAME and IMAGE_TAG. The tag is mandatory: a
# rollout has to name an exact image, and `latest` moves under you.
split_image() {
  local image="$1" last_segment
  last_segment="${image##*/}"
  case "$last_segment" in
    *:*) ;;
    *) die "IMAGE must carry an explicit tag, e.g. registry/ns/$APP:v0.3.1 (got '$image')" ;;
  esac
  IMAGE_TAG="${last_segment##*:}"
  IMAGE_NAME="${image%:*}"
  [ -n "$IMAGE_TAG" ] || die "IMAGE has an empty tag: '$image'"
  [ "$IMAGE_TAG" != "latest" ] || die "IMAGE must not be tagged 'latest': a rollout needs a tag that cannot move"
}

# cluster_arches prints the distinct node architectures, one per line.
cluster_arches() {
  oc get nodes -o 'jsonpath={range .items[*]}{.status.nodeInfo.architecture}{"\n"}{end}' 2>/dev/null |
    grep -v '^$' | sort -u
}

# check_arch_against_cluster fails when ARCH is not one the nodes run.
# Skipped, with a note, when the cluster cannot be reached — this is a
# cross-check, not the primary guard. The primary guard is that ARCH has
# to be stated explicitly.
check_arch_against_cluster() {
  local arch="$1" arches
  if ! oc whoami >/dev/null 2>&1; then
    note "not logged in, so ARCH was not cross-checked against the nodes"
    return 0
  fi
  arches="$(cluster_arches)" || true
  if [ -z "$arches" ]; then
    note "could not read node architectures, so ARCH was not cross-checked"
    return 0
  fi
  if ! printf '%s\n' "$arches" | grep -qx "$arch"; then
    die "ARCH=$arch is not an architecture any node runs ($(printf '%s' "$arches" | tr '\n' ' ')).
An image built for the wrong architecture starts and dies with 'Exec format error'."
  fi
  note "ARCH=$arch matches the cluster nodes"
}

cmd_image() {
  need podman
  require_var IMAGE
  require_var ARCH
  # IMAGE and ARCH come from the environment — a make variable, or the
  # caller's shell. require_var above is what guarantees they are set.
  # shellcheck disable=SC2153
  split_image "$IMAGE"

  local build_arch
  case "$(uname -m)" in
    arm64|aarch64) build_arch=arm64 ;;
    x86_64|amd64) build_arch=amd64 ;;
    *) die "unrecognised build machine architecture: $(uname -m)" ;;
  esac

  # shellcheck disable=SC2153  # ARCH is an input; require_var checked it
  check_arch_against_cluster "$ARCH"

  printf 'Building %s for linux/%s on linux/%s\n' "$IMAGE" "$ARCH" "$build_arch"
  podman build \
    --platform "linux/$ARCH" \
    --build-arg "BUILDPLATFORM=linux/$build_arch" \
    --build-arg TARGETOS=linux \
    --build-arg "TARGETARCH=$ARCH" \
    -t "$IMAGE" \
    "$REPO"

  # Verify rather than trust the flags. Getting this wrong produces a
  # pod that crashes with 'Exec format error' and no other clue.
  local got
  got="$(podman image inspect "$IMAGE" --format '{{.Os}}/{{.Architecture}}')"
  [ "$got" = "linux/$ARCH" ] ||
    die "built image reports $got, expected linux/$ARCH"
  note "image reports $got"

  printf 'Pushing %s\n' "$IMAGE"
  podman push "$IMAGE" || die "push failed.
This script does not log in to registries. Authenticate first, e.g.
  oc registry login
  podman login <registry>
then re-run."
  note "pushed"
}

# --- credentials -----------------------------------------------------

# normalise_token writes the token at $1 to $2 with trailing CR and LF
# removed, and refuses anything else.
#
# Only CR and LF are stripped, and only from the end. A token with a
# space in it is a file holding something other than a token, and
# trimming that quietly would turn a visible problem into a 401 nobody
# can explain. Done in python so the value never lands in a shell
# variable or in any process's arguments.
normalise_token() {
  local src="$1" dst="$2" label="$3" status=0
  python3 -c '
import sys
data = sys.stdin.buffer.read()
value = data.rstrip(b"\r\n")
if not value:
    sys.exit(3)
if any(b in value for b in b" \t\r\n"):
    sys.exit(4)
sys.stdout.buffer.write(value)
' <"$src" >"$dst" || status=$?
  case "$status" in
    0) ;;
    3) die "$label is empty: $src" ;;
    4) die "$label contains whitespace inside the value: $src
Only trailing CR/LF is removed. Anything else is left alone on purpose,
because a token with a space in it is not a token." ;;
    *) die "could not read $label from $src" ;;
  esac
}

require_mode_600() {
  local path="$1" mode
  mode="$(stat -f '%Lp' "$path" 2>/dev/null || stat -c '%a' "$path")"
  [ "$mode" = "600" ] ||
    die "$path has mode $mode, expected 600. Fix it deliberately:
  chmod 600 $path"
}

# stage_credentials fills $STAGE with the two Secret keys.
stage_credentials() {
  local offline="$CRED_DIR/token" shared="$CRED_DIR/http-token"

  [ -d "$CRED_DIR" ] || die "credential directory not found: $CRED_DIR
Create it and put the Red Hat offline token in $offline:
  mkdir -p $CRED_DIR && chmod 700 $CRED_DIR
  install -m 600 /dev/null $offline   # then write the token into it
Set RH_SC_MCP_CREDENTIAL_DIR to use a different directory."
  [ -f "$offline" ] || die "offline token not found: $offline
This script does not create it: it is a live Red Hat credential."
  require_mode_600 "$offline"

  # The shared bearer token is this deployment's own secret, so it is
  # generated here if absent — and kept, because regenerating it on
  # every deploy would 401 every client that already has it.
  if [ ! -f "$shared" ]; then
    need openssl
    ( umask 077; openssl rand -hex 32 >"$shared" )
    note "generated $shared"
  fi
  require_mode_600 "$shared"

  normalise_token "$offline" "$STAGE/offlineToken" "offline token"
  normalise_token "$shared" "$STAGE/httpToken" "shared HTTP token"

  # The server refuses to start when these match; catch it here instead
  # of in a CrashLoopBackOff. cmp reports difference by status alone.
  if cmp -s "$STAGE/offlineToken" "$STAGE/httpToken"; then
    die "the offline token and the shared HTTP token are the same value.
They must differ: a client holding the offline token could reach the
support API directly, outside this server's scope."
  fi
}

# --- rendering -------------------------------------------------------

# A transient overlay that points at the real one and swaps the image.
#
# It lives inside the repository because kustomize rejects an absolute
# path in `resources` ("new root ... cannot be absolute"), and a relative
# path reaching out of the repository needs --load-restrictor
# LoadRestrictionsNone. Staying in the tree keeps the default
# restriction on. Measured both ways; this is the one that renders
# without relaxing anything.
# Removes only what this run created. .deploy-tmp is shared: a
# concurrent plan or apply has its own directory under it, and rm -rf on
# the parent would take theirs too. The parent goes only when empty,
# which rmdir enforces for us.
cleanup() {
  [ -n "${WORKDIR:-}" ] && rm -rf "$WORKDIR"
  [ -n "${STAGE:-}" ] && rm -rf "$STAGE"
  rmdir "$REPO/.deploy-tmp" 2>/dev/null || true
  return 0
}

# A directory this run owns. Not plan.$$ — a pid comes round again, and
# the second run would adopt the first one's leftovers and then delete
# them on the way out.
new_workdir() {
  mkdir -p "$REPO/.deploy-tmp"
  local dir
  dir="$(mktemp -d "$REPO/.deploy-tmp/$1.XXXXXX")"
  # mktemp -d is already 700; said out loud because the rendered
  # manifests go in here, and this directory sits in a shared tree
  # rather than under TMPDIR.
  chmod 700 "$dir"
  printf '%s\n' "$dir"
}

make_overlay() {
  local dir="$1"
  mkdir -p "$dir"
  cat >"$dir/kustomization.yaml" <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

resources:
  - ../../deploy/overlays/openshift

images:
  - name: $MATCH_IMAGE
    newName: $IMAGE_NAME
    newTag: $IMAGE_TAG
EOF
}

# deploy/base/config.yaml becomes a ConfigMap, and a ConfigMap is
# readable by whoever holds read on ConfigMaps there — which is a wider
# and quieter audience than the Secret's. Two keys in the config schema
# hold credentials — offlineToken at the top level and token under
# http — so neither may appear there with a value.
#
# This runs before plan and before apply. It is also what makes the
# rendered output safe to summarise: the credentials in play are the
# Secret's, and the Secret is not part of the overlay.
# What this catches, and what it does not. PyYAML is not present, so
# this is a line matcher rather than a parse:
#
#   caught    offlineToken: x / token: x / "token": x / 'token': x
#             at any indentation, with or without surrounding quotes,
#             including block scalars (token: |) and aliases (token: *a)
#   caught    a flow mapping on one line: http: {token: x}
#   not caught  the file rewritten as JSON across several lines
#   not caught  a key spelled with an escape, e.g. "token"
#
# The gaps need someone to go out of their way, and this is a guard
# against a credential pasted into the wrong file, not against a person
# trying to smuggle one past it. The Secret is the supported place; see
# deploy/base/secret.example.yaml.
check_configmap_clean() {
  local cfg="${1:-$REPO/deploy/base/config.yaml}"
  # Comments first: the key names appear in this file's own commentary,
  # and widening the match to find `{token: x}` would otherwise trip on
  # every one of them.
  if sed -e 's/^[[:space:]]*#.*$//' -e 's/[[:space:]]#.*$//' "$cfg" |
      grep -qE '(^|[[:space:]]|\{|,)["'"'"']?(offlineToken|token)["'"'"']?[[:space:]]*:[[:space:]]*[^[:space:],}]'; then
    die "deploy/base/config.yaml sets a credential key (offlineToken or http.token).
That file becomes a ConfigMap, readable by anyone with read access to
the namespace. Remove the key: credentials belong in the Secret this
script builds from $CRED_DIR."
  fi
}

show_target() {
  printf 'Cluster:   %s\n' "$(oc whoami --show-server)"
  printf 'User:      %s\n' "$(oc whoami)"
  printf 'Namespace: %s\n' "$NAMESPACE"
  printf 'Image:     %s\n' "$IMAGE"
}

cmd_plan() {
  need oc; need python3
  require_var NAMESPACE; require_var IMAGE
  split_image "$IMAGE"
  check_configmap_clean
  oc whoami >/dev/null 2>&1 || die "not logged in to a cluster (oc whoami failed)"
  show_target
  printf '\n'

  # Names only, never the rendered manifests. The ConfigMap is built
  # from deploy/base/config.yaml, so printing the render would print
  # whatever is in that file — including a credential somebody added to
  # it. check_configmap_clean above rejects the known credential keys,
  # but a summary that cannot leak is better than a check that has to
  # be exhaustive.
  WORKDIR="$(new_workdir plan)"
  trap cleanup EXIT
  make_overlay "$WORKDIR"
  local work="$WORKDIR"
  printf 'Resources that would be applied to %s:\n' "$NAMESPACE"
  oc kustomize "$work" | oc apply -n "$NAMESPACE" --dry-run=client -f - -o name | sed 's/^/  /'

  printf '\nSecret/%s would be upserted with keys: offlineToken, httpToken\n' "$APP"
  printf '  sources: %s/token, %s/http-token\n' "$CRED_DIR" "$CRED_DIR"
  printf '  no value, hash or length of either is printed, compared or stored\n'
  printf '\nNothing is deleted: apply runs without --prune.\n'
}

cmd_apply() {
  need oc; need python3
  require_var NAMESPACE; require_var IMAGE
  split_image "$IMAGE"
  check_configmap_clean
  oc whoami >/dev/null 2>&1 || die "not logged in to a cluster (oc whoami failed)"
  show_target
  printf '\n'

  STAGE="$(mktemp -d "${TMPDIR:-/tmp}/$APP-secret.XXXXXX")"
  WORKDIR="$(new_workdir apply)"
  trap cleanup EXIT
  chmod 700 "$STAGE"

  # What can be checked locally is checked before the cluster is
  # touched: a missing token, the wrong file mode, whitespace inside a
  # value, a kustomize error. Each of those used to abort after the
  # namespace had been created, leaving one behind for no reason.
  #
  # It does not make the whole command atomic. Once the namespace is
  # created below, a Secret or an apply can still fail on RBAC or the
  # network, and then an empty namespace does remain. Re-running is
  # safe; the operations after this point are upserts.
  stage_credentials
  make_overlay "$WORKDIR"
  ( umask 077; oc kustomize "$WORKDIR" >"$WORKDIR/rendered.yaml" ) ||
    die "kustomize could not render the overlay; nothing was applied"
  note "credentials and manifests check out locally"

  # ---- from here on the cluster is being changed ----

  if oc get namespace "$NAMESPACE" >/dev/null 2>&1; then
    note "namespace $NAMESPACE exists, using it as is"
  else
    oc create namespace "$NAMESPACE" >/dev/null
    note "created namespace $NAMESPACE"
  fi

  # --from-file, not --from-literal: a literal would put the token in
  # this process's argv. --dry-run | apply makes it an upsert.
  oc create secret generic "$APP" \
    --from-file="offlineToken=$STAGE/offlineToken" \
    --from-file="httpToken=$STAGE/httpToken" \
    --dry-run=client -o yaml |
    oc apply -n "$NAMESPACE" -f - >/dev/null
  note "upserted Secret/$APP (both keys taken from $CRED_DIR)"

  oc apply -n "$NAMESPACE" -f "$WORKDIR/rendered.yaml"

  # The ConfigMap is hashed into its name, so a scope change rolls the
  # pods by itself. The Secret has a fixed name, so a changed token
  # leaves the running pods on the old value until they restart.
  oc rollout restart "deployment/$APP" -n "$NAMESPACE" >/dev/null
  note "restarted deployment/$APP so the pods pick up the Secret"

  oc rollout status "deployment/$APP" -n "$NAMESPACE" --timeout="$ROLLOUT_TIMEOUT" || die "rollout did not complete within $ROLLOUT_TIMEOUT. Nothing was rolled back. To look:
  oc get pods -n $NAMESPACE
  oc describe deployment/$APP -n $NAMESPACE
  oc logs deployment/$APP -n $NAMESPACE"

  printf '\n'
  cmd_status
}

cmd_status() {
  need oc
  require_var NAMESPACE
  oc rollout status "deployment/$APP" -n "$NAMESPACE" --timeout=10s || true

  # NotFound is separated from every other failure. Swallowing both
  # into "no Route yet" would answer a permissions problem with a
  # sentence that sounds like the deploy is simply still in progress.
  #
  # cmd_apply already owns the EXIT trap, so this cleans up its own
  # scratch file inline rather than installing a second one.
  local errfile host probe status=0
  errfile="$(mktemp "${TMPDIR:-/tmp}/$APP-status.XXXXXX")"
  host="$(oc get route "$APP" -n "$NAMESPACE" -o jsonpath='{.spec.host}' 2>"$errfile")" || status=$?
  probe="$(cat "$errfile" 2>/dev/null || true)"
  rm -f "$errfile"

  if [ "$status" -eq 0 ]; then
    if [ -n "$host" ]; then
      printf 'MCP endpoint: https://%s/mcp\n' "$host"
      printf '  Clients must send the shared bearer token.\n'
      printf '  The router certificate has not been validated against a client trust\n'
      printf '  store here; see limitation 7 in README.md before publishing this URL.\n'
    else
      printf 'Route %s exists in %s but has no host yet.\n' "$APP" "$NAMESPACE"
    fi
    return 0
  fi

  case "$probe" in
    *NotFound*|*"not found"*) printf 'No Route named %s in %s yet.\n' "$APP" "$NAMESPACE" ;;
    *) die "could not read route/$APP in $NAMESPACE:
  $probe" ;;
  esac
}

main() {
  local sub="${1:-}"
  case "$sub" in
    image) cmd_image ;;
    plan) cmd_plan ;;
    apply) cmd_apply ;;
    status) cmd_status ;;
    *) die "usage: $0 {image|plan|apply|status}
  image   build and push (needs IMAGE, ARCH)
  plan    show what would be applied (needs NAMESPACE, IMAGE)
  apply   namespace, Secret, manifests, rollout (needs NAMESPACE, IMAGE)
  status  rollout state and Route URL (needs NAMESPACE)" ;;
  esac
}

# Guarded so the test can source this file and exercise the pieces
# directly — normalisation and the image rules are worth testing without
# a cluster in the way.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
