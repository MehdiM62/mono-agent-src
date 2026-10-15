#!/usr/bin/env bash
# Smoke test for `monoagentcli doctor` (setup & health check) against a
# throwaway HOME: the JSON contract, the required-failure exit code, the
# fix → re-check loop, `doctor fix`'s NDJSON stream, and `setup` from an
# empty HOME (with no terminal, and with --yes for the core group). Runs in CI
# (.github/workflows/ci.yml, job doctor-smoke) and locally:
#
#   go build -tags devaccount -o /tmp/monoagentcli ./cmd/monoagentcli && scripts/doctor-smoke.sh /tmp/monoagentcli
#
# doctor and setup need a monoes.me sign-in. The build carries the devaccount tag so that the checks
# can run in the warn period (MONOAGENT_DEV_ENFORCE_FROM in the future, nothing refused); the last
# section sets the date in the past, with no session, and asserts that they are refused.
set -euo pipefail

cli="${1:?usage: doctor-smoke.sh <path to monoagentcli>}"
command -v jq >/dev/null || { echo "::error::doctor smoke: jq is required"; exit 1; }
root="$(mktemp -d)"
home="$root/home"
out="$root/out" # reports live outside HOME, so "doctor wrote nothing" can be checked
mkdir -p "$home" "$out"
trap 'rm -r "$root" 2>/dev/null || true' EXIT
# Everything a run could write to goes under the scratch folder, locally too.
export MONOAGENT_DEV_ENFORCE_FROM=2099-01-01T00:00:00Z
export HOME="$home" USERPROFILE="$home" TMPDIR="$root/tmp" \
  XDG_CONFIG_HOME="$home/.config" XDG_CACHE_HOME="$home/.cache" XDG_DATA_HOME="$home/.local/share" XDG_STATE_HOME="$home/.local/state"
mkdir -p "$TMPDIR"
# shellcheck source=lib/mono.sh
. "$(dirname "$0")/lib/mono.sh"
run() { mono_guard "$cli" --db-path "$home/.monoagent/monoagent.db" "$@"; }
prof=default # the profile doctor creates in an empty HOME; every scoped call names it
fail() { echo "::error::doctor smoke: $*"; exit 1; }

echo "== fresh HOME: plain doctor writes nothing; a required check fails, exit 1, valid v1 report"
set +e
run --profile "$prof" --json doctor --group core > "$out/fresh.json"
code=$?
set -e
[ "$code" -eq 1 ] || fail "fresh HOME: exit $code, want 1"
[ -z "$(ls -A "$home")" ] || fail "plain doctor wrote to HOME: $(ls -A "$home" | tr '\n' ' ')"
jq -e '.v == 1 and (.generated_at | type == "string") and (.summary | type == "object")' "$out/fresh.json" >/dev/null || fail "report is not schema v1"
jq -e '.results[] | select(.id == "core.home") | .status == "fail" and .required == true and .fix.id == "core.home.create" and .fix.safety == "auto"' "$out/fresh.json" >/dev/null \
  || fail "core.home should fail (required) with the auto fix core.home.create"
jq -e '.results[] | select(.id == "core.db") | .status == "skip"' "$out/fresh.json" >/dev/null \
  || fail "core.db should wait on the data folder"
jq -e 'all(.results[]; (.id | length > 0) and (.group | length > 0) and (.title | type == "string") and (.summary | type == "string")
        and (.status | IN("ok","warn","fail","skip","info")) and ((.required // false) | type == "boolean")
        and (.fix == null or (.fix.safety | IN("auto","confirm","manual"))))' "$out/fresh.json" >/dev/null \
  || fail "every result needs id, group, title, summary, a known status, and a fix with a known safety"

echo "== doctor --fix: applies the fixes over several passes, exit 0"
run --profile "$prof" --json doctor --group core --fix > "$out/fixed.json" || fail "doctor --fix exited $?"
for id in core.home core.db core.profile; do
  jq -e --arg id "$id" '.results[] | select(.id == $id) | .status == "ok"' "$out/fixed.json" >/dev/null || fail "$id not ok after --fix"
done
jq -e '[.fixes[] | select(.outcome == "applied") | .id] | (index("core.home.create") != null) and (index("core.db.migrate") != null) and (index("core.profile.layout") != null)' "$out/fixed.json" >/dev/null \
  || fail "--fix should report core.home.create, core.db.migrate and core.profile.layout as applied"

echo "== doctor fix <id> --json: NDJSON, ends with done, idempotent"
run --profile "$prof" --json doctor fix core.db.migrate > "$out/fix.ndjson" || fail "doctor fix exited $?"
jq -se 'all(.[]; .kind | IN("line","done","error")) and (last.kind == "done")' "$out/fix.ndjson" >/dev/null \
  || fail "doctor fix output is not NDJSON ending in done"

echo "== unknown fix: exit 2"
set +e
run --profile "$prof" doctor fix no.such.fix >/dev/null 2>&1
code=$?
set -e
[ "$code" -eq 2 ] || fail "unknown fix: exit $code, want 2"

echo "== setup with no terminal: exit 0, NDJSON progress, nothing that needs a yes is applied"
setup_home="$root/setup-home"
mkdir -p "$setup_home"
set +e
HOME="$setup_home" USERPROFILE="$setup_home" mono_guard timeout 120 "$cli" --db-path "$setup_home/.monoagent/monoagent.db" --profile "$prof" --json setup \
  < /dev/null > "$out/setup.json" 2> "$out/setup.ndjson"
code=$?
set -e
[ "$code" -eq 0 ] || fail "setup with no terminal: exit $code, want 0 (124 = it hung)"
jq -e '.v == 1' "$out/setup.json" >/dev/null || fail "setup report is not schema v1"
# stderr also carries plain log lines (a CI runner has no keychain, so the
# database setup warns there); the progress events are the JSON lines.
grep '^{' "$out/setup.ndjson" > "$out/setup.events" || fail "setup streamed no progress events"
jq -se 'length > 0 and all(.[]; .kind | type == "string")' "$out/setup.events" >/dev/null || fail "setup progress events are not NDJSON"
auto_ok='["core.home.create","core.db.migrate","core.profile.layout"]'
jq -e --argjson ok "$auto_ok" '([.fixes[] | select(.outcome == "applied") | .id] - $ok) | length == 0' "$out/setup.json" >/dev/null \
  || fail "setup with no terminal applied a fix that needs a yes: $(jq -c '[.fixes[] | select(.outcome == "applied") | .id]' "$out/setup.json")"

echo "== setup --yes --json --group core in a clean HOME: healthy core, stage and per-fix events"
core_home="$root/core-home"
mkdir -p "$core_home"
set +e
HOME="$core_home" USERPROFILE="$core_home" mono_guard timeout 120 "$cli" --db-path "$core_home/.monoagent/monoagent.db" --profile "$prof" --json setup --group core --yes --mcp \
  < /dev/null > "$out/setup-core.json" 2> "$out/setup-core.ndjson"
code=$?
set -e
[ "$code" -eq 0 ] || fail "setup --group core --yes: exit $code, want 0"
[ ! -e "$core_home/.claude" ] || fail "setup --group core wrote ~/.claude"
# The account row may fail after the date on this deliberately unsigned-in HOME.
jq -e 'all(.results[] | select(.id != "core.monoes_account"); .group == "core" and .status != "fail")' "$out/setup-core.json" >/dev/null \
  || fail "setup --group core left a failing or non-core row: $(jq -c '[.results[] | select(.id != "core.monoes_account") | select(.status == "fail" or .group != "core") | .id]' "$out/setup-core.json")"
# core.home.create applied proves the first check saw the empty HOME (no pre-run made the folder first).
jq -e '[.fixes[] | select(.outcome == "applied") | .id] == ["core.home.create","core.db.migrate","core.profile.layout"]' "$out/setup-core.json" >/dev/null \
  || fail "setup --group core applied $(jq -c '[.fixes[] | select(.outcome == "applied") | .id]' "$out/setup-core.json")"
jq -e '.fixes[] | select(.id == "integrations.mcp_monoagent.register") | .outcome == "skipped" and (.reason | length > 0)' "$out/setup-core.json" >/dev/null \
  || fail "--mcp that can't be done should be reported as skipped, with a reason"
grep '^{' "$out/setup-core.ndjson" > "$out/setup-core.events" || fail "setup streamed no events"
jq -se '[.[] | select(.kind == "stage") | .message] | index("Fixing what is missing") != null' "$out/setup-core.events" >/dev/null \
  || fail "setup streamed no stage events"
jq -se '([.[] | select(.kind == "fix_start") | .fix_id] == ["core.home.create","core.db.migrate","core.profile.layout"])
        and ([.[] | select(.kind == "fix_end" and .outcome == "applied") | .fix_id] == ["core.home.create","core.db.migrate","core.profile.layout"])' \
  "$out/setup-core.events" >/dev/null || fail "setup's fix_start/fix_end events don't match the applied fixes"

echo "== full report: every group renders as valid v1 JSON, and nothing required fails after --fix"
set +e
run --profile "$prof" --json doctor > "$out/full.json"
code=$?
set -e
[ "$code" -eq 0 ] || fail "full report after --fix: exit $code; required failures: $(jq -c '[.results[] | select(.required and .status == "fail") | .id]' "$out/full.json")"
jq -e '.v == 1 and (.results | length) > 5' "$out/full.json" >/dev/null || fail "full report is not a valid v1 report"
jq -r '.results[] | select(.parent == null) | "\(.status)\t\(.id)\t\(.summary)"' "$out/full.json"

echo "== signed out after the date: doctor, doctor fix and setup are refused (exit 4, login_required); version, help and account status run"
locked_home="$root/locked-home"
mkdir -p "$locked_home"
login_line="Log in to monoes.me first: monoagentcli account login"
locked() { HOME="$locked_home" USERPROFILE="$locked_home" MONOAGENT_DEV_ENFORCE_FROM=2020-01-01T00:00:00Z \
  mono_guard "$cli" --db-path "$locked_home/.monoagent/monoagent.db" "$@" < /dev/null; }
for args in "doctor" "doctor fix core.db.migrate" "setup"; do
  set +e
  # shellcheck disable=SC2086 # the words are the command line
  locked --profile "$prof" $args > "$out/locked.out" 2> "$out/locked.err"
  code=$?
  set -e
  [ "$code" -eq 4 ] || fail "signed out, $args: exit $code, want 4"
  [ "$(head -n 1 "$out/locked.err")" = "$login_line" ] || fail "signed out, $args: stderr $(head -c 200 "$out/locked.err")"
done
set +e
locked --profile "$prof" --json doctor > "$out/locked.json" 2>/dev/null
set -e
jq -e '.login_required == true' "$out/locked.json" >/dev/null || fail "signed out, doctor --json: no login_required document"
locked version > /dev/null || fail "signed out, version should run"
locked help > /dev/null || fail "signed out, help should run"
set +e
locked account status > "$out/status.out" 2>&1
code=$?
set -e
[ "$code" -eq 4 ] && grep -q "Not logged in" "$out/status.out" || fail "signed out, account status: exit $code, $(head -c 200 "$out/status.out")"
[ ! -e "$locked_home/.monoagent/monoagent.db" ] || fail "a refused command created the database"

mono_check_clean
echo "doctor smoke: ok"
