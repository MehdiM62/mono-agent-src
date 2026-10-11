# CLAUDE.md

## Project

mono-agent (`github.com/monoes/mono-agent`) — workflow automation engine that runs on your machine (n8n alternative) in a single Go binary: `monoagentcli`.
Full agent guidance lives in **AGENTS.md** (root) — read that first.

## Build / test / lint

```bash
go build ./...                        # build everything
go build ./cmd/monoagentcli           # the CLI binary
go build -tags nosocial ./cmd/monoagentcli # opt-out build without social platform nodes
go test ./...                         # run tests (no Chrome required)
go vet ./...                          # lint
gofmt -l .                            # formatting check
```

The desktop GUI (`wails-app/`) is optional and needs the Wails toolchain.

## Notes

- Default builds include the social platform nodes (Instagram, LinkedIn, X,
  TikTok, Hacker News, Product Hunt); `-tags nosocial` leaves them out.
- CLI state lives in `~/.monoagent/` (global, not per-repo).
- Gated commands need a monoes.me sign-in (enforced since 2026-10-11 UTC). For
  development build the real binary with `-tags devaccount` and run it with
  `MONOAGENT_DEV_ENFORCE_FROM=2999-01-01T00:00:00Z` so that nothing locks (see
  CONTRIBUTING.md); never ship that binary.
- Commit messages: conventional style (`type(scope): description`).
- Never commit secrets or `.env` files.
# monomind:start instructions:claude
# Monomind

Use the `monomind` MCP tools for graph navigation, impact analysis, memory, and organization work.
For multi-step work, load only the applicable `mastermind-*` skill; do not load all workflows at once.
If MCP is unavailable, run `npx -y monomind@latest doctor` and use `npx -y monomind@latest` commands.
# monomind:end instructions:claude
