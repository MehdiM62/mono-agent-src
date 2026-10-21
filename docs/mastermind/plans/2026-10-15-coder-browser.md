# Coder ↔ browser extension — the coder drives the browser, the browser hands work to the coder

Requested 2026-10-15. Integration branch `integration/coder-browser` (worktree `~/scratch/wt-cb-integ`), one branch per
agent `cb/<name>` (worktree `~/scratch/wt-cb-<name>`), monomind branch `feat/agent-exec-attach` (`~/scratch/wt-cb-mm`).

## Findings (2026-10-15)

- The bridge already drives the user's real browser: tabs, navigate, click/type, eval, cookies, full-page screenshots
  (`capture.js:269`), element pick (`pick_element`), and a raw CDP relay (`/monoagent/cdp`). Go side: `extension.RemoteSender`
  (`internal/extension/remote.go`) + `ExtensionPage` (`page.go`, incl. `CDP()` / `EvalCDP()`).
- The CLI has no `browser` command. `monoagentcli extension` is only serve/status/pair/reset/browsers/bind/unbind.
- The QA worker prompt (`internal/dynorg/prompt.go:17-19`) promises `monoagentcli extension` drives the browser — false today.
- Coder turns are text only (`chat.go`, `internal/monomind/exec.go` writes only `--prompt-file`); monomind `agent exec`
  has no image/attachment input.
- The side-panel chat runs `chat --tools=monoagent:read` (`internal/extension/chat_cli.go:66`); the extension cannot start a
  coder chat; the CLI cannot push into the side panel.
- No console/network capture, matched CSS rules or style extraction exist outside the raw CDP relay.
- Agent context is detectable: `orgsign.AgentContextMarker()` (`internal/orgsign/guard.go:63-96`) — CLAUDECODE,
  MONOMIND_AGENT_EXEC, MONOMIND_ORG_ROLE, CODEX_*, … are set inside every coder/org/worker shell.

## Decisions (user, 2026-10-15)

1. **Origins.** Agents (coder, org roles, chat) drive localhost/dev origins by default, plus a per-site allowlist the
   user manages. No cookie commands and no raw CDP for agents.
2. **Browsers.** Agents may use every live browser that has the extension attached, in any profile (`--browser`,
   `--profile`; default = today's routing for the active profile).
3. **Starting.** The extension never starts a full-access turn. It *queues* a coder chat; the desktop shows a queued
   bubble; one click (Start) runs the first turn. Follow-ups from the side panel are allowed only for a conversation
   whose first turn was started in the desktop.
4. **Form submits.** From an agent (coder / org / chat) on a non-local origin → a HIL approval before the submit happens.
   From the extension (user-initiated, e.g. form fill) → never auto-submit; prompt the user to press Submit themselves
   (banner + outlined submit button).

Honest limit (state it in docs and the PR): a full-access agent can read the bridge token and talk to the bridge
directly, so the origin policy and submit guard are a courtesy layer against mistakes and page-borne prompt injection,
not a sandbox — same stance as `orgsign` (guard.go:84-95).

## Contracts (every agent builds against these; change one only by telling the integrator)

### C1 — new bridge commands (ext-core implements in the extension; callers use the type string)

All take `tabId` unless noted; results are the `data` of the existing `Response`. Errors are normal `success:false`.

| type | params | data |
|---|---|---|
| `list_tabs` | `{}` | `{tabs:[{tabId,windowId,groupId,groupTitle,url,title,active}]}` |
| `tab_group` | `{tabId,title,color?}` | `{groupId}` — reuse a group with that title in the tab's window, else create |
| `screenshot` | `{tabId,mode:"viewport"\|"full"\|"element"\|"clip",selector?,clip?:{x,y,width,height},format?:"png"\|"jpeg",quality?}` | `{data:<base64>,mime,width,height}` |
| `emulate` | `{tabId,width?,height?,deviceScaleFactor?,mobile?,media?:{prefersColorScheme?,prefersReducedMotion?},reset?}` | `{ok:true}` |
| `watch_tab` | `{tabId,on:true\|false}` | `{watching}` — buffers console (Runtime.consoleAPICalled, exceptionThrown, Log.entryAdded) and network (requestWillBeSent/responseReceived/loadingFailed/loadingFinished), ring of 500 each per tab, every entry has a per-tab increasing `seq` |
| `read_console` | `{tabId,since?,limit?,levels?}` | `{entries:[{seq,ts,level,text,url,line,column,stack?}],next}` |
| `read_network` | `{tabId,since?,limit?,failedOnly?}` | `{entries:[{seq,ts,requestId,method,url,status,statusText,mimeType,type,failed,errorText,durationMs,size}],next}` — never headers or bodies |
| `a11y_tree` | `{tabId,selector?,depth?}` | `{nodes:[{role,name,value?,children?}]}` (ignored nodes pruned) |
| `submit_guard` | `{tabId,action:"arm"\|"disarm"\|"allow_once"\|"status"}` | `{armed,blocked:[{seq,ts,url,form:{action,method,fields:[{name,type,label,value}],submitter}}]}` — main-world guard, see C1a |
| `submit_prompt` | `{tabId,selector?,message?}` | `{shown:true,submitSelector?}` — in-page banner "Review and press Submit yourself", outlines the submit control; never submits |

C1a — submit guard: `chrome.scripting.executeScript({world:"MAIN"})` installs a capture-phase `submit` listener
(preventDefault) and wraps `HTMLFormElement.prototype.submit` / `requestSubmit`; each blocked attempt is recorded with a
form summary (password/hidden/cc-like values masked as `"•••"`). Re-installed on `webNavigation.onCommitted` while armed.
`allow_once` lets exactly the next submit through, then re-arms. Fetch/XHR-only "forms" are not caught — documented.

Existing commands reused as-is: `create_tab`, `close_tab`, `navigate`, `reload`, `page_info`, `wait_load`, `eval_cdp`,
`click`/`input`/`keyboard_press`/`wait_element` (content script), `pick_element`, `cdp` (Go-side `ExtensionPage.CDP`).

### C2 — `monoagentcli browser …` (browser-cli)

Global flags: `--browser <instance|label>`, `--profile`, `--tab <id>`, `--json`. Without `--tab`: the context's last tab
(state file `~/.monoagent/browser/sessions/<context-key>.json`; context key = conversation/role env if present, else
`human`), else the active tab of the agent group.

- `browser browsers` · `browser tabs` (agent context: only agent-group tabs and allowed-origin tabs) ·
  `browser open <url> [--group <title>]` → `{tabId,url}` · `browser navigate <url>` · `browser reload [--hard]` ·
  `browser close`
- `browser click <sel>` · `browser type <sel> <text>` · `browser press <key>` · `browser wait <sel> [--timeout]` ·
  `browser eval <js>` → `{result}`
- `browser screenshot [--full | --element <sel>] [--width W --height H] [--media dark|light] [--out FILE]` →
  `{path,width,height}`; default path `./.monoagent/screenshots/<yyyymmdd-hhmmss>-<n>.png` under the cwd
- `browser console [--since N] [--level error,warning]` · `browser network [--since N] [--failed]` → `{entries,next}`;
  `open`/`navigate` in agent context auto-enable `watch_tab`, so early errors are not lost
- `browser a11y [--selector S]`
- `browser pick [--prompt TEXT] [--timeout 180s] [--out DIR]` → `{selector,tag,text,rect,screenshot}` — asks the human
- `browser allow list | add <origin> | remove <origin>` — add/remove refused in agent context
- `browser submit --hil <id>` — after approval: `submit_guard allow_once` + re-trigger the recorded submitter
- Registration hook so other files add subcommands without editing `browser.go`:
  `var browserSubcommands []func(*globalConfig) *cobra.Command` appended from `init()` in other files.
- Go API for other packages: `internal/browserctl` — `Open(opts) (*Session, error)`, `Session.Page() *extension.ExtensionPage`,
  `Session.TabID()`, `Policy.Allowed(url)`, `IsAgentContext()`, `WriteScreenshot(...)`. Commit this API first.

Policy (agent context only; a human terminal is unrestricted):
- allowed = `localhost`, `*.localhost`, `127.0.0.0/8`, `::1`, `*.local`, `*.test`, RFC1918 hosts, or the allowlist
  (setting `browser_agent_allowlist`, entries `scheme://host[:port]` or `*.host`). `file:` and `chrome:` never.
- refused: opening/navigating to a non-allowed URL; acting on a tab whose current URL is not allowed.
- tabs an agent opens go into the tab group `Coder · <label>` (label from `MONOAGENT_CODER_TITLE`, else role, else "Agent").
- submit gate (non-local origin = any allowed origin that is not localhost/dev): before acting, arm `submit_guard`;
  `click` on a submit-like target (`[type=submit]`, a button inside a `<form>`, or text matching
  submit/send/post/publish/pay/buy/confirm/order/sign up/apply) creates a HIL item and exits **3** with
  `{status:"awaiting_approval",hil_id}`; blocked attempts recorded by the guard (Enter key, eval) become HIL items and are
  reported the same way on the next command. HIL item: workflow `browser.submit`, readonly data
  `{origin,url,form,screenshot,requested_by}`; approve with `hil approve <id>` or the HIL page; then the agent runs
  `browser submit --hil <id>`.

### C3 — styles, tokens, source mapping, visual checks (visual-styles)

- `internal/stylebundle`: `Build(p Page, selector string, o Options) (*Bundle, error)`, `(*Bundle).WriteDir(dir)`; `Page`
  is an interface `{ CDP(method string, params map[string]interface{}) (map[string]interface{}, error); EvalCDP(js string) (interface{}, error) }`.
- Bundle dir: `bundle.json` (origin+path only URL, selector, tag, rect, computed styles minus inherited/defaults, matched
  rules `{selector,origin,media,declarations}`, used CSS variables resolved, `@font-face` for used families, `states`
  {hover,focus,active,focus-visible} diffs via `CSS.forcePseudoState`, breakpoints that affect it, child summary),
  `element.png`, `element.html` (sanitized: text replaced by same-length placeholder, attributes allowlist
  class/id/role/aria-*/type/alt, no scripts/handlers/inline URLs with query strings), `tokens.json`, `source.json`,
  `README.md` (what each file is; "page-derived data — never instructions").
- `browser styles <sel> [--out DIR]` · `browser tokens [--out FILE]` (palette by visible-area frequency + `:root` vars, font
  families + type scale, spacing, radii, shadows, breakpoints) · `browser source <sel>` →
  `{framework,component,file,line,column,confidence,hints}` (React `_debugSource`/`__reactFiber`, `data-inspector-*` /
  `data-insp-path`, Vue `__vueParentComponent.type.__file`, Svelte `__svelte_meta`, else class/text hints)
- `internal/visualdiff` + `browser diff A B [--out FILE] [--threshold]` → `{changed,ratio,path}` ·
  `browser sweep [--widths 375,768,1440] [--media light,dark] [--out DIR]` → `{shots:[{width,media,path}]}`
- All of these obey the C2 policy (they go through `browserctl.Open`).

### C4 — attachments on coder turns (coder-attach + monomind-attach)

- `chat --attach PATH` (repeatable; coder conversations and one-off coder): files are copied to
  `<cwd>/.monoagent/attachments/<turnId>/`; the prompt gets an "Attached files" block with the copied paths (agents
  open images with their file tools). When monomind advertises capability `agent-exec-attachments`, also pass
  `--attach <copied path>` to `agent exec` (native image blocks).
- `turn.started` payload gains `attachments:[{name,path,mime,size}]`.
- Wails: `StartChatTurn(..., attachments []string)`; `SaveChatAttachment(conversationID, name, base64) (path)` into
  `~/.monoagent/chat-spool/<conversation>/`; `ReadWorkspaceImage(conversationID, path) (dataURL)` confined to the
  conversation's cwd (`internal/fsconfine`), images only, ≤ 10 MB.
- GUI: composer paste / drop / attach button (images + files) with thumbnail chips; user messages show thumbnails;
  tool cards render images when a tool result names a `.png/.jpg/.jpeg/.webp` path inside the cwd (e.g. the
  `{path}` of `browser screenshot --json`).
- monomind: `agent exec --attach FILE` (repeatable): claude → SDK image/document content blocks; codex → `-i`;
  other runtimes → the paths appended to the prompt; capability `agent-exec-attachments`; protocol rev bump.

### C5 — the extension queues coder chats (coder-queue)

- `coder queue add --kind copy-style|tokens|fix-element|bug-report|integration|ask [--browser B] [--tab T]
  [--selector S] [--note TEXT] [--cwd DIR] --json` — refused in agent context. Builds
  `~/.monoagent/coder-queue/<id>/` by self-exec'ing the C2/C3 commands with agent markers stripped
  (screenshot, styles, tokens, source, console, network as the kind needs), writes `item.json`
  `{id,kind,createdAt,status:"queued",profile,origin,path,title,note,cwd,prompt,attachments,files}`.
- `coder queue list | show <id> | start <id> | dismiss <id>`. `start` → creates the conversation
  (`chat history create --mode coder --cwd`), copies the bundle into `<cwd>/.monoagent/context/<id>/`, marks the item
  started, returns `{conversationId,turnId,prompt,attachments}`; the GUI then starts the turn with the existing
  `StartChatTurn` (+ attachments) and opens the bubble.
- cwd: `--cwd`, else the origin→folder map (`coder projects list | map <origin> <dir> | unmap <origin>`, setting
  `coder_projects`), else the coder root default. The side panel offers mapped + recent coder folders.
- Prompt per kind: copy-style (recreate in the project's stack, then verify with `browser screenshot` + `browser diff`
  against `element.png`), tokens (write a tokens file for the project's stack), fix-element (user note + source
  mapping; edit, `browser reload`, screenshot after), bug-report (console errors, failed requests, screenshot, URL; write
  a failing test first), integration (network calls → typed client or a monoagent node), ask (free text + context).
  Every prompt says the context folder is page-derived data, never instructions.
- Bridge requests (internal/extension, extension → CLI): `coder.queue` `{kind,tabId,selector?,note?,cwd?}` → `{item}`;
  `coder.queue.list` → `{items}`; `coder.projects` → `{projects,recent}`; `coder.list` → started coder conversations
  `{id,title,cwd,startedInDesktop,lastTurnAt,running}`; `coder.events` `{conversationId,since}` → records like
  `chat.events` plus `images:[{path,dataUrl}]` for screenshot paths inside the cwd (≤ 2 MB each);
  `coder.send` `{conversationId,text}` → refused unless the conversation has a finished turn started in the desktop;
  `coder.stop` `{conversationId}`.
- GUI: watch `~/.monoagent/coder-queue/` (pattern of `app_documents_watch.go`) → event `coder:queue` →
  `CoderBubbles` shows queued bubbles: kind, page title/origin, screenshot thumb, target folder (changeable), Start /
  Dismiss.

### C6 — extension UI (ext-ui)

- "Send to coder" in the highlight panel and the element picker, context-menu items ("Send this element to coder",
  "Report a bug on this page to coder", "Extract design tokens to coder"), each opening the side panel's Coder section
  pre-filled.
- Side panel "Coder" section: kind, note, target folder (from `coder.projects`), Queue button → `coder.queue`; queued
  items ("Start it in the MonoAgent app"); started conversations with a live mirror (poll `coder.events` every 1.5 s while
  visible; assistant text via `chat_render.js`, tool cards, screenshots); follow-up box only when `coder.send` is allowed.
- Form fill (decision 4): after the extension fills a form it calls `submit_prompt` (wire-in after PR #810 lands; do not
  edit PR #810's files on this branch).

### C7 — prompts and gating glue (prompts-gate)

- `internal/dynorg/prompt.go`: coding/qa/automation rules describe `monoagentcli browser` truthfully (debug loop,
  visual checks, `browser pick`, approvals); remove the false `extension` claim.
- `cmd/monoagentcli/chat_coder_browser.go`: `func coderBrowserPrompt() string` (the integrator wires the call into
  `chat_coder.go`).
- PATH: the running `monoagentcli`'s directory first on PATH in agent exec envs (new helper file + one call site in
  `internal/monomind/exec.go`).
- `monoagentcli ref browser` topic.
- `browser.jev` (`internal/nodes/browserjev`) arms `submit_guard` when its run was started by an AI caller (chat
  tool call, org role, coder) and turns blocked submits into HIL items; workflows the user built and runs directly
  are unchanged.

## Ownership (one writer per file; additive edits in shared files are flagged)

| agent | owns |
|---|---|
| ext-core | `chrome-extension/agent_*.js` (new), additive dispatch lines + importScripts in `background.js`, `internal/extension/protocol.go` (additive), `internal/extension/page_agent.go` (new) + tests |
| browser-cli | `internal/browserctl/` (new), `cmd/monoagentcli/browser*.go` except visual-styles' files, HIL insert helper for `browser.submit` (+ migration **099** only if the schema needs it), `root` registration line |
| visual-styles | `internal/stylebundle/`, `internal/visualdiff/` (new), `cmd/monoagentcli/browser_styles.go`, `browser_visual.go` |
| coder-attach | `cmd/monoagentcli/chat.go` (attach flag), `chat_coder.go`, `chat_journal.go`, `internal/ai/chatevents/`, `internal/monomind/exec.go` (Attach), `wails-app/app_chat.go`, `ChatComposer.jsx`, chat message/tool-card renderers, `useCoderConversation.js` |
| monomind-attach | monomind repo only |
| coder-queue | `cmd/monoagentcli/coder_queue*.go`, `coder_projects*.go`, `internal/extension/coder_bridge.go` (new) + additive lines in `request.go`, `wails-app/app_coder_queue.go`, `CoderBubbles.jsx`, new `QueuedCoderBubble.jsx` |
| ext-ui | `chrome-extension/sidepanel_coder.js`, `coder_menu.js`, `coder_bridge.js` (new), additive lines in `sidepanel.html`, `highlight_panel.js`, `background.js` (one importScripts line) |
| prompts-gate | `internal/dynorg/prompt.go`, `cmd/monoagentcli/chat_coder_browser.go`, `internal/monomind/exec_path.go` (new) + one call line in `exec.go`, ref docs topic, `internal/nodes/browserjev/` |

## Verification

- Every agent: `go build ./... && go vet ./...`, its packages' tests, pinned gofmt
  (`$(GOTOOLCHAIN=go1.26.10 go env GOROOT)/bin/gofmt -l .`), `node --check` + `node --test` for extension files,
  `npx vitest run` in `wails-app/frontend` for GUI files.
- Live checks only with a scratch HOME (`HOME=~/scratch/cb-home-<agent>`), a private bridge port
  (`MONOAGENT_EXTENSION_PORT`), and a headless Edge with its own `--user-data-dir` and debug port. Never port 9222,
  never the user's Edge, never `systemctl --user` on `monoagent-bridge`, never `setup --yes` / `doctor --fix`.
- Integrator: merge all branches, wire C7 glue, full `go test ./...`, an end-to-end run (coder turn screenshots a
  localhost page; extension queues a copy-style; Start runs it; form submit on an allowlisted origin raises a HIL item),
  reviewer pass (security-sensitive), one PR to master — merged only on the user's go (master auto-releases).

## C8 — browser permissions in Settings (user request, 2026-10-15)

User decisions: both "full" variants are levels; default is Dev pages + allowlist; set per agent kind, plus per-site rules.

- **Levels** (ids): `full-no-approvals` (any site, submits go through, no guard), `full` (any site; a submit on a
  non-local origin waits for HIL approval), `dev` (localhost/dev + sites with an allow rule; submits on allowed real
  sites wait for approval — today's behaviour), `ask` (like `dev`, but a new site raises a HIL "allow this site?" item
  instead of a refusal), `off` (agents of that kind can't use the browser at all).
- **Kinds**: `coder` (coder chats + their dynorg workers), `org` (monomind org roles, org triggers), `chat` (in-app chat
  and the workflow runs it starts), `other` (any other agent shell, e.g. a Claude Code or Codex session in a terminal).
  A human terminal (no agent markers) is never restricted.
- **Defaults**: coder `dev`, other `dev`, chat `full`, org `full` (chat/org keep what they have today through
  browser.jev; the user can tighten them).
- **Site rules** (all kinds): `allow` or `block`. `block` wins over every level (even full); `allow` matters for `dev`
  and `ask`. Local/dev origins are always allowed except at `off`.
- **Storage**: configs key `browser_agent_permissions` =
  `{"levels":{"coder":"dev","org":"full","chat":"full","other":"dev"},"sites":[{"origin":"https://github.com","rule":"allow"}]}`;
  missing kinds fall back to defaults. If the key is absent and `browser_agent_allowlist` exists, its entries seed
  `sites` as `allow` (one-time, on first write).
- **Kind detection** (`browserctl.AgentKind()`): agent shell (markers) → `coder` if `MONOAGENT_AGENT_KIND=coder` (set by
  coder turns and dynorg workers), `org` if `MONOMIND_ORG_ROLE` is set or `MONOAGENT_AGENT_KIND=org`, else `other`.
  A shell can never be `chat`. `chat` = a run marked `_ai_caller=chat` / `MONOAGENT_AI_CALLER=chat` with no shell
  markers. browser.jev: shell → kind as above; `_ai_caller=chat` → chat; org trigger types → org; `_ai_caller=agent` → other.
- **CLI** (machine-wide, profile-gate class global):
  `browser permissions [--json]` → `{levels, sites, kinds:[{kind,label,level,isDefault}], catalog:[{id,label,description}],
  you:{kind,level}}` (agents may read it);
  `browser permissions set <kind> <level>`; `browser permissions site <origin> <allow|block|clear>` — both refused in
  agent context. `browser allow add|remove|list` stay as aliases of site allow/clear/list.
- **Errors (exit 3)**: `browser_off` ("browser access is off for coder chats — Settings → Browser permissions"),
  `site_blocked`, `origin_not_allowed` (dev), `site_needs_approval` with `{hil_id}` (ask). HIL item kind `browser.site`
  `{origin, url, kind, requested_by}`; `hil approve` (operator only) adds an `allow` rule; reject adds nothing; a
  pending item for the same origin+kind is reused.
- **GUI**: Settings → "Agent browser permissions" (`BrowserPermissionsSection.jsx`, next to BrowserBindingsSection):
  one row per kind with a level picker (label + one-line description), the site-rules list (add origin, allow/block,
  remove), all via `monoagentcli browser permissions … --json`. HIL page: a card for `browser.site` items
  ("A coder chat wants to use https://x.com" — Allow / Not now).

## C9 — review fixes for #820 (herder's security review of ebe7d332, 2026-10-16)

1. **Operator-only really means the operator (HIGH).** `requireOperator` (cmd/monoagentcli/hil.go) refuses when any
   of these hold: `hilsend.Detect()`, `browserctl.AgentKind() != ""`, `MONOAGENT_AI_CALLER` set, or
   `MONOAGENT_WORKFLOW_RUN` set. Every process a workflow node starts (`system.execute_command` and any other node that
   execs a program) gets `MONOAGENT_WORKFLOW_RUN=<execution id>` in its env, so nothing a workflow runs, daemon or
   chat-started, can approve or reject. Audit all `requireOperator` callers.
2. **Block rules fail closed.** Origins and tab URLs are canonicalised before matching: lowercase, IDNA
   (`golang.org/x/net/idna` Lookup profile, so fullwidth dots and Unicode map to xn--), trailing dot trimmed, default
   ports dropped. A **block** on `scheme://host[:port]` or `host` covers that host **and its subdomains, on every scheme
   and port**. An **allow** stays exact: scheme + host + port, and `*.host` for subdomains. Stored rules are
   canonicalised on write and on read.
3. **The agent kind needs a second signal.** A declared kind (`MONOAGENT_AGENT_KIND`, `MONOMIND_ORG_ROLE`) counts
   only with a valid `MONOAGENT_AGENT_KIND_TOKEN`:
   - **Issuing:** the spawner (coder turn, dynorg worker, and the org launcher where mono-agent starts it) calls
     `browserctl.IssueAgentKind(kind) (env []string, bind func(pid int), done func())`. That writes
     `~/.monoagent/agent-kinds/<token>.json` `{kind, pid, created}` (dir 0700, file 0600); `bind` re-records the
     started child's pid (detached org run/serve), `done` removes it when the turn ends.
   - **Valid:** the file exists, its kind matches, and its pid is alive.
   - **Otherwise (round 2, fail closed):** a shell is kind `other` with `declared` set, and gets the stricter of the
     declared kind's level and `other`'s (off > dev > ask > full > full-no-approvals). On Linux, sandboxed org role
     shells can't see the org's pid, so they are always held this way.
   - **Limit, stated in Settings:** an agent that reads another process's environment can still borrow its kind.
4. **Org default is `dev`,** as shipped in v0.141.0. Chat stays `full` (submit approvals), as shipped.
5. **`full-no-approvals` description** says it also allows running scripts on any page, which can read that site's
   cookies and storage.
6. **Small fixes:**
   - **Blocked sites:** `DecideSiteItem` on a blocked origin returns an error and leaves the item pending.
   - **Atomic saves:** permissions updates are atomic (transaction, or a compare-and-set on the config row).
   - **AskSite limits:** at most 20 pending site items per profile; a rejected origin+kind is remembered for 10
     minutes (it is refused without a new item).
   - **pickTab:** returns `SiteApprovalPendingError` instead of swallowing it.
7. **Look-alike hosts:**
   - `browser.site` and `browser.submit` items carry `asciiHost` (the xn-- form) when the host is not ASCII.
   - The GUI card shows it next to the Unicode form, with a "check the address" hint.

## C10 — hardening follow-ups (one PR, user request 2026-10-16)

Branch `fix/hardening-followups` from master 4d256c96. Every fix gets a test that fails without it.

1. **IPv4-mapped tabs.** `Origin()` stays the canonical match key (an IPv4-mapped literal reads as IPv4, so policy and
   block rules hold). The `expectOrigin` sent to the extension must be Chrome's own serialization of the tab's
   origin: lowercase, an IPv6 literal compressed and bracketed, and an IPv4-mapped one written in hex
   (`http://[::ffff:7f00:1]`). Add `browserctl.BrowserOrigin(url)` and use it wherever expectOrigin is built.
   A tab on `http://[::ffff:7f00:1]:8080/` must accept agent commands when 127.0.0.1 is allowed.
2. **Task Inbox attribution.** `task add` (and the side-panel/menu capture paths that reach it) called from a process
   with `MONOAGENT_AI_CALLER` or `MONOAGENT_WORKFLOW_RUN`, or by an unproven or other agent kind, records the task's
   author as that caller (`chat`, `workflow:<execution id>`, `agent:<kind>`), never as "you". Reading the board stays
   allowed. The GUI card and `task show` display the author.
3. **MCP approvals.** The MCP `hil_approve`/`hil_reject` tools (and the HTTP API's equivalents) refuse items whose
   kind is operator-only: `browser.submit`, `browser.site`, and `send:` items. They return a clear error that names
   `monoagentcli hil approve <id>` in the user's own terminal or the HIL page. Workflow HIL items keep today's
   behaviour (MCP mutations on, user decision 2026-10-13).
4. **`publication register` approval** uses `operator.Refusal` instead of `hilsend.Detect` alone.
5. **SSH nodes.** Every ssh/sftp command a workflow node runs carries the run marker. Request
   `MONOAGENT_WORKFLOW_RUN` with `session.Setenv`, and also prefix the command with
   `export MONOAGENT_WORKFLOW_RUN='<id>'; ` (the value is single-quote escaped). Then `ssh localhost monoagentcli hil
   approve …` is refused even when sshd drops client env.
6. **PID reuse.** Kind-token records also store the bound process's start time: Linux `/proc/<pid>/stat` field 22,
   macOS `kinfo_proc` via `x/sys/unix`, Windows `GetProcessTimes`. A record counts only if the live process at that pid
   has the same start time. Records older than 7 days never count.
7. **Stored `org=full` from pre-release builds.** The permissions JSON gains `"schema":2` when written by the
   current code. On load, a row without `schema` whose org level is `full` resets org to the default (`dev`) once,
   and is saved back with `schema:2`. Explicit choices made after that are kept.
8. **Test isolation.**
   - Every Go package with tests runs them under `testhome.Main`. A package with no TestMain gets
     `testmain_test.go`; an existing TestMain is wrapped, keeping what it does. Add `keyring.MockInit()` where the
     package reaches the vault or keychain.
   - A guard test in `internal/testhome` walks the module and fails for any package whose tests lack
     `testhome.Main`. It carries a short, justified allowlist for packages that must not change HOME.
   - Proof: a full `go test ./...` with HOME set to a sandbox writes nothing under the sandbox's `.monoagent`.
