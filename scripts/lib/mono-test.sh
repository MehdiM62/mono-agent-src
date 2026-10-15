#!/usr/bin/env bash
# Self-test of scripts/lib/mono.sh with a fake binary: a clean call passes, a
# call whose stderr carries the profile warning is flagged (exit 97 from the
# call, ::error:: line, and mono_check_clean fails the script), a warning in a
# background log is flagged by mono_scan_log, and stdout/exit codes pass through.
#
#   bash scripts/lib/mono-test.sh
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -r "$work" 2>/dev/null || true' EXIT
fail() { echo "mono-test: $*" >&2; exit 1; }

# The fake binary warns exactly like the gate unless --profile is among its arguments.
cat >"$work/fake" <<'EOF'
#!/bin/sh
for a in "$@"; do
  [ "$a" = --profile ] && { echo "hello"; exit "${FAKE_RC:-0}"; }
done
echo "warning: this command acts on a profile: pass --profile <id|name>; falling back to the active profile" >&2
echo "hello"
EOF
chmod +x "$work/fake"

# Each case runs in its own shell, as a script that sources the helper would.
run_case() { # name body...
  local body="$2"
  TMPDIR="$work" MONO_BIN="$work/fake" bash -c ". '$here/mono.sh'; $body" >"$work/$1.out" 2>"$work/$1.err"
}

run_case named 'out=$(mono --profile p task list); [ "$out" = hello ]; mono_check_clean' \
  || fail "a call that names its profile was flagged"
grep -q "::error::" "$work/named.err" && fail "unexpected error line for a named call"

if run_case missing 'mono task list; mono_check_clean'; then fail "a call without --profile passed"; fi
grep -q "::error::profile guard: this call named no profile: .*task list" "$work/missing.err" || fail "no ::error:: line for the missing profile"

# Inside $(...) the call cannot stop the script, so the final check must.
run_case subshell 'out=$(mono task list || true); mono_check_clean; echo reached' && fail "mono_check_clean did not fail after a call in a substitution"
grep -q reached "$work/subshell.out" && fail "script went on past mono_check_clean"

# Exit codes of the wrapped command pass through when the profile was named.
run_case rc 'FAKE_RC=3 mono --profile p x' && fail "exit code 3 was swallowed"

# A background process's stderr log is scanned.
echo "warning: this command acts on a profile: pass --profile" >"$work/bg.log"
if run_case scan "mono_scan_log '$work/bg.log'; mono_check_clean"; then fail "a warning in a log passed"; fi
echo "quiet" >"$work/ok.log"
run_case scanok "mono_scan_log '$work/ok.log'; mono_check_clean" || fail "a clean log was flagged"

echo "mono-test: ok"
