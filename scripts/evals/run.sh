#!/usr/bin/env bash
# Entry point of the agent capability eval suite (see scripts/evals/README.md).
#
#   scripts/evals/run.sh --reference                      # CI self-test: score the reference solutions, no model
#   scripts/evals/run.sh --bin /path/monoagentcli --reference
#   OPENROUTER_API_KEY=... scripts/evals/run.sh --model nvidia/nemotron-3-super-120b-a12b:free --tasks rss-read,sort-limit
#
# Without --bin it builds monoagentcli with -tags devaccount into a temp dir
# first: the eval sandboxes have no monoes.me sign-in, and only a devaccount
# build honours MONOAGENT_DEV_ENFORCE_FROM, which the harness sets to a future
# date (the account gate's warn period). Every other argument goes to evals.py.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
command -v python3 >/dev/null || { echo "evals: python3 is required" >&2; exit 1; }

have_bin=0
for a in "$@"; do [ "$a" = "--bin" ] && have_bin=1; done

if [ "$have_bin" = 0 ]; then
  # Same place as the sandboxes: $EVALS_SCRATCH, else ~/scratch, else the temp dir.
  tmp_root="${EVALS_SCRATCH:-}"
  if [ -z "$tmp_root" ]; then
    if [ -d "$HOME/scratch" ]; then tmp_root="$HOME/scratch"; else tmp_root="${TMPDIR:-/tmp}"; fi
  fi
  build="$(mktemp -d "$tmp_root/monoagent-evals-bin.XXXXXX")"
  trap 'rm -rf "$build"' EXIT
  echo "evals: building monoagentcli (-tags devaccount)" >&2
  (cd "$repo" && go build -tags devaccount -o "$build/monoagentcli" ./cmd/monoagentcli)
  set -- --bin "$build/monoagentcli" "$@"
fi

jobs=(--jobs "${EVALS_JOBS:-6}")
for a in "$@"; do [ "$a" = "--jobs" ] && jobs=(); done
python3 "$here/evals.py" "$@" "${jobs[@]}"
