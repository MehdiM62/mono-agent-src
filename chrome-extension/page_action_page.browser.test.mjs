/**
 * page_action_page.js, in a real browser: how a card's words find one
 * element (exact before partial, hidden and disabled ignored, never a guess
 * between two), real pointer events with a real dblclick, typing that
 * reaches the page's own listeners, and a click that could submit a form
 * held back.
 * WITHOUT CHROME the suite skips (see highlight_page.browser.test.mjs).
 */

import { after, describe, it } from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { fileUrl, findChrome, launch } from "./browser_harness.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const CHROME = findChrome();
let browser = null;
let why = "no Chrome found — set CHROME_PATH to run these";
if (CHROME) {
  try {
    browser = await launch(CHROME);
  } catch (error) {
    why = `${CHROME} would not start: ${error.message.split("\n")[0]} (MONO_CHROME_NO_SANDBOX=1 may help)`;
  }
}
const skip = browser ? false : why;
let dir = null;
after(async () => {
  if (browser) await browser.close();
  if (dir) await rm(dir, { recursive: true, force: true }).catch(() => {});
});

const PAGE = `<!doctype html><meta charset=utf-8><body>
<div id=grid>
  <div role=option id=cv aria-label="CV - Morteza Nokhodian 2025 Apr"><div><span>CV - Morteza Nokhodian 2025 Apr</span></div></div>
  <div role=option id=cv2><span>CV - Morteza Nokhodian 2025 Apr.pdf</span></div>
  <div role=option id=po><span>Purchase order 2025.pdf</span></div>
  <div role=option id=twin1><span>Twin</span></div>
  <div role=option id=twin2><span>Twin</span></div>
  <div role=option id=partial1><span>Annual report final draft</span></div>
  <div role=option id=partial2><span>Annual report final draft v2</span></div>
</div>
<button id=plain type=button><span>Open menu</span></button>
<button id=ghost type=button style="display:none">Ghost</button>
<button id=ghost2 type=button style="visibility:hidden">Phantom</button>
<button id=off type=button disabled>Disabled one</button>
<button id=live type=button>Disabled one</button>
<button id=dupe1 type=button>Duplicate</button>
<button id=dupe2 type=button>Duplicate</button>
<button id=exact type=button>Share</button>
<button id=partialshare type=button>Share with everyone in the world</button>
<label for=email>Email address</label><input id=email name=email>
<input id=search placeholder="Search files" name=search>
<textarea id=note aria-label=Notes></textarea>
<input id=pw type=password aria-label="Account password">
<div id=rich contenteditable=true role=textbox aria-label="Rich body"></div>
<form id=f onsubmit="window.__log.push('SUBMITTED'); return false">
  <button id=insideform type=button>Next step</button>
  <button id=plainsubmit>Continue</button>
  <input id=subinput type=submit value="Go on">
</form>
<button id=postit type=button>Post</button>
<button id=fine type=button>Save as draft</button>
<a id=sendlink href="#x">Send us feedback</a>
<script>
  window.__log = [];
  for (const el of document.querySelectorAll("[id]")) {
    for (const t of ["click", "dblclick", "mousedown", "pointerdown", "mouseup"]) {
      el.addEventListener(t, (e) => { if (e.target === el || el.contains(e.target)) window.__log.push(t + ":" + el.id + ":" + e.detail + (e.isTrusted ? "T" : "")); });
    }
  }
  document.querySelector("#email").addEventListener("input", (e) => window.__log.push("input:" + e.target.id + ":" + e.target.value));
  document.querySelector("#email").addEventListener("change", (e) => window.__log.push("change:" + e.target.id));
</script>`;

describe("page_action_page.js in a browser", { skip, concurrency: 1 }, () => {
  async function open(html = PAGE) {
    dir = await mkdtemp(join(tmpdir(), "mono-act-"));
    const page = join(dir, "page.html");
    await writeFile(page, html, "utf8");
    await browser.navigate(fileUrl(page));
    await browser.evaluate(await readFile(join(HERE, "page_action_page.js"), "utf8"));
  }
  const json = async (expr) => JSON.parse(await browser.evaluate(`JSON.stringify(${expr})`));
  const run = (action, target, text) => json(`MonoPageAction.run(${JSON.stringify(action)}, ${JSON.stringify(target)}, ${JSON.stringify(text)})`);
  const log = (prefix) => json("window.__log").then((l) => (prefix ? l.filter((x) => x.startsWith(prefix)) : l));

  it("finds an element by its aria-label once, even with the same words nested inside it", async () => {
    await open();
    assert.deepEqual(await run("click", "cv - morteza nokhodian 2025 apr"), { ok: true }, "case-insensitive; the label and the text inside are one target");
    assert.ok((await log("click:cv:")).length === 1);
  });

  it("prefers an exact match to a longer text that contains the words", async () => {
    await open();
    assert.deepEqual(await run("click", "Share"), { ok: true });
    assert.deepEqual(await log("click:"), ["click:exact:1"], "'Share' is the button called Share, not 'Share with everyone…'");
  });

  it("falls back to a text that contains the words when nothing is an exact match", async () => {
    await open();
    assert.deepEqual(await run("click", "Open men"), { ok: true });
    assert.deepEqual(await log("click:"), ["click:plain:1"], "the button, not the span inside it");
  });

  it("does not guess between two equally good matches", async () => {
    await open();
    assert.deepEqual(await run("click", "Twin"), { ok: false, code: "ambiguous", error: "ambiguous (2 matches)" });
    assert.deepEqual(await run("click", "Annual report"), { ok: false, code: "ambiguous", error: "ambiguous (2 matches)" }, "two partial matches");
    assert.deepEqual(await run("click", "Duplicate"), { ok: false, code: "ambiguous", error: "ambiguous (2 matches)" });
    assert.deepEqual(await log("click:"), [], "nothing was clicked");
  });

  it("says not found for nothing, and ignores hidden and disabled elements", async () => {
    await open();
    for (const target of ["No such thing", "Ghost", "Phantom"]) {
      assert.deepEqual(await run("click", target), { ok: false, code: "not_found", error: "not found" }, target);
    }
    assert.deepEqual(await run("click", "Disabled one"), { ok: true }, "the disabled twin does not make it ambiguous");
    assert.deepEqual(await log("click:"), ["click:live:1"]);
  });

  it("a double-click is two clicks and a real dblclick, on the item", async () => {
    await open();
    assert.deepEqual(await run("double_click", "CV - Morteza Nokhodian 2025 Apr.pdf"), { ok: true });
    const l = await log("");
    assert.deepEqual(l.filter((x) => /^(click|dblclick):cv2:/.test(x)), ["click:cv2:1", "click:cv2:2", "dblclick:cv2:2"]);
  });

  it("a file whose name says 'order' is not a button, so it opens", async () => {
    await open();
    assert.deepEqual(await run("double_click", "Purchase order 2025.pdf"), { ok: true });
    assert.ok((await log("dblclick:po:")).length === 1);
  });

  it("types like a person: focus, the value, input and change", async () => {
    await open();
    assert.deepEqual(await run("type", "Email address", "ada@example.com"), { ok: true }, "by its label");
    assert.equal(await browser.evaluate('document.querySelector("#email").value'), "ada@example.com");
    assert.deepEqual(await log(""), ["input:email:ada@example.com", "change:email"]);
    assert.equal(await browser.evaluate("document.activeElement.id"), "email");
    assert.deepEqual(await run("type", "search files", "q"), { ok: true }, "by its placeholder");
    assert.deepEqual(await run("type", "Notes", "line 1\nline 2"), { ok: true }, "by its aria-label");
    assert.equal(await browser.evaluate('document.querySelector("#note").value'), "line 1\nline 2");
    assert.deepEqual(await run("type", "Rich body", "hello"), { ok: true });
    assert.equal(await browser.evaluate('document.querySelector("#rich").textContent'), "hello");
  });

  it("types only into fields, never a password", async () => {
    await open();
    assert.deepEqual(await run("type", "Share", "x"), { ok: false, code: "not_found", error: "not found" }, "a button is not a field");
    const pw = await run("type", "Account password", "hunter2");
    assert.deepEqual([pw.ok, pw.code], [false, "not_allowed"]);
    assert.equal(await browser.evaluate('document.querySelector("#pw").value'), "");
  });

  it("never clicks what could submit a form", async () => {
    await open();
    for (const target of ["Continue", "Go on", "Next step", "Post"]) {
      const out = await run("click", target);
      assert.deepEqual([out.ok, out.code], [false, "submit_blocked"], target);
      assert.match(out.error, /press that button yourself/);
    }
    assert.deepEqual(await run("click", "Save as draft"), { ok: true }, "a plain button outside a form");
    assert.deepEqual((await log("")).filter((x) => /SUBMITTED/.test(x) || /^click:(plainsubmit|subinput|insideform|postit):/.test(x)), [], "nothing was clicked or submitted");
    assert.deepEqual(await run("double_click", "Continue").then((o) => o.code), "submit_blocked");
  });

  it("holds back a link-like 'send' only when it is a button", async () => {
    await open();
    assert.deepEqual(await run("click", "Send us feedback"), { ok: true }, "a link is followed");
  });
});
