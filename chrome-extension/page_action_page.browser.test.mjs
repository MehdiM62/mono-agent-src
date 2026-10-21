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
  <div role=option id=partial2><span>Annual report v2 draft</span></div>
  <div role=option id=longer><span>Quarterly sales numbers for the whole of the northern region</span></div>
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
<a id=postal href="#p">Postal information</a>
<button id=del type=button>Delete forever</button>
<button id=loesch type=button>Löschen</button>
<button id=bez type=button>Jetzt bezahlen</button>
<button id=comp type=button>Comprar ahora</button>
<button id=logout type=button>Log out</button>
<button id=cart type=button>Add to cart</button>
<span id=oc onclick="window.__log.push('oc')" role=menuitem>Remove item</span>
<form id=f2 onsubmit="return false"><a id=formlink href="#y">Read more</a><div role=option id=formrow>Report.pdf</div><span id=bare>Plain words</span></form>
<input id=hiddensub type=submit form=f style="display:none"><label id=lab for=hiddensub>Proceed to the next page</label>
<button id=lb type=button aria-labelledby=lbl></button><span id=lbl hidden>Delete my account</span>
<a id=alt href="#z"><img alt="Subscribe now" src="data:image/gif;base64,R0lGODlhAQABAAAAACw=" width=20 height=20></a>
<input id=tb type=button value="Send it" onclick="window.__log.push('tb')">
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
  const full = (action, target, text, origin) => json(`MonoPageAction.run(${JSON.stringify(action)}, ${JSON.stringify(target)}, ${JSON.stringify(text)}, ${JSON.stringify(origin)})`);
  // What matters of a result: ok, or why not.
  const run = async (action, target, text) => {
    const { matched, exact, ...rest } = await full(action, target, text);
    return rest;
  };
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
    const out = await full("click", "Save as");
    assert.deepEqual([out.ok, out.exact, out.matched], [true, false, "Save as draft"], "it says what it matched");
    assert.deepEqual(await log("click:"), ["click:fine:1"]);
    await full("click", "Open menu");
    assert.deepEqual(await log("click:plain"), ["click:plain:1"], "the button, not the span inside it");
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
      assert.match(out.error, /press that yourself/);
    }
    assert.deepEqual(await run("click", "Save as draft"), { ok: true }, "a plain button outside a form");
    assert.deepEqual((await log("")).filter((x) => /SUBMITTED/.test(x) || /^click:(plainsubmit|subinput|insideform|postit):/.test(x)), [], "nothing was clicked or submitted");
    assert.deepEqual(await run("double_click", "Continue").then((o) => o.code), "submit_blocked");
  });

  it("a link whose words are a submit-like verb is held back too; a longer word is not the verb", async () => {
    await open();
    assert.equal((await run("click", "Send us feedback")).code, "submit_blocked");
    assert.deepEqual(await run("click", "Postal information"), { ok: true }, "'Postal' is not 'Post'");
  });

  it("holds back destructive and account verbs, in English, German, Spanish, on buttons, menu items and [onclick]", async () => {
    await open();
    for (const target of ["Delete forever", "Löschen", "Jetzt bezahlen", "Comprar ahora", "Log out", "Add to cart", "Remove item", "Send it"]) {
      assert.equal((await run("click", target)).code, "submit_blocked", target);
    }
    assert.deepEqual((await log("")).filter((x) => /^(click|mousedown|pointerdown|mouseup):(del|loesch|bez|comp|logout|cart|oc|tb)/.test(x) || x === "oc" || x === "tb"), [], "none was clicked");
  });

  it("anything clickable inside a form is held back, whatever it says; a row outside is not", async () => {
    await open();
    assert.equal((await run("click", "Read more")).code, "submit_blocked", "a link in a form");
    assert.equal((await run("double_click", "Report.pdf")).code, "submit_blocked", "a role=option row in a form");
    assert.deepEqual(await run("click", "Plain words"), { ok: true }, "plain text with nothing clickable around it");
  });

  it("a visible label of a hidden submit is held back: the control is what the click reaches", async () => {
    await open();
    assert.equal((await run("click", "Proceed to the next page")).code, "submit_blocked");
    assert.deepEqual((await log("")).filter((x) => /hiddensub|SUBMITTED/.test(x)), []);
  });

  it("reads aria-labelledby text and an image's alt, as the resolver does", async () => {
    await open();
    assert.equal((await run("click", "Delete my account")).code, "submit_blocked", "the button is named by a hidden span");
    assert.equal((await run("click", "Subscribe now")).code, "submit_blocked", "a link named by its image");
  });

  it("partial matches are whole words in a name at most twice as long, and never used for typing", async () => {
    await open();
    assert.deepEqual(await run("click", "Open men"), { ok: false, code: "not_found", error: "not found" }, "not inside a word");
    assert.deepEqual(await run("click", "Save a"), { ok: false, code: "not_found", error: "not found" }, "'a' ends inside 'as'");
    assert.equal((await full("click", "Share")).matched, "Share", "an exact match says its own name");
    assert.deepEqual(await run("click", "sales"), { ok: false, code: "not_found", error: "not found" }, "the name is far longer than the words");
    assert.deepEqual(await run("type", "Search", "q"), { ok: false, code: "not_found", error: "not found" }, "typing wants the exact name (the placeholder is 'Search files')");
    assert.deepEqual(await run("type", "Search files", "q"), { ok: true });
  });

  it("acts only on the origin the card was made for: the page checks its own", async () => {
    await open();
    const out = await full("click", "Share", undefined, "https://other.example");
    assert.deepEqual([out.ok, out.code], [false, "page_changed"]);
    assert.deepEqual(await log("click:"), []);
    const here = await browser.evaluate("location.origin");
    assert.equal((await full("click", "Share", undefined, here)).ok, true);
  });

  const QUIZ = `<!doctype html><meta charset=utf-8><body style="height:4000px">
<form id=q onsubmit="window.__log.push('SUBMITTED'); return false">
  <div class=row id=r1><input type=radio name=a id=a1><div>GBP 700 total profit</div></div>
  <div class=row id=r2><input type=radio name=a id=a2><div>GBP 200 premium only</div></div>
  <label><input type=checkbox id=news> Send me the newsletter</label>
  <label for=country>Country</label><select id=country><option>France</option><option>Ireland</option></select>
  <button id=cont>Continue</button>
</form>
<div id=host></div>
<div id=menu>Products</div>
<script>
  window.__log = [];
  document.querySelector("#host").attachShadow({ mode: "open" }).innerHTML = "<label>Shadow choice <input type=checkbox id=sc></label><button id=sb type=button>Inside shadow</button>";
  document.querySelector("#menu").addEventListener("mouseover", () => window.__log.push("hover:menu"));
  document.addEventListener("keydown", (e) => window.__log.push("key:" + e.key));
  document.querySelector("#country").addEventListener("change", (e) => window.__log.push("country:" + e.target.value));
</script>`;

  it("ticks a radio inside a form by the words beside it, without submitting", async () => {
    await open(QUIZ);
    assert.deepEqual(await run("check", "GBP 200 premium only"), { ok: true });
    assert.equal(await json("document.querySelector('#a2').checked"), true);
    assert.equal(await json("document.querySelector('#a1').checked"), false);
    assert.deepEqual(await log("SUBMITTED"), []);
    assert.equal((await run("check", "Send me the newsletter")).ok, true);
    assert.equal(await json("document.querySelector('#news').checked"), true);
    assert.equal((await run("uncheck", "Send me the newsletter")).ok, true);
    assert.equal(await json("document.querySelector('#news').checked"), false);
    assert.equal((await run("uncheck", "GBP 200 premium only")).code, "not_allowed", "a radio is unchecked by choosing another");
  });

  it("check is for boxes: a button's words are not a box, and Continue is still held back", async () => {
    await open(QUIZ);
    assert.equal((await run("check", "Continue")).code, "not_found");
    assert.equal((await run("click", "Continue")).code, "submit_blocked");
    assert.deepEqual(await log("SUBMITTED"), []);
  });

  it("chooses an option in a drop-down, like a person: value, input, change", async () => {
    await open(QUIZ);
    assert.deepEqual(await run("select", "Country", "Ireland"), { ok: true });
    assert.deepEqual(await log("country:"), ["country:Ireland"]);
    assert.equal((await run("select", "Country", "Atlantis")).code, "not_found");
  });

  it("reaches an element inside an open shadow root", async () => {
    await open(QUIZ);
    assert.equal((await run("check", "Shadow choice")).ok, true);
    assert.equal(await json("document.querySelector('#host').shadowRoot.querySelector('#sc').checked"), true);
    assert.equal((await run("click", "Inside shadow")).ok, true);
  });

  const CONSENT = `<!doctype html><meta charset=utf-8><body>
<form onsubmit="window.__log.push('SUBMITTED'); return false">
  <label><input type=checkbox id=del> Delete my account and all data</label>
  <label for=ctc>Allow access to my contacts</label><input type=checkbox id=ctc>
  <div role=switch aria-checked=false id=sw tabindex=0>Authorize payments</div>
  <label for=ok>Remind me weekly</label><input type=checkbox id=ok>
  <div id=terms><input type=checkbox id=t1><span>Terms of service</span><span>Marketing emails</span></div>
  <div id=solo><input type=checkbox id=t2><span>Remember me</span></div>
  <div id=lby><span id=lbl>Show hints</span><input type=checkbox id=t3 aria-labelledby=lbl></div>
  <label for=act>Action</label><select id=act><option>Keep</option><option>Delete everything</option></select>
  <label for=risky>Delete the selected rows</label><select id=risky><option>Yes</option><option>No</option></select>
</form>
<script>
  window.__log = [];
  for (const id of ["act", "risky"]) for (const t of ["input", "change"]) document.getElementById(id).addEventListener(t, () => window.__log.push(t + ":" + id));
</script>`;

  it("check holds back a box whose label is a consent or destructive verb; a plain box is ticked", async () => {
    await open(CONSENT);
    for (const [target, id] of [["Delete my account and all data", "del"], ["Allow access to my contacts", "ctc"], ["Authorize payments", "sw"]]) {
      const out = await run("check", target);
      assert.deepEqual([out.ok, out.code], [false, "submit_blocked"], target);
      assert.match(out.error, /press that yourself/);
      assert.notEqual(await json(`document.getElementById("${id}").checked || document.getElementById("${id}").getAttribute("aria-checked") === "true"`), true, id + " stays off");
    }
    assert.equal((await run("check", "Remind me weekly")).ok, true);
    assert.equal(await json("document.getElementById('ok').checked"), true);
  });

  it("select holds back a destructive label or chosen option, and fires nothing", async () => {
    await open(CONSENT);
    assert.equal((await run("select", "Action", "Delete everything")).code, "submit_blocked", "the chosen option says delete");
    assert.equal((await run("select", "Delete the selected rows", "Yes")).code, "submit_blocked", "the select's label says delete");
    assert.deepEqual(await log("input:"), []);
    assert.deepEqual(await log("change:"), []);
    assert.equal((await run("select", "Action", "Keep")).ok, true, "a harmless option still works");
  });

  it("check resolves its box by the label, never by proximity: one label cannot tick a box beside it", async () => {
    await open(CONSENT);
    const out = await run("check", "Marketing emails");
    assert.deepEqual([out.ok, out.code], [false, "ambiguous"], "the lone box in that row belongs to 'Terms of service'");
    assert.equal(await json("document.getElementById('t1').checked"), false);
    assert.equal((await run("check", "Remember me")).ok, true, "a box and its one text, side by side, are tied");
    assert.equal(await json("document.getElementById('t2').checked"), true);
    assert.equal((await run("check", "Show hints")).ok, true, "aria-labelledby ties a box to its text");
    assert.equal(await json("document.getElementById('t3').checked"), true);
    assert.deepEqual(await log("SUBMITTED"), []);
  });

  it("find says the frame it searched: its origin and address (a subframe step is checked against them)", async () => {
    await open();
    const out = await json("MonoPageAction.find('click', 'Share')");
    assert.equal(out.origin, await json("location.origin"), "find says the frame's origin");
    assert.ok(out.href, "and its address");
  });

  const SNEAKY = `<!doctype html><meta charset=utf-8><body>
<form id=f onsubmit="window.__log.push('SUBMITTED'); return false"><div id=host></div></form>
<div id=d1 tabindex=0 onfocus="void 0">Delete account</div>
<div id=d2 role=switch tabindex=0>Revoke access</div>
<a id=js href="javascript:window.__log.push('JS')">Open the guide</a>
<a id=dl href="/account/delete?confirm=1">Download CV</a>
<a id=ok href="/docs/guide">Read the guide</a>
<button id=acc type=button>Accept all</button>
<button id=agr type=button>I agree</button>
<a id=dat href="data:text/html,hi">Preview thing</a>
<script>
  window.__log = [];
  document.querySelector("#host").attachShadow({ mode: "open" }).innerHTML = "<button type=button id=sb>Shadow go</button>";
  for (const el of document.querySelectorAll("[id]")) el.addEventListener("click", (e) => { window.__log.push("click:" + el.id); e.preventDefault(); });
  document.querySelector("#host").shadowRoot.querySelector("#sb").addEventListener("click", () => window.__log.push("click:sb"));
</script>`;

  it("a button in a shadow root inside a form is held back like any in a form", async () => {
    await open(SNEAKY);
    assert.equal((await run("click", "Shadow go")).code, "submit_blocked");
    assert.deepEqual(await log("click:sb"), []);
  });

  it("every actionable target is checked by its words, not just buttons and links", async () => {
    await open(SNEAKY);
    for (const t of ["Delete account", "Revoke access", "Accept all", "I agree"]) assert.equal((await run("click", t)).code, "submit_blocked", t);
    assert.deepEqual(await log("click:"), []);
  });

  it("a link is judged by where it goes: javascript: and data: never, a path of delete held, a plain one fine", async () => {
    await open(SNEAKY);
    for (const t of ["Open the guide", "Preview thing", "Download CV"]) assert.equal((await run("click", t)).code, "submit_blocked", t);
    assert.deepEqual(await log(""), [], "the javascript: link did not run");
    assert.deepEqual(await run("click", "Read the guide"), { ok: true });
  });

  it("hovers, scrolls the page, and presses only the safe keys", async () => {
    await open(QUIZ);
    assert.equal((await run("hover", "Products")).ok, true);
    assert.deepEqual(await log("hover:"), ["hover:menu"]);
    assert.equal((await run("scroll", "", "bottom")).ok, true);
    assert.ok((await json("window.scrollY")) > 500);
    assert.equal((await run("scroll", "", "top")).ok, true);
    assert.equal(await json("window.scrollY"), 0);
    assert.equal((await run("press_key", "", "Tab")).ok, true);
    assert.deepEqual(await log("key:"), ["key:Tab"]);
    assert.equal((await run("press_key", "", "Enter")).code, "not_allowed");
    assert.deepEqual(await log("key:"), ["key:Tab"], "Enter was not pressed");
  });
});
