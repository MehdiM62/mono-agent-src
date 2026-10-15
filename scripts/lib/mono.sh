#!/usr/bin/env bash
# Profile guard for scripts that drive monoagentcli. Source it, then run every
# invocation through mono_guard (or mono, for the plain binary):
#
#   . "$(dirname "$0")/lib/mono.sh"
#   MONO_BIN=/path/to/monoagentcli
#   mono --profile smoke task list
#   mono_guard env HOME="$h" "$cli" --profile smoke task list   # any command line
#   mono_check_clean                                            # last line of the script
#
# A command that acts on a profile and was not told which one prints
# "this command acts on a profile" on stderr (internal/cli/profilegate). The
# guard passes stderr through unchanged, and when it contains that text it
# prints an ::error:: line, records the invocation in $MONO_GUARD_LOG and makes
# the call return 97. A call inside $(...) or a pipeline cannot stop the script,
# so mono_check_clean, called once at the end, fails the whole script (exit 97)
# for any recorded violation. Background processes whose stderr goes to a log
# are checked with mono_scan_log <file>.
MONO_GUARD_NEEDLE='this command acts on a profile'
: "${MONO_GUARD_LOG:=$(mktemp "${TMPDIR:-/tmp}/mono-guard.XXXXXX")}"
export MONO_GUARD_LOG

mono_guard() {
  local errf rc=0 oldtraps
  errf="$(mktemp "${TMPDIR:-/tmp}/mono-stderr.XXXXXX")"
  # Do not leave the capture file behind when the call is interrupted.
  oldtraps="$(trap -p INT TERM HUP)"
  trap 'rm -f "$errf"; exit 130' INT TERM HUP
  # stderr goes through tee: it is shown as the command writes it (a server
  # that runs for minutes is not silent) and captured for the check below.
  # stdout is carried around the pipe on fd 3.
  { "$@" 2>&1 1>&3 3>&- | tee "$errf" >&2; rc="${PIPESTATUS[0]}"; } 3>&1
  if grep -q "$MONO_GUARD_NEEDLE" "$errf"; then
    printf '%s\n' "$*" >>"$MONO_GUARD_LOG"
    echo "::error::profile guard: this call named no profile: $*" >&2
    rc=97
  fi
  rm -f "$errf"
  trap - INT TERM HUP
  [ -z "$oldtraps" ] || eval "$oldtraps"
  return "$rc"
}

mono() { mono_guard "${MONO_BIN:?set MONO_BIN to the monoagentcli binary}" "$@"; }

mono_scan_log() { # file: a background process's stderr
  if [ -f "$1" ] && grep -q "$MONO_GUARD_NEEDLE" "$1"; then
    echo "$1" >>"$MONO_GUARD_LOG"
    echo "::error::profile guard: $1 contains the profile warning" >&2
  fi
}

mono_check_clean() {
  if [ -s "$MONO_GUARD_LOG" ]; then
    echo "::error::profile guard: $(wc -l <"$MONO_GUARD_LOG") call(s) named no profile:" >&2
    cat "$MONO_GUARD_LOG" >&2
    exit 97
  fi
}
