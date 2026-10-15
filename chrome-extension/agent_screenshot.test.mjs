// Tests for screenshot and emulate (agent_screenshot.js) against a fake
// debugger. `node --test 'chrome-extension/**/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

/** png is the first bytes of a PNG of w×h: the signature and IHDR. */
function png(w, h) {
  const b = Buffer.alloc(33);
  Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]).copy(b, 0);
  b.writeUInt32BE(13, 8);
  b.write("IHDR", 12, "latin1");
  b.writeUInt32BE(w, 16);
  b.writeUInt32BE(h, 20);
  return b.toString("base64");
}

/** jpeg is a JPEG header of w×h: SOI, an APP0 and a DQT segment, then SOF0. */
function jpeg(w, h) {
  const app0 = Buffer.concat([Buffer.from([0xff, 0xe0, 0x00, 0x10]), Buffer.alloc(14)]);
  const dqt = Buffer.concat([Buffer.from([0xff, 0xdb, 0x00, 0x43]), Buffer.alloc(65)]);
  const sof = Buffer.from([0xff, 0xc0, 0x00, 0x11, 0x08, h >> 8, h & 0xff, w >> 8, w & 0xff, 0x03]);
  return Buffer.concat([Buffer.from([0xff, 0xd8]), app0, dqt, sof, Buffer.alloc(16)]).toString("base64");
}

function setup(answers = {}) {
  const g = loadExtensionScripts(["agent_cdp.js", "agent_screenshot.js"], {});
  const sent = [];
  let detach = null;
  g.MonoAgentCdp.install({
    attach: async () => {},
    cdp: async (tabId, method, params) => {
      sent.push([method, params]);
      const a = answers[method];
      return typeof a === "function" ? a(params) : a || {};
    },
    onDebuggerEvent: () => {},
    onDebuggerDetach: (fn) => (detach = fn),
  });
  return { shot: g.MonoAgentShot, cdp: g.MonoAgentCdp, sent, detach: (tabId, reason) => detach({ tabId }, reason) };
}

const metrics = {
  cssContentSize: { x: 0, y: 0, width: 1280.4, height: 5000 },
  cssVisualViewport: { pageX: 0, pageY: 300, clientWidth: 1280, clientHeight: 720 },
};

test("imageSize reads PNG and JPEG headers, and says 0×0 for anything else", () => {
  const { shot } = setup();
  assert.deepEqual(shot.imageSize(png(1280, 720), "image/png"), { width: 1280, height: 720 });
  assert.deepEqual(shot.imageSize(jpeg(375, 2400), "image/jpeg"), { width: 375, height: 2400 });
  assert.deepEqual(shot.imageSize("bm90IGFuIGltYWdl", "image/png"), { width: 0, height: 0 });
  assert.deepEqual(shot.imageSize("bm90IGFuIGltYWdl", "image/jpeg"), { width: 0, height: 0 });
});

test("viewport is the default: one capture, no clip, nothing painted beyond the screen", async () => {
  const t = setup({ "Page.captureScreenshot": { data: png(1280, 720) } });
  const out = await t.shot.screenshot({ tabId: 3 });
  assert.deepEqual(out, { data: png(1280, 720), mime: "image/png", width: 1280, height: 720 });
  assert.deepEqual(t.sent, [["Page.captureScreenshot", { format: "png", fromSurface: true }]]);
});

test("full captures the whole document, capped like capture.js, without touching the metrics override", async () => {
  const t = setup({ "Page.getLayoutMetrics": { ...metrics, cssContentSize: { width: 1280.4, height: 40000 } }, "Page.captureScreenshot": { data: png(1281, 16384) } });
  const out = await t.shot.screenshot({ tabId: 3, mode: "full" });
  assert.equal(out.height, 16384);
  const [, params] = t.sent.find(([m]) => m === "Page.captureScreenshot");
  assert.deepEqual(params, { format: "png", fromSurface: true, clip: { x: 0, y: 0, width: 1281, height: 16384, scale: 1 }, captureBeyondViewport: true });
  assert.equal(t.sent.some(([m]) => m.startsWith("Emulation.")), false, "an emulation the coder set must survive a screenshot");
});

test("element finds the selector's box in document coordinates; on screen it needs no beyond-viewport paint", async () => {
  let expression = "";
  const t = setup({
    "Page.getLayoutMetrics": metrics,
    "Runtime.evaluate": (p) => {
      expression = p.expression;
      return { result: { value: { x: 10, y: 400, width: 200, height: 50 } } };
    },
    "Page.captureScreenshot": { data: jpeg(200, 50) },
  });
  const out = await t.shot.screenshot({ tabId: 3, mode: "element", selector: 'button[data-x="a\\"b"]', format: "jpeg", quality: 80 });
  assert.deepEqual([out.mime, out.width, out.height], ["image/jpeg", 200, 50]);
  assert.ok(expression.includes(JSON.stringify('button[data-x="a\\"b"]')), "the selector is quoted, never spliced");
  const [, params] = t.sent.find(([m]) => m === "Page.captureScreenshot");
  assert.deepEqual(params, { format: "jpeg", fromSurface: true, quality: 80, clip: { x: 10, y: 400, width: 200, height: 50, scale: 1 } });
});

test("element mode says what went wrong with the selector", async () => {
  const missing = setup({ "Page.getLayoutMetrics": metrics, "Runtime.evaluate": { result: { type: "object", subtype: "null", value: null } } });
  await assert.rejects(missing.shot.screenshot({ tabId: 3, mode: "element", selector: "#nope" }), /no element matches #nope/);
  const invalid = setup({ "Page.getLayoutMetrics": metrics, "Runtime.evaluate": { exceptionDetails: { text: "SyntaxError" } } });
  await assert.rejects(invalid.shot.screenshot({ tabId: 3, mode: "element", selector: "##" }), /invalid selector ##/);
  const hidden = setup({ "Page.getLayoutMetrics": metrics, "Runtime.evaluate": { result: { value: { x: 0, y: 0, width: 0, height: 0 } } } });
  await assert.rejects(hidden.shot.screenshot({ tabId: 3, mode: "element", selector: "#h" }), /has no size/);
  await assert.rejects(setup().shot.screenshot({ tabId: 3, mode: "element" }), /needs a selector/);
});

test("clip below the fold is painted beyond the viewport", async () => {
  const t = setup({ "Page.getLayoutMetrics": metrics, "Page.captureScreenshot": { data: png(100, 100) } });
  await t.shot.screenshot({ tabId: 3, mode: "clip", clip: { x: 0, y: 3000, width: 100, height: 100 } });
  const [, params] = t.sent.find(([m]) => m === "Page.captureScreenshot");
  assert.equal(params.captureBeyondViewport, true);
  await assert.rejects(t.shot.screenshot({ tabId: 3, mode: "clip", clip: { x: 0, y: 0, width: 0, height: 5 } }), /clip needs numbers/);
});

test("bad params are refused before anything is captured", async () => {
  const t = setup();
  await assert.rejects(t.shot.screenshot({ tabId: 3, mode: "page" }), /mode must be one of viewport, full, element, clip/);
  await assert.rejects(t.shot.screenshot({ tabId: 3, format: "webp" }), /format must be png or jpeg/);
  await assert.rejects(t.shot.screenshot({ tabId: 3, format: "jpeg", quality: 101 }), /quality must be 0 to 100/);
  await assert.rejects(t.shot.screenshot({}), /tabId is required/);
  assert.deepEqual(t.sent, []);
});

test("an image too large for the bridge is refused with what to do instead", async () => {
  const t = setup({ "Page.captureScreenshot": { data: "A".repeat(30 * 1024 * 1024 + 4) } });
  await assert.rejects(t.shot.screenshot({ tabId: 3 }), /too large for the bridge .* use format jpeg, or a clip/);
});

test("emulate sets exactly the metrics named, holds the tab, and reset lets it go", async () => {
  const t = setup();
  const emulation = () => t.sent.filter(([m]) => m.startsWith("Emulation."));
  assert.deepEqual(await t.shot.emulate({ tabId: 5, width: 375, height: 812, deviceScaleFactor: 3, mobile: true }), { ok: true });
  assert.deepEqual(emulation(), [["Emulation.setDeviceMetricsOverride", { width: 375, height: 812, deviceScaleFactor: 3, mobile: true }]]);
  assert.equal(t.cdp.holds(5), true, "the override lasts only while the debugger stays attached");
  await t.shot.emulate({ tabId: 5, media: { prefersColorScheme: "dark" } });
  assert.deepEqual(emulation()[1], ["Emulation.setEmulatedMedia", { features: [{ name: "prefers-color-scheme", value: "dark" }, { name: "prefers-reduced-motion", value: "" }] }]);
  assert.equal(emulation().length, 2, "media alone leaves the metrics as they are");
  await t.shot.emulate({ tabId: 5, reset: true });
  assert.deepEqual(emulation().slice(2), [
    ["Emulation.clearDeviceMetricsOverride", {}],
    ["Emulation.setEmulatedMedia", { media: "", features: [] }],
  ]);
  assert.equal(t.cdp.holds(5), false);
});

test("emulate answers once the page has drawn with the change, or after a second at most", async () => {
  const t = setup();
  await t.shot.emulate({ tabId: 5, reset: true });
  const [method, params] = t.sent[t.sent.length - 1];
  assert.equal(method, "Runtime.evaluate");
  assert.equal(params.awaitPromise, true);
  assert.match(params.expression, /requestAnimationFrame\(\(\) => requestAnimationFrame/);
  const stuck = setup({ "Runtime.evaluate": () => new Promise(() => {}) });
  const started = Date.now();
  assert.deepEqual(await stuck.shot.emulate({ tabId: 5, width: 375 }), { ok: true });
  assert.ok(Date.now() - started < 1500, "a tab that never draws does not hang the command");
});

test("emulate with media cleared and no metrics lets the tab go", async () => {
  const t = setup();
  await t.shot.emulate({ tabId: 5, media: { prefersReducedMotion: "reduce" } });
  assert.equal(t.cdp.holds(5), true);
  await t.shot.emulate({ tabId: 5, media: {} });
  assert.equal(t.cdp.holds(5), false);
});

test("emulate refuses what Chrome would misread", async () => {
  const t = setup();
  await assert.rejects(t.shot.emulate({ tabId: 5 }), /name width, height, deviceScaleFactor, mobile or media, or reset/);
  await assert.rejects(t.shot.emulate({ tabId: 5, width: -1 }), /width must be 0 to 10000/);
  await assert.rejects(t.shot.emulate({ tabId: 5, deviceScaleFactor: 11 }), /deviceScaleFactor must be 0 to 10/);
  await assert.rejects(t.shot.emulate({ tabId: 5, mobile: "yes" }), /mobile must be true or false/);
  await assert.rejects(t.shot.emulate({ tabId: 5, media: { prefersColorScheme: "sepia" } }), /prefersColorScheme must be light or dark/);
  await assert.rejects(t.shot.emulate({ tabId: 5, media: "dark" }), /media must be an object/);
  assert.deepEqual(t.sent, []);
  assert.equal(t.cdp.holds(5), false);
});

test("Cancel on the debugging banner ends the emulation's hold", async () => {
  const t = setup();
  await t.shot.emulate({ tabId: 5, width: 375 });
  t.detach(5, "canceled_by_user");
  assert.equal(t.cdp.holds(5), false);
});
