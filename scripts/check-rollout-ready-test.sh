#!/usr/bin/env bash
# Tests scripts/check-rollout-ready.sh with stand-in rollout.go files. Runs in CI (job
# release-guard-test) and locally:
#
#   bash scripts/check-rollout-ready-test.sh
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
now=1790000000 # 2026-09-21T...Z, any fixed instant
ahead() { date -u -d "@$((now + $1 * 86400))" +%Y-%m-%d 2>/dev/null || date -u -r "$((now + $1 * 86400))" +%Y-%m-%d; }
rollout_with() { printf 'package account\n\nvar enforceFrom = %s\n' "$1" > "$work/rollout.go"; }
month_of() { case "$1" in 01) echo January ;; 02) echo February ;; 03) echo March ;; 04) echo April ;; 05) echo May ;; 06) echo June ;;
  07) echo July ;; 08) echo August ;; 09) echo September ;; 10) echo October ;; 11) echo November ;; 12) echo December ;; esac; }
dated() { local d="$1"; echo "time.Date(${d%%-*}, time.$(month_of "$(echo "$d" | cut -d- -f2)"), $((10#${d##*-})), 0, 0, 0, 0, time.UTC)"; }
printf 'token' > "$work/fixture.jwt"

run() { ACCOUNT_ENFORCEMENT_RELEASE="$1" bash "$here/check-rollout-ready.sh" --rollout "$work/rollout.go" --fixture "${2:-$work/fixture.jwt}" --now "$now"; }
expect_ok() { local n="$1"; shift; run "$@" > "$work/out" 2>&1 || { cat "$work/out"; fail "$n: the guard failed"; }; echo "ok: $n"; }
expect_fail() { local n="$1" why="$2"; shift 2; if run "$@" > "$work/out" 2>&1; then cat "$work/out"; fail "$n: the guard passed"; fi
  grep -q "$why" "$work/out" || { cat "$work/out"; fail "$n: should say '$why'"; }; echo "ok: $n"; }

rollout_with 'time.Time{}'
expect_ok "a dormant release" ""
expect_ok "a dormant release, flag false" "false"
expect_fail "the flag without a date" "still the zero time" "true"

rollout_with "$(dated "$(ahead 30)")"
expect_fail "a date without the owner's flag" "only the owner's release R" ""
expect_ok "a date 30 days ahead, flagged, with the fixture" "true"
expect_fail "a date without the fixture" "fixture" "true" "$work/absent.jwt"

rollout_with "$(dated "$(ahead 10)")"
expect_fail "a date under 21 days ahead" "less than 21 days" "true"

# The owner may waive the notice (no users to warn): a date that has already passed is then accepted.
rollout_with "$(dated "$(ahead -5)")"
expect_fail "a past date with the default notice" "less than 21 days" "true"
ACCOUNT_ENFORCEMENT_MIN_DAYS=0 expect_ok "a past date with the notice waived" "true"
ACCOUNT_ENFORCEMENT_MIN_DAYS=0 expect_fail "a past date, waived, without the fixture" "fixture" "true" "$work/absent.jwt"
ACCOUNT_ENFORCEMENT_MIN_DAYS=0 expect_fail "a waived notice does not bring back an unflagged date" "only the owner's release R" ""

rollout_with 'time.Date(2030, time.Smarch, 1, 0, 0, 0, 0, time.UTC)'
expect_fail "a month that does not exist" "unknown month" "true"
rollout_with 'time.Date(2030, time.March, 1, 12, 0, 0, 0, time.UTC)'
expect_fail "a date that is not midnight UTC" "not 'time.Date" "true"

echo "check-rollout-ready test: ok"
