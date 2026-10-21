/**
 * content.js openedFileInput, in a real browser: clicking a control that
 * opens the file chooser hands back the input and cancels the chooser.
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

// content.js is a content script (it needs chrome.*), so the one function is
// cut out of its source and run in an ordinary page.
async function openedFileInputSource() {
  const src = await readFile(join(HERE, "content.js"), "utf8");
  const start = src.indexOf("function openedFileInput(");
  const end = src.indexOf("async function setFiles(");
  assert.ok(start > 0 && end > start, "openedFileInput not found in content.js");
  return src.slice(start, end);
}

const PAGE = `<!doctype html><meta charset=utf-8><body>
<input id=real type=file style="display:none">
<button id=styled onclick="document.getElementById('real').click()">Upload</button>
<label id=lab for=real>Pick</label>
<input id=visible type=file>
<button id=detached onclick="const i=document.createElement('input');i.type='file';i.click()">Detached</button>
<button id=nothing>Nothing</button>
<script>window.__clicks = []; document.addEventListener('click', (e) => window.__clicks.push(e.target.id), true);</script>`;

describe("content.js openedFileInput in a browser", { skip, concurrency: 1 }, () => {
  async function open() {
    dir = await mkdtemp(join(tmpdir(), "mono-upload-"));
    const page = join(dir, "page.html");
    await writeFile(page, PAGE, "utf8");
    await browser.navigate(fileUrl(page));
    await browser.evaluate(`${await openedFileInputSource()}; window.opened = openedFileInput;`);
  }
  const idOf = (trigger) =>
    browser.evaluate(`opened(document.getElementById(${JSON.stringify(trigger)}), 400).then((el) => el.id, (e) => "ERR:" + e.message)`);

  it("finds the input behind a styled button, a label, or the input itself", async () => {
    await open();
    assert.equal(await idOf("styled"), "real");
    assert.equal(await idOf("lab"), "real");
    assert.equal(await idOf("visible"), "visible");
  });

  it("says so when the click opens no chooser", async () => {
    await open();
    assert.match(await idOf("nothing"), /^ERR:.*did not open a file chooser/);
    assert.match(await idOf("detached"), /^ERR:.*did not open a file chooser/);
  });

  it("stops listening afterwards", async () => {
    await open();
    await idOf("styled");
    const before = await browser.evaluate("window.__clicks.length");
    await browser.evaluate("document.getElementById('nothing').click()");
    assert.equal(await browser.evaluate("window.__clicks.length"), before + 1);
  });
});
