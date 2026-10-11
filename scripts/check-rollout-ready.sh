#!/usr/bin/env bash
# Release guard for the monoes.me account rollout (plan B5a Tasks 1-2, spec D9/D22). The enforcement
# date (internal/account/rollout.go) and the production-key proof are the OWNER's decisions, so a
# release may carry a date only when the owner says so, and then only when it is ready:
#
#   - ACCOUNT_ENFORCEMENT_RELEASE unset or not "true" (an ordinary, dormant release): the date must
#     be the zero time. A date in the source fails the release: it would lock users unannounced.
#   - ACCOUNT_ENFORCEMENT_RELEASE=true (release R, set by the owner as a repository variable): the
#     date must be set, at least MIN_DAYS (21, spec D9) after the release, and the production-token
#     fixture (internal/account/testdata/production-key-token.jwt) must exist, so the pinned keys have
#     been proven against a token the production key signed.
#
#   scripts/check-rollout-ready.sh [--rollout FILE] [--fixture FILE] [--now EPOCH]
#
# The owner's activation is one line (the same date goes into wantEnforceFrom in
# internal/account/rollout_date_test.go):
#   var enforceFrom = time.Date(YYYY, time.Month, D, 0, 0, 0, 0, time.UTC)
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
rollout="$root/internal/account/rollout.go"
fixture="$root/internal/account/testdata/production-key-token.jwt"
now="$(date -u +%s)"
min_days=21
while [ "$#" -gt 0 ]; do
  case "$1" in
    --rollout) rollout="${2:?--rollout needs a file}"; shift 2 ;;
    --fixture) fixture="${2:?--fixture needs a file}"; shift 2 ;;
    --now) now="${2:?--now needs an epoch}"; shift 2 ;;
    *) echo "usage: check-rollout-ready.sh [--rollout FILE] [--fixture FILE] [--now EPOCH]" >&2; exit 2 ;;
  esac
done
[ -f "$rollout" ] || { echo "check-rollout-ready: $rollout does not exist" >&2; exit 2; }

line="$(grep -E '^var enforceFrom[[:space:]]*=' "$rollout" || true)"
[ -n "$line" ] || { echo "::error::check-rollout-ready: no 'var enforceFrom =' line in $rollout (the owner's one-line change moved)" >&2; exit 1; }
flag="${ACCOUNT_ENFORCEMENT_RELEASE:-}"

if printf '%s' "$line" | grep -Eq '=[[:space:]]*time\.Time\{\}[[:space:]]*$'; then
  if [ "$flag" = "true" ]; then
    echo "::error::ACCOUNT_ENFORCEMENT_RELEASE=true but the enforcement date is still the zero time (dormant): the owner has not set it in internal/account/rollout.go" >&2
    exit 1
  fi
  echo "check-rollout-ready: dormant release (no enforcement date), ok"
  exit 0
fi

if [ "$flag" != "true" ]; then
  echo "::error::internal/account/rollout.go carries an enforcement date but ACCOUNT_ENFORCEMENT_RELEASE is not true: only the owner's release R may ship a date" >&2
  exit 1
fi

re='time\.Date\(([0-9]{4}),[[:space:]]*time\.([A-Z][a-z]+),[[:space:]]*([0-9]{1,2}),[[:space:]]*0,[[:space:]]*0,[[:space:]]*0,[[:space:]]*0,[[:space:]]*time\.UTC\)'
if ! [[ "$line" =~ $re ]]; then
  echo "::error::the enforceFrom initializer is not 'time.Date(Y, time.Month, D, 0, 0, 0, 0, time.UTC)' (00:00 UTC): $line" >&2
  exit 1
fi
year="${BASH_REMATCH[1]}"; month="${BASH_REMATCH[2]}"; day="${BASH_REMATCH[3]}"
case "$month" in
  January) mm=01 ;; February) mm=02 ;; March) mm=03 ;; April) mm=04 ;; May) mm=05 ;; June) mm=06 ;;
  July) mm=07 ;; August) mm=08 ;; September) mm=09 ;; October) mm=10 ;; November) mm=11 ;; December) mm=12 ;;
  *) echo "::error::unknown month time.$month" >&2; exit 1 ;;
esac
ymd="$(printf '%s-%s-%02d' "$year" "$mm" "$((10#$day))")"
if at="$(date -u -d "$ymd 00:00:00" +%s 2>/dev/null)"; then :
elif at="$(date -u -j -f '%Y-%m-%d %H:%M:%S' "$ymd 00:00:00" +%s 2>/dev/null)"; then :
else echo "::error::cannot parse the enforcement date $ymd" >&2; exit 1; fi

if [ "$at" -lt "$((now + min_days * 86400))" ]; then
  echo "::error::the enforcement date $ymd is less than $min_days days after this release (spec D9 promises users at least three weeks)" >&2
  exit 1
fi
if [ ! -s "$fixture" ]; then
  echo "::error::the enforcement date is set but the production-token fixture $fixture is missing: mint it from the production signing key (the pinned keys are unproven against a real token)" >&2
  exit 1
fi
echo "check-rollout-ready: release R ready, enforcement from $ymd"
