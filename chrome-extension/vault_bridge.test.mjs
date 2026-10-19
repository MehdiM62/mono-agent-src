// Saving a page's file to the vault (vault_bridge.js, issue #451): only the
// side panel asks, on the person's click on a chat card; the item must be on
// the page the person is on now; the worker fetches it itself, capped, checks
// it is an image, a PDF or text, and sends vault.save under the card's key.
// `CHROME_PATH=/nonexistent node --test chrome-extension/vault_bridge.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const PNG = Uint8Array.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3, 4]);
const PANEL = { id: "ext", url: "chrome-extension://ext/sidepanel.html" };
const PAGE = { id: "ext", url: "https://a.example/page", tab: { id: 5 } };
const NONCE = "0123456789abcdef";

function world(o = {}) {
  const body = o.body || PNG;
  const calls = { scripts: [], fetches: [], requests: [], cancelled: false };
  const listeners = [];
  const store = { captureProfilesCache: [{ id: "p-work", name: "Work" }] };
  const chrome = {
    runtime: { id: "ext", getURL: (p) => `chrome-extension://ext/${p}`, onMessage: { addListener: (fn) => listeners.push(fn) } },
    storage: { local: { get: async (k) => Object.fromEntries([].concat(k).filter((x) => x in store).map((x) => [x, store[x]])), set: async () => {} } },
    scripting: {
      executeScript: async (opts) => {
        calls.scripts.push(opts);
        if (o.scriptFails) throw new Error("Cannot access contents of the page");
        if (opts.func.name === "itemOnPage") return [{ frameId: 0, result: o.onPage !== false }, { frameId: 3, result: false }];
        return [{ frameId: 0, result: o.selection || "" }, { frameId: 3, result: "" }];
      },
    },
    tabs: { get: async (id) => ({ id, url: "https://a.example/page", title: "A page" }) },
  };
  const headers = { "content-type": o.contentType || "image/png", "content-length": o.contentLength !== undefined ? o.contentLength : String(body.length) };
  const fetch = async (url, init) => {
    calls.fetches.push({ url, init });
    let given = false;
    return {
      ok: (o.status || 200) < 400,
      status: o.status || 200,
      headers: { get: (h) => headers[h.toLowerCase()] ?? null },
      body: {
        getReader: () => ({
          read: async () => (given ? { done: true } : ((given = true), { done: false, value: body })),
          cancel: async () => (calls.cancelled = true),
        }),
      },
    };
  };
  const sandbox = loadExtensionScripts(["capture_profile.js", "chat_proposals.js", "vault_bridge.js"], { chrome, fetch });
  sandbox.MonoAsk = {
    supports: async (m) => (o.methods || ["ping", "vault.save"]).includes(m),
    request: async (method, params) => {
      calls.requests.push({ method, params });
      if (o.fail) throw Object.assign(new Error(o.fail.message), { code: o.fail.code });
      return o.reply || { id: "v1", created: true, name: params.name, mime: params.mime, size_bytes: 12 };
    },
  };
  sandbox.MonoVault.install({ isConnected: () => o.connected !== false, storage: chrome.storage.local });
  const send = (msg, sender = PANEL) =>
    new Promise((resolve) => {
      const handled = listeners.map((fn) => fn(msg, sender, resolve));
      if (!handled.some(Boolean)) resolve(undefined);
    });
  return { calls, send, sandbox };
}

const proposal = (extra) => Object.assign({ id: "toolu_v", kind: "vault", url: "https://a.example/img/chart.png", selection: false, name: "", nonce: NONCE }, extra);
const ask = (extra, msg) => Object.assign({ type: "vault_save_proposal", profile: "p-work", tab: { tabId: 5, url: "https://a.example/page" }, proposal: proposal(extra) }, msg);

test("an item on the page is fetched after the click and saved under the card's key", async () => {
  const w = world();
  const out = await w.send(ask());
  assert.deepEqual([out.status, out.created, out.feedback.text], ["saved", true, "Saved to the vault in Work: chart.png"]);
  assert.equal(w.calls.scripts[0].target.tabId, 5);
  assert.equal(w.calls.scripts[0].target.allFrames, true, "every frame is asked whether the item is there");
  assert.deepEqual(w.calls.scripts[0].args, ["https://a.example/img/chart.png"]);
  assert.deepEqual(w.calls.fetches.map((f) => [f.url, f.init.credentials]), [["https://a.example/img/chart.png", "include"]]);
  assert.deepEqual(w.calls.requests, [{ method: "vault.save", params: {
    client_id: `chat-${NONCE}-toolu_v`, profile: "p-work", name: "chart.png", mime: "image/png",
    data: Buffer.from(PNG).toString("base64"), source_url: "https://a.example/img/chart.png",
  } }]);
});

test("only the side panel may ask, and only for a vault card", async () => {
  const w = world();
  assert.equal(await w.send(ask(), PAGE), undefined, "a page cannot send one");
  for (const bad of [{ kind: "task" }, { url: "javascript:alert(1)" }, { url: "" }, { id: "" }]) {
    const out = await w.send(ask(bad));
    assert.equal(out.status, "refused", JSON.stringify(bad));
  }
  assert.deepEqual([w.calls.fetches.length, w.calls.requests.length], [0, 0]);
});

test("an address that is not on the page the person is on is neither fetched nor saved", async () => {
  const w = world({ onPage: false });
  const out = await w.send(ask());
  assert.equal(out.status, "retry", "pressing Save again checks again");
  assert.match(out.feedback.text, /not on the page you are on/);
  assert.deepEqual([w.calls.fetches.length, w.calls.requests.length], [0, 0]);
});

test("the selection saved is the text selected in the page now, as a text file", async () => {
  const w = world({ selection: "Picked from the page" });
  const out = await w.send(ask({ url: "", selection: true }));
  assert.equal(out.status, "saved");
  const p = w.calls.requests[0].params;
  assert.deepEqual([p.mime, p.name, Buffer.from(p.data, "base64").toString("utf8"), p.source_url], ["text/plain", "Selection from a.example.txt", "Picked from the page", "https://a.example/page"]);
  assert.equal(w.calls.fetches.length, 0);
  const none = world({ selection: "" });
  const empty = await none.send(ask({ url: "", selection: true }));
  assert.deepEqual([empty.status, none.calls.requests.length], ["retry", 0]);
  assert.match(empty.feedback.text, /select/i);
});

test("a file too big for the side panel is refused for good, before or while it is read", async () => {
  const declared = world({ contentLength: String(6 * 1024 * 1024) });
  const out = await declared.send(ask());
  assert.equal(out.status, "refused");
  assert.match(out.feedback.text, /too big/);
  assert.equal(declared.calls.requests.length, 0);

  const big = new Uint8Array(5 * 1024 * 1024 + 1);
  big.set(PNG);
  const streamed = world({ body: big, contentLength: null });
  const out2 = await streamed.send(ask());
  assert.equal(out2.status, "refused");
  assert.ok(streamed.calls.cancelled, "the rest is not read");
  assert.equal(streamed.calls.requests.length, 0);
});

test("only images, PDFs and text files are saved, by what the bytes are", async () => {
  const exe = world({ body: Uint8Array.from([0x4d, 0x5a, 0x90, 0, 3]), contentType: "image/png" });
  const out = await exe.send(ask());
  assert.equal(out.status, "refused");
  assert.match(out.feedback.text, /images|PDF/);
  assert.equal(exe.calls.requests.length, 0);

  const pdf = world({ body: new TextEncoder().encode("%PDF-1.7\n..."), contentType: "application/octet-stream" });
  await pdf.send(ask({ url: "https://a.example/files/q3", name: "Q3 report" }));
  assert.deepEqual([pdf.calls.requests[0].params.mime, pdf.calls.requests[0].params.name], ["application/pdf", "Q3 report.pdf"]);

  const csv = world({ body: new TextEncoder().encode("a,b\n1,2\n"), contentType: "text/csv; charset=utf-8" });
  await csv.send(ask({ url: "https://a.example/data.csv" }));
  assert.equal(csv.calls.requests[0].params.mime, "text/csv");
});

test("a name is cleaned: no separators, control or bidi characters, at most 255 bytes, the right extension", async () => {
  const w = world();
  await w.send(ask({ name: "../../etc/pass‮wd\u0007" + "é".repeat(200) }));
  const name = w.calls.requests[0].params.name;
  assert.ok(!/[\\/\u0000-\u001f‪-‮]/.test(name), name);
  assert.ok(new TextEncoder().encode(name).length <= 255);
  assert.ok(name.endsWith(".png"));
});

test("Already saved, final refusals and retries say what happened", async () => {
  const again = await world({ reply: { id: "v1", created: false, name: "chart.png" } }).send(ask());
  assert.deepEqual([again.status, again.feedback.text], ["saved", "Already saved to the vault in Work: chart.png"]);
  const invalid = await world({ fail: { code: "invalid_input", message: "the bytes are not a png" } }).send(ask());
  assert.deepEqual([invalid.status, invalid.feedback.text], ["refused", "Not saved: the bytes are not a png"]);
  for (const code of ["unavailable", "internal", "offline", "timeout"]) {
    const out = await world({ fail: { code, message: "MonoAgent is busy" } }).send(ask());
    assert.equal(out.status, "retry", code);
  }
});

test("without a profile, a bridge, or vault.save on it, nothing is fetched and Save can be pressed again", async () => {
  for (const [o, msg, why] of [
    [{}, { profile: "" }, /profile/],
    [{ connected: false }, {}, /isn't connected/],
    [{ methods: ["ping"] }, {}, /update/i],
    [{}, { tab: null }, /page/],
    [{ scriptFails: true }, {}, /cannot be read/],
  ]) {
    const w = world(o);
    const out = await w.send(ask({}, msg));
    assert.equal(out.status, "retry", JSON.stringify(o) + JSON.stringify(msg));
    assert.match(out.feedback.text, why);
    assert.deepEqual([w.calls.fetches.length, w.calls.requests.length], [0, 0]);
  }
});

// itemOnPage runs in the page: the item must be one the page itself shows or
// links to, wherever its address is written.
test("an item is on the page when the page shows it, links to it, or is it", () => {
  const el = (attrs) => ({ getAttribute: (k) => (k in attrs ? attrs[k] : null), currentSrc: attrs.currentSrc || "" });
  const lists = {
    img: [el({ src: "/img/a.png", currentSrc: "https://a.example/img/a.png" }), el({ src: "b.png", srcset: "/img/b-1x.png 1x, /img/b%202x.png 2x" })],
    "picture source[srcset]": [el({ srcset: "https://cdn.example/c.webp 640w" })],
    "a[href]": [el({ href: "../files/q3.pdf#page=2" })],
    "embed[src], iframe[src], object[data], video[poster]": [el({ data: "/doc.pdf" })],
  };
  const document = { baseURI: "https://a.example/blog/post", querySelectorAll: (sel) => lists[sel] || [] };
  const { MonoVault: V } = loadExtensionScripts(["vault_bridge.js"], { document, location: { href: "https://a.example/blog/post" } });
  for (const url of [
    "https://a.example/img/a.png", "https://a.example/blog/b.png", "https://a.example/img/b-1x.png", "https://a.example/img/b 2x.png",
    "https://cdn.example/c.webp", "https://a.example/files/q3.pdf", "https://a.example/doc.pdf", "https://a.example/blog/post#top",
  ]) assert.equal(V.itemOnPage(url), true, url);
  for (const url of ["https://a.example/img/secret.png", "https://evil.example/img/a.png", "not a url"]) assert.equal(V.itemOnPage(url), false, url);
});
