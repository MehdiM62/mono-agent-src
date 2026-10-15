# HTTP API quickstart

`monoagentcli httpapi` starts a REST/JSON server for external agents that
can't speak the stdio MCP protocol (`monoagentcli mcp`). Read-only by
default; loopback-only bind (`127.0.0.1:9322`). Full endpoint list:
`internal/httpapi/openapi.yaml`.

## Start the server

```bash
monoagentcli httpapi
# HTTP API listening on 127.0.0.1:9322 (mutations disabled — read-only). Press Ctrl+C to stop.
```

One server serves every profile; a request names its profile in the path
(`/profiles/<profile>/...`, the profile's id or name). Each profile has its
own bearer token, generated when the server starts and stored in that
profile's secrets vault:

```bash
monoagentcli --profile work secret list | grep httpapi-token
```

Reveal it with `monoagentcli --profile work secret reveal httpapi-token --reveal --json`
(the `--reveal` flag is a deliberate confirmation step, not optional), or
use `secret export` for a scripted bulk-export flow.

A token opens only the profile it belongs to. A token of another profile, a
profile that does not exist and a missing token all get the same `401`, so a
caller cannot find out which profiles exist. Workflow reads and mutations
are scoped to the profile in the path, including when
`monoagentcli daemon --allow-mutations` hosts the API: another profile's
workflow returns 404. Legacy workflows without a profile ID belong to
`default`. The daemon's own triggers serve all profiles.

The unprefixed `/workflows*` and `/hil*` routes of earlier releases are gone:
they answer `404` with a `hint` naming the new path.

## List workflows, then run one

With the token in `$TOKEN`:

```bash
# 1. List saved workflows
curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:9322/profiles/work/workflows | jq .

# [
#   { "id": "wf-001", "name": "Morning briefing", "active": true, "node_count": 5 }
# ]

# 2. Run one (mutating endpoints need --allow-mutations on the server)
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"input": {"prompt": "a red bicycle"}, "timeout_seconds": 60}' \
  http://127.0.0.1:9322/profiles/work/workflows/wf-001/run | jq .

# {
#   "execution_id": "exec-042",
#   "status": "SUCCESS",
#   "nodes": [ ... ]
# }
```

Output items are redacted the same way as `workflow run --json` and the
MCP tools (credential-shaped keys like `token`/`password`/`api_key` are
masked). To see unredacted values for one call, add
`-H "X-Full-Outputs: 1"`, mirroring `workflow run --full-outputs`.

## Unauthenticated / unauthorized requests

```bash
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:9322/profiles/work/workflows
# 401

curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer wrong" http://127.0.0.1:9322/profiles/work/workflows
# 401

# work's token on another profile's path (or on a profile that does not exist): the same 401
curl -s -o /dev/null -w '%{http_code}\n' -H "Authorization: Bearer $TOKEN" http://127.0.0.1:9322/profiles/home/workflows
# 401

curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:9322/health
# 200 — /health is the only unauthenticated route
```
