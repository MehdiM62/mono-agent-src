/**
 * Looking at a page in a real browser (issue #941): a page drawn on a canvas,
 * which has no text and an empty accessibility tree, is captured by the real
 * worker (chrome.tabs.captureVisibleTab), capped, and refused where it must be
 * (a private-network page, a tab that is not showing). Nothing here talks to
 * MonoAgent. WITHOUT CHROME the suite skips.
 */

import { after, describe, it } from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { findChrome } from "./browser_harness.mjs";
import { sleep, start } from "./extension_harness.mjs";

const PAGE = `<!doctype html><html><head><meta charset="utf-8"><title>Canvas only</title>
<style>html,body{margin:0;background:#fff}</style></head>
<body><canvas id="c" width="800" height="500"></canvas>
<script>
const g = document.getElementById("c").getContext("2d");
g.fillStyle = "#ff0000"; g.fillRect(0, 0, 400, 500);
g.fillStyle = "#0000ff"; g.beginPath(); g.arc(600, 250, 150, 0, 7); g.fill();
</script></body></html>`;

const CHROME = findChrome();
let browser = null;
let why = "no Chrome found — set CHROME_PATH to run these";
const HOST = "canvas.example"; // resolves to the fixture server, and is not a private name
if (CHROME && typeof WebSocket === "function") {
  try {
    browser = await start(CHROME, { extraArgs: [`--host-resolver-rules=MAP ${HOST} 127.0.0.1`] });
  } catch (e) {
    why = `${CHROME} would not start: ${e.message.split("\n")[0]}`;
  }
}
const server = createServer((req, res) => {
  res.writeHead(200, { "content-type": "text/html; charset=utf-8" });
  res.end(PAGE);
});

after(async () => {
  if (browser) await browser.close();
  server.close();
});

// In the worker: what the picture holds, without sending the picture back.
const SUMMARY = `(async (out) => {
  if (!out.ok) return { ok: false, code: out.code, error: out.error };
  const blob = await (await fetch("data:" + out.image.mime + ";base64," + out.image.data)).blob();
  const bmp = await createImageBitmap(blob);
  const c = new OffscreenCanvas(bmp.width, bmp.height).getContext("2d");
  c.drawImage(bmp, 0, 0);
  const px = c.getImageData(0, 0, bmp.width, bmp.height).data;
  let red = 0, blue = 0;
  for (let i = 0; i < px.length; i += 4) {
    if (px[i] > 200 && px[i + 1] < 60 && px[i + 2] < 60) red++;
    if (px[i + 2] > 200 && px[i] < 60 && px[i + 1] < 60) blue++;
  }
  return { ok: true, mime: out.image.mime, bytes: out.image.bytes, width: out.image.width, height: out.image.height, real: blob.size, red, blue, kind: MonoChatLook.kindOf(out.image.data), url: out.url };
})`;

describe("looking at a canvas page", { skip: browser ? false : why, concurrency: 1 }, () => {
  let sw;
  let port;
  const ev = (expr) => browser.cdp.evaluate(sw, expr);

  async function open(url) {
    const { targetId } = await browser.cdp.send("Target.createTarget", { url });
    await browser.cdp.send("Target.activateTarget", { targetId });
    await sleep(700);
    return targetId;
  }
  const tabFor = (url) => ev(`chrome.tabs.query({}).then((ts) => { const t = ts.find((x) => x.url === ${JSON.stringify(url)}); return t ? t.id : 0; })`);
  const look = (tabId, url) => ev(`MonoChatLook.capture({ type: "page_look_capture", tab: { tabId: ${tabId}, url: ${JSON.stringify(url)} } }).then(${SUMMARY})`);

  async function setup() {
    if (sw) return;
    await new Promise((r) => server.listen(0, "127.0.0.1", r));
    port = server.address().port;
    assert.ok(browser.worker, browser.workerError);
    const { sessionId } = await browser.cdp.send("Target.attachToTarget", { targetId: browser.worker.targetId, flatten: true });
    await browser.cdp.send("Runtime.enable", {}, sessionId);
    sw = sessionId;
  }

  it("captures what the canvas shows, as a capped JPEG, with the address masked", async () => {
    await setup();
    const url = `http://${HOST}:${port}/chart?token=hunter2`;
    await open(url);
    const out = await look(await tabFor(url), url);
    assert.equal(out.ok, true, JSON.stringify(out));
    assert.deepEqual([out.mime, out.kind], ["image/jpeg", "image/jpeg"]);
    assert.ok(out.bytes <= 2 * 1024 * 1024 && out.real <= 2 * 1024 * 1024);
    assert.ok(Math.max(out.width, out.height) <= 1600);
    assert.ok(out.red > 5000, `the red half of the canvas is in the picture (${out.red})`);
    assert.ok(out.blue > 5000, `the blue circle is in the picture (${out.blue})`);
    assert.ok(!out.url.includes("hunter2"), out.url);
  });

  it("a page on the person's own network is never captured", async () => {
    const url = `http://127.0.0.1:${port}/router`;
    await open(url);
    const out = await look(await tabFor(url), url);
    assert.deepEqual([out.ok, out.code], [false, "not_allowed"]);
    assert.match(out.error, /private network/);
  });

  it("a tab that is not the one showing is not captured", async () => {
    const back = `http://${HOST}:${port}/background`;
    await open(back);
    const id = await tabFor(back);
    await open(`http://${HOST}:${port}/front`);
    const out = await look(id, back);
    assert.deepEqual([out.ok, out.code], [false, "not_visible"]);
  });

  it("shrink makes any picture fit: a large noisy image comes out at most 2 MB and 1600 px", async () => {
    const out = await ev(`(async () => {
      const c = new OffscreenCanvas(2000, 1500);
      const g = c.getContext("2d");
      const img = g.createImageData(2000, 1500);
      for (let i = 0; i < img.data.length; i++) img.data[i] = (i % 4 === 3) ? 255 : (Math.random() * 256) | 0;
      g.putImageData(img, 0, 0);
      const png = await c.convertToBlob({ type: "image/png" });
      const r = await MonoChatLook.shrink(png, MonoChatLook.realEnv());
      return { input: png.size, bytes: r.bytes, width: r.width, height: r.height, kind: MonoChatLook.kindOf(r.data) };
    })()`);
    assert.ok(out.input > 2 * 1024 * 1024, `the input is over the cap (${out.input})`);
    assert.ok(out.bytes <= 2 * 1024 * 1024, JSON.stringify(out));
    assert.ok(Math.max(out.width, out.height) <= 1600);
    assert.equal(out.kind, "image/jpeg");
  });
});
