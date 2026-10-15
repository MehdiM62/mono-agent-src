#!/usr/bin/env bash
# Mono Agent vs n8n on a 1 GB / 1 CPU box. See README.md in this directory.
#
# Usage (from the repository root):
#   BENCH_WORK=~/scratch/bench-vs-n8n scripts/bench/vs-n8n/run.sh
#
# Environment:
#   BENCH_WORK   work directory for binaries, data dirs and raw output
#                (default: ${TMPDIR:-/tmp}/bench-vs-n8n). Wiped and recreated
#                on every run, except bin/ (reused when the commit matches).
#   MONO_BIN     prebuilt devaccount monoagentcli to run (default: built here)
#   MONO_PROD_BIN prebuilt default-tag monoagentcli, only measured for size
#   N8N_IMAGE    default docker.n8n.io/n8nio/n8n:2.43.4
#   ROUNDS       rounds per tool, alternating Mono Agent / n8n (default 3)
#   RUNS         workflow runs per throughput round (default 1000)
#   CONC         concurrent webhook callers (default 10)
#   IDLE_WAIT    seconds to wait after ready before sampling idle memory and
#                starting the load (60; n8n answers 404 for a while after ready)
#   STALL        stop waiting for runs after this many seconds without one (30)
#
# Every container it starts is named vsn8n-* and removed on exit. Results go
# to scripts/bench/vs-n8n/results.json (override with RESULTS).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/../../.." && pwd)"
work="${BENCH_WORK:-${TMPDIR:-/tmp}/bench-vs-n8n}"
# shellcheck source=../../lib/mono.sh
. "$repo/scripts/lib/mono.sh"
n8n_image="${N8N_IMAGE:-docker.n8n.io/n8nio/n8n:2.43.4}"
base_image="${BASE_IMAGE:-alpine:3.22}"
rounds="${ROUNDS:-3}"
runs="${RUNS:-1000}"
conc="${CONC:-10}"
idle_wait="${IDLE_WAIT:-60}"
stall="${STALL:-30}"
results="${RESULTS:-$here/results.json}"
limits=(--memory 1g --memory-swap 1g --cpus 1)
uidgid="$(id -u):$(id -g)"
# The account gate's development override (see README "Account gate"): a
# devaccount build with an enforcement date far in the future runs in the
# warn period, so no monoes.me sign-in is needed. Same as scripts/e2e/*.sh.
dev_enforce="MONOAGENT_DEV_ENFORCE_FROM=2999-01-01T00:00:00Z"

net=vsn8n-net
mono=vsn8n-mono
n8n=vsn8n-n8n

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" >&2; }

cleanup() { docker rm -f "$net" "$mono" "$n8n" >/dev/null 2>&1 || true; }
trap cleanup EXIT

for tool in docker jq sqlite3 go; do
	command -v "$tool" >/dev/null || { echo "missing: $tool" >&2; exit 1; }
done

cleanup
mkdir -p "$work/bin"
find "$work" -mindepth 1 -maxdepth 1 ! -name bin -exec rm -r {} +
mkdir -p "$work/raw"

# ── Binaries ────────────────────────────────────────────────────────────────
commit="$(git -C "$repo" rev-parse --short HEAD)"
build() { # build <out> [tags]
	log "building $1 ${2:+(-tags $2)}"
	(cd "$repo" && CGO_ENABLED=0 go build ${2:+-tags "$2"} -o "$1" ./cmd/monoagentcli)
}
if [[ -z "${MONO_BIN:-}" ]]; then
	MONO_BIN="$work/bin/monoagentcli-devaccount"
	[[ -x "$MONO_BIN" && "$(cat "$work/bin/commit" 2>/dev/null)" == "$commit" ]] || build "$MONO_BIN" devaccount
fi
if [[ -z "${MONO_PROD_BIN:-}" ]]; then
	MONO_PROD_BIN="$work/bin/monoagentcli"
	[[ -x "$MONO_PROD_BIN" && "$(cat "$work/bin/commit" 2>/dev/null)" == "$commit" ]] || build "$MONO_PROD_BIN"
fi
echo "$commit" >"$work/bin/commit"
loadgen="$work/bin/loadgen"
(cd "$repo" && CGO_ENABLED=0 go build -o "$loadgen" ./scripts/bench/vs-n8n/loadgen)

docker image inspect "$n8n_image" >/dev/null 2>&1 || docker pull "$n8n_image" >&2
docker image inspect "$base_image" >/dev/null 2>&1 || docker pull "$base_image" >&2

mono_run=(docker run --user "$uidgid" -e HOME=/data -e BROWSER=/bin/true -e "$dev_enforce"
	-v "$MONO_BIN:/usr/local/bin/monoagentcli:ro")
n8n_env=(-e N8N_DIAGNOSTICS_ENABLED=false -e N8N_VERSION_NOTIFICATIONS_ENABLED=false
	-e N8N_TEMPLATES_ENABLED=false -e N8N_PERSONALIZATION_ENABLED=false)

# ── Seed data directories (setup is not timed) ──────────────────────────────
log "seeding Mono Agent data dir"
mkdir -p "$work/seed-mono" "$work/seed-n8n"
# Every scoped call names this profile (the seed data dir is scratch, never ~/.monoagent).
profile=bench
"${mono_run[@]}" --rm -v "$work/seed-mono:/data" "$base_image" \
	monoagentcli profile create "$profile" >"$work/raw/mono-profile.txt" 2>&1 || { cat "$work/raw/mono-profile.txt" >&2; exit 1; }
"${mono_run[@]}" --rm -v "$work/seed-mono:/data" -v "$here/monoagent-workflow.json:/wf.json:ro" \
	"$base_image" monoagentcli workflow import --profile "$profile" --file /wf.json >"$work/raw/mono-import.txt" 2>&1
wf_id="$(grep -o 'as id: [0-9a-f-]*' "$work/raw/mono-import.txt" | awk '{print $3}')"
[[ -n "$wf_id" ]] || { cat "$work/raw/mono-import.txt" >&2; exit 1; }
"${mono_run[@]}" --rm -v "$work/seed-mono:/data" "$base_image" \
	monoagentcli workflow activate --profile "$profile" "$wf_id" >>"$work/raw/mono-import.txt" 2>&1
mono_scan_log "$work/raw/mono-import.txt"

log "seeding n8n data dir"
docker run --rm --user "$uidgid" "${n8n_env[@]}" -v "$work/seed-n8n:/home/node/.n8n" \
	-v "$here/n8n-workflow.json:/wf.json:ro" --entrypoint n8n "$n8n_image" \
	import:workflow --input=/wf.json >"$work/raw/n8n-import.txt" 2>&1
docker run --rm --user "$uidgid" "${n8n_env[@]}" -v "$work/seed-n8n:/home/node/.n8n" \
	--entrypoint n8n "$n8n_image" publish:workflow --id=benchWebhookSet1 >>"$work/raw/n8n-import.txt" 2>&1

# The helper container owns the network namespace both servers join, so each
# keeps its default bind and the load generator reaches them on loopback.
docker run -d --name "$net" -v "$loadgen:/usr/local/bin/loadgen:ro" "$base_image" sleep infinity >/dev/null

# ── Measurements ────────────────────────────────────────────────────────────
rss_kb() { docker top "$1" -o pid,rss | awk 'NR>1 {s+=$2} END {print s+0}'; }
cg() { docker exec "$1" cat "/sys/fs/cgroup/$2"; }
ns_of() { date -d "$1" +%s%N; }

count_done() { # count_done <tool> <datadir>
	case "$1" in
	mono) sqlite3 "$2/.monoagent/monoagent.db" "select count(*) from workflow_executions where status='SUCCESS'" ;;
	n8n) sqlite3 "$2/database.sqlite" "select count(*) from execution_entity where status='success'" ;;
	esac 2>/dev/null || echo -1
}

round() { # round <tool> <n>
	local tool=$1 n=$2 name url data
	data="$work/data-$tool-$n"
	cp -a "$work/seed-$tool" "$data"
	if [[ $tool == mono ]]; then
		name=$mono url=http://127.0.0.1:9321/webhook/bench
	else
		name=$n8n url=http://127.0.0.1:5678/webhook/bench
	fi
	local marker="$work/raw/$tool-$n.poll"
	docker exec "$net" loadgen wait -timeout 180s -url "$url" >"$work/raw/$tool-$n.ready" 2>"$marker" &
	local poller=$!
	until grep -q polling "$marker" 2>/dev/null; do sleep 0.05; done

	if [[ $tool == mono ]]; then
		"${mono_run[@]}" -d --name "$mono" --network "container:$net" "${limits[@]}" \
			-v "$data:/data" "$base_image" monoagentcli daemon >/dev/null
	else
		docker run -d --name "$n8n" --network "container:$net" "${limits[@]}" --user "$uidgid" \
			"${n8n_env[@]}" -v "$data:/home/node/.n8n" "$n8n_image" >/dev/null
	fi
	wait "$poller"
	local started ready cold
	started="$(ns_of "$(docker inspect "$name" --format '{{.State.StartedAt}}')")"
	ready="$(cat "$work/raw/$tool-$n.ready")"
	cold=$(((ready - started) / 1000000))
	log "$tool round $n: ready ${cold} ms after process start"

	sleep "$idle_wait"
	local idle_rss idle_cg
	idle_rss="$(rss_kb "$name")"
	idle_cg="$(cg "$name" memory.current)"

	# Completion is read from each tool's own database every 0.25 s (a WAL
	# reader, so it does not block the writer). A run that never lands there
	# is lost: polling stops once the count has not moved for $stall seconds,
	# and the wall time ends at the last increase.
	local base fire done_ns completed=0 now last_change count
	base="$(count_done "$tool" "$data")"
	docker exec "$net" loadgen fire -url "$url" -n "$runs" -c "$conc" >"$work/raw/$tool-$n.fire"
	done_ns="$(date +%s%N)" last_change=$(date +%s)
	while :; do
		count=$(($(count_done "$tool" "$data") - base))
		now=$(date +%s)
		if ((count > completed)); then
			completed=$count done_ns="$(date +%s%N)" last_change=$now
		fi
		((completed >= runs || now - last_change > stall)) && break
		sleep 0.25
	done
	fire="$(cat "$work/raw/$tool-$n.fire")"
	local start_ns wall
	start_ns="$(jq -r .start_ns <<<"$fire")"
	wall=$(((done_ns - start_ns) / 1000000))
	log "$tool round $n: $completed/$runs runs done in ${wall} ms"

	local load_rss peak_cg
	load_rss="$(rss_kb "$name")"
	peak_cg="$(cg "$name" memory.peak 2>/dev/null || echo null)"
	docker logs "$name" >"$work/raw/$tool-$n.log" 2>&1 || true
	[[ $tool == mono ]] && mono_scan_log "$work/raw/$tool-$n.log"
	docker rm -f "$name" >/dev/null

	jq -n --arg tool "$tool" --argjson round "$n" --argjson cold "$cold" \
		--argjson idle_rss "$idle_rss" --argjson idle_cg "$idle_cg" \
		--argjson fire "$fire" --argjson completed "$completed" --argjson wall "$wall" \
		--argjson load_rss "$load_rss" --argjson peak_cg "$peak_cg" '{
		tool: $tool, round: $round,
		cold_start_ms: $cold,
		idle_rss_kb: $idle_rss, idle_cgroup_bytes: $idle_cg,
		requests_ok: $fire.ok, requests_failed: $fire.failed, requests_ms: $fire.requests_ms,
		runs_completed: $completed, runs_missing: ($fire.sent - $completed), completion_wall_ms: $wall,
		runs_per_s: (if $wall > 0 then ($completed * 1000 / $wall * 10 | round / 10) else null end),
		after_load_rss_kb: $load_rss, peak_cgroup_bytes: $peak_cg
	}' >"$work/raw/$tool-$n.json"
}

load_before="$(cut -d' ' -f1-3 /proc/loadavg)"
for ((i = 1; i <= rounds; i++)); do
	round mono "$i"
	round n8n "$i"
done
load_after="$(cut -d' ' -f1-3 /proc/loadavg)"

# ── Sizes and versions ──────────────────────────────────────────────────────
mono_size="$(stat -c %s "$MONO_PROD_BIN")"
mono_dev_size="$(stat -c %s "$MONO_BIN")"
n8n_size="$(docker image inspect "$n8n_image" --format '{{.Size}}')"
n8n_digest="$(docker image inspect "$n8n_image" --format '{{index .RepoDigests 0}}')"
base_size="$(docker image inspect "$base_image" --format '{{.Size}}')"
n8n_version="$(docker run --rm --entrypoint n8n "$n8n_image" --version 2>/dev/null | tail -1)"
mono_describe="$(git -C "$repo" describe --tags --always --dirty)"
mono_version="$("${mono_run[@]}" --rm -v "$work/seed-mono:/data" "$base_image" monoagentcli version 2>/dev/null | head -1)"

jq -s --arg commit "$commit" --arg mono_version "$mono_version" --arg mono_describe "$mono_describe" --arg n8n_version "$n8n_version" \
	--arg n8n_image "$n8n_image" --arg n8n_digest "$n8n_digest" --arg base_image "$base_image" \
	--argjson mono_size "$mono_size" --argjson mono_dev_size "$mono_dev_size" \
	--argjson n8n_size "$n8n_size" --argjson base_size "$base_size" \
	--arg load_before "$load_before" --arg load_after "$load_after" \
	--argjson runs "$runs" --argjson conc "$conc" --argjson idle_wait "$idle_wait" \
	--arg date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" --arg kernel "$(uname -r)" \
	--arg cpu "$(lscpu | sed -n 's/^Model name: *//p' | head -1)" \
	--arg mem "$(free -h | awk '/^Mem:/ {print $2}')" '
	def median: sort | if length == 0 then null elif length % 2 == 1 then .[length / 2 | floor]
		else (.[length / 2 - 1] + .[length / 2]) / 2 end;
	def summary(t): map(select(.tool == t)) as $r | {
		cold_start_ms: ($r | map(.cold_start_ms) | median),
		idle_rss_mib: ($r | map(.idle_rss_kb) | median / 1024 * 10 | round / 10),
		idle_cgroup_mib: ($r | map(.idle_cgroup_bytes) | median / 1048576 * 10 | round / 10),
		completion_wall_ms: ($r | map(.completion_wall_ms) | median),
		runs_per_s: ($r | map(.runs_per_s) | median),
		after_load_rss_mib: ($r | map(.after_load_rss_kb) | median / 1024 * 10 | round / 10),
		peak_cgroup_mib: ($r | map(.peak_cgroup_bytes // 0) | median / 1048576 * 10 | round / 10),
		requests_timed_out_or_failed: ($r | map(.requests_failed) | add),
		runs_missing: ($r | map(.runs_missing) | add),
		all_runs_completed: ($r | all(.runs_completed >= $runs))
	};
	{
		date: $date,
		method: {
			limits: "docker run --memory 1g --memory-swap 1g --cpus 1 (both tools)",
			trigger: "POST webhook, JSON body {\"n\":i}; workflow = webhook -> set one field",
			runs_per_round: $runs, concurrency: $conc, idle_wait_s: $idle_wait,
			cold_start: "container State.StartedAt -> first 2xx from the webhook (5 ms polling)",
			throughput: "first request sent -> runs_per_round successful executions persisted in the tool database",
			medians_over_rounds: true
		},
		machine: {cpu: $cpu, memory_total: $mem, kernel: $kernel,
			loadavg_before: $load_before, loadavg_after: $load_after},
		mono_agent: {
			version: $mono_describe, commit: $commit, binary_version_output: $mono_version,
			binary_bytes: $mono_size, devaccount_binary_bytes: $mono_dev_size,
			base_image: $base_image, base_image_bytes: $base_size,
			summary: summary("mono")
		},
		n8n: {
			version: $n8n_version, image: $n8n_image, digest: $n8n_digest, image_bytes: $n8n_size,
			summary: summary("n8n")
		},
		rounds: .
	}' "$work"/raw/*.json >"$results"
mono_check_clean
log "wrote $results"
jq '{mono_agent: .mono_agent.summary, n8n: .n8n.summary}' "$results"
