# Mono Agent vs n8n on a 1 GB / 1 CPU box

A reproducible side-by-side of Mono Agent and n8n under the same resource
limits: idle memory, cold start, throughput of a trivial webhook workflow, and
install size. `run.sh` does everything and writes `results.json`; the numbers
below are from the committed `results.json`.

Published under LICENSE section 5. Versions tested: **Mono Agent at
`integration/research-wave` e9d76126** (`v0.135.0-1120-ge9d76126`, the build
the next release is cut from) and **n8n 2.43.4** (`docker.n8n.io/n8nio/n8n:2.43.4`,
digest `sha256:c34ee9107a42b837e25cec0c8a927ab90a45b776aa2ccbf15e5b9cda466fad2c`).

## Results (2026-10-14, median of 3 rounds, host load 6-12)

| | Mono Agent e9d76126 | n8n 2.43.4 |
|---|---:|---:|
| Idle memory, RSS of all processes, 60 s after ready | **101 MiB** | 447 MiB |
| Idle memory, container cgroup `memory.current` | 116 MiB | 468 MiB |
| Cold start, process start to first accepted webhook | **0.52 s** | 5.8 s |
| 1,000 runs, first request to last run persisted | **5.2 s** | 74 s |
| Throughput | **191 runs/s** | 13.4 runs/s |
| Peak cgroup memory during the 1,000 runs | 267 MiB | 780 MiB |
| Webhook requests that timed out (> 10 s, of 3 × 1,000) | 0 | 0 |
| Runs missing after the load (of 3 × 1,000) | 0 | 0 |
| Install size | **131 MB** static binary | 1.69 GB Docker image |

Per round (from the run log):

| Round | Tool | Cold start | 1,000 runs |
|---|---|---:|---:|
| 1 | Mono Agent | 324 ms | 5.2 s |
| 1 | n8n | 5,804 ms | 74.4 s |
| 2 | Mono Agent | 523 ms | 3.1 s |
| 2 | n8n | 4,692 ms | 88.6 s |
| 3 | Mono Agent | 523 ms | 5.6 s |
| 3 | n8n | 6,228 ms | 68.1 s |

Ratios: Mono Agent finished 1,000 runs about 14x faster, was ready about 11x
sooner, idled in about 4.4x less memory, and installs in about 13x less space.

### Earlier run: v0.135.0 (2026-10-13, busy host, load 39 -> 18)

| | Mono Agent v0.135.0 (2d2c135f) | n8n 2.43.4 |
|---|---:|---:|
| Idle memory (RSS) | 49 MiB | 448 MiB |
| Cold start | 0.22 s | 9.6 s |
| 1,000 runs | 5.8 s (171 runs/s) | 129 s (7.7 runs/s) |
| Peak cgroup memory | 204 MiB | 787 MiB |
| Install size | 88.6 MB | 1.69 GB |

Between v0.135.0 and e9d76126 (the research wave: 370+ node types incl. ~240
embedded API packages, the n8n importer), Mono Agent's idle memory went from
49 to 101 MiB, cold start from 0.22 to 0.52 s and the binary from 88.6 to
131 MB. That is the cost of the larger built-in catalog; it is reported as a
follow-up (load the API-package catalog lazily).

## Method

**Same limits for both.** Each server runs in its own container started with
`docker run --memory 1g --memory-swap 1g --cpus 1` (1 GiB RAM, no swap, one
CPU). The two are never running at the same time: rounds alternate Mono Agent,
n8n, Mono Agent, … and each container is removed before the next starts.

- **Mono Agent**: the binary built from this checkout
  (`CGO_ENABLED=0 go build -tags devaccount ./cmd/monoagentcli`, see Account
  gate) bind-mounted into `alpine:3.22` and started as `monoagentcli daemon`
  with default flags (HTTP API, extension bridge and webhook server all on).
  SQLite in `HOME=/data`, a fresh copy of a seeded data dir per round.
- **n8n**: the official image with its default command and default SQLite
  database, telemetry off (`N8N_DIAGNOSTICS_ENABLED`,
  `N8N_VERSION_NOTIFICATIONS_ENABLED`, `N8N_TEMPLATES_ENABLED`,
  `N8N_PERSONALIZATION_ENABLED` all `false`); everything else default. Fresh
  copy of a seeded `.n8n` dir per round.

**Workflow.** Webhook trigger (POST `/webhook/bench`) → set one string field
(`greeting = "hello <n>"`) → done. `monoagent-workflow.json` and
`n8n-workflow.json` are the two files used. n8n's webhook uses
`responseMode: onReceived`, so both tools answer the request and run the
workflow asynchronously. Setup is not timed: Mono Agent's workflow is added
with `monoagentcli workflow import` + `workflow activate`, n8n's with
`n8n import:workflow` + `n8n publish:workflow`, each in a throwaway container.

**Network.** A helper container (`vsn8n-net`) owns the network namespace and
each server joins it (`--network container:vsn8n-net`), so both keep their
default bind (Mono Agent's webhook server is loopback-only by default) and
nothing is published on the host. The load generator (`loadgen/`, a small Go
program) runs in the helper container, outside the 1 CPU limit.

**Measurements.**

1. *Idle memory*: 60 s after the server first accepts a webhook, the sum of
   the RSS of every process in the container (`docker top -o pid,rss`; n8n
   runs a main process plus its JS task runner) and the container's cgroup
   `memory.current`.
2. *Cold start*: from the container's `State.StartedAt` (the moment the
   process starts, so container creation is excluded) to the first 2xx answer
   on the webhook URL, polled every 5 ms by a poller started before the
   container. The data dir is already migrated, so this is a restart, not a
   first install.
3. *Throughput*: 1,000 POSTs with body `{"n": i}` from 10 concurrent callers
   (10 s client timeout). The clock runs from the first request to the moment
   the 1,000th successful execution is recorded in the tool's own database
   (`workflow_executions.status = 'SUCCESS'` / `execution_entity.status =
   'success'`), polled every 0.25 s with `sqlite3` from the host (a WAL
   reader, which does not block the writer). Peak memory is the cgroup's
   `memory.peak` after the load.
4. *Install size*: the size of the default-tag release-style binary
   (`go build ./cmd/monoagentcli`, CGO off) and `docker image inspect .Size`
   of the n8n image and the Alpine base.

## Machine

```
Architecture:       x86_64
CPU(s):             24
Model name:         AMD Ryzen AI 9 HX 470 w/ Radeon 890M
Thread(s) per core: 2
Core(s) per socket: 12
CPU max MHz:        5297.2979
L3 cache:           24 MiB (2 instances)

               total        used        free      shared  buff/cache   available
Mem:            27Gi        16Gi       2.0Gi       106Mi       9.9Gi        10Gi
Swap:           54Gi        16Gi        37Gi
```

Linux 7.2.5 (cgroup v2), Docker 29.8.1, Go 1.27.1.

## Reproduce

```bash
# from the repository root; needs docker, jq, sqlite3, go
BENCH_WORK=~/scratch/bench-vs-n8n scripts/bench/vs-n8n/run.sh
```

It builds both binaries (or takes `MONO_BIN` / `MONO_PROD_BIN`), pulls the
pinned n8n image if missing, seeds both data dirs under `BENCH_WORK`, runs
`ROUNDS` (3) rounds of each tool and writes `results.json` here. Raw output
(poller files, load generator summaries, server logs) stays in
`$BENCH_WORK/raw`. Every container it starts is called `vsn8n-*` and removed
on exit; the images are kept. Knobs: `ROUNDS`, `RUNS`, `CONC`, `IDLE_WAIT`,
`STALL`, `N8N_IMAGE`, `RESULTS` (see the header of `run.sh`).

No Mono Agent command runs on the host: the setup commands and the daemon all
run in containers with `HOME=/data` on the work dir, so `~/.monoagent` is never
touched.

## Account gate

Since 2026-10-11 most `monoagentcli` commands need a monoes.me sign-in, and
the daemon drops every trigger while the account is locked, so the stock
binary cannot run this benchmark without signing in. The benchmark uses the
repository's existing development override, the same one
`scripts/library-official.sh`, `scripts/tasks-mcp-smoke.sh` and
`scripts/e2e/*.sh` use: a build with the `devaccount` tag and
`MONOAGENT_DEV_ENFORCE_FROM=2999-01-01T00:00:00Z`, which moves the enforcement
date into the future so the binary runs in the warn period. The tag only
changes three things in `internal/account` (the trusted signing key, the
monoes.me host and this date override); the workflow engine is the same code,
and the two binaries differ by 337 bytes. The release guard
(`scripts/check-release-tags.sh`) keeps this tag out of shipped binaries, and
the reported install size is the default-tag binary's. No product code is
changed or bypassed.

## Caveats

- **Shared host.** The machine was shared with other work in both runs: load
  average 12 -> 8 for the 2026-10-14 run (24 CPUs), 39 -> 18 for the
  2026-10-13 run. `--cpus 1` caps a container at one CPU but does not
  guarantee it one, so absolute numbers, throughput above all, are noisy and
  probably pessimistic for both. Re-run on an idle 1 GB VM for headline
  numbers; the ratios are the robust part.
- **Defaults, not tuning.** Mono Agent runs at most 3 workflows at once by
  default (`EngineConfig.MaxConcurrent`); n8n's regular mode has no
  concurrency cap. n8n's queue mode with Redis and workers, Postgres, or
  `N8N_RUNNERS_MODE=external` would change its numbers and its footprint; none
  of that fits a 1 GB box with the defaults measured here.
- **n8n after ready.** For several seconds after n8n first accepts a webhook
  it still answers some concurrent requests with 404 "webhook is not
  registered" (seen in trial runs with a 5-10 s wait). The 60 s idle wait is
  past that window: no request got a 404 in the measured rounds.
- **n8n timeouts.** In round 2 of the 2026-10-13 run (none on 2026-10-14), 98 of n8n's requests took longer than the
  10 s client timeout to be acknowledged; n8n still ran all of them (1,000
  executions recorded), so throughput counts them.
- **Mono Agent under heavy contention.** In a 200-run trial while the host load
  average was about 160, Mono Agent acknowledged every webhook with 200 but
  lost 4 runs: creating the execution record failed with `database is locked
  (SQLITE_BUSY)` after the 5 s busy timeout (`engine: handleTrigger: failed to
  create execution record`). It did not happen in the measured rounds. A
  webhook that answers 200 for a run that is never recorded is reported as a
  product issue.
- **Container overhead excluded.** Cold start starts at process start.
  Container creation by `docker run` took 15-40 s on this busy host whatever
  the image, which says more about the host than about either tool.
- RSS counts shared file-backed pages (the 131 MB binary, node's modules);
  cgroup memory is the container's own charge. Both are reported.
- The load generator, the poller and `sqlite3` run outside the limited
  container and use the host's other CPUs.
