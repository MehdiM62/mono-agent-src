# Agent capability evals

How well can an external agent author *working* workflows through
`monoagentcli`, from the CLI or from the MCP server? Each task is a natural
language request ("fetch this URL, keep the items over 10, write a CSV") with
deterministic checks. An agent works the task in a throwaway HOME; then a
scorer, which trusts nothing the agent said, finds the saved workflow and
validates, dry-runs and runs it with the CLI, then checks the run's output and
the files it wrote.

Everything is offline: fixtures are served from `127.0.0.1:<random port>` by
the harness, file outputs go into the sandbox, no social or messaging node is
used, and no workflow is left active.

## Run it

```bash
# Self-test, no model: score every reference solution (and the mutants, below).
scripts/evals/run.sh --reference                     # builds monoagentcli -tags devaccount first
scripts/evals/run.sh --reference --bin /path/to/monoagentcli

# Live: the built-in agent loop on an OpenRouter :free model.
OPENROUTER_API_KEY=... scripts/evals/run.sh --model nvidia/nemotron-3-super-120b-a12b:free \
  --tasks rss-read,sort-limit --modes cli,mcp
```

`--reference` (alias `--dry-check`) exits non-zero when a reference fails or
when the scorer accepts a mutant. It takes about a minute of scoring on a
laptop (`--jobs`, default 6 from run.sh), plus the build when no `--bin` is
given. It needs Go (for the build) and Python 3.8+; nothing else.

Live runs write `results/<date>-<model>.json` (every check with its failure
detail, steps, seconds, the transcript path) and the same name `.md` (the
summary table). Transcripts stay in `<scratch>/monoagent-evals-<stamp>/transcripts/`;
the sandboxes are removed unless `--keep` is given.

Options: `--tasks a,b` (default all), `--modes cli|mcp|cli,mcp` (live only,
default `cli`; a task runs only in the modes its `mode` allows),
`--max-steps` (default 30 model turns), `--max-seconds` (default 600 per task),
`--results-dir`, `--keep`.

## The account gate

A sandbox has no monoes.me sign-in, and a release build refuses gated commands
with exit 4. The harness therefore needs a `-tags devaccount` build and sets
`MONOAGENT_DEV_ENFORCE_FROM=2999-01-01T00:00:00Z`, which moves that build's
enforcement date into the future (the warn period). This is the same test seam
`scripts/tasks-mcp-smoke.sh` and `internal/accountsmoke` use; it changes
nothing in release builds. The harness probes the binary first and stops with
a clear message when it gets exit 4.

## Sandbox

Per task (and per mode) the harness creates `<scratch>/monoagent-evals-<stamp>/<task>-XXXX/`
with `home/` (HOME, XDG dirs), `work/` (the agent's cwd, where it drafts JSON),
`out/` (task output files) and `tmp/`. The scratch root is `$EVALS_SCRATCH`,
else `~/scratch` when it exists, else the system temp dir. Every CLI call runs
with a clean environment (no agent markers leak in), `BROWSER=/bin/true` and
`MONOAGENT_WEBHOOK_ADDR` on a free loopback port, so it never collides with a
daemon on 9321.

## The agent

`evallib/agent.py` is a small, deterministic loop over OpenRouter chat
completions with tool calling (temperature 0). Per mode the model gets:

- `cli`: a `monoagentcli` tool that runs the binary with an argv list (no
  shell) in the sandbox, `write_file` for a file in the work dir, and `done`.
- `mcp`: the tools of `monoagentcli mcp --allow-mutations` that author and run
  workflows (`workflow_*`, `node_*`, `docs`, `hil_*`), proxied as they are, and `done`.

Both get the same short system prompt (save under the given name, do not
activate, stay offline, validate before finishing). Only `:free` models are
accepted.

## Tasks

`tasks/<id>.json`:

```json
{
  "id": "sort-limit",                 // = file name
  "mode": "both",                     // cli | mcp | both
  "workflow_name": "eval-sort-limit", // what the scorer looks for
  "prompt": "... {{FIXTURE_URL}}/products.json ...",
  "fixtures": ["products.json"],      // documentation: files of fixtures/ it reads
  "input": {"first": "Ada"},          // optional trigger data for the scorer's run
  "notes": "...",                     // optional
  "checks": [{"type": "validate_ok"}, {"type": "run_status", "equals": "SUCCESS"}, ...]
}
```

Placeholders in prompts, checks and references: `{{FIXTURE_URL}}` (the fixture
server), `{{FIXTURE_DIR}}` (`fixtures/` on disk), `{{OUT_DIR}}` (the sandbox's
`out/`), `{{WORK_DIR}}`.

Check types (`evallib/scorer.py` has the details): `validate_ok`, `dry_run_ok`,
`run_status`, `node_types`, `node_config`, `inactive`, `output` (a `[*]` /
`[n]` / `.key` path over the run's output items, compared with `equals`, `in`,
`contains` or `count`), `file`, `mcp_validate_ok`, `mcp_run_status`. Before a
run check the scorer activates the workflow itself (a run needs an active
workflow); tasks that must not run (schedule, Human-in-Loop, read-page) have
no run checks and assert `inactive`.

| task | what it asks for |
|---|---|
| aggregate-sum | HTTP GET, sum a field with core.aggregate |
| code-transform | reshape items in a JavaScript core.code node |
| csv-roundtrip | read a CSV file, filter, write a CSV file |
| datetime-format | reformat a timestamp with data.datetime |
| dedupe | remove duplicates by a field |
| hash-field | SHA-256 a field with data.crypto |
| hil-approval | Human-in-Loop review step (validate and dry-run only) |
| html-extract | HTTP GET a page, extract the h1 with data.html |
| http-filter-csv | HTTP GET, filter price > 10, write a CSV |
| if-routing | core.if per item into two branches, merge |
| merge-sources | two parallel fetches merged |
| read-page-markdown | ai.read_page config (validate and dry-run only, see notes) |
| rss-read | RSS from the fixture server, first 2 entries |
| schedule-daily | daily 09:00 schedule + fetch (validate only, never activated) |
| schedule-only | weekday 07:30 Europe/Berlin single trigger (MCP can do this one) |
| set-template | core.set with template expressions on trigger input |
| shell-command | system.execute_command |
| sort-limit | sort numerically, keep the top 2 |
| split-batches | core.split_in_batches loop, count with aggregate |
| switch-routing | core.switch with a default branch, merge |
| webhook-set-respond | webhook trigger, set computed fields (run with --input) |
| write-text-file | write a UTF-8 text file |
| xml-parse | HTTP GET XML, parse with data.xml |

## References and mutants

`reference/<id>.json` is a known-good workflow per task, in the shape
`workflow import` reads. In reference mode each is imported into a fresh
sandbox under the task's workflow name and scored; every one must pass every
check. `MUTANTS` in `evals.py` imports a reference under another task's name
(sort-limit as aggregate-sum, and so on); the scorer must reject each, which
catches checks too loose to tell workflows apart.

Adding a task: write `tasks/<id>.json` and `reference/<id>.json`, put any new
fixture in `fixtures/`, run `scripts/evals/run.sh --reference`.

## CI

`scripts/evals/run.sh --reference` is a self-contained CI step (Go +
Python 3, no secrets, no network beyond loopback). It is not wired into
`.github/workflows` yet.
