# Coder as the front door for the app's AI functions

Requested 2026-10-22: "the application and other AI functions invoke a coder agent and perform the tasks from there, so the
user can interact; the coder should have full capability."

Builds on coder mode (#202-#205, v0.85.0), all runtimes (#234), dynamic org (#224), bubbles (#227/#229), coder-browser
(2026-10-15), bubble v2 (2026-10-13). Nothing here replaces them; it adds a way for *other* features to open them.

## 1. Where we are

Two separate AI paths exist today.

| Path | Callers | Shape |
|---|---|---|
| Coder session | chat (coder mode), bubbles, extension `coder queue`, dynamic org | full access, folder, journaled, interactive, `chat history …` |
| One-shot `monomind.Exec` | `ai/generate` (workflow generate), `config/agentgen` (selector/config gen), `recordanalyze`, `capturesummary`, `nodes/browserjev`, `cli/agent`, extension `doc.ask`/summaries | no tools, sandboxed, empty folder, budget-capped, no history, not interactive |

The one-shot path is deliberate: it is safe to run unattended and offline-first. Its cost is that the user cannot steer it, see
its work, or continue from its result. The coder path is interactive but is operator-enabled (`coder enable`), opt-in per
runtime, and heavyweight (folder, `monomind init`, slow start).

## 2. Recommendation (pushback)

Do **not** send every AI call to a full-access session. Summaries of every capture, matching/classification and selector
generation inside a node run are background, high-volume, unattended transforms; making them full-access would be a security
regression (unattended + full access is exactly what the org-ack rules forbid), a cost/latency regression, and would break
offline-first. Route by **kind**:

- **A. Transform** (stays one-shot): capture summaries, matching, Jev, `doc.ask`. Gain: a "Continue in coder" action that opens a
  session seeded with the input and the answer.
- **B. Task** (moves to coder): workflow generate/repair, recording analysis, automation/selector authoring and repair,
  "fix this failed run", browserjev failures, extension "ask/fix". Interactive by default.
- **C. Already coder**: chat coder mode, bubbles, dynamic org.

One setting per kind, operator-only: `ai.route.<kind> = oneshot | coder | ask`. Defaults: A=oneshot, B=coder when coder is
enabled, otherwise oneshot (never a silent escalation, and the UI says why it did not open a chat).

### 2b. Per tab (added 2026-10-22)

Today, application filling is **not** AI-driven in the app: `internal/apply.Prepare` only generates the CV and cover letter,
and `OpenForApplication` opens the posting for a human. Its spec (2026-09-05 apply-automation-design) deliberately forbids
auto-fill, and a test enforces that `apply/browser.go` has no form-interaction call. The only AI filling is the extension's
`form.fill`: one tool-less model turn that returns values and never submits. Making the Applications tab coder-driven therefore
**reverses that spec**; it needs the user's explicit yes (open decision 6) and the spec and the guard test change with it.

| Tab | Today | Route |
|---|---|---|
| Applications: Apply / Process pending | CV + letter documents, open the posting, human fills | **Coder task `application-apply`**: context = application, job text, CV/letter, profile facts; the agent fills the form through the browser CLI and stops before submit |
| Applications: evaluate / match | `matching` one-shot, Jev | stays one-shot; "Discuss in coder" |
| Extension form fill | tool-less `form.fill` | stays the fast path; "Fill with coder" for multi-page, uploads, custom widgets, login walls |
| Communications / People | drafts and replies | stays a tool-less one-shot: synced messages never reach a shell turn (§4b.2); "Discuss in coder" only on text the user selects |
| Workflows / Node runner | workflow generate, failed runs | Phase 2 pilot and "fix this run" |
| Automations (recordings) | `AnalyzeRecording` one-shot | Task |
| Documents / Image vault | generation, captions | CV/letter authoring is a Task; captions and tags stay one-shot |
| Orgs, Agents | already staff coder / dynamic org | unchanged |
| Library, Logs, Info, Connections, Dashboard | no AI action today | none, except "Ask coder about this" on Logs and Connections errors (Phase 5) |

`application-apply` rules, all enforced in the CLI and not in the prompt:
- the agent fills but never submits; the submit click is a `browser submit` HIL item for any non-local origin (decision 4 of
  2026-10-15), shown on the application card with a screenshot of the filled form;
- it may attach only files registered for that application (the generated CV/letter, profile documents), via
  `--attach`/`upload`, never arbitrary paths;
- passwords, card numbers and one-time codes are never given to it; a login wall becomes "user, please sign in" in the bubble;
- generated free-text answers (cover letter, "why us") are marked as generated in the card, as `form.fill` does today;
- the status moves to `ready-to-send` only when the agent calls `aitask complete`, and to `applied` only on the user's approve;
- one application per task and one task per application at a time; bulk "Process pending" opens one queued bubble per item,
  capped by the existing concurrency limit, never an unattended batch.

### 2c. User decisions (2026-10-22), supersede §2 and §8 where they differ

1. **Button-driven, not automatic.** One-shot stays the default for every AI action; each AI surface gets a "Send to coder"
   button that opens a task. No kind is silently rerouted. `ai.route.<kind>` shrinks to "which kinds show the button and
   whether the primary action (Apply, workflow generate) opens coder"; Apply opens coder by default.
2. **Coder disabled:** the button falls back to one-shot and says why, with a link to Settings (enabling stays the user's own
   click; an agent can never do it).
3. **Workflow nodes never wait on a person.** An `ai.task` node starts a task and proceeds at once with `{taskId, conversationId,
   status: "queued"}`; the result is read later (`aitask result`) or lands in the bubble. Phase 6 (pause/resume) is dropped.
4. **One chat mode.** Assistant mode becomes coder-backed: a single conversation type with a per-conversation access level
   (read-only/scoped, coding, workspace, full) instead of two modes. Old assistant conversations stay readable. New chats
   default to the level the operator set; with coder disabled, the scoped level is all that is offered. Needs its own
   migration and a security review, because "default chat" now sits on the coder path.
5. **Budgets are UI settings, per kind and per agent** (conversation, role, worker), each editable in the UI. Raising a cap is
   operator-only; an agent can lower its own, never raise it. Defaults come from `coder set --budget-usd`.
6. **Applications: reverse "no auto-fill".** The agent fills the form, then parks the application at `awaiting_submit` and
   moves to the next application while the human submits (§2d).

### 2d. Applications: fill, park, next

- Each application gets its own browser tab, opened and tracked by the task (`application_id` ↔ tab). The agent never closes
  a filled tab and never clicks submit.
- After filling it records a screenshot and the field report, sets `awaiting_submit`, and starts the next queued
  application. Concurrency is bounded (default 3 open tabs awaiting submit; setting in the UI).
- The human submits in the tab. The extension reports the submit/confirmation page, which moves the application to `applied`
  and logs it in the status history; "Mark applied" and "Discard" are always available when detection fails.
- A parked tab that was closed, or a session that expired, turns the card into "Reopen and refill" (a new task).
- Login walls and CAPTCHAs park the application as `needs_you` with the reason, and the agent moves on.

### 2e. Application page: everything around the application in one place

`application show <id> --json --assets` returns, and the Applications detail page renders:
- the posting: captured page text, URL, deadline and the saved capture;
- documents: the generated CV and cover letter (HTML/PDF, versions), the profile's uploaded documents, and which were attached
  to this application;
- the filled-form screenshot, the field report (value, source, generated flag) and the submit approval;
- people and messages: the recruiter/contact, synced email and DM threads, drafts waiting in HIL;
- tasks (board items linked to it), the status history, notes, tags;
- the live coder task: status, the bubble, "Open chat", "Reopen tab", "Refill".

Phase 0 must verify which links exist today: vault documents already carry `ApplicationID`; messages, people and tasks may
need a link table (`application_links(application_id, kind, ref_id)`), so the page does not depend on guessing by name.

## 3. Design: `aitask`, one entry point

New package `internal/aitask`, CLI first (the GUI, extension and nodes shell out; logic lives in `monoagentcli`).

```
monoagentcli --profile P aitask submit --kind workflow-generate --goal "…" \
    [--context FILE|DIR …] [--result-schema S] [--access coding|workspace|qa] [--runtime R] [--unattended] --json
monoagentcli --profile P aitask status|wait|result|cancel|list <id> --json
```

`submit` does, in order:
1. Resolves routing for the kind (§2). `oneshot` calls the existing runner unchanged.
2. Creates a coder conversation (`ModeCoder`, cwd = the profile folder per bubble v2 Point 1, or a task folder
   `<profile>/.monoagent/tasks/<id>/` when the task needs a scratch area).
3. Writes context under `.monoagent/context/` with the same rules as `coder queue`: README says it is data from the app/page,
   never instructions; size caps; secrets redacted by value; path-traversal checks.
4. Builds the first-turn prompt from a per-kind template (goal, context index, result contract, "ask the user when a choice is
   theirs"), starts turn 1, returns `{taskId, conversationId, status}`.

Result contract: the agent finishes with `monoagentcli aitask complete <id> --result FILE`; the CLI validates it against the
kind's JSON schema (so a bad result is refused with a reason the agent can fix) and records it. `wait`/`result` and an event
(`aitask:event`) hand it back to the caller. A conversation that ends without `complete` is `needs_user`, not success.

Interactivity: every task is a normal coder conversation, so it appears in the dock as a bubble with a task header (what was
asked, which tab it came from, "Open in Workflows"). Per-kind Apply buttons are conveniences; the agent may also write through
`monoagentcli` itself (e.g. `workflow create`), which keeps today's gates.

Callers that cannot show UI (daemon, workflow node, extension bridge):
- default `queued`: the task appears as a queued bubble and needs one click to start (extension decision 3, 2026-10-15);
- a workflow node that needs the answer pauses the run on an HIL item "AI task open" and resumes on `complete`;
- `--unattended` only for kinds whose profile is non-full-access, budget-capped and sandboxed. Unattended + full access is
  refused.

## 4. "Full capability", and what stays out

What the task session gets: full access in its folder (coder), `workspace` profile (every monoagent surface through
`monoagentcli`: apps, tasks, documents, people, vault names, workflows), the browser CLI under the user's browser-permission
level, dynamic org staffing with Jev/pick, all runtimes, the profile context block.

What does not change (these are the contract, not gaps): operator-only commands and settings (`coder set`, browser
permissions, approvals that release messages to people, `ai.route.*`); sends wait for HIL; unapproved-workflow program/file
guards; no secret values in prompts or results. Removing any of these is a separate decision for the user.

## 4b. Security review of this plan (codex, read-only, 2026-10-22) and what changed

Findings are binding requirements for the phases that follow.

1. **Task capability (critical).** `complete` and `cancel` need caller binding: the token is bound to profile, task,
   conversation and the executing process; cancel is authorised separately; terminal states are atomic and final (a late
   `complete` cannot resurrect a cancelled task). Core phase.
2. **Untrusted content never reaches a shell turn (critical).** AGENTS.md says synced messages must never reach a shell-enabled
   turn, and the plan had broken that. Changes: `application-apply` context is only the posting (web data, fenced like coder
   queue), the profile's own facts and its registered documents, never messages or people threads; the `draft-reply` kind
   is **dropped from coder routing** and stays a tool-less one-shot (Communications gets "Discuss in coder" only on text the
   user pastes or selects). The application page may show messages; no task is given them.
3. **Confinement is transitive.** A workflow run that is unapproved or AI-authored may not start a coder task (the `ai.task`
   node refuses, like `execute_command` does); the caller's restrictions bound the task. Node phase.
4. **Upload/fill gates stay.** Today an agent `browser fill`/upload on a non-local origin waits for approval. Autonomous
   application filling therefore needs its own browser-permission **kind** (`apply`), set by the operator in Settings, never
   by an agent; until the operator sets it, fills ask. An attachment grant is bound to application + file digest + origin +
   task, and "profile documents" means the documents registered to that application only.
5. **Disclosure before submit.** Typing is disclosure. The agent fills only visible fields it can map to a profile fact, using the
   `form.fill` vetting (hidden fields, secrets, card/OTP fields dropped); anything unmapped is left empty and listed in the
   report. The submit guard does not cover fetch/XHR-only sends, which is documented as a known gap.
6. **State machine for submit.** Approval is authorisation only. `applied` needs an extension observation bound to task, tab and
   a trusted submit event plus navigation, and an ambiguous result always asks the person ("Mark applied"). A page that merely
   shows "submitted" never moves the status.
7. **Tab isolation.** Task-scoped browser capabilities, revoked while a tab is parked or the person is signing in, and
   re-validated against the application on every operation; resume is an explicit, task-bound action. No shared profile-root
   context between concurrent applications.
8. **Unified chat.** Migration defaults are the stricter of chat's and coder's; upgrades are operator-only; a downgrade
   terminates privileged processes and revokes capabilities before the next turn.
9. **Budgets.** One operator-owned aggregate ledger across descendants and retries with atomic reservation; zero is "no
   spend", never "unlimited"; a runtime that cannot enforce or report cost cannot run a capped task.
10. **Queue admission is not start authority.** The click binds an immutable context hash and the effective capabilities;
    policy is re-checked at start; unattended means a verified-confined profile only.

11. **The application-apply agent is confined, not a full-access coder (decided 2026-10-22, the one deliberate narrowing).** The user
    asked for coder with full capability; this kind is the exception because its input is hostile web content (a job posting,
    and the form page itself). A full-access turn in the profile folder could, on a prompt injection, read other
    applications' documents and facts, rewrite their queued context, or run curl past the browser policy. So its turns (the
    first and every follow-up in its conversation, operator-run only) run as: monomind scoped access (no `--access full`, no
    settings sources, no bridged tools); Bash only for literal prefixes `monoagentcli --profile P browser <verb>` for the
    listed verbs (not eval, console, network, submit, close, permissions) and `aitask complete|fail`, which monomind checks
    before running and which refuses chaining; a working folder of its own outside the profile folder holding read-only copies of
    that task's context only (`aitask.SnapshotContext`, hash-verified copy); the browser kind `apply` read only from the task
    row, so a configured `coder` level, even full-no-approvals, never applies to it. The context text is given in the
    session instructions as data, because the agent has no file tool. Enforcement of the prefix list is verified for the
    claude runtime only; on any other runtime the task is refused, not run unconfined. Known residual: `browser upload` takes any
    path the CLI can read; on a real site it is held for approval, and a files-by-application restriction is still to be built.

## 5. Phases

0. **Audit (small).** Verify which of bubble v2 Points 1-4 and #226 follow-ups shipped; read `nodes/browserjev` and
   `cli/agent` call sites (not read in this plan); table every `monomind.Exec` caller with its kind, interactivity need and
   unattended use. Output: the kind table committed to this file.
1. **`aitask` core.** Package + CLI + migration (task rows: id, kind, conversation, status, result, caller). Fake-monomind
   tests. No UI.
2. **Kind: workflow-generate** (the pilot): route `workflow generate` and the GUI button; schema = workflow JSON + validation
   report; "Save as draft" card. Compare against the one-shot output on the same briefs.
3. **GUI**: task header in bubbles, "Open in coder" on AI buttons, routing settings in Settings (operator group), queued-task
   bubbles. Browser-verified via `wails dev`.
4. **Kinds**: recording analysis, automation/selector authoring + repair, failed-run fix, browserjev failure, extension
   ask/fix. One PR per kind group.
5. **Escalation from A**: "Continue in coder" on summaries, `doc.ask`, matching results.
6. **Applications**: `application-apply` kind, tab tracking, `awaiting_submit` state and submit detection (extension),
   concurrency cap, spec reversal + guard-test rewrite, and the `--assets` application page (§2d, §2e).
7. **One chat mode** (decision 4): unify assistant and coder conversations behind access levels; migration; security review.
8. **Budgets UI** (decision 5) and the non-blocking `ai.task` node (decision 3).
9. **Docs + release**: AGENTS.md section, CHANGELOG, `ref` topic for agents.

## 5a. Phase 0 results (read-only audit, 2026-10-22; nothing was run)

**AI call sites (18 found).** Interactive and coder: assistant chat (`chat_run.go:426`), coder (`chat_coder.go:157`), dynorg
workers, extension chat and coder bridge. Button-triggered one-shots: recording analysis (`recordanalyze/llm.go:159`,
`RecordingReview.jsx:88`), application evaluate (`matching/evaluate.go:113`), profile info AI-fill (`Info.jsx:66`), extension
form fill (`capturesummary.ExecRunner` after a model-free match), `agent test`, roster validation. Unattended and staying
one-shot: capture summaries (hook after every capture), `browser.jev`, `agent.ask`, judge and guardrail nodes, org decisions,
the OpenAI-compatible gateway, `agentgen` selector generation. Jev is a typed-answer classifier with many callers; it never
writes text and is out of scope. `workflow generate` has no GUI caller. CV and cover-letter generation is template rendering
with no model, and no AI-written reply exists in Communications or People today.

**Applications.** DB statuses are pending, applied, rejected, cancelled (`applications.go:23-27`); "ready-to-send" is a UI
step. `applied` is human-only and stays so: `application send` (ActorUser) is the only path, refused at
`internal/cli/application/application.go:258`, `wails-app/app_applications.go:255` and `nodes/applications/set_status.go:41`.
`TestBrowserFileNeverClicksAnything` (`internal/apply/browser_test.go:86`) source-greps `internal/apply` for form-interaction
calls, so filling lives in a new package and the spec, the `application apply` help text and the `internal/apply` comments are
corrected in the same change. New statuses need a table rebuild if migration 031 uses a CHECK constraint.

**Links.** Only `vault_documents.application_id` (035) links to an application; messages, people, tasks, captures, HIL and
conversations do not, so `application_links` (migration 115) carries them. `apply.Prepare` assumes exactly four generated
documents in creation order; a coder-authored CV needs it to read links instead.

**Chat merge (#959).** One shared `ai_chat_conversations` row already. About 25-35 non-test files and 30+ tests. Hard parts:
`runCoderTurn` against the sandboxed assistant turn, the extension's one `--tools` mode per conversation, and keeping the
operator-enable gate for the higher levels.

**GUI.** `openDraft()` takes no arguments, so a button cannot seed a bubble: add `openDraft({prompt, setup, links})`
(`setView(key,{draft,...})` already pre-fills in `CoderBubbles.jsx:~104`) and a context so pages outside `AIChatPanel` can
reach `openCoderChat`. Attach points: Apply (`ApplicationsProcessPendingFlow.jsx:101`, `Applications.jsx`), Evaluate
(`:88`, "Discuss"), `RecordingReview.jsx:88`, `Info.jsx:66`, `RunProblemPanel.jsx` (failed nodes as seed text),
`Logs.jsx` error rows, a new Workflows entry for generate.

**Blockers to design around.**
1. The coder's browser level defaults to `dev` (`browserctl/permissions.go:77`): real job sites are refused until the operator
   allows the origin or raises the level. The `apply` kind (§4b.4) needs operator-set levels and per-site allow rules.
2. The submit block is the extension guard (`agent_submit_guard.js`), which cannot tell the person's submit from the
   agent's. Unverified: whether the CLI arms it for KindCoder on non-local origins. Verify before the apply task is built.
3. No files-by-application restriction exists in `browserctl`; it is new code (attachment grants, §4b.4).
4. `ai.task` nodes only queue, never run, because unattended + full access is forbidden.

## 6. Verification

- Unit: routing matrix (kind × coder enabled/disabled × caller kind); result-schema refusal and retry; context-folder escape
  and redaction; `TestEveryAitaskFlagIsClassified` (operator-only vs anyone), modelled on the `coder set` classifier test.
- Revert-proof: unattended + full access refused (test fails if the guard is removed); size caps asserted by work bound.
- Integration: scratch HOME and `MONOES_BASE_URL=http://127.0.0.1:9`, never a copy of the real DB; branch builds with
  `HOME=~/scratch/...` because any CLI call migrates the DB.
- Live: one real coder turn per pilot kind in the scratch HOME (a full-access org grant cannot be tested from an agent shell).
- GUI: drive it over HTTP with `wails dev`; render tests for the task header and queued bubble.

## 7. Process

- Issues in `monoes/mono-agent` (epic + one per phase); monomind asks (e.g. `agent exec --result-schema`) in `monoes/monomind`.
- Waves with file ownership; integration branch, one PR, herder lands it; no merge without the user's go (master merge =
  release). Run the pinned gofmt before push.

## 8. Open decisions for the user

1. Accept the A/B split, or force every call through coder (not recommended, see §2)?
2. Default for B when coder is disabled: fall back to one-shot (recommended) or prompt to enable?
3. May a workflow node block on a human-driven session (pause on HIL), or must nodes stay non-interactive?
4. Should plain assistant-mode chat also become coder-backed, ending the two chat modes?
5. Per-task spend caps: reuse `coder set --budget-usd`, or per-kind caps?
6. Reverse the 2026-09-05 "no auto-fill" apply spec so the Applications tab can have the coder fill forms (stopping before
   submit, submit = HIL)? Recommended yes, as the first kind after the workflow pilot.

## 9. Verified gates (apply worker, 2026-10-22)

Question: can a coder-kind agent submit, fill or upload on a non-local origin without a person's approval? Answer: no,
at every level below `full-no-approvals`, through the CLI. Read from the code at the commit this section was added.

- **Which level binds.** A coder turn is issued kind `coder` (`internal/cli/chat/chat_coder.go:129`, vouched by
  `browserctl.IssueAgentKind`, `kindtoken.go:64`). `AgentCaller` (`permissions.go:458`) turns that token into
  `Caller{Kind: coder}`, and a shell that declares a kind it cannot prove is kind `other` with the stricter of the two
  levels (`LevelOf`, `permissions.go:482`). The coder default is `dev` (`permissions.go:77`).
- **Reaching a non-local site.** `Policy.Check` (`policy.go:106`) lets an agent drive a non-local origin only at
  `full`/`full-no-approvals`, or with an allow rule, or (at `ask`) after a HIL item; `dev` refuses it.
- **Submit.** `Policy.SubmitGated` (`policy.go:161`) is true for an agent on an allowed, non-local origin at every level
  except `full-no-approvals`. `Session.Gated` (`gate.go:60`) uses it. Every browser verb runs inside `browserRun.act`
  (`cmd/monoagentcli/browser.go:121-132`): `BeforeAct` (`gate.go:88`) arms the extension guard on the tab and fails
  closed if it cannot be armed; `AfterAct` (`gate.go:128`) turns a submit the guard blocked into a HIL item. A click on a
  submit-like control is probed and held up front (`browser_act.go:35`); Enter in a field and script submits are caught by
  the page guard (`chrome-extension/agent_submit_guard.js`, header). `browser eval` is refused on a gated tab
  (`browser_act.go:175`).
- **Fill and upload.** `browser fill` (`browser_fill.go:59`) and `browser upload` (`browser_upload.go:55`) are held with a
  HIL item when `Gated()`; they run only through `--hil <id>` after approval, bound to the page by a digest.
- **Per tab and origin.** The guard is armed again before every action; the extension disarms a tab whose top page leaves
  the armed origin and drops its records (`gate.go:104-107`, guard header), so a submit the person makes elsewhere is not
  stopped.
- **Gaps (not closed by this work).** (1) Fetch/XHR-only "forms" are not caught (`gate.go` header). (2) The guard cannot
  tell the person's submit from the agent's (it shows a bar). (3) At `full-no-approvals` nothing is held; the coder level
  can be set to it by the operator. (4) The coder agent's own Claude/Codex shell can use tools outside `browser` (its own
  browser, curl): the policy is a courtesy layer, not a sandbox (`policy.go` header).
- **Consequence for `apply`.** An apply task's turn is a coder turn, so today it runs at the coder level. The new `apply`
  kind needs the turn to be issued `apply` (`chat_coder.go` is not in this worker's files: see NEEDS in the report).
  Until then it is bound by the coder level, which is never looser than the apply one for submit.
