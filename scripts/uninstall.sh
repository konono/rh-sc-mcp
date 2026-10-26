#!/usr/bin/env bash
#
# Remove what an install left behind, one surface at a time.
#
# There is no "uninstall everything". The four ways this server gets
# deployed leave different things in different places, several of them
# shared with software that has nothing to do with this project, and one
# command would have to guess which you meant. Nothing here guesses:
# every target is named, and anything ambiguous is reported instead of
# acted on.
#
# Never removed unless you ask for it by name:
#
#   - the tokens in ~/.config/rh-sc-mcp. A Red Hat offline token means a
#     trip to the portal to reissue.
#   - the OpenShift namespace. It is usually older than this app and
#     usually holds other things.
#   - the go-installed binary. stdio and HTTP can both be pointed at the
#     same file, so removing it for one breaks the other.
#   - any process. A listener on a port has an owner and a restart
#     policy this script cannot see.

set -euo pipefail

readonly APP=rh-sc-mcp
readonly STDIO_NAME=rh-sc            # the name install.sh registers
readonly HTTP_NAME=rh-sc-http        # the HTTP client registration
readonly DEFAULT_SCOPE=user          # the scope install.sh uses

CRED_DIR="${RH_SC_MCP_CREDENTIAL_DIR:-$HOME/.config/$APP}"

die() { printf '%s: %s\n' "$APP-uninstall" "$*" >&2; exit 1; }
note() { printf '  %s\n' "$*"; }
need() { command -v "$1" >/dev/null 2>&1 || die "$1 is required but not on PATH"; }

require_var() {
  local name="$1" example="$2"
  [ -n "${!name:-}" ] || die "$name is required. Example: $example"
}

# --- Claude Code registrations ---------------------------------------

# Removes one registration, in one scope.
#
# The scopes are separate registries: user, project and local can each
# hold an entry under the same name, and `claude mcp remove` acts on the
# one you name. Removing "the" registration without saying which is how
# a colleague's project-scope entry disappears while you meant to clear
# your own. So: the scope in use is read back, and a mismatch stops the
# command rather than being resolved in either direction.
remove_registration() {
  local name="$1" scope="$2" expect_url="${3:-}"
  need claude

  local existing
  if ! existing="$(claude mcp get "$name" 2>/dev/null)"; then
    note "Claude Code has no MCP server named \"$name\"; nothing to remove"
    return 0
  fi

  # Never print what `claude mcp get` returns. An HTTP registration can
  # carry an Authorization header, a URL can carry a credential in its
  # query string, and a command line can carry one as an argument — all
  # of which would land in a terminal and a log.
  #
  # Name, scope and cwd are the three things needed to know which entry
  # is about to go, and none of them can hold a secret. Look at the
  # registration yourself with `claude mcp get` if you want the rest.
  printf 'Registered MCP server "%s"\n' "$name"
  printf '  scope: %s\n' "$(printf '%s\n' "$existing" | sed -n 's/^ *Scope: *//p' | head -1)"
  printf '  cwd:   %s\n' "$PWD"
  printf '  (a project- or local-scope entry belongs to this directory)\n\n'

  local actual_scope actual_word
  actual_scope="$(printf '%s\n' "$existing" | sed -n 's/^ *Scope: *//p' | head -1)"
  # The reported scope is wordier than the flag ("User config" against
  # "user"), so compare the leading word.
  actual_word="$(printf '%s' "${actual_scope:-}" | tr '[:upper:]' '[:lower:]' | awk '{print $1}')"
  if [ -n "$actual_word" ] && [ "$actual_word" != "$scope" ]; then
    die "that registration is in the ${actual_word} scope, and SCOPE=$scope was asked for.
Nothing was removed. Name the scope it is actually in:
  SCOPE=$actual_word"
  fi

  # When a URL is given, it has to be the registered one. Compared
  # against the URL field alone and in full: a substring search over
  # the whole output would also match a header value, which is both a
  # false positive and a reason to have been reading headers at all.
  if [ -n "$expect_url" ]; then
    local actual_url
    actual_url="$(printf '%s\n' "$existing" | sed -n 's/^ *URL: *//p' | head -1)"
    if [ -z "$actual_url" ]; then
      die "URL= was given, but no URL field could be read from that registration.
Nothing was removed. It may be a stdio entry, or the output format may
have changed; either way this cannot confirm the match. Re-run without
URL= to remove it as named."
    fi
    if [ "$actual_url" != "$expect_url" ]; then
      die "URL= does not match the registered URL.
Nothing was removed. Neither value is printed here: a URL can carry a
credential in its query string. Check it with:
  claude mcp get $name"
    fi
    note "URL matches"
  fi

  claude mcp remove "$name" --scope "$scope" >/dev/null ||
    die "claude mcp remove failed for \"$name\" in scope $scope"
  note "removed \"$name\" from the $scope scope"
  note "other scopes, and every other server, are untouched"
}

# stdio: the registration install.sh creates, pointing at the binary.
cmd_stdio() {
  remove_registration "${SERVER_NAME:-$STDIO_NAME}" "${SCOPE:-$DEFAULT_SCOPE}"
  printf '\n'
  note "the binary itself is still installed — see: make uninstall-binary"
}

# HTTP: the client registration is the thing this removes. The server
# process is reported, never stopped, and only when PORT says where to
# look — a remote server has no local port, and its registration should
# still come out.
cmd_http() {
  remove_registration "${SERVER_NAME:-$HTTP_NAME}" "${SCOPE:-$DEFAULT_SCOPE}" "${URL:-}"

  if [ -z "${PORT:-}" ]; then
    printf '\n'
    note "no PORT given, so nothing local was inspected"
    note "if a server is running here, pass PORT= to see what holds it"
    return 0
  fi

  need lsof
  local listeners
  listeners="$(lsof -nP -iTCP:"$PORT" -sTCP:LISTEN 2>/dev/null || true)"
  printf '\n'
  if [ -z "$listeners" ]; then
    printf 'Nothing is listening on port %s.\n' "$PORT"
    return 0
  fi

  printf 'Still listening on port %s:\n\n' "$PORT"
  printf '%s\n\n' "$listeners"
  printf 'Not stopping it. Whatever started it decides how it stops:\n'
  printf '  a shell job          stop it in that shell\n'
  printf '  launchd or systemd   disable the unit, or it comes straight back\n'
  printf '  a container          see: make uninstall-podman\n'
  printf '\nOnce you know the owner, stop it yourself:  kill <PID>\n'
}

# --- the installed binary --------------------------------------------

go_install_target() {
  local dir
  dir="$(go env GOBIN)"
  [ -n "$dir" ] || dir="$(go env GOPATH | cut -d: -f1)/bin"
  [ -n "$dir" ] && [ "$dir" != "/bin" ] || die "cannot work out where go install writes"
  printf '%s/%s\n' "$dir" "$APP"
}

# Removes the go-installed binary. Opt-in, and only if it says it is
# ours.
#
# Opt-in because stdio and HTTP can point at the same file: uninstalling
# one transport is not a reason to break the other. The version check is
# separate — GOBIN holds everything else you have ever `go install`ed,
# and it separates "this is the thing being uninstalled" from "this is
# whatever sits at the path we computed".
cmd_binary() {
  need go
  local path
  path="$(go_install_target)"

  if [ ! -e "$path" ]; then
    note "no file at $path; nothing to remove"
    return 0
  fi

  local reported
  if ! reported="$("$path" --version 2>/dev/null)"; then
    die "$path exists but would not run with --version.
Not removing it: this only deletes a binary that identifies itself as
$APP. Remove it by hand if you are sure."
  fi
  case "$reported" in
    "$APP "*) ;;
    *) die "$path reports \"$reported\", which is not $APP. Not removing it." ;;
  esac

  if [ "${CONFIRM:-}" != "$path" ]; then
    printf 'Would remove:\n  %s  (%s)\n\n' "$path" "$reported"
    printf 'Not removing it yet. Both the stdio and the HTTP setup can point at\n'
    printf 'this one file, so deleting it for one breaks the other.\n\n'
    printf 'Name the path back to confirm:\n'
    printf '  make uninstall-binary CONFIRM=%s\n' "$path"
    return 0
  fi

  rm -f "$path"
  note "removed $path ($reported)"
  note "any Claude Code registration still points here — see: make uninstall-stdio"
}

# --- Podman -----------------------------------------------------------

# Removes a container and an image you name. No patterns.
#
# `podman rmi $(podman images -q rh-sc-mcp)` is the shape of command
# that takes out a similarly-named image belonging to someone else, so
# names are required and used literally.
cmd_podman() {
  need podman
  local container="${CONTAINER:-}" image="${IMAGE:-}"

  if [ -z "$container" ] && [ -z "$image" ]; then
    die "name what to remove; neither is guessed:
  make uninstall-podman CONTAINER=rh-sc-mcp
  make uninstall-podman IMAGE=localhost/rh-sc-mcp:v0.3.1
  make uninstall-podman CONTAINER=rh-sc-mcp IMAGE=localhost/rh-sc-mcp:v0.3.1"
  fi

  if [ -n "$container" ]; then
    if podman container exists "$container"; then
      podman rm -f "$container" >/dev/null
      note "removed container $container"
    else
      note "no container named $container"
    fi
  fi

  if [ -n "$image" ]; then
    if podman image exists "$image"; then
      podman rmi "$image" >/dev/null
      note "removed image $image"
    else
      note "no image $image"
    fi
  fi

  note "no other container, image or build cache was touched"
}

# --- OpenShift --------------------------------------------------------

# Generated ConfigMaps carry a content hash in the name, so they cannot
# be named in advance. Matched on the exact generated shape — the app
# name, a dash, kustomize's ten-character hash — rather than on a
# prefix, which would also take out a hand-made rh-sc-mcp-staging.
#
# A failed list is not an empty list. Swallowing an RBAC denial or a
# dropped connection here would report "nothing to delete" and leave
# the ConfigMaps in place, which is the one outcome that looks like
# success and is not.
openshift_generated_configmaps() {
  local raw
  raw="$(oc get configmap -n "$NAMESPACE" -o name)" || return 1
  printf '%s\n' "$raw" |
    sed 's|^configmap/||' |
    grep -E "^$APP-[0-9a-z]{10}$" || true
  return 0
}

cmd_openshift() {
  need oc
  require_var NAMESPACE "make uninstall-openshift NAMESPACE=rh-sc-mcp"
  oc whoami >/dev/null 2>&1 || die "not logged in to a cluster (oc whoami failed)"

  printf 'Cluster:   %s\n' "$(oc whoami --show-server)"
  printf 'Namespace: %s\n\n' "$NAMESPACE"

  # Preflight. The generated ConfigMap names carry a content hash and
  # have to be read from the cluster, so this call has to happen
  # anyway — and doing it first means a denied read, an unreachable
  # API or a missing namespace stops the command before anything has
  # been deleted.
  #
  # There is no separate `oc get namespace` check. It would answer
  # "does not exist" for a Forbidden just as readily as for a real
  # absence, and turning an access problem into "nothing to do" is the
  # failure this whole function is arranged to avoid.
  local cm_list
  if ! cm_list="$(openshift_generated_configmaps)"; then
    die "could not list ConfigMaps in $NAMESPACE, so nothing was deleted.
The namespace may not exist, the API may be unreachable, or this
account may not be allowed to read ConfigMaps there. Fix that and run
this again — repeating it is safe."
  fi

  # delete --ignore-not-found, not get-then-delete: `oc get X >/dev/null
  # 2>&1` reports the same failure for "no such resource" and "you may
  # not look", and the second of those must not read as done. This
  # exits non-zero on a real error and says nothing when there was
  # nothing to remove.
  local kind out
  for kind in "deployment/$APP" "service/$APP" "route/$APP"; do
    out="$(oc delete "$kind" -n "$NAMESPACE" --ignore-not-found=true)" ||
      die "could not delete $kind in $NAMESPACE. Stopped here; what came
before this line is already gone, the rest is not."
    if [ -n "$out" ]; then note "deleted $kind"; else note "no $kind"; fi
  done

  local cm found=0
  for cm in $cm_list; do
    oc delete "configmap/$cm" -n "$NAMESPACE" --ignore-not-found=true >/dev/null ||
      die "could not delete configmap/$cm in $NAMESPACE. Stopped here."
    note "deleted configmap/$cm"
    found=1
  done
  [ "$found" -eq 1 ] || note "no generated configmap/$APP-<hash>"

  # secret/$APP is deleted with the rest of the app, because leaving it
  # is the worse default: the workload goes, and a live Red Hat offline
  # token stays behind in a namespace nobody is watching any more. The
  # original in $CRED_DIR is what survives an uninstall — this is a copy
  # that `make deploy` writes again from it.
  #
  # KEEP_SECRET=1 covers the case where the Secret was not put there by
  # this deployment, or where a redeploy is coming in a minute.
  local secret_kept=0
  if [ "${KEEP_SECRET:-}" = "1" ]; then
    # Only here is a read needed, to decide whether the warning below
    # applies. NotFound is separated from every other failure by hand,
    # because "cannot tell" must not print as "nothing left behind".
    local probe
    if probe="$(oc get "secret/$APP" -n "$NAMESPACE" -o name 2>&1)"; then
      secret_kept=1
      note "kept secret/$APP (KEEP_SECRET=1)"
    else
      case "$probe" in
        *NotFound*|*"not found"*) note "no secret/$APP" ;;
        *) die "KEEP_SECRET=1 was given, but secret/$APP could not be read:
  $probe
Everything else is already deleted. Whether a Secret is still sitting in
$NAMESPACE is unknown from here — check it directly." ;;
      esac
    fi
  else
    out="$(oc delete "secret/$APP" -n "$NAMESPACE" --ignore-not-found=true)" ||
      die "could not delete secret/$APP in $NAMESPACE.
Everything else is deleted. A Secret holding a live Red Hat offline
token may still be there — this could not remove it, so remove it
yourself or fix the access problem and run this again."
    if [ -n "$out" ]; then note "deleted secret/$APP"; else note "no secret/$APP"; fi
  fi

  printf '\n'
  note "namespace $NAMESPACE was not deleted"
  note "the tokens in $CRED_DIR were not touched"
  note "nothing outside deployment/service/route/configmap/secret named $APP was touched"

  if [ "$secret_kept" -eq 1 ]; then
    printf '\n'
    printf '  !! secret/%s is still in %s, and still holds a live\n' "$APP" "$NAMESPACE"
    printf '  !! Red Hat offline token. The workload is gone; the credential is not.\n'
    printf '  !! Anyone who can read Secrets in this namespace can read it.\n'
    printf '  !!\n'
    printf '  !! Delete it:  make uninstall-openshift NAMESPACE=%s\n' "$NAMESPACE"
  fi
}

# --- local credentials ------------------------------------------------

# Deletes the token files. Requires the directory to be named back: the
# offline token is a credential you have to go and reissue.
cmd_credentials() {
  if [ ! -d "$CRED_DIR" ]; then
    note "no credential directory at $CRED_DIR"
    return 0
  fi

  if [ "${CONFIRM:-}" != "$CRED_DIR" ]; then
    printf 'Would delete the token files in:\n  %s\n\n' "$CRED_DIR"
    [ -f "$CRED_DIR/token" ] &&
      printf '  token        the Red Hat offline token — reissuing means a trip to access.redhat.com\n'
    [ -f "$CRED_DIR/http-token" ] &&
      printf '  http-token   the shared bearer token — every configured client would need the new one\n'
    printf '\nNot deleting. Name the directory back to confirm:\n'
    printf '  make uninstall-credentials CONFIRM=%s\n' "$CRED_DIR"
    return 0
  fi

  rm -f "$CRED_DIR/token" "$CRED_DIR/http-token"
  note "removed token and http-token from $CRED_DIR"
  note "the directory itself, and anything else in it, were left alone"
}

main() {
  case "${1:-}" in
    stdio) cmd_stdio ;;
    http) cmd_http ;;
    binary) cmd_binary ;;
    podman) cmd_podman ;;
    openshift) cmd_openshift ;;
    credentials) cmd_credentials ;;
    *) die "usage: $0 {stdio|http|binary|podman|openshift|credentials}

  stdio        remove the \"$STDIO_NAME\" Claude Code registration   [SCOPE=user]
  http         remove the \"$HTTP_NAME\" registration, and report a
               local listener if asked                      [SCOPE=user] [URL=] [PORT=]
  binary       remove the go-installed binary                [CONFIRM=<path>]
  podman       remove a named container and/or image         [CONTAINER=] [IMAGE=]
  openshift    remove this app's resources in a namespace    [NAMESPACE=] [KEEP_SECRET=1]
  credentials  remove the local token files                  [CONFIRM=<dir>]

Not removed unless named: the tokens in $CRED_DIR, the OpenShift
namespace, the shared binary, and any running process." ;;
  esac
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  main "$@"
fi
