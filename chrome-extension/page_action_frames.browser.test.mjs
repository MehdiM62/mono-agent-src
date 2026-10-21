/**
 * The chat's page actions across frames, through the real extension: the
 * worker's MonoPageActionBridge.run (chrome.scripting, allFrames) acts on a
 * cross-origin iframe, holds a consent box inside it, says which frame it
 * acted in, and refuses a frame that is no longer the one it searched.
 *
 * No bridge (see extension_harness). WITHOUT CHROME the suite skips.
 */

import { after, describe, it } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { findChrome } from "./browser_harness.mjs";
import { attach, sleep, start } from "./extension_harness.mjs";

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

const FRAME = `<!doctype html><meta charset=utf-8><body>
<button type=button id=hi onclick="window.__hits=(window.__hits||0)+1">Say hi</button>
<label for=em>Email address</label><input id=em>
<label><input type=checkbox id=me> Remember me</label>
<label><input type=checkbox id=ctc> Allow access to my contacts</label>`;
const topPage = (frameBase) => `<!doctype html><meta charset=utf-8><body><h1>Quiz</h1><iframe id=f src="${frameBase}/frame" width=500 height=200></iframe>`;

const inner = createServer((req, res) => {
  res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
  res.end(FRAME);
});
let frameBase = "";
const outer = createServer((req, res) => {
  res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
  res.end(topPage(frameBase));
});

after(async () => {
  if (browser) await browser.close();
  inner.close();
  outer.close();
});

describe("page actions across a cross-origin iframe", { skip: browser ? false : why, concurrency: 1 }, () => {
  let sw;
  let tab;
  const listen = (s) => new Promise((r) => s.listen(0, "127.0.0.1", r));
  const inSw = (expr) => browser.cdp.evaluate(sw, expr);

  async function setup() {
    if (tab) return;
    assert.ok(browser.worker, browser.workerError);
    await listen(inner);
    await listen(outer);
    frameBase = `http://127.0.0.1:${inner.address().port}`;
    const url = `http://127.0.0.1:${outer.address().port}/top?t=1`;
    sw = await attach(browser.cdp, { targetId: browser.worker.targetId });
    await attach(browser.cdp, { url });
    const found = `chrome.tabs.query({}).then((ts) => { const t = ts.find((x) => x.url === ${JSON.stringify(url)} && x.status === "complete"); return t ? { tabId: t.id, url: t.url } : null; })`;
    for (let i = 0; i < 150 && !tab; i++) {
      tab = await inSw(found);
      if (!tab) await sleep(100);
    }
    assert.ok(tab, "the page loaded");
    // The iframe's own document must be there too.
    for (let i = 0; i < 100; i++) {
      const n = await inSw(`chrome.webNavigation.getAllFrames({ tabId: ${tab.tabId} }).then((f) => f.filter((x) => x.url.endsWith("/frame")).length)`);
      if (n) break;
      await sleep(100);
    }
  }

  const act = async (action, target, text = "", frameOrigin) => {
    await setup();
    const proposal = { id: "toolu_a", kind: "action", action, target, text };
    return JSON.parse(await inSw(`MonoPageActionBridge.run({ tab: ${JSON.stringify(tab)}, ${frameOrigin ? `frameOrigin: ${JSON.stringify(frameOrigin)}, ` : ""}proposal: ${JSON.stringify(proposal)} }).then((r) => JSON.stringify(r))`));
  };
  const frameEval = async (fn) => {
    const r = await inSw(`chrome.scripting.executeScript({ target: { tabId: ${tab.tabId}, allFrames: true }, world: "MAIN", func: ${fn} }).then((rs) => JSON.stringify(rs))`);
    return JSON.parse(r).find((x) => x.frameId > 0);
  };

  it("clicks in the iframe, and says which frame it acted in", async () => {
    const out = await act("click", "Say hi");
    assert.equal(out.ok, true, JSON.stringify(out));
    assert.ok(out.frame > 0);
    assert.equal(out.frameOrigin, frameBase);
    const f = await frameEval("() => window.__hits || 0");
    assert.equal(f.result, 1, "the iframe's page got the click");
  });

  it("typing in a cross-origin iframe is refused with the frame's origin until that origin is confirmed", async () => {
    const asked = await act("type", "Email address", "ada@example.com");
    assert.deepEqual([asked.ok, asked.code, asked.frameOrigin], [false, "frame_confirm", frameBase]);
    assert.equal((await frameEval("() => document.getElementById('em').value")).result, "", "nothing was typed");
    const done = await act("type", "Email address", "ada@example.com", frameBase);
    assert.equal(done.ok, true, JSON.stringify(done));
    assert.equal((await frameEval("() => document.getElementById('em').value")).result, "ada@example.com");
  });

  it("ticks a plain box in the iframe, and holds back a consent one", async () => {
    assert.equal((await act("check", "Remember me", "", frameBase)).ok, true);
    const held = await act("check", "Allow access to my contacts", "", frameBase);
    assert.deepEqual([held.ok, held.code], [false, "submit_blocked"]);
    const f = await frameEval("() => [document.getElementById('me').checked, document.getElementById('ctc').checked]");
    assert.deepEqual(f.result, [true, false]);
  });

  it("refuses a frame whose document is no longer the one that was searched", async () => {
    await setup();
    const out = JSON.parse(
      await inSw(`(async () => {
        const [f] = (await chrome.scripting.executeScript({ target: { tabId: ${tab.tabId}, allFrames: true }, func: () => MonoPageAction.find("click", "Say hi") })).filter((r) => r.frameId > 0);
        const r = await chrome.scripting.executeScript({ target: { tabId: ${tab.tabId}, frameIds: [f.frameId] }, func: (h) => MonoPageAction.run("click", "Say hi", "", undefined, h), args: ["http://127.0.0.1:1/other"] });
        return JSON.stringify(r[0].result);
      })()`)
    );
    assert.deepEqual([out.ok, out.code], [false, "page_changed"]);
    assert.equal((await frameEval("() => window.__hits || 0")).result, 1, "no second click");
  });
});
