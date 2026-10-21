// Pictures for the chat (chat_look.js, issue #941): the limits, what may be
// captured, and the capture itself against a fake chrome.
// `CHROME_PATH=/nonexistent node --test chrome-extension/chat_look.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoChatLook: L } = loadExtensionScripts(["private_net.js", "agent_redact.js", "chat_look.js"]);

const b64 = (bytes) => Buffer.from(bytes).toString("base64");
const PNG = b64([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0]);
const JPG = b64([0xff, 0xd8, 0xff, 0xe0, 0, 0, 0, 0, 0, 0, 0, 0]);
const GIF = b64(Buffer.from("GIF89a......"));
const WEBP = b64(Buffer.from("RIFF....WEBPVP8 "));

test("kindOf reads the type from the bytes, not the name", () => {
  assert.deepEqual([PNG, JPG, GIF, WEBP].map(L.kindOf), ["image/png", "image/jpeg", "image/gif", "image/webp"]);
  for (const text of ["#!/bin/sh\nrm", "<svg onload=x>", "%PDF-1.7 ......", "<html>"]) assert.equal(L.kindOf(b64(Buffer.from(text))), "", text);
  assert.equal(L.kindOf("not base64 !!"), "");
});

test("checkImages: at most three, 2 MB each, only what their bytes say", () => {
  assert.deepEqual(L.checkImages(undefined), []);
  assert.deepEqual(L.checkImages([{ mime: "image/png", data: PNG }, { data: JPG }]), [{ mime: "image/png", data: PNG }, { mime: "image/jpeg", data: JPG }]);
  assert.throws(() => L.checkImages([{ data: PNG }, { data: PNG }, { data: PNG }, { data: PNG }]), /at most 3/);
  assert.throws(() => L.checkImages([{ mime: "image/jpeg", data: PNG }]), /not a PNG/);
  assert.throws(() => L.checkImages([{ mime: "image/png", data: b64(Buffer.from("#!/bin/sh")) }]), /not a PNG/);
  assert.throws(() => L.checkImages([{ mime: "image/png", data: "" }]), /no data/);
  assert.throws(() => L.checkImages("x"), /list/);
  const edge = (bytes) => b64(Buffer.concat([Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]), Buffer.alloc(bytes - 8)]));
  assert.equal(L.checkImages([{ data: edge(L.MAX_BYTES) }]).length, 1, "exactly the cap is fine");
  assert.throws(() => L.checkImages([{ data: edge(L.MAX_BYTES + 1) }]), /over 2 MB/);
});

test("whyNot: only web pages, never the person's own network", () => {
  assert.equal(L.whyNot("https://example.com/a"), "");
  assert.equal(L.whyNot("http://example.com/a"), "");
  for (const url of ["http://192.168.1.1/admin", "http://localhost:3000/", "http://10.0.0.5/", "https://nas.local/", "http://169.254.169.254/latest", "http://[::1]/", "http://127.0.0.1.nip.io/"]) {
    assert.match(L.whyNot(url), /private network/, url);
  }
  for (const url of ["file:///etc/passwd", "chrome://settings", "about:blank", "data:text/html,x", "javascript:1", "nonsense"]) assert.notEqual(L.whyNot(url), "", url);
});

// A fake canvas whose JPEG size depends on its area and quality, so the loop can be watched.
function fakeEnv({ bytesPer = 1, bitmap = { width: 4000, height: 2000 } } = {}) {
  const drawn = [];
  const closed = [];
  return {
    drawn,
    closed,
    env: {
      createImageBitmap: async (b) => {
        if (b.broken) throw new Error("decode");
        return { ...bitmap, close: () => closed.push(1) };
      },
      canvas: (w, h) => ({
        getContext: () => ({ fillStyle: "", fillRect() {}, drawImage: (_b, _x, _y, dw, dh) => drawn.push([dw, dh]) }),
        convertToBlob: async ({ type, quality }) => ({ size: Math.round(w * h * quality * bytesPer), type, arrayBuffer: async () => new Uint8Array([0xff, 0xd8, 0xff, 1, 2, 3]).buffer }),
      }),
    },
  };
}

test("shrink: longest side 1600, JPEG, and as small as the cap needs", async () => {
  const f = fakeEnv({ bytesPer: 0.1 }); // 1600x800x.85x.1 = 108 KB
  const out = await L.shrink({}, f.env);
  assert.deepEqual([out.mime, out.width, out.height], ["image/jpeg", 1600, 800]);
  assert.ok(out.bytes <= L.MAX_BYTES);
  assert.equal(L.kindOf(out.data), "image/jpeg");
  assert.equal(f.closed.length, 1, "the bitmap is released");
  const small = await L.shrink({}, fakeEnv({ bitmap: { width: 300, height: 200 }, bytesPer: 0.1 }).env);
  assert.deepEqual([small.width, small.height], [300, 200], "a small picture is not enlarged");
});

test("shrink lowers quality, then size, until it fits; a picture that never fits is refused", async () => {
  // 1600x800 = 1.28M px: at 8 bytes/px/quality even .4 is 4 MB; needs a smaller scale.
  const f = fakeEnv({ bytesPer: 8 });
  const out = await L.shrink({}, f.env);
  assert.ok(out.bytes <= L.MAX_BYTES);
  assert.ok(out.width < 1600, "it had to shrink the picture itself");
  await assert.rejects(L.shrink({}, fakeEnv({ bytesPer: 1e6 }).env), /cannot be made small enough/);
  await assert.rejects(L.shrink({ broken: true }, fakeEnv().env), /not a picture/);
});

function chromeFor({ tab = { id: 5, windowId: 2, active: true, url: "https://app.example/dash?token=abc123" }, after, shot = "data:image/jpeg;base64,/9j/AAAA" } = {}) {
  const calls = [];
  let gets = 0;
  return {
    calls,
    chrome: {
      tabs: {
        get: async (id) => {
          calls.push(["get", id]);
          if (tab === null) throw new Error("No tab");
          return ++gets > 1 && after ? { ...tab, ...after } : tab;
        },
        captureVisibleTab: async (win, opts) => {
          calls.push(["capture", win, opts]);
          return shot;
        },
      },
    },
    fetch: async () => ({ blob: async () => ({ fromShot: true }) }),
  };
}
const MSG = { type: "page_look_capture", tab: { tabId: 5, url: "https://app.example/dash" } };
const captured = (c) => c.calls.some(([k]) => k === "capture");

test("capture takes the visible tab's picture, capped, with the address masked", async () => {
  const c = chromeFor();
  const out = await L.capture(MSG, { chrome: c.chrome, fetch: c.fetch, env: fakeEnv({ bytesPer: 0.1 }).env });
  assert.equal(out.ok, true);
  assert.equal(out.image.mime, "image/jpeg");
  assert.ok(out.image.bytes <= L.MAX_BYTES && out.image.width <= L.MAX_EDGE);
  assert.ok(!out.url.includes("abc123"), `the address shown keeps no secret: ${out.url}`);
  assert.deepEqual(c.calls.find(([k]) => k === "capture"), ["capture", 2, { format: "jpeg", quality: 90 }]);
});

test("capture refuses, without taking a picture: a private-network page, a tab not showing, a page that moved, a non-web page, a gone tab", async () => {
  const dep = (c) => ({ chrome: c.chrome, fetch: c.fetch, env: fakeEnv().env });
  const cases = [
    ["private", chromeFor({ tab: { id: 5, windowId: 2, active: true, url: "http://192.168.0.1/admin" } }), { tab: { tabId: 5, url: "http://192.168.0.1/admin" } }, "not_allowed"],
    ["localhost", chromeFor({ tab: { id: 5, windowId: 2, active: true, url: "http://localhost:8080/" } }), { tab: { tabId: 5, url: "http://localhost:8080/" } }, "not_allowed"],
    ["background tab", chromeFor({ tab: { id: 5, windowId: 2, active: false, url: "https://app.example/dash" } }), MSG, "not_visible"],
    ["other site", chromeFor({ tab: { id: 5, windowId: 2, active: true, url: "https://evil.example/" } }), MSG, "page_changed"],
    ["file page", chromeFor(), { tab: { tabId: 5, url: "file:///home/x/a.html" } }, "not_allowed"],
    ["no tab id", chromeFor(), { tab: { url: "https://app.example/" } }, "invalid"],
    ["gone", chromeFor({ tab: null }), MSG, "page_changed"],
  ];
  for (const [name, c, extra, code] of cases) {
    const out = await L.capture({ ...MSG, ...extra }, dep(c));
    assert.deepEqual([out.ok, out.code], [false, code], name);
    assert.equal(captured(c), false, `${name}: no picture was taken`);
  }
});

test("a tab that moved to another site while the picture was taken is not handed over", async () => {
  const c = chromeFor({ after: { url: "https://bank.example/account" } });
  const out = await L.capture(MSG, { chrome: c.chrome, fetch: c.fetch, env: fakeEnv().env });
  assert.deepEqual([out.ok, out.code], [false, "page_changed"]);
  assert.equal(out.image, undefined);
});

test("a browser failure is an answer, not a throw", async () => {
  const c = chromeFor();
  c.chrome.tabs.captureVisibleTab = async () => {
    throw new Error("Cannot access contents of the page");
  };
  const out = await L.capture(MSG, { chrome: c.chrome, fetch: c.fetch, env: fakeEnv().env });
  assert.deepEqual([out.ok, out.code, out.error], [false, "failed", "Cannot access contents of the page"]);
});

test("install answers the side panel only", async () => {
  let listener;
  const sandbox = loadExtensionScripts(["private_net.js", "agent_redact.js", "chat_look.js"], {
    chrome: { runtime: { id: "me", getURL: () => "chrome-extension://me/", onMessage: { addListener: (fn) => (listener = fn) } } },
  });
  sandbox.MonoChatLook.install();
  assert.equal(listener({ type: "chat_send" }, { id: "me", url: "chrome-extension://me/sidepanel.html" }, () => {}), false);
  assert.equal(listener(MSG, { id: "me", url: "https://web.example/" }, () => {}), false, "a web page cannot ask");
  assert.equal(listener(MSG, { id: "other", url: "chrome-extension://me/x.html" }, () => {}), false);
  assert.equal(listener(MSG, { id: "me" }, () => {}), false);
  const answered = new Promise((resolve) => assert.equal(listener(MSG, { id: "me", url: "chrome-extension://me/sidepanel.html" }, resolve), true));
  const out = await answered;
  assert.equal(out.ok, false, "answered (the fake tabs API has no tab)");
});
