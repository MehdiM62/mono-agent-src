# Strict Explicit Profile Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `Skill("mastermind-execute")` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking. This is a **program plan**: Phase 1 is fully specified (code, tests, commands); each later phase lists exact files, the change, and its acceptance test, and gets its own detailed plan before it starts (see "Scope check").

**Goal:** Every CLI command, MCP tool, HTTP API call and GUI-to-CLI call names the profile it acts on. There is no active profile, no `profile switch`, no `MONOAGENT_PROFILE`, no `"default"` fallback and no `COALESCE(profile_id,'default')`. A missing profile is an error, so an agent can never write to the wrong profile.

**Architecture:** One fail-closed classification table (the same idea as `internal/cli/accountgate`) marks every command `scoped` or `global`; a `PersistentPreRunE` gate refuses a scoped command that has no `--profile`. The gate lands first with a one-release warn mode, the repo's own callers are fixed against it, MCP and HTTP get per-call profiles, the data fallbacks are removed, then the gate flips to enforce and the old selection mechanism is deleted.

**Tech Stack:** Go (cobra CLI, `internal/mcp`, `internal/httpapi`), SQLite migrations in `data/migrations/`, Wails app (`wails-app/`, Go + React), Chrome extension (`chrome-extension/`).

## Global Constraints

- Source of truth is `monoes/mono-agent-src` `origin/master`; the inventory below was taken at `4107e065` (before PR #799 merged). Re-confirm file:line references against the branch you start from.
- A profile is named by id or by name (`resolveProfileID`, `cmd/monoagentcli/root.go`); an ambiguous name is an error, never a guess.
- The profile with id `default` stays a valid profile. Only the implicit fallback to it goes away.
- `_machine` stays reserved as a profile **id** only (`profiledir.ValidProfileID`).
- No environment variable selects a profile (decision 2026-10-14: `MONOAGENT_PROFILE` is dropped).
- The GUI keeps "the profile I am looking at" as UI state only; every CLI call it makes passes `--profile`. The CLI and MCP never learn what the GUI shows.
- Legacy rows with NULL/empty `profile_id` are stamped with the profile whose id is `default` if it exists, else the oldest profile.
- Machine-wide data stays machine-wide: `templates`, `configs`, `settings` (minus profile-qualified keys), `agent_model_*`, `automation_selector_health`, the monoes.me account, `api config`. Those commands are `global` and take no profile.
- Keep files under 500 lines. Conventional commits (`type(scope): description`). No attribution lines. Run the pinned formatter before pushing: `$(GOTOOLCHAIN=go1.26.9 go env GOROOT)/bin/gofmt -l .`
- Branch builds must not touch the real database: run CLI/tests with a scratch `HOME` under `~/scratch` (never `/tmp`).
- A push to `master` is a release. Every phase is one PR to an `integration/strict-profile-*` branch; the final enforce flip is its own release with a version bump and release notes.

## Open decisions (need an answer before the phase that depends on them)

| # | Decision | Recommendation | Blocks |
|---|---|---|---|
| D1 | How the daemon-hosted HTTP API (`internal/httpapi`) learns the profile | Per request, as a path segment: `/profiles/{profile}/workflows/...`. It shows in logs and cannot be forgotten. `/health`, `/nodes` stay global. The OpenAI-compatible API is unchanged (the profile comes from the API-key row). | Phase 3 |
| D2 | Extension captures with no chosen profile (today: shared inbox) | Keep the shared inbox as an explicit **Unassigned** pile that a person files into a profile; the extension picker shows "no profile chosen" instead of silently marking one default. Nothing is written into a profile without a choice. | Phase 5 |
| D3 | `templates` (message templates) | Stay global (machine-wide), confirmed here as a deliberate exception. | none, record it |
| D4 | Cross-profile commands (`--all-profiles`, `workflow migrate`, `org reconcile`, `chat history reconcile`) | Keep, but they must be spelled explicitly (`--all-profiles`); an empty profile never means "all". | Phase 1 |

## Scope check

This spans six independent subsystems (CLI gate, repo callers, MCP/HTTP, data, GUI/extension, release). Write one detailed plan per phase, in order; do not start a phase before the previous PR is merged to the integration branch. Phase 1 below is detailed enough to execute now.

## Inventory (what exists today, at `4107e065`)

- **One implicit resolver for the CLI:** `initDB` (`cmd/monoagentcli/root.go`, ~line 227) reads `settings.active_profile_id`, falls back to `"default"`, stores it in `cfg.ProfileID`. It has 46 callsites in 40 files, **including about 25 global commands** (agent, config, message-template, workflow validate, library list, api config, …). A "profile required" check therefore cannot live in `initDB`.
- **MCP and HTTP:** `internal/mcp/engine.go:211-229` and `internal/httpapi/runtime.go:190-209` resolve `--profile` → `MONOAGENT_PROFILE` → setting → `"default"` once at startup. 0 of 80 MCP tools take a profile argument.
- **Fallbacks:** ~97 non-test `profileID = "default"` assignments, `vault.ProfileIDFromContext` returns `"default"` (34 callers), and `COALESCE(profile_id,'default')` in ~40 queries plus an expression index (`026`).
- **Callers that name no profile:** GUI Go (`wails-app/app_orgs.go:84,250,398`, `app_orgs_design_save.go:203`; in-process DB reads keyed by `getActiveProfileID()`, 52 calls in 25 files), Chrome extension (`capture_profile.js`, `sidepanel*.js`, `extension/chat_cli.go`, `recording_methods.go`), autostart units (`doctor/env_services.go:279-288`), `scripts/e2e/*.sh`, `scripts/*smoke*.sh`, `Dockerfile`/`docker-compose.yml`, `AGENTS.md`, `README.md`, `data/skills/*.md`.
- **Known scoping bugs found on the way:** `hil recipient` reads `people WHERE id=?` unscoped (`internal/cli/hil/send.go:~290`); three different rules for an empty workflow owner (HTTP: `"default"`; MCP and CLI: accessible by anyone); `ListWorkflows("")` means "every profile"; `workflow_daily_counters.profile_key` is unexplained; `threads` has `profile_id` but no live writer.

## File Structure (whole program)

| Path | Responsibility |
|---|---|
| `internal/cli/profilegate/classes.go` (new) | The scoped/global table, `Class(cmd)`, nearest-listed-ancestor lookup, fail-closed default |
| `internal/cli/profilegate/gate.go` (new) | `Check(cmd, profileFlagSet, mode) error`: judges one invocation, builds the "pass --profile" error with the profile list |
| `internal/cli/profilegate/*_test.go` (new) | Table-vs-real-command-tree test, gate behaviour tests |
| `cmd/monoagentcli/profile_gate.go` (new) | Installs the gate in the root `PersistentPreRunE`; wires profile listing |
| `cmd/monoagentcli/root.go` | `initDB` stops resolving a profile; `--profile` help text |
| `internal/cli/profile/profile.go` | Remove `switch`/`current`, `Active` field |
| `internal/mcp/*`, `internal/httpapi/*` | Required `profile` argument / path segment |
| `data/migrations/098_*.sql` … | Stamp legacy rows, remove the active setting |
| `wails-app/*`, `chrome-extension/*` | Pass `--profile` explicitly; own selection state |

---

## Phase 0: Prerequisites

### Task 0.1: Land PR #799 and re-base the inventory

**Files:** none (process)

- [ ] **Step 1:** Confirm PR https://github.com/monoes/mono-agent-src/pull/799 is merged to `master` (it removes the per-profile Jev key and scopes `posts`, which the gate work touches).
- [ ] **Step 2:** `git fetch origin && git checkout -b integration/strict-profile-1 origin/master`.
- [ ] **Step 3:** Re-run the four greps that produced the inventory and diff against the numbers above; update this plan if they moved:

```bash
git grep -c "active_profile_id" -- '*.go' | grep -v _test
git grep -n 'profileID = "default"\|ProfileID = "default"' -- '*.go' | grep -v _test | wc -l
git grep -n "COALESCE(profile_id" -- '*.go' '*.sql' | grep -v _test | wc -l
git grep -n "MONOAGENT_PROFILE" -- '*.go' '*.md' '*.sh' | grep -v _test
```

### Task 0.2: Fix the unscoped read in `hil recipient`

**Files:**
- Modify: `internal/cli/hil/send.go` (the `people WHERE id=?` read near line 290)
- Test: `internal/cli/hil/send_test.go`

- [ ] **Step 1:** Write a failing test: two profiles, a person in profile B, `hil recipient` run for profile A with B's person id must return not-found.
- [ ] **Step 2:** Run `go test ./internal/cli/hil/ -run TestRecipientIsProfileScoped -count=1`; expect FAIL.
- [ ] **Step 3:** Add `AND profile_id = ?` to the query with the command's profile.
- [ ] **Step 4:** Re-run; expect PASS. Commit `fix(hil): scope the recipient lookup to the profile`.

### Task 0.3: Resolve the three unknown tables

**Files:** none (investigation, results go into this plan)

- [ ] **Step 1:** Find what `workflow_daily_counters.profile_key` encodes (`internal/nodes/action_storage.go:~214-233`); record whether it is a profile id.
- [ ] **Step 2:** Confirm `threads` has no writer: `git grep -n "INTO threads\|FROM threads" -- '*.go'`.
- [ ] **Step 3:** Record answers in the Inventory section; if `profile_key` is not a profile id, add a task to Phase 4 to key it by `profile_id`.

---

## Phase 1: The CLI gate (warn mode)

Goal: every scoped command without `--profile` prints a clear warning and still runs (one release), every global command is untouched, and the classification of all ~479 commands is held to the real tree by a test.

### Task 1.1: The classification table

**Files:**
- Create: `internal/cli/profilegate/classes.go`
- Create: `internal/cli/profilegate/classes_test.go`

**Interfaces:**
- Produces: `profilegate.Annotation string`, `profilegate.ClassScoped = "scoped"`, `profilegate.ClassGlobal = "global"`, `profilegate.Class(c *cobra.Command) string`, `profilegate.Listed() map[string]string`, `profilegate.CommandKey(c *cobra.Command) string`.

- [ ] **Step 1: Write the failing test.**

```go
package profilegate

import (
	"testing"

	"github.com/spf13/cobra"
)

func tree() *cobra.Command {
	root := &cobra.Command{Use: "monoagentcli"}
	people := &cobra.Command{Use: "people"}
	people.AddCommand(&cobra.Command{Use: "list"})
	cfg := &cobra.Command{Use: "config"}
	cfg.AddCommand(&cobra.Command{Use: "get"})
	unknown := &cobra.Command{Use: "brand-new"}
	root.AddCommand(people, cfg, unknown)
	return root
}

func find(root *cobra.Command, path ...string) *cobra.Command {
	c, _, _ := root.Find(path)
	return c
}

func TestClassFollowsNearestListedAncestor(t *testing.T) {
	root := tree()
	if got := Class(find(root, "people", "list")); got != ClassScoped {
		t.Fatalf("people list = %q, want scoped", got)
	}
	if got := Class(find(root, "config", "get")); got != ClassGlobal {
		t.Fatalf("config get = %q, want global", got)
	}
}

func TestAnUnlistedCommandIsScoped(t *testing.T) {
	if got := Class(find(tree(), "brand-new")); got != ClassScoped {
		t.Fatalf("unlisted command = %q, want scoped (fail closed)", got)
	}
}
```

- [ ] **Step 2:** `go test ./internal/cli/profilegate/ -count=1`; expect FAIL (package does not compile).
- [ ] **Step 3: Write the implementation.** The table lists **global** paths only (everything else is scoped); copy the shape of `internal/cli/accountgate/classes.go` (`CommandKey`, nearest-listed ancestor, `Listed()` copy).

```go
package profilegate

import "github.com/spf13/cobra"

const (
	Annotation  = "monoagent.profile"
	ClassScoped = "scoped"
	ClassGlobal = "global"
)

// globalClasses lists the commands that act on the machine, not on a profile.
// A command takes the class of its nearest listed ancestor. A command that
// nothing lists is scoped, so a command added later demands --profile until
// someone decides it is global here.
var globalClasses = map[string]string{
	"version": ClassGlobal, "update": ClassGlobal, "release": ClassGlobal,
	"help": ClassGlobal, "ref": ClassGlobal, "init": ClassGlobal,
	"nodejs": ClassGlobal, "integration": ClassGlobal, "action": ClassGlobal,
	"account": ClassGlobal, "agent": ClassGlobal, "config": ClassGlobal,
	"message-template": ClassGlobal, "api config": ClassGlobal,
	"automation": ClassGlobal, "db": ClassGlobal, "extension": ClassGlobal,
	"profile list": ClassGlobal, "profile create": ClassGlobal, "profile get": ClassGlobal,
	"profile folder": ClassGlobal, "profile move": ClassGlobal, "profile projects": ClassGlobal,
	"node list": ClassGlobal, "node palette": ClassGlobal, "node schema": ClassGlobal, "node curl": ClassGlobal,
	"library list": ClassGlobal, "library show": ClassGlobal,
	"workflow validate": ClassGlobal, "workflow templates list": ClassGlobal, "workflow templates show": ClassGlobal,
	"secret keyring": ClassGlobal, "coder status": ClassGlobal, "coder enable": ClassGlobal,
	"coder disable": ClassGlobal, "coder set": ClassGlobal,
	"daemon install": ClassGlobal, "daemon uninstall": ClassGlobal, "daemon restart": ClassGlobal,
}

func CommandKey(c *cobra.Command) string {
	if !c.HasParent() {
		return ""
	}
	parts := []string{}
	for p := c; p.HasParent(); p = p.Parent() {
		parts = append([]string{p.Name()}, parts...)
	}
	return joinKey(parts)
}

func Class(c *cobra.Command) string {
	for p := c; p != nil && p.HasParent(); p = p.Parent() {
		if _, ok := globalClasses[CommandKey(p)]; ok {
			return ClassGlobal
		}
	}
	return ClassScoped
}

func Listed() map[string]string {
	out := make(map[string]string, len(globalClasses))
	for k, v := range globalClasses {
		out[k] = v
	}
	return out
}
```

(`joinKey` is `strings.Join(parts, " ")`; import `strings`.) The list above is the starting point from the inventory; Task 1.3 corrects it against the real tree.

- [ ] **Step 4:** Re-run the test; expect PASS. Commit `feat(profilegate): classify commands as scoped or global`.

### Task 1.2: The gate

**Files:**
- Create: `internal/cli/profilegate/gate.go`
- Create: `internal/cli/profilegate/gate_test.go`

**Interfaces:**
- Consumes: `Class`, `CommandKey` from Task 1.1.
- Produces: `type Mode int` with `ModeWarn`, `ModeEnforce`; `type Lister func() ([]ProfileRef, error)`; `type ProfileRef struct{ ID, Name string }`; `func Check(c *cobra.Command, flagSet bool, mode Mode, list Lister) (warning string, err error)`.

- [ ] **Step 1: Write the failing tests.**

```go
func TestScopedWithoutProfileIsRefusedWhenEnforcing(t *testing.T) {
	list := func() ([]ProfileRef, error) { return []ProfileRef{{ID: "a1", Name: "Work"}}, nil }
	_, err := Check(find(tree(), "people", "list"), false, ModeEnforce, list)
	if err == nil || !strings.Contains(err.Error(), "--profile") || !strings.Contains(err.Error(), "Work") {
		t.Fatalf("want an error naming --profile and the available profiles, got %v", err)
	}
}

func TestScopedWithoutProfileWarnsInWarnMode(t *testing.T) {
	w, err := Check(find(tree(), "people", "list"), false, ModeWarn, nil)
	if err != nil || w == "" {
		t.Fatalf("want a warning and no error, got %q / %v", w, err)
	}
}

func TestGlobalNeverNeedsAProfile(t *testing.T) {
	w, err := Check(find(tree(), "config", "get"), false, ModeEnforce, nil)
	if err != nil || w != "" {
		t.Fatalf("global command must pass silently, got %q / %v", w, err)
	}
}

func TestScopedWithProfilePasses(t *testing.T) {
	if _, err := Check(find(tree(), "people", "list"), true, ModeEnforce, nil); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2:** `go test ./internal/cli/profilegate/ -count=1`; expect FAIL (`Check` undefined).
- [ ] **Step 3: Implement.** `Check` returns early for global commands and for help/completion; for a scoped command without the flag it builds the message `"this command acts on a profile: pass --profile <id|name>. Profiles: Work (a1), Home (b2)"` (listing is best-effort; if `list` is nil or fails, omit the list). `ModeWarn` returns it as the warning with `err == nil`; `ModeEnforce` returns it as an error.
- [ ] **Step 4:** Re-run; expect PASS. Commit `feat(profilegate): refuse scoped commands that name no profile`.

### Task 1.3: Hold the table to the real command tree

**Files:**
- Create: `cmd/monoagentcli/profile_gate_test.go`

- [ ] **Step 1: Write the test.** Build the real root (`newRootCmd(cfg)`), walk every command, and (a) fail if any `profilegate.Listed()` key matches no real command (typo guard), (b) print every **scoped** command that never reads `cfg.ProfileID`/`Deps.ProfileID` is NOT feasible statically, so instead assert the inventory's counts: the number of global leaf commands equals the expected constant and update it deliberately when it changes.
- [ ] **Step 2:** Run `go test ./cmd/monoagentcli/ -run TestProfileGateTable -count=1`; fix the table until green. Resolve every "M" (mixed) command from the inventory by splitting it (`jev` is scoped except `jev key`, `api` is scoped except `api config`, `coder` is global except `coder workspace`/`coder stop-background`, `daemon` runs per profile only through its own per-profile engines — classify with the owner of the daemon work).
- [ ] **Step 3:** Commit `test(profilegate): hold the table to the command tree`.

### Task 1.4: Install the gate and stop `initDB` choosing a profile (warn mode)

**Files:**
- Create: `cmd/monoagentcli/profile_gate.go`
- Modify: `cmd/monoagentcli/root.go` (`PersistentPreRun` → also call the gate; `--profile` help text; `initDB` profile block)
- Test: `cmd/monoagentcli/profile_gate_test.go`

**Interfaces:**
- Consumes: `profilegate.Check`, `profilegate.Lister`.

- [ ] **Step 1:** Write a failing end-to-end test with a scratch DB: `people list` with no `--profile` exits 0 and writes the warning to stderr (warn mode); `config list` writes nothing.
- [ ] **Step 2:** Run it; expect FAIL.
- [ ] **Step 3:** In `profile_gate.go`, a function `installProfileGate(root *cobra.Command, cfg *globalConfig)` that records whether `--profile` was set (`cmd.Flags().Changed("profile")`) before `initDB` runs, and calls `profilegate.Check` with a lister reading `profiles`. In `root.go`, change the flag help to `"Profile to act on (id or name); required by commands that act on a profile"`.
- [ ] **Step 4:** In warn mode `initDB` still falls back (unchanged behaviour) but only for commands where the flag was not set; the warning says "falling back to the active profile; this becomes an error in the next release".
- [ ] **Step 5:** Run `go build ./... && go vet ./... && go test ./cmd/monoagentcli/ ./internal/cli/profilegate/ -count=1`; expect PASS. Commit `feat(cli): warn when a profile-scoped command names no profile`.

**Phase 1 acceptance:** all tests green; running any scoped command without `--profile` prints the warning on stderr and works; global commands print nothing; the table test passes.

---

## Phase 2: Fix every in-repo caller (still warn mode)

One PR per area; each ends with the area's own tests plus a run of its commands under warn mode showing **no warnings**.

| Task | Files | Change | Acceptance |
|---|---|---|---|
| 2.1 GUI Go | `wails-app/app_orgs.go:84,250,398`, `app_orgs_design_save.go:203`, every `runMonoCLI`/`rawCLI`/`cliJSON` call that omits `--profile`; `app_profiles.go:141` | Always pass `--profile <id>`; org calls get `--profile` besides `--project`. In-process DB readers (`app_tasks*.go`, `app_documents_watch.go`, `app_images*.go`, `app_monomind_init.go`, `main.go:74-76`) take the profile id as a parameter from the GUI's own selection | GUI run under warn mode: stderr contains no gate warning (`go test ./wails-app/...`) |
| 2.2 Extension | `chrome-extension/capture_profile.js`, `sidepanel*.js`, `recorder_wiring.js`; `internal/extension/chat_cli.go:55-59`, `recording_methods.go:133-141`, `profile_list.go:84-89` | Every CLI-backed request carries a profile or is refused with a clear message; `profile.list` stops marking a default; picker shows "choose a profile" (decision D2) | Extension unit tests + `scripts/e2e/capture-ask.sh` green |
| 2.3 Daemon/autostart | `internal/cli/doctor/env_services.go:279-288`, `internal/cli/daemon/daemon.go:31,249`, `orgsvc/services.go:35-55` | `daemonArgs` always writes `--profile`; the daemon runs workflows under each workflow's own profile and takes no implicit one | `go test ./internal/cli/daemon/ ./internal/cli/doctor/` |
| 2.4 Library, templates, nodes | node `profile_id` config defaults (`internal/nodes/applications/*`, `discovery/search_jobs.go`, `people/save*.go`, `documents/render.go` and the 10 JSON schemas in `internal/workflow/schemas/`) | Remove `default=default`; the engine supplies the execution's profile. Re-generate schemas (`Generated node schemas up to date` CI job) | CI schema job + `go test ./internal/nodes/...` |
| 2.5 Scripts/CI/Docker | `scripts/e2e/*.sh`, `scripts/doctor-smoke.sh`, `scripts/tasks-mcp-*.sh`, `scripts/evals/evallib/*`, `Dockerfile`, `docker-compose.yml`, `.github/workflows/ci.yml` | Add `--profile` to every scoped call (create a named test profile in setup) | all CI jobs green with the gate in warn mode and a CI step that fails on any gate warning |
| 2.6 Docs and skills | `AGENTS.md` (163 invocations, 16 with `--profile`), `README.md`, `data/skills/monoagent-*.md`, `internal/cli/ref/*`, `internal/mcp/docs.go:61`, `internal/health/core.go:141`, `internal/i18n/locales/{en,es}.json` | Every scoped example shows `--profile`; remove `profile switch`/`profile use`/"active profile" wording | `git grep -n "active profile\|profile switch\|MONOAGENT_PROFILE" -- '*.md' '*.json' '*.go'` returns only history docs |

---

## Phase 3: MCP and HTTP per-call profiles

### Task 3.1: MCP tools take a required `profile`

**Files:** `internal/mcp/engine.go:211-229` (drop `MONOAGENT_PROFILE`, the setting, `"default"`), `internal/mcp/tools*.go`, `task_tools.go`, `monoagent_tools.go`, `internal/ai/chat/monoagent_tools*.go` (constructor `profileID:"default"` at `:57`, `tools.go:29`), `data/skills/monoagent-tasks.md`.

- [ ] **Step 1:** Failing test: `tools/list` over all 80 tools — every tool not in the global set (`node_list`, `node_schema`, `docs`, `workflow_template_list/show`, `template_list`, `api_models_list`, `api_status`, `api_config_*`) has `profile` in `required`; a call without it returns an invalid-input error naming the profiles.
- [ ] **Step 2:** Implement the argument in the shared tool-definition helper so a new tool cannot omit it; resolve with `resolveProfileID` per call; delete `rt.profileID`.
- [ ] **Step 3:** `--grant` servers keep `--profile` and check it against the grant row; `grantScope` must reject a call whose `profile` differs from the grant's.
- [ ] **Step 4:** The chat-tool constructors require a profile (no default); `SetProfileID` callers (`mcp/engine.go:66`, `cli/chat/chat_run.go:205,221`) become constructor arguments so a forgotten call cannot compile.
- [ ] **Step 5:** `go test ./internal/mcp/... ./internal/ai/chat/...`; commit per step.

### Task 3.2: HTTP API per-request profile (decision D1)

**Files:** `internal/httpapi/server.go:129-144`, `runtime.go:190-209`, `handlers*.go`, OpenAPI spec (CI job "Validate HTTP API OpenAPI spec").

- [ ] Routes become `/profiles/{profile}/workflows…` and `/profiles/{profile}/hil…`; `workflowInProfile` takes the path profile; `/health` and `/nodes*` stay global. Tests: a request without a profile is 404; a workflow of another profile is 404. Update the OpenAPI document and the daemon's `Profile:` option (`daemon.go:249`).

---

## Phase 4: Data — remove the fallbacks

### Task 4.1: Stamp legacy rows (migration `098`)

**Files:** `data/migrations/098_stamp_legacy_profile_ids.sql` (write it with the Write tool), test in `internal/storage/`.

- [ ] For every table with a nullable/empty-allowed `profile_id` (`vault_keys`, rows with `''` in `jev_usage`, `publications`, `ai_chat_messages`, and any NULL in the `DEFAULT 'default'` tables), `UPDATE … SET profile_id = <stamp> WHERE profile_id IS NULL OR profile_id = ''` where `<stamp>` is the profile `default` if it exists else the oldest profile. Test with a two-profile DB and an ownerless row for each table.
- [ ] Also stamp the workflow file store (`~/.monoagent/workflows/*.json` with empty `profile_id`) in `workflowio`.

### Task 4.2: Delete the in-code fallbacks

**Files:** the ~97 `profileID = "default"` sites and ~40 `COALESCE(profile_id,'default')` queries listed in the inventory; `vault.ProfileIDFromContext` (`internal/vault/vault.go:59-64`); `026_workflows_profile_default_index.sql` (replace the expression index with a plain `(profile_id, …)` index in a new migration).

- [ ] **Step 1:** Make `vault.ProfileIDFromContext` return `(string, error)` and fix its 34 callers; an empty profile is an error, not `"default"`.
- [ ] **Step 2:** Replace each `COALESCE(profile_id,'default') = ?` with `profile_id = ?`.
- [ ] **Step 3:** Unify the empty-owner rule: an empty `ProfileID` on a workflow is an error in HTTP, MCP and CLI (`crossProfile`, `OwnedWorkflow`).
- [ ] **Step 4:** Remove `ListWorkflows("")` semantics ("every profile"): `workflow migrate` and `workflow_all_profiles` iterate profiles explicitly.
- [ ] **Step 5 (guard):** add a Go test that fails if `git grep` finds `COALESCE(profile_id` or `= "default"` assignments outside an allow-list file (`migrations`, tests, the stamp code).
- [ ] **DDL defaults:** do **not** rebuild the 21 tables that have `DEFAULT 'default'`; once every insert supplies `profile_id` the default is inert. Add a repository test that inserts without a profile and expects the app-level API to refuse before reaching SQL.

---

## Phase 5: Delete the old mechanism

- [ ] `profile switch` and `profile current` removed (`internal/cli/profile/profile.go:226-285`); `Active` field and the star in `profile list`/`get`.
- [ ] Migration `099` deletes `settings.active_profile_id`; remove every reader (`root.go`, `doctor/env.go`, `capture/classify.go`, `mcp`, `httpapi`, `health/core.go`, `wails-app/app.go`), plus the seed in `011` stays (history) but is deleted at runtime.
- [ ] `profiledir.List` loses the `Default` marking and `Default()` (no callers); callers in `extension/profile_list.go`, `library/adopt.go` (`activeFirst`), `capture/profiles.go`, `capturehooks/index.go`, `extensioncli/browsers.go`, `summary/all.go`, `org/summary.go`, `workflowmanage/all_profiles.go` adjust.
- [ ] GUI: `getActiveProfileID`/`setActiveProfileID` become the GUI's own persisted selection (its own file or localStorage), never a DB setting.
- [ ] Remove `MONOAGENT_PROFILE` text from help and docs.

---

## Phase 6: Enforce and release

- [ ] Change the default `Mode` from `ModeWarn` to `ModeEnforce` (one-line change in `profile_gate.go`, own commit).
- [ ] CI step that runs the whole smoke suite with the gate enforced.
- [ ] Release notes: breaking change, how to migrate scripts (add `--profile`), MCP clients (add `profile` argument; per-profile server registration no longer needed), HTTP clients (new path).
- [ ] Version bump; merge the integration branch as one release (the Doctor smoke job must run to completion — never cancel it).

## Testing summary

- Table-vs-tree test (Task 1.3) so no command ships unclassified.
- `tools/list` test for MCP (Task 3.1).
- A grep guard test for `"default"` fallbacks (Task 4.2).
- Two-profile fixtures in every area plan: an action on profile A must never be visible from profile B, and a call with no profile must fail.
- End-to-end: `scripts/e2e/*.sh` run with the gate enforced.

## Risks

- Breaking release: every saved script, workflow and external MCP/HTTP client that omits the profile stops working; mitigated by one warn-mode release and the CI step that fails on any gate warning.
- `initDB` also runs migrations and key migrations; moving profile resolution out of it must not skip them for global commands.
- Daemon: one process, many profiles; its per-run profile must come from the workflow, never from process state.
- Extension must not block a capture; D2 keeps the Unassigned pile.
- `workflow_daily_counters.profile_key` and `threads` are unverified (Task 0.3).

## Self-review

- **Spec coverage:** gate (Phase 1), no active profile/switch/current (Phase 5), no env var (Phases 2.6, 5), MCP required arg (3.1), HTTP (3.2), GUI explicit (2.1), legacy rows (4.1), `"default"`/COALESCE removal (4.2), release (6). Settings-page and posts/Jev work is already in PR #799.
- **Placeholders:** Phase 1 is complete; Phases 2-6 are tables of exact files and acceptance tests and each gets its own detailed plan before execution (stated in the header and "Scope check"). Open decisions D1-D4 are explicit.
- **Type consistency:** `Check(c, flagSet, mode, list)`, `Mode`, `ModeWarn`/`ModeEnforce`, `ProfileRef`, `Lister`, `Class`, `ClassScoped`/`ClassGlobal`, `CommandKey`, `Listed` are used consistently in Tasks 1.1-1.4.
