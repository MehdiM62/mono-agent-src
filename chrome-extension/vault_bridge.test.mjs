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
  const calls = { scripts: [], fetches: [], requests: [], cancelled: false, reads: 0 };
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
    tabs: { get: async (id) => ({ id, url: o.pageUrl || "https://a.example/page", title: "A page" }) },
  };
  const headers = { "content-type": o.contentType || "image/png", "content-length": o.contentLength !== undefined ? o.contentLength : String(body.length) };
  const fetch = async (url, init) => {
    calls.fetches.push({ url, init });
    // The body arrives in o.chunks when given (a big file read piece by piece), else in one piece.
    const pieces = o.chunks ? o.chunks.slice() : [body];
    if (o.redirects) return { ok: false, status: 0, type: "opaqueredirect", headers: { get: () => null } };
    return {
      ok: (o.status || 200) < 400,
      status: o.status || 200,
      headers: { get: (h) => headers[h.toLowerCase()] ?? null },
      body: {
        getReader: () => ({
          read: async () => (calls.reads++, pieces.length ? { done: false, value: pieces.shift() } : { done: true }),
          cancel: async () => (calls.cancelled = true),
        }),
      },
    };
  };
  const sandbox = loadExtensionScripts(["capture_profile.js", "private_net.js", "chat_proposals.js", "vault_bridge.js"], { chrome, fetch });
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
  assert.equal(w.calls.fetches[0].init.redirect, "manual", "a public page's file is not followed elsewhere");
  assert.equal(w.calls.fetches[0].init.credentials, "include", "the page's own origin: its cookies, as the page itself would send");
  assert.match(w.calls.requests[0].params.client_id, new RegExp(`^chat-${NONCE}-[0-9a-f]{8}-toolu_v$`), "scoped to the profile, as tasks are");
  delete w.calls.requests[0].params.client_id;
  assert.deepEqual(w.calls.requests, [{ method: "vault.save", params: {
    profile: "p-work", name: "chart.png", mime: "image/png",
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
  // Content-Length over the cap: refused before a single byte is read.
  const declared = world({ contentLength: String(21 * 1024 * 1024) });
  const out = await declared.send(ask());
  assert.equal(out.status, "refused");
  assert.match(out.feedback.text, /too big/);
  assert.deepEqual([declared.calls.reads, declared.calls.requests.length], [0, 0], "nothing read, nothing sent");

  // No Content-Length, 25 MiB in 1 MiB pieces: reading stops at the cap, not at the end.
  const MiB = 1024 * 1024;
  const chunks = Array.from({ length: 25 }, () => new Uint8Array(MiB));
  chunks[0].set(PNG);
  const streamed = world({ chunks, contentLength: null });
  const out2 = await streamed.send(ask());
  assert.equal(out2.status, "refused");
  assert.match(out2.feedback.text, /too big/);
  assert.equal(streamed.calls.reads, 21, "the 21st MiB is the one past the cap; nothing after it is read");
  assert.ok(streamed.calls.cancelled, "the rest is cancelled");
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

// #451 review: a public page must not make the extension fetch from the
// person's own network with their cookies, directly or by a redirect.
test("a private network address is refused on a public page, and fetched from a page on that network", async () => {
  for (const url of ["http://127.0.0.1/a.png", "http://10.0.0.5/scan.pdf", "http://192.168.1.2/x.png", "http://169.254.169.254/latest", "http://[::1]/a.png", "http://intranet/r.pdf"]) {
    const w = world();
    const out = await w.send(ask({ url }));
    assert.equal(out.status, "refused", url);
    assert.match(out.feedback.text, /private network/);
    assert.deepEqual([w.calls.scripts.length, w.calls.fetches.length, w.calls.requests.length], [0, 0, 0], url);
  }
  const inside = world({ pageUrl: "http://192.168.1.10/dashboard" });
  const out = await inside.send(ask({ url: "http://192.168.1.10/chart.png" }));
  assert.equal(out.status, "saved", "a page on that network may save its own files");
  assert.equal(inside.calls.fetches[0].init.redirect, "follow");
});

test("a public page's file that redirects is not followed, and nothing is saved", async () => {
  const w = world({ redirects: true });
  const out = await w.send(ask());
  assert.equal(out.status, "refused");
  assert.match(out.feedback.text, /redirects/);
  assert.equal(w.calls.requests.length, 0);
});

// Vault card review: a page counts as private only when it is an http or https
// page on a private host. A file, a blob, about:, data:, view-source: or the
// browser's own pages are public: a public page can open a blob of its own.
test("only an http(s) page on a private host may save a private address", async () => {
  for (const pageUrl of ["blob:https://a.example/0b6c3e4a-1d2e-4f5a-8b9c-0d1e2f3a4b5c", "file:///home/me/notes.html", "about:blank", "about:srcdoc",
    "data:text/html,<img src=http://127.0.0.1/a.png>", "view-source:http://127.0.0.1/", "chrome://newtab/", "chrome-extension://abc/page.html", ""]) {
    const w = world({ pageUrl });
    const out = await w.send(ask({ url: "http://127.0.0.1:9222/json/list.txt" }));
    assert.equal(out.status, "refused", pageUrl);
    assert.match(out.feedback.text, /private network/, pageUrl);
    assert.deepEqual([w.calls.scripts.length, w.calls.fetches.length], [0, 0], pageUrl);
  }
});

// Vault card review: a GET through the extension must not carry the person's
// cookies to another site (a page could link a bank's action URL).
test("a file from another origin is fetched without cookies; the page's own origin with them", async () => {
  const cross = world({ contentType: "image/png" });
  await cross.send(ask({ url: "https://cdn.example/img/chart.png" }));
  assert.deepEqual([cross.calls.fetches[0].init.credentials, cross.calls.fetches[0].init.redirect], ["omit", "manual"]);

  const inside = world({ pageUrl: "http://192.168.1.10/dashboard" });
  await inside.send(ask({ url: "http://192.168.1.10/chart.png" }));
  assert.deepEqual([inside.calls.fetches[0].init.credentials, inside.calls.fetches[0].init.redirect], ["include", "follow"]);
  const elsewhere = world({ pageUrl: "http://192.168.1.10/dashboard" });
  await elsewhere.send(ask({ url: "http://192.168.1.11/chart.png" }));
  assert.deepEqual([elsewhere.calls.fetches[0].init.credentials, elsewhere.calls.fetches[0].init.redirect], ["omit", "follow"]);
});

// Vault card review: the cap is MonoAgent's own, 20 MiB: a large PDF fits.
test("a file up to 20 MiB is saved", async () => {
  const pdf = new Uint8Array(6 * 1024 * 1024);
  pdf.set(new TextEncoder().encode("%PDF-1.7\n"));
  const w = world({ body: pdf, contentType: "application/pdf" });
  const out = await w.send(ask({ url: "https://a.example/files/big.pdf" }));
  assert.equal(out.status, "saved");
  assert.equal(w.calls.requests[0].params.mime, "application/pdf");
});

// Vault card review: the page rule is narrow. A page on a wildcard-DNS name,
// a tailnet or an office suffix is public, however the item rule sees it.
test("a page on a wildcard-DNS, tailnet or office name cannot save a private address", async () => {
  for (const pageUrl of ["https://203-0-113-5.sslip.io/", "https://x.ts.net/", "https://nip.io/", "http://127.0.0.1.nip.io/", "https://router.lan/"]) {
    const w = world({ pageUrl });
    const out = await w.send(ask({ url: "http://127.0.0.1:9222/json/list.txt" }));
    assert.equal(out.status, "refused", pageUrl);
    assert.match(out.feedback.text, /private network/, pageUrl);
    assert.deepEqual([w.calls.scripts.length, w.calls.fetches.length], [0, 0], pageUrl);
  }
  for (const pageUrl of ["http://127.0.0.1:8080/", "http://localhost/"]) {
    const w = world({ pageUrl });
    const out = await w.send(ask({ url: `${pageUrl}chart.png` }));
    assert.equal(out.status, "saved", pageUrl);
    assert.deepEqual([w.calls.fetches[0].init.redirect, w.calls.fetches[0].init.credentials], ["follow", "include"], pageUrl);
  }
});

// Vault card review: base64 is built in slices, never as one binary string
// the size of the file; the result is the same.
test("base64 in slices is the plain encoding, across slice boundaries and for an odd size", () => {
  const { MonoVault: V } = loadExtensionScripts(["vault_bridge.js"]);
  const bytes = new Uint8Array(6 * 1024 * 1024 + 7);
  for (let i = 0; i < bytes.length; i++) bytes[i] = (i * 31 + 7) & 255;
  assert.equal(V.toBase64(bytes), Buffer.from(bytes).toString("base64"));
  assert.equal(V.toBase64(Uint8Array.of(1, 2)), Buffer.from([1, 2]).toString("base64"));
  assert.equal(V.toBase64(new Uint8Array(0)), "");
});
