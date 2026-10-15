/**
 * The coder's browser commands (plan C1) through the real command path: the
 * unpacked extension's worker takes {type, tabId, params} in handleCommand
 * (origin check included) and answers through sendResponse, swapped for a
 * collector so the test reads exactly what Go would receive. Chrome does the
 * rest for real: chrome.debugger, chrome.scripting, the tab groups.
 *
 * Covers: list_tabs/tab_group, screenshot (viewport, element, full),
 * emulate, watch_tab + read_console/read_network across a reload, a11y_tree,
 * the submit guard (button, Enter, form.submit(), requestSubmit(), a
 * single-page app's handler, allow_once, re-arming after a navigation,
 * masking) and submit_prompt.
 *
 * No bridge (see extension_harness); the gate is told the person signed in,
 * which a bridge's ping reply would do, so the relay's messages pass it.
 */

import { after, describe, it } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { findChrome } from "./browser_harness.mjs";
import { attach, sleep, start } from "./extension_harness.mjs";

const FORM = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Checkout</title>
<style>body{font:16px sans-serif;margin:20px} #box{width:200px;height:100px;background:#09f}</style></head><body>
<form id="checkout" action="/next" method="get">
  <label for="email">Email</label><input id="email" name="email" value="ann@example.com">
  <label>Password <input type="password" name="pw" value="hunter2"></label>
  <input type="hidden" name="csrf" value="tok-123">
  <label>Card <input name="cardnumber" value="4111 1111 1111 1111"></label>
  <input name="note" value="4111111111111111">
  <input name="order" value="1234567812345678">
  <textarea name="memo">card 4111-1111-1111-1111 exp 12/29</textarea>
  <input type="checkbox" name="news" value="yes">
  <button id="pay">Pay</button>
</form>
<form id="spa"><input id="q" name="q" value="x"><button id="go">Search</button></form>
<div id="box"></div>
<div style="height:2400px"></div>
<script>document.getElementById("spa").addEventListener("submit", (e) => { e.preventDefault(); window.spaSent = true; });</script>
</body></html>`;

const NEXT = `<!doctype html><html><head><meta charset="utf-8"><title>Next</title></head><body>
<form id="again" action="/final" method="get"><input name="a" value="1"><button id="again-btn">Send</button></form></body></html>`;

const CONSOLE = `<!doctype html><html><head><meta charset="utf-8"><title>Console</title></head><body>
<script>
console.log("hello %s", "world");
console.warn("careful");
fetch("/missing.json").catch(() => {});
setTimeout(() => { throw new Error("boom"); }, 0);
</script></body></html>`;

// A submit button right where the banner would go: top centre.
const TOP = `<!doctype html><html><head><meta charset="utf-8"><title>Top</title></head><body>
<form id="top" action="/next"><input name="a" value="1">
<button id="pay" style="position:absolute;top:20px;left:calc(50% - 30px);width:60px">Pay</button></form></body></html>`;

const PAGES = { "/form": FORM, "/next": NEXT, "/final": "<!doctype html><title>Final</title>", "/console": CONSOLE, "/top": TOP };

const CHROME = findChrome();
let browser = null;
let why = "no Chrome found — set CHROME_PATH to run these";
if (CHROME && typeof WebSocket === "function") {
  try {
    browser = await start(CHROME);
  } catch (e) {
    why = `${CHROME} would not start: ${e.message.split("\n")[0]}`;
  }
}
const server = createServer((req, res) => {
  const page = PAGES[new URL(req.url, "http://x").pathname];
  res.writeHead(page ? 200 : 404, { "content-type": "text/html; charset=utf-8" });
  res.end(page || "not found");
});

after(async () => {
  if (browser) await browser.close();
  server.close();
});

describe("the coder's browser commands", { skip: browser ? false : why, concurrency: 1 }, () => {
  let sw;
  let base;
  let n = 0;
  const pages = {}; // path -> {session, tabId}

  async function setup() {
    if (sw) return;
    await new Promise((r) => server.listen(0, "127.0.0.1", r));
    base = `http://127.0.0.1:${server.address().port}`;
    assert.ok(browser.worker, browser.workerError);
    sw = await attach(browser.cdp, { targetId: browser.worker.targetId });
    await browser.cdp.evaluate(
      sw,
      `(() => {
        self.__responses = {};
        sendResponse = (id, success, data, error) => { self.__responses[id] = { success, data, error }; };
        gate.observe({ kind: "reply", ok: true, data: { pong: true, account: { state: "ok" } } });
        return true;
      })()`
    );
  }

  let opened = 0;
  /** open loads path in a new tab; a query makes its URL unique, so the tab can be found by it. */
  async function open(path) {
    await setup();
    const url = `${base}${path}?tab=${++opened}`;
    const session = await attach(browser.cdp, { url });
    let tabId = 0;
    const loaded = `chrome.tabs.query({}).then((ts) => (ts.find((t) => t.url === ${JSON.stringify(url)} && t.status === "complete") || {}).id || 0)`;
    await until(async () => (tabId = await browser.cdp.evaluate(sw, loaded)), `${url} to load`);
    pages[path] = { session, tabId, url };
    return pages[path];
  }

  /** until polls check until it holds; a loaded machine (the whole suite at once) is slow. */
  async function until(check, what, ms = 15000) {
    for (const end = Date.now() + ms; Date.now() < end; await sleep(100)) if (await check()) return;
    throw new Error(`timed out waiting for ${what}`);
  }

  /** command runs one command through handleCommand and returns what Go would receive. */
  async function command(type, tabId, params = {}) {
    const id = `agent-${++n}`;
    await browser.cdp.evaluate(sw, `(handleCommand({ id: ${JSON.stringify(id)}, type: ${JSON.stringify(type)}, tabId: ${tabId}, params: ${JSON.stringify(params)} }), true)`);
    for (let t = 0; t < 20000; t += 50) {
      const r = await browser.cdp.evaluate(sw, `JSON.stringify(self.__responses[${JSON.stringify(id)}] || null)`);
      if (r !== "null") return JSON.parse(r);
      await sleep(50);
    }
    throw new Error(`no response to ${type}`);
  }

  async function ok(type, tabId, params) {
    const res = await command(type, tabId, params);
    assert.equal(res.success, true, `${type}: ${res.error}`);
    return res.data;
  }

  const inPage = (p, expr) => browser.cdp.evaluate(p.session, expr);
  const pngSize = (b64) => {
    const b = Buffer.from(b64, "base64");
    assert.equal(b.subarray(1, 4).toString(), "PNG");
    return [b.readUInt32BE(16), b.readUInt32BE(20)];
  };

  it("list_tabs sees the tab, and tab_group puts it in a group by title", async () => {
    const p = await open("/form");
    const group = await ok("tab_group", p.tabId, { title: "Coder · test", color: "cyan" });
    assert.ok(Number.isInteger(group.groupId));
    const again = await ok("tab_group", p.tabId, { title: "Coder · test" });
    assert.equal(again.groupId, group.groupId, "the same title reuses the group");
    const { tabs } = await ok("list_tabs", 0);
    const row = tabs.find((t) => t.tabId === p.tabId);
    assert.deepEqual([row.groupId, row.groupTitle, row.url, row.title], [group.groupId, "Coder · test", p.url, "Checkout"]);
  });

  it("screenshot: viewport, one element at its CSS size, and the full page", async () => {
    const p = pages["/form"];
    const vp = await ok("screenshot", p.tabId, {});
    assert.equal(vp.mime, "image/png");
    assert.deepEqual(pngSize(vp.data), [vp.width, vp.height]);
    assert.ok(vp.width > 0 && vp.height > 0);
    const el = await ok("screenshot", p.tabId, { mode: "element", selector: "#box" });
    assert.deepEqual([el.width, el.height], [200, 100]);
    const full = await ok("screenshot", p.tabId, { mode: "full" });
    assert.ok(full.height > vp.height, `full ${full.height} > viewport ${vp.height}`);
    const jpg = await ok("screenshot", p.tabId, { format: "jpeg", quality: 50 });
    assert.equal(jpg.mime, "image/jpeg");
    // Not the same height: Chrome's "started debugging" bar can appear in between.
    assert.ok(jpg.width === vp.width && jpg.height > 0, JSON.stringify([jpg.width, jpg.height]));
    const missing = await command("screenshot", p.tabId, { mode: "element", selector: "#nope" });
    assert.deepEqual([missing.success, missing.error], [false, "screenshot: no element matches #nope"]);
  });

  it("emulate changes the viewport and colour scheme, and reset puts them back", async () => {
    const p = pages["/form"];
    const before = await inPage(p, "innerWidth");
    await ok("emulate", p.tabId, { width: 375, height: 700, deviceScaleFactor: 2, mobile: true, media: { prefersColorScheme: "dark" } });
    assert.equal(await inPage(p, "innerWidth"), 375);
    assert.equal(await inPage(p, "devicePixelRatio"), 2);
    assert.equal(await inPage(p, `matchMedia("(prefers-color-scheme: dark)").matches`), true);
    const shot = await ok("screenshot", p.tabId, {});
    assert.equal(shot.width, 750, "the screenshot sees the emulated viewport, at its scale factor");
    assert.equal(await browser.cdp.evaluate(sw, `MonoAgentCdp.holds(${p.tabId})`), true);
    await ok("emulate", p.tabId, { reset: true });
    assert.equal(await inPage(p, "innerWidth"), before);
    assert.equal(await inPage(p, `matchMedia("(prefers-color-scheme: dark)").matches`), false);
  });

  it("a11y_tree describes the form by role and name, password value not readable", async () => {
    const p = pages["/form"];
    const { nodes } = await ok("a11y_tree", p.tabId, { selector: "#checkout" });
    const flat = [];
    const walk = (list) => list.forEach((x) => (flat.push(x), walk(x.children || [])));
    walk(nodes);
    assert.ok(flat.some((x) => x.role === "textbox" && x.name === "Email" && x.value === "ann@example.com"), JSON.stringify(nodes));
    assert.ok(flat.some((x) => x.role === "button" && x.name === "Pay"));
    const valueOf = (name) => (flat.find((x) => x.role === "textbox" && x.name.trim() === name) || {}).value;
    assert.equal(valueOf("Password"), "•••", JSON.stringify(nodes));
    assert.equal(valueOf("Card"), "•••");
    const values = flat.filter((x) => x.role === "textbox").map((x) => x.value);
    assert.ok(values.includes("1234567812345678"), "a number that fails Luhn is kept");
    assert.equal(JSON.stringify(nodes).match(/hunter2|•••••••|4111/), null, "neither the password nor its length, nor a card number");
  });

  it("watch_tab buffers console and network across a reload", async () => {
    const p = await open("/console");
    assert.deepEqual(await ok("watch_tab", p.tabId, { on: true }), { watching: true });
    await browser.cdp.send("Page.reload", {}, p.session);
    // The reload's fetch is the proof it ran: Chrome replays earlier console
    // messages on a new watch, never requests.
    let net;
    await until(async () => {
      net = await ok("read_network", p.tabId, { failedOnly: true });
      return net.entries.some((e) => e.url.endsWith("/missing.json"));
    }, "the reload's failed fetch");
    await sleep(500);
    const cons = await ok("read_console", p.tabId, {});
    const texts = cons.entries.map((e) => `${e.level}:${e.text}`);
    assert.ok(texts.includes("log:hello world"), texts.join("\n"));
    assert.ok(texts.includes("warning:careful"));
    assert.ok(texts.some((t) => t.startsWith("error:Uncaught Error: boom")));
    assert.ok(cons.entries.every((e) => Number.isInteger(e.seq) && Number.isInteger(e.ts)));
    assert.ok(cons.entries.every((e) => e.origin === base), "every entry carries the page's origin");
    assert.deepEqual((await ok("read_console", p.tabId, { origin: "https://elsewhere.test" })).entries, []);
    assert.ok((await ok("read_network", p.tabId, { origin: base })).entries.length > 0);
    const errors = await ok("read_console", p.tabId, { levels: ["error"], since: 0 });
    assert.ok(errors.entries.every((e) => e.level === "error"));
    const miss = net.entries.find((e) => e.url.endsWith("/missing.json"));
    assert.deepEqual([miss.status, miss.method, miss.failed], [404, "GET", false]);
    assert.equal(Object.keys(miss).some((k) => /header|body/i.test(k)), false);
    const later = await ok("read_console", p.tabId, { since: cons.next });
    assert.ok(later.next >= cons.next && later.entries.every((e) => e.seq > cons.next), "a cursor never reads an entry twice");
    await ok("watch_tab", p.tabId, { on: false });
    const unwatched = await command("read_console", 999999, {});
    assert.equal(unwatched.success, false);
  });

  // ── the submit guard ────────────────────────────────────────────────────

  const guard = (tabId, action) => ok("submit_guard", tabId, { action });
  const blocked = (p, n) => until(async () => (await guard(p.tabId, "status")).blocked.length >= n, `${n} blocked submits`);
  // A page mid-navigation has no context to evaluate in yet: not there yet.
  const at = (p, path) => until(() => inPage(p, "location.pathname").then((x) => x === path, () => false), path);

  it("armed, a click on Pay is stopped and recorded with its secrets masked", async () => {
    const p = await open("/form");
    assert.deepEqual(await guard(p.tabId, "arm"), { armed: true, origin: base, blocked: [] });
    await inPage(p, `document.getElementById("pay").click()`);
    await blocked(p, 1);
    assert.equal(await inPage(p, "location.pathname"), "/form", "nothing was submitted");
    const st = await guard(p.tabId, "status");
    assert.equal(st.blocked.length, 1);
    const [b] = st.blocked;
    assert.equal(b.url, p.url);
    assert.deepEqual([b.form.action, b.form.method, b.form.submitter, b.form.selector], [`${base}/next`, "GET", "#pay", "#checkout"]);
    const values = Object.fromEntries(b.form.fields.map((f) => [f.name, f.value]));
    assert.deepEqual(values, { email: "ann@example.com", pw: "•••", csrf: "•••", cardnumber: "•••", note: "•••", order: "1234567812345678", memo: "•••" });
    assert.equal(b.frameId, 0);
    assert.equal(b.form.fields.find((f) => f.name === "email").label, "Email");
    assert.equal(JSON.stringify(st).match(/hunter2|tok-123|4111/), null);
  });

  it("Enter in a field, form.submit() and requestSubmit() are stopped too; so is an app's own handler", async () => {
    const p = pages["/form"];
    // A page cannot switch the guard off by pretending to be put in the back/forward cache.
    await inPage(p, `window.dispatchEvent(new PageTransitionEvent("pagehide", { persisted: true })), true`);
    await inPage(p, `document.getElementById("email").focus()`);
    for (const type of ["keyDown", "keyUp"]) {
      await browser.cdp.send("Input.dispatchKeyEvent", { type, key: "Enter", code: "Enter", windowsVirtualKeyCode: 13, text: type === "keyDown" ? "\r" : undefined }, p.session);
    }
    await inPage(p, `document.getElementById("checkout").submit()`);
    await inPage(p, `document.getElementById("checkout").requestSubmit()`);
    await inPage(p, `document.getElementById("go").click()`);
    await blocked(p, 5);
    assert.equal(await inPage(p, "location.pathname"), "/form");
    assert.equal(await inPage(p, "window.spaSent === true"), false, "the app's onSubmit never ran");
    const st = await guard(p.tabId, "status");
    assert.equal(st.blocked.length, 5);
    assert.deepEqual(st.blocked.map((b) => b.seq), [1, 2, 3, 4, 5]);
    assert.equal(st.blocked[4].form.selector, "#spa");
  });

  it("allow_once lets exactly the next submit through, and the next page is guarded again", async () => {
    const p = pages["/form"];
    // The latest blocked submit was #spa's: name the approved form instead.
    await ok("submit_guard", p.tabId, { action: "allow_once", selector: "#checkout" });
    await inPage(p, `document.getElementById("pay").click()`);
    await at(p, "/next");
    await until(() => inPage(p, `document.readyState === "complete"`).catch(() => false), "/next to load");
    await inPage(p, `document.getElementById("again-btn").click()`);
    await blocked(p, 6);
    assert.equal(await inPage(p, "location.pathname"), "/next", "re-armed on the new document");
    const st = await guard(p.tabId, "status");
    assert.equal(st.blocked[st.blocked.length - 1].form.selector, "#again");
    await guard(p.tabId, "disarm");
    await inPage(p, `document.getElementById("again-btn").click()`);
    await at(p, "/final"); // disarmed, the page submits
  });

  it("the guard ends when the tab's page leaves the armed origin; the other site submits freely", async () => {
    const p = await open("/form");
    await guard(p.tabId, "arm");
    // localhost and 127.0.0.1 are two origins on the same server.
    const other = base.replace("127.0.0.1", "localhost");
    await browser.cdp.send("Page.navigate", { url: `${other}/form?elsewhere=1` }, p.session);
    await until(() => inPage(p, `location.host.startsWith("localhost") && document.readyState === "complete"`).catch(() => false), "the other origin to load");
    assert.deepEqual(await guard(p.tabId, "status"), { armed: false, origin: "", blocked: [] });
    await inPage(p, `document.getElementById("pay").click()`);
    await at(p, "/next");
  });

  it("expectOrigin: a command for a tab on another origin is refused with origin_changed", async () => {
    const p = await open("/form");
    const wrong = await command("page_info", p.tabId, { expectOrigin: "https://elsewhere.test" });
    assert.equal(wrong.success, false);
    assert.match(wrong.error, /^origin_changed: the tab is on http:\/\/127\.0\.0\.1:\d+, not https:\/\/elsewhere\.test$/);
    const right = await command("page_info", p.tabId, { expectOrigin: base });
    assert.equal(right.success, true, right.error);
    assert.equal((await command("page_info", p.tabId, {})).success, true, "no expectOrigin: no check");
  });

  it("submit_prompt shows its banner in a closed root, rings Pay, and never submits", async () => {
    const p = await open("/form");
    const out = await ok("submit_prompt", p.tabId, { selector: "#checkout" });
    assert.deepEqual(out, { shown: true, submitSelector: "#pay" });
    await sleep(300);
    assert.equal(await inPage(p, `document.getElementById("monoagent-submit-prompt") !== null`), true);
    assert.equal(await inPage(p, `document.getElementById("monoagent-submit-prompt").shadowRoot`), null, "closed: the page cannot reach in");
    assert.equal(await inPage(p, `document.body.innerText.includes("Review and press Submit yourself")`), false);
    assert.equal(await inPage(p, "location.pathname"), "/form");
    const custom = await ok("submit_prompt", p.tabId, { message: "Check the dates, then Pay" });
    assert.equal(custom.submitSelector, "#pay", "no selector: the page's form");
    assert.equal(await inPage(p, `document.querySelectorAll("#monoagent-submit-prompt").length`), 1, "one banner at a time");
    await inPage(p, `document.getElementById("pay").click()`);
    await at(p, "/next"); // the person's submit goes through
    const none = await command("submit_prompt", p.tabId, { selector: "#nope" });
    assert.deepEqual([none.success, none.error], [false, "submit_prompt: no element matches #nope"]);
  });

  it("submit_prompt moves its banner out of the way of a control at the top", async () => {
    const p = await open("/top");
    assert.deepEqual(await ok("submit_prompt", p.tabId, {}), { shown: true, submitSelector: "#pay" });
    const reachable = `(() => { const r = document.getElementById("pay").getBoundingClientRect();
      return document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2) === document.getElementById("pay"); })()`;
    await sleep(300);
    assert.equal(await inPage(p, reachable), true, "the person can press the control the banner asks for");
  });
});
