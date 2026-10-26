#!/usr/bin/env bash
#
# Tests for scripts/deploy-openshift.sh.
#
# No cluster and no real credentials. The credential directory is
# redirected with RH_SC_MCP_CREDENTIAL_DIR, which is the reason that
# override exists: a test must never read the operator's own token.
#
# The central assertion is negative — that a token placed in the
# credential directory never reaches stdout or stderr, in whole, as a
# hash, or as a length.

set -uo pipefail

readonly HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly SCRIPT="$HERE/deploy-openshift.sh"

# A distinctive value, so a leak is unambiguous when grepping output.
readonly MARKER='rhscmcptestsecretzzz0123456789abcdef'

pass=0
fail=0

ok() { printf '  ok   %s\n' "$1"; pass=$((pass + 1)); }
no() { printf '  FAIL %s\n' "$1"; fail=$((fail + 1)); }

check() { # check <description> <condition-exit-status>
  if [ "$2" -eq 0 ]; then ok "$1"; else no "$1"; fi
}

WORK="$(mktemp -d "${TMPDIR:-/tmp}/deploy-test.XXXXXX")"
trap 'rm -rf "$WORK"' EXIT

# --- unit: token normalisation ---------------------------------------

printf '\ntoken normalisation\n'

# shellcheck source=deploy-openshift.sh
source "$SCRIPT"
# The script sets -e for its own run. Keep it off here: half these
# tests assert that something fails, and -e would end the suite at the
# first one.
set +e

norm() { # norm <file-contents-as-printf-format> -> writes $WORK/out
  printf "$1" >"$WORK/in"
  ( normalise_token "$WORK/in" "$WORK/out" "test token" ) >"$WORK/err" 2>&1
}

norm 'abc123\n'
check 'trailing LF is stripped' \
  "$( [ "$(cat "$WORK/out")" = "abc123" ] && [ ! -s "$WORK/err" ]; echo $? )"

norm 'abc123\r\n'
check 'trailing CRLF is stripped' \
  "$( [ "$(cat "$WORK/out")" = "abc123" ]; echo $? )"

norm 'abc123\n\n\n'
check 'several trailing newlines are stripped' \
  "$( [ "$(cat "$WORK/out")" = "abc123" ]; echo $? )"

norm 'abc123'
check 'a file with no trailing newline is accepted' \
  "$( [ "$(cat "$WORK/out")" = "abc123" ]; echo $? )"

norm 'abc123\n'
check 'no trailing byte is left behind' \
  "$( [ "$(wc -c <"$WORK/out" | tr -d ' ')" = "6" ]; echo $? )"

norm 'abc 123\n'
check 'an interior space is rejected, not trimmed' \
  "$( grep -q 'whitespace inside' "$WORK/err"; echo $? )"

norm 'abc\t123\n'
check 'an interior tab is rejected' \
  "$( grep -q 'whitespace inside' "$WORK/err"; echo $? )"

norm 'abc\n123\n'
check 'an interior newline is rejected' \
  "$( grep -q 'whitespace inside' "$WORK/err"; echo $? )"

norm '\n'
check 'an empty token is rejected' \
  "$( grep -q 'is empty' "$WORK/err"; echo $? )"

# --- unit: image rules -----------------------------------------------

printf '\nimage rules\n'

try_split() { ( split_image "$1" ) >"$WORK/err" 2>&1; }

try_split 'reg.example.com/ns/rh-sc-mcp'
check 'an untagged image is rejected' "$( grep -q 'explicit tag' "$WORK/err"; echo $? )"

try_split 'reg.example.com/ns/rh-sc-mcp:latest'
check 'the latest tag is rejected' "$( grep -q "must not be tagged" "$WORK/err"; echo $? )"

( split_image 'reg.example.com:5000/ns/rh-sc-mcp:v0.3.1' ) >/dev/null 2>&1
split_image 'reg.example.com:5000/ns/rh-sc-mcp:v0.3.1'
check 'a registry port is not mistaken for the tag' \
  "$( [ "$IMAGE_NAME" = "reg.example.com:5000/ns/rh-sc-mcp" ] && [ "$IMAGE_TAG" = "v0.3.1" ]; echo $? )"

# --- unit: ConfigMap credential guard --------------------------------

printf '\nConfigMap credential guard\n'

guard_with() { # guard_with <config.yaml contents>
  printf '%s\n' "$1" >"$WORK/cfg.yaml"
  ( check_configmap_clean "$WORK/cfg.yaml" ) >"$WORK/err" 2>&1
}

guard_with 'readOnly: true'
check 'a clean config passes' "$( [ ! -s "$WORK/err" ]; echo $? )"

guard_with 'offlineToken: sometoken'
check 'offlineToken in the ConfigMap is rejected' \
  "$( grep -q 'credential key' "$WORK/err"; echo $? )"

guard_with 'http:
  token: sometoken'
check 'http.token in the ConfigMap is rejected' \
  "$( grep -q 'credential key' "$WORK/err"; echo $? )"

guard_with 'ssoTokenURL: https://sso.example.com/token'
check 'ssoTokenURL is not mistaken for a credential' \
  "$( [ ! -s "$WORK/err" ]; echo $? )"

guard_with '# offlineToken: sometoken'
check 'a commented-out credential key passes' "$( [ ! -s "$WORK/err" ]; echo $? )"

# --- integration: nothing derived from the token is ever printed -----

printf '\nno secret value, hash or length in output\n'

CRED="$WORK/creds"
mkdir -p "$CRED"
chmod 700 "$CRED"
printf '%s\n' "$MARKER" >"$CRED/token"
chmod 600 "$CRED/token"

# Values that would betray the token without quoting it.
marker_len=${#MARKER}
marker_sha="$(printf '%s' "$MARKER" | shasum -a 256 | awk '{print $1}')"
marker_b64="$(printf '%s' "$MARKER" | base64)"

run_all_subcommands() { # captures stdout+stderr of every entry point
  local out="$1"
  : >"$out"

  # Stub oc. Not for speed: a real oc against an unreachable cluster
  # prints DNS errors carrying timestamps and pids, and the assertion
  # below — that the token's length does not appear as a bare number —
  # then passes or fails depending on what digits those happen to hold.
  # Observed failing once that way. The subject here is what this
  # script prints, so everything else is kept out of the capture.
  local stub="$WORK/stub"
  mkdir -p "$stub"
  printf '#!/usr/bin/env bash\nexit 0\n' >"$stub/oc"
  chmod +x "$stub/oc"
  local PATH="$stub:$PATH"
  # `image` is left out: it runs a real podman build, whose output is
  # full of unrelated digests and byte counts, and it never touches the
  # credential directory. Its own guards are tested separately.
  for sub in '' plan apply status bogus; do
    RH_SC_MCP_CREDENTIAL_DIR="$CRED" \
    NAMESPACE=test-ns IMAGE=reg.example.test/ns/rh-sc-mcp:v0test ARCH=amd64 \
      bash "$SCRIPT" $sub >>"$out" 2>&1
  done
  # And the credential staging path on its own, which is the only code
  # that handles the value at all.
  (
    RH_SC_MCP_CREDENTIAL_DIR="$CRED"
    STAGE="$WORK/stage"; mkdir -p "$STAGE"; chmod 700 "$STAGE"
    CRED_DIR="$CRED"
    source "$SCRIPT"
    CRED_DIR="$CRED"
    stage_credentials
  ) >>"$out" 2>&1
}

OUT="$WORK/output.txt"
run_all_subcommands "$OUT"

check 'the token value never appears' \
  "$( ! grep -qF "$MARKER" "$OUT"; echo $? )"
check 'a base64 of the token never appears' \
  "$( ! grep -qF "$marker_b64" "$OUT"; echo $? )"
check 'a sha256 of the token never appears' \
  "$( ! grep -qiF "$marker_sha" "$OUT"; echo $? )"
check 'the token length never appears as a bare number' \
  "$( ! grep -qE "(^|[^0-9])$marker_len([^0-9]|$)" "$OUT"; echo $? )"

# The generated shared token must not leak either.
if [ -f "$CRED/http-token" ]; then
  generated="$(cat "$CRED/http-token")"
  check 'the generated shared token never appears' \
    "$( ! grep -qF "$generated" "$OUT"; echo $? )"
  mode="$(stat -f '%Lp' "$CRED/http-token" 2>/dev/null || stat -c '%a' "$CRED/http-token")"
  check 'the generated shared token is mode 600' "$( [ "$mode" = "600" ]; echo $? )"
fi

# --- integration: file-mode and missing-file handling ----------------

printf '\ncredential directory handling\n'

loose="$WORK/loose"
mkdir -p "$loose"
printf '%s\n' "$MARKER" >"$loose/token"
chmod 644 "$loose/token"
RH_SC_MCP_CREDENTIAL_DIR="$loose" NAMESPACE=test-ns \
  IMAGE=reg.example.test/ns/rh-sc-mcp:v0test \
  bash "$SCRIPT" apply >"$WORK/err" 2>&1
check 'a token file that is not 600 is refused' \
  "$( grep -q 'expected 600' "$WORK/err" || grep -q 'not logged in' "$WORK/err"; echo $? )"
check 'that refusal does not quote the token' \
  "$( ! grep -qF "$MARKER" "$WORK/err"; echo $? )"

empty="$WORK/empty"
mkdir -p "$empty"
RH_SC_MCP_CREDENTIAL_DIR="$empty" NAMESPACE=test-ns \
  IMAGE=reg.example.test/ns/rh-sc-mcp:v0test \
  bash "$SCRIPT" apply >"$WORK/err" 2>&1
check 'a missing token file is reported' \
  "$( grep -qE 'offline token not found|not logged in' "$WORK/err"; echo $? )"

# --- integration: required variables ---------------------------------

printf '\nrequired variables\n'

for sub in plan apply; do
  NAMESPACE= IMAGE= bash "$SCRIPT" "$sub" >"$WORK/err" 2>&1
  check "$sub refuses to run without NAMESPACE" \
    "$( grep -q 'NAMESPACE is required' "$WORK/err"; echo $? )"
done

NAMESPACE=test-ns IMAGE= bash "$SCRIPT" plan >"$WORK/err" 2>&1
check 'plan refuses to run without IMAGE' \
  "$( grep -q 'IMAGE is required' "$WORK/err"; echo $? )"

IMAGE=reg.example.test/ns/rh-sc-mcp:v0test ARCH= bash "$SCRIPT" image >"$WORK/err" 2>&1
check 'image refuses to run without an explicit ARCH' \
  "$( grep -q 'ARCH is required' "$WORK/err"; echo $? )"

# --- summary ---------------------------------------------------------

printf '\n%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
