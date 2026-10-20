# Coder bubble v2 — profile folder, wider staffing, app integration, org bubble

Requested 2026-10-13. Builds on #227 (bubbles), #229 (org bubble), `internal/dynorg` (conductor).

## Findings (what exists today)

- Folder: `CoderChatOverlay.create()` uses `api.coderWorkspaceRoot(runtime)` → `rootCoderWorkspace(settings.WorkspaceRoot)`
  (default `~/monoagent-coder`). The active profile's folder (`profiledir.Root`) is not involved.
- Staffing: `dynorg.Staffer.staffRole` takes the lead's role, else `monomind pick`, else one of 5 built-ins
  (`coder, reviewer, tester, planner, researcher`). Model: lead → Jev → fallback rules; the lead prompt says "leave out to have it picked".
- App tools: only the `automation` access profile gets `monoagentcli` (workflows/automation/node/extension). `qa` gets browser/extension.
  No profile points at tasks, documents, people, vault or orgs, and every worker prompt says
  "Never read, send or change the user's messages or people records".
- Org bubble: `OrgBubbleOverlay` + `OrgChatView` already answer questions, approvals and gates inline and show role-to-role messages
  as collapsed rows. It shows only the boss thread; workflow HIL (`monoagentcli hil`) is not in the bubbles; no per-role threads.

## Decisions (user, 2026-10-13)

- Scope now: plan + issues only.
- Agents use every monoagent tool without prompts, **except sending**: see Point 3 (revised 2026-10-13, supersedes "no prompts").
- Point 1 revised: the chat folder IS the active profile's folder (not a subfolder, not `~/monoagent-coder`).

## Point 1 — the chat works in the active profile's folder, fully profile-aware

- Default cwd = `profiledir.Root` of the active profile itself. `coder workspace default` returns `{path, profile{id,name}, source}`;
  add-only init for the runtime (never overwrite the profile's files). GUI calls it instead of `coderWorkspaceRoot`; picker keeps
  "Choose another folder…". Switching profile changes the default for new chats; open chats keep theirs.
- Profile awareness: a generated `profile context` block (CLI `profile context --json`) injected into the lead's and every worker's
  system prompt: profile name/id, folder, connected accounts (platforms, not secrets), counts and recent items of tasks, documents,
  people, workflows, orgs, vault item names (no values), and the extension/browser status. Cached per turn, refreshed on `org_roster`.
- Test: two profiles in a scratch HOME; the prompt names the right one; no secret values appear in the context.

## Point 2 — roles and skills from monomind, routed by Jev or by the model

- Roles/skills are monomind's registered agents and skills first (`pick`, `Library.AgentBody/SkillText`); built-ins are only the fallback,
  plus the app-aware built-ins (task-manager, doc-writer, people-researcher, workflow-builder, browser-operator, org-designer…)
  for monoagent surfaces monomind has no agent for.
- Routing modes (setting `coder.router`): `jev` (Jev chooses from pick's shortlist, today's low-confidence path made the default for roles
  and skills), `model` (the lead picks from `org_roster`, which lists registry agents by category with one-line descriptions), `auto`
  (default: pick when confident, else Jev, else the lead).
- Efficiency rule, enforced in `Staffer` and the lead prompt: spawn only when the part is parallel or needs a specialty; the smallest
  sufficient role, skills ≤3, cheapest model that fits the category, reuse veterans first; `org_roster` returns cost/quality hints.
  Add a test: a trivial brief spawns nothing / one worker; a 3-concern brief spawns 3 distinct roles.

## Point 3 — full monoagent integration; sending always waits for approval (HIL)

- `workspace` access profile: coding + `monoagentcli` on PATH + the surface rules (job app / social → that app's tab via `node run`/`extension`;
  to-dos → `task`; files → `profile documents`; contacts → `people`; credentials → vault; repeatable work → a workflow first, then org roles).
  Prompt rule against touching people/messages is replaced by: prepare freely, never finalize a send.
- **Sending = HIL by default.** Anything outbound (email, DM, comment, post, publish, connection request) is prepared by the agent, then
  stored as a pending HIL item (`hil` queue) instead of sent. Enforcement is in the CLI/nodes, not the prompt: send/publish nodes and
  `publication register` called from an agent context check `hil` approval and refuse without one (setting `coder.sendApproval`, default on;
  per-destination "always allow" only by the user).
- **Approval card** (in the bubble, same card as org gates/HIL): recipient (with the person's profile), channel/account, the exact text,
  every attachment as a preview (images inline, files with name/size/open, links), the workflow/role that prepared it, and Approve /
  Edit / Reject. On approve the exact previewed payload is sent and registered; an edit re-previews. Same card for org runs.
- Other writes (tasks, documents, people records, workflows) stay prompt-free with tool cards attributed to the role; vault reads
  are shown as cards with names only. Kill switch `coder.appAccess`.
- Model choice per task as before (category fit + `agentroster.Quality`; test with two ready models).
- Tool cards for task/people/workflow calls link to the tab (`onNavigate`).

## Point 4 — a normal org run shows up as a bubble with live roles

- Starting an org from the Org tab (or a schedule/daemon run) auto-adds its bubble to the dock (`org:event` run-started); no need to open
  the chat. Ring states: working / needs (any pending HIL, question, gate, approval) / done / error; unread count.
- Expanded: the org stage on top (all roles, live status, who is talking to whom — animated edges from team messages), chat below.
  Per-role threads: click a role (stage node) to filter the thread to that role's messages, its tool activity and its asks; a "Team"
  view shows the role-to-role conversation as a timeline. Boss thread stays the default.
- Every ask is answerable in the chat: question, approval, gate, workflow HIL and the send-approval card of Point 3, one `AskCard`.
- Org tab keeps its view, but links to the bubble for answering. Stop/pause/resume buttons in the bubble header.
- Test: render tests with fixtures per card kind; run a real org from the GUI in `wails dev` over HTTP; CLI check of `hil resolve`.

## Order

1. P1 folder (small, independent) → 2. roles → 3. `workspace` profile + prompts + model choice → 4. HIL/role threads.
Points 2 and 3 share `dynorg/staff.go` and `prompt.go`: same wave, one writer. Branch builds use `HOME=~/scratch/...`.
