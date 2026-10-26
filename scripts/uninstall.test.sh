#!/usr/bin/env bash
#
# Tests for scripts/uninstall.sh.
#
# No cluster, no Claude Code, no real credentials. `claude` and `oc` are
# replaced by stubs on PATH, so the tests drive the script's decisions
# rather than the tools'.
#
# The assertion that matters most is negative: a registration can hold
# an Authorization header and a URL with a credential in its query
# string, and none of that may reach stdout or stderr.

set -uo pipefail

readonly HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT="$HERE/uninstall.sh"

readonly BEARER='Bearer rhscmcptestbearerzzz0123456789'
readonly URL_SECRET='https://mcp.example.test/mcp?access_token=rhscmcptesturlsecretzzz987654321'
readonly BIN_ARG_SECRET='rhscmcptestargsecretzzz55555'

pass=0
fail=0
ok() { printf '  ok   %s\n' "$1"; pass=$((pass + 1)); }
no() { printf '  FAIL %s\n' "$1"; fail=$((fail + 1)); }
check() { if [ "$2" -eq 0 ]; then ok "$1"; else no "$1"; fi; }

WORK="$(mktemp -d "${TMPDIR:-/tmp}/uninstall-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT
BIN="$WORK/bin"
mkdir -p "$BIN"

# --- stubs -----------------------------------------------------------

# `claude mcp get` prints a registration shaped like the real one, with
# a secret in every field a secret could be in.
make_claude_stub() { # make_claude_stub <scope-line> [extra-lines-file]
  cat >"$BIN/claude" <<EOF
#!/usr/bin/env bash
case "\$1 \$2" in
  "mcp get")
    cat <<'REG'
$1
REG
    ;;
  "mcp remove")
    echo "removed" ;;
  *) exit 1 ;;
esac
EOF
  chmod +x "$BIN/claude"
}

no_claude_stub() {
  cat >"$BIN/claude" <<'EOF'
#!/usr/bin/env bash
exit 1
EOF
  chmod +x "$BIN/claude"
}

run() { # run <subcommand> ; env vars come from the caller
  PATH="$BIN:$PATH" bash "$SCRIPT" "$@" >"$WORK/out" 2>&1
}

# --- registrations: nothing sensitive is echoed ----------------------

printf '\nregistration output holds no credential\n'

make_claude_stub "  Scope: User config
  Command: /usr/local/bin/rh-sc-mcp --token $BIN_ARG_SECRET
  URL: $URL_SECRET
  Headers: Authorization: $BEARER
  Type: http"

SCOPE=user run http
check 'the Authorization header never appears' \
  "$( ! grep -qF "$BEARER" "$WORK/out"; echo $? )"
check 'the URL query credential never appears' \
  "$( ! grep -qF "$URL_SECRET" "$WORK/out"; echo $? )"
check 'a secret in the command line never appears' \
  "$( ! grep -qF "$BIN_ARG_SECRET" "$WORK/out"; echo $? )"
check 'the word Authorization never appears' \
  "$( ! grep -qi 'authorization' "$WORK/out"; echo $? )"
check 'the registration is still identified by name and scope' \
  "$( grep -q 'rh-sc-http' "$WORK/out" && grep -qi 'scope' "$WORK/out"; echo $? )"
check 'the removal happens' \
  "$( grep -q 'removed "rh-sc-http" from the user scope' "$WORK/out"; echo $? )"

# --- scope mismatch stops, and does not remove -----------------------

printf '\nscope handling\n'

make_claude_stub "  Scope: Project config
  Command: /usr/local/bin/rh-sc-mcp"

SCOPE=user run stdio
check 'a scope mismatch refuses' \
  "$( grep -q 'is in the project scope' "$WORK/out"; echo $? )"
check 'a scope mismatch removes nothing' \
  "$( ! grep -q 'removed "' "$WORK/out"; echo $? )"
check 'the refusal names the scope to use' \
  "$( grep -q 'SCOPE=project' "$WORK/out"; echo $? )"

make_claude_stub "  Scope: Project config
  Command: /usr/local/bin/rh-sc-mcp"
SCOPE=project run stdio
check 'naming the right scope proceeds' \
  "$( grep -q 'removed "rh-sc" from the project scope' "$WORK/out"; echo $? )"

no_claude_stub
run stdio
check 'a missing registration is not an error' \
  "$( grep -q 'nothing to remove' "$WORK/out"; echo $? )"

# --- URL matching ----------------------------------------------------

printf '\nURL matching\n'

make_claude_stub "  Scope: User config
  URL: $URL_SECRET
  Headers: Authorization: $BEARER"

SCOPE=user URL="$URL_SECRET" run http
check 'an exact URL match proceeds' \
  "$( grep -q 'URL matches' "$WORK/out"; echo $? )"
check 'the matching URL is still not printed' \
  "$( ! grep -qF "$URL_SECRET" "$WORK/out"; echo $? )"

make_claude_stub "  Scope: User config
  URL: $URL_SECRET
  Headers: Authorization: $BEARER"
SCOPE=user URL='https://mcp.example.test/mcp' run http
check 'a URL that only matches as a prefix is rejected' \
  "$( grep -q 'does not match the registered URL' "$WORK/out"; echo $? )"
check 'the rejection removes nothing' \
  "$( ! grep -q 'removed "' "$WORK/out"; echo $? )"
check 'the rejection prints neither URL' \
  "$( ! grep -qF "$URL_SECRET" "$WORK/out"; echo $? )"

make_claude_stub "  Scope: User config
  Command: /usr/local/bin/rh-sc-mcp"
SCOPE=user URL='https://mcp.example.test/mcp' run http
check 'URL= against an entry with no URL field refuses' \
  "$( grep -q 'no URL field could be read' "$WORK/out"; echo $? )"

# --- binary ----------------------------------------------------------

printf '\nbinary\n'

cat >"$BIN/go" <<EOF
#!/usr/bin/env bash
case "\$2" in
  GOBIN) echo "$WORK/gobin" ;;
  GOPATH) echo "$WORK/gopath" ;;
esac
EOF
chmod +x "$BIN/go"
mkdir -p "$WORK/gobin"

cat >"$WORK/gobin/rh-sc-mcp" <<'EOF'
#!/usr/bin/env bash
[ "$1" = "--version" ] && echo "rh-sc-mcp 0.3.1"
EOF
chmod +x "$WORK/gobin/rh-sc-mcp"

run binary
check 'the binary is kept without CONFIRM' \
  "$( [ -x "$WORK/gobin/rh-sc-mcp" ] && grep -q 'Not removing it yet' "$WORK/out"; echo $? )"
check 'the refusal explains the shared-binary risk' \
  "$( grep -q 'stdio and the HTTP setup can point at' "$WORK/out"; echo $? )"

CONFIRM="$WORK/gobin/rh-sc-mcp" run binary
check 'CONFIRM removes it' \
  "$( [ ! -e "$WORK/gobin/rh-sc-mcp" ]; echo $? )"

cat >"$WORK/gobin/rh-sc-mcp" <<'EOF'
#!/usr/bin/env bash
echo "some-other-tool 1.0"
EOF
chmod +x "$WORK/gobin/rh-sc-mcp"
CONFIRM="$WORK/gobin/rh-sc-mcp" run binary
check 'a binary that is not ours is kept even with CONFIRM' \
  "$( [ -x "$WORK/gobin/rh-sc-mcp" ] && grep -q 'is not rh-sc-mcp' "$WORK/out"; echo $? )"

# --- podman ----------------------------------------------------------

printf '\npodman\n'

cat >"$BIN/podman" <<'EOF'
#!/usr/bin/env bash
case "$1 $2" in
  "container exists") exit 1 ;;
  "image exists") exit 1 ;;
esac
exit 0
EOF
chmod +x "$BIN/podman"

run podman
check 'podman refuses without a name' \
  "$( grep -q 'name what to remove' "$WORK/out"; echo $? )"

CONTAINER=rh-sc-mcp run podman
check 'a named container that is absent is reported, not an error' \
  "$( grep -q 'no container named rh-sc-mcp' "$WORK/out"; echo $? )"
check 'podman says what it did not touch' \
  "$( grep -q 'no other container, image or build cache was touched' "$WORK/out"; echo $? )"

# --- openshift -------------------------------------------------------

printf '\nopenshift\n'

# The stub reads its behaviour from files, so each case can make one
# verb fail the way a cluster would: Forbidden is an error with a
# message, NotFound is an error that means absence, and they must not
# be treated alike.
cat >"$BIN/oc" <<'EOF'
#!/usr/bin/env bash
w="$OC_STUB_STATE"
case "$*" in
  "whoami") echo tester ;;
  "whoami --show-server") echo https://api.cluster.test:6443 ;;
  "get configmap -n "*" -o name")
    [ "$(cat "$w/cm_exit")" = "0" ] || {
      echo 'Error from server (Forbidden): configmaps is forbidden' >&2; exit 1; }
    cat "$w/cm_list" ;;
  "get secret/rh-sc-mcp -n "*" -o name")
    case "$(cat "$w/secret_get")" in
      exists) echo secret/rh-sc-mcp ;;
      notfound) echo 'Error from server (NotFound): secrets "rh-sc-mcp" not found' >&2; exit 1 ;;
      *) echo 'Error from server (Forbidden): secrets is forbidden' >&2; exit 1 ;;
    esac ;;
  "delete secret/rh-sc-mcp "*)
    [ "$(cat "$w/secret_delete")" = "0" ] || {
      echo 'Error from server (Forbidden): secrets is forbidden' >&2; exit 1; }
    echo 'secret "rh-sc-mcp" deleted' ;;
  "delete "*)
    [ "$(cat "$w/delete_exit")" = "0" ] || {
      echo 'Error from server (Forbidden)' >&2; exit 1; }
    echo "$2 deleted" ;;
  *) exit 0 ;;
esac
EOF
chmod +x "$BIN/oc"

export OC_STUB_STATE="$WORK/ocstate"
mkdir -p "$OC_STUB_STATE"
oc_state() { # oc_state <cm_exit> <cm_list> <secret_get> <secret_delete> <delete_exit>
  printf '%s' "$1" >"$OC_STUB_STATE/cm_exit"
  printf '%s' "$2" >"$OC_STUB_STATE/cm_list"
  printf '%s' "$3" >"$OC_STUB_STATE/secret_get"
  printf '%s' "$4" >"$OC_STUB_STATE/secret_delete"
  printf '%s' "$5" >"$OC_STUB_STATE/delete_exit"
}

oc_state 1 '' exists 0 0
NAMESPACE=test-ns run openshift
check 'a forbidden ConfigMap list aborts before deleting' \
  "$( grep -q 'nothing was deleted' "$WORK/out"; echo $? )"
check 'the abort happens before the deployment is removed' \
  "$( ! grep -q 'deleted deployment/rh-sc-mcp' "$WORK/out"; echo $? )"

oc_state 0 'configmap/rh-sc-mcp-g8799bg6mt' exists 0 0
NAMESPACE=test-ns run openshift
check 'the generated configmap is deleted' \
  "$( grep -q 'deleted configmap/rh-sc-mcp-g8799bg6mt' "$WORK/out"; echo $? )"
check 'the secret is deleted by default' \
  "$( grep -q 'deleted secret/rh-sc-mcp' "$WORK/out"; echo $? )"
check 'the namespace is not deleted' \
  "$( grep -q 'namespace test-ns was not deleted' "$WORK/out" && ! grep -q 'delete namespace' "$WORK/out"; echo $? )"
check 'the home credentials are reported as untouched' \
  "$( grep -q 'were not touched' "$WORK/out"; echo $? )"

oc_state 0 '' exists 0 1
NAMESPACE=test-ns run openshift
check 'a forbidden deployment delete stops instead of reporting success' \
  "$( grep -q 'could not delete deployment/rh-sc-mcp' "$WORK/out"; echo $? )"

oc_state 0 '' exists 1 0
NAMESPACE=test-ns run openshift
check 'a forbidden secret delete is an error, not a silent leftover' \
  "$( grep -q 'could not delete secret/rh-sc-mcp' "$WORK/out"; echo $? )"
check 'that error warns the credential may still be there' \
  "$( grep -q 'live Red Hat offline' "$WORK/out"; echo $? )"

oc_state 0 '' notfound 0 0
NAMESPACE=test-ns KEEP_SECRET=1 run openshift
check 'KEEP_SECRET with no secret present says so' \
  "$( grep -q 'no secret/rh-sc-mcp' "$WORK/out"; echo $? )"

oc_state 0 '' forbidden 0 0
NAMESPACE=test-ns KEEP_SECRET=1 run openshift
check 'KEEP_SECRET with a forbidden read refuses to guess' \
  "$( grep -q 'could not be read' "$WORK/out"; echo $? )"

oc_state 0 '' exists 0 0
NAMESPACE=test-ns KEEP_SECRET=1 run openshift
check 'KEEP_SECRET keeps it' \
  "$( grep -q 'kept secret/rh-sc-mcp' "$WORK/out"; echo $? )"
check 'keeping it prints a prominent warning' \
  "$( grep -q '!! secret/rh-sc-mcp is still in test-ns' "$WORK/out"; echo $? )"

# --- credentials -----------------------------------------------------

printf '\ncredentials\n'

CRED="$WORK/creds"
mkdir -p "$CRED"
printf 'tok\n' >"$CRED/token"
printf 'htok\n' >"$CRED/http-token"

RH_SC_MCP_CREDENTIAL_DIR="$CRED" run credentials
check 'credentials are kept without CONFIRM' \
  "$( [ -f "$CRED/token" ] && grep -q 'Not deleting' "$WORK/out"; echo $? )"

RH_SC_MCP_CREDENTIAL_DIR="$CRED" CONFIRM="$CRED" run credentials
check 'CONFIRM removes the token files' \
  "$( [ ! -e "$CRED/token" ] && [ ! -e "$CRED/http-token" ]; echo $? )"
check 'the directory itself survives' "$( [ -d "$CRED" ]; echo $? )"

# --- usage -----------------------------------------------------------

printf '\nusage\n'

run
check 'usage lists every subcommand' \
  "$( for s in stdio http binary podman openshift credentials; do
        grep -q "  $s" "$WORK/out" || exit 1; done; echo $? )"
check 'usage names the real KEEP_SECRET flag' \
  "$( grep -q 'KEEP_SECRET=1' "$WORK/out" && ! grep -q 'INCLUDE_SECRET' "$WORK/out"; echo $? )"

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
