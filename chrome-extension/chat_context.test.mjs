// The chat's page context (chat_context.js): what may be sent about the page,
// how much of it, and the URL redaction.
// `CHROME_PATH=/nonexistent node --test chrome-extension/chat_context.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const { MonoChatCore: C } = loadExtensionScripts(["chat_context.js", "chat_core.js"]);

test("page context is capped, trimmed and refused for non-web addresses", () => {
  const big = "x".repeat(50000);
  const c = C.buildContext({ url: "https://a.example/p", title: "  T  ", text: big, selection: big });
  assert.equal(c.title, "T");
  assert.equal(c.text.length, C.TEXT_CAP);
  assert.equal(c.selection.length, C.SELECTION_CAP);
  assert.equal(C.buildContext({ url: "chrome://settings", title: "x", text: "", selection: "" }), null);
  assert.equal(C.buildContext({ url: "javascript:alert(1)" }), null);
  assert.equal(C.buildContext(null), null);
  assert.deepEqual(C.buildContext({ url: "https://a.example/", title: 5, text: null }), { url: "https://a.example/", title: "", text: "", selection: "" });
});

test("a cap never splits an emoji", () => {
  const text = "a".repeat(C.TEXT_CAP - 1) + "😀😀";
  const out = C.buildContext({ url: "https://a.example/", text }).text;
  assert.equal(out.length, C.TEXT_CAP - 1);
  assert.doesNotMatch(out, /[\ud800-\udbff]$/);
});

const utf8 = (s) => new TextEncoder().encode(s).length;
const ctxOf = (o) => C.buildContext({ url: "https://a.example/", title: "", text: "", selection: "", ...o });

// The server counts bytes (24,576 for the text, 8,192 for the selection, 512
// for the title), so the client clips by bytes too: a page in any script is
// shortened, never refused.
test("caps count UTF-8 bytes, so a CJK, Cyrillic or emoji page still fits the server's limits", () => {
  const server = { text: 24 * 1024, selection: 8 * 1024, title: 512, url: 2048 };
  for (const [name, unit] of [["ascii", "a"], ["cjk", "世"], ["cyrillic", "д"], ["emoji", "😀"]]) {
    const big = unit.repeat(30000);
    const c = ctxOf({ title: big, text: big, selection: big });
    for (const k of ["text", "selection", "title"]) {
      assert.ok(utf8(c[k]) <= server[k], `${name} ${k}: ${utf8(c[k])} bytes`);
      assert.ok(utf8(c[k]) > server[k] / 3, `${name} ${k} was cut far too short`);
      assert.doesNotMatch(c[k], /[\ud800-\udbff]$/, `${name} ${k} ends in half an emoji`);
      assert.ok(!c[k].includes("�"), `${name} ${k}`);
    }
  }
  // 9,000 CJK characters (27,000 bytes): the report that started this.
  assert.ok(utf8(ctxOf({ text: "世".repeat(9000) }).text) <= 24 * 1024);
  const msg = C.checkMessage("世".repeat(9000));
  assert.ok(utf8(msg) <= 16 * 1024, "the typed message is clipped to the server's 16 KiB too");
});

test("the address sent keeps its query with secret-looking values redacted, and no credentials; comparisons ignore the query", () => {
  const cases = [
    ["https://a.example/p?token=secret&x=1#frag", "https://a.example/p?token=REDACTED&x=1#frag"],
    ["https://user:pw@a.example:8443/p/q?x=1", "https://a.example:8443/p/q?x=1"],
    ["http://a.example/#/route?token=1", "http://a.example/"],
    ["https://a.example", "https://a.example/"],
  ];
  for (const [raw, want] of cases) assert.equal(ctxOf({ url: raw }).url, want, raw);
  assert.equal(C.stripUrl("https://a.example/p?q=1#h"), "https://a.example/p");
  assert.equal(C.stripUrl("chrome://settings"), "");
  assert.equal(C.stripUrl("https://"), "");
});

test("a local file page is named, but its text and selection are not sent", () => {
  const c = ctxOf({ url: "file:///home/me/notes.html?x=1#y", title: "notes", text: "private", selection: "private" });
  assert.deepEqual(c, { url: "file:///home/me/notes.html", title: "notes", text: "", selection: "" });
});
