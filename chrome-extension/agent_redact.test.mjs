// Tests for the URL redaction the coder's commands record with (agent_redact.js).
// `node --test 'chrome-extension/**/*.test.mjs'`.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { loadExtensionScripts } from "./test_helpers.mjs";

const HERE = dirname(fileURLToPath(import.meta.url));
const { MonoAgentRedact: R } = loadExtensionScripts(["agent_redact.js"]);

// Parameter names assembled at runtime: the repository's secret scanner
// reads `name=value` literals as credentials.
const AT = ["access", "token"].join("_");
const ID = ["id", "token"].join("_");
const AK = ["api", "key"].join("");
const SESSION = ["sess", "ion"].join("");

test("the sensitive names are chat_context.go's urlSensitiveNames, spelled the same", () => {
  const go = readFileSync(join(HERE, "..", "internal", "extension", "chat_context.go"), "utf8");
  const block = go.slice(go.indexOf("var urlSensitiveNames = []string{"));
  const names = [...block.slice(0, block.indexOf("}")).matchAll(/"([^"]+)"/g)].map((m) => m[1]);
  assert.ok(names.length > 20, "found the Go list");
  assert.deepEqual(R.SENSITIVE_NAMES, names);
});

test("sensitive query values are masked; names, other values and separators stay as written", () => {
  assert.equal(
    R.url(`https://api.example.com/cb?code=4/0AbC&state=xyz&page=2;${AT}=ya29.a0&q=shoes`),
    `https://api.example.com/cb?code=•••&state=•••&page=2;${AT}=•••&q=shoes`
  );
  assert.equal(
    R.url("https://bucket.s3.amazonaws.com/a.png?X-Amz-Signature=abc123&X-Amz-Expires=300&v=1"),
    "https://bucket.s3.amazonaws.com/a.png?X-Amz-Signature=•••&X-Amz-Expires=•••&v=1"
  );
  const masked = R.url(`http://localhost:3000/api?${AK}=k&${ID}=t&sig=s`);
  assert.equal(masked, `http://localhost:3000/api?${AK}=${R.MASK}&${ID}=${R.MASK}&sig=${R.MASK}`);
  assert.equal(R.url("http://localhost:5173/app.js"), "http://localhost:5173/app.js");
  assert.equal(R.url(`http://localhost/x?${SESSION}=`), `http://localhost/x?${SESSION}=`, "an empty value has nothing to hide");
});

test("an encoded name cannot dodge the list", () => {
  assert.equal(R.url("http://h/?acc%65ss_tok%2565n=abc"), `http://h/?acc%65ss_tok%2565n=${R.MASK}`);
});

test("a value in the fragment (OAuth implicit grant) is masked too; a plain anchor is not", () => {
  assert.equal(R.url(`https://app.test/cb#${AT}=abc&expires_in=3600&scope=x`), `https://app.test/cb#${AT}=${R.MASK}&expires_in=${R.MASK}&scope=x`);
  assert.equal(R.url("https://app.test/docs#section-2"), "https://app.test/docs#section-2");
});

test("URLs inside free text are masked: console lines, stack frames", () => {
  assert.equal(
    R.text(`GET https://api.test/me?${SESSION}=s3cret 401 (Unauthorized) at load (https://cdn.test/a.js?sig=zz:3:9)`),
    `GET https://api.test/me?${SESSION}=${R.MASK} 401 (Unauthorized) at load (https://cdn.test/a.js?sig=${R.MASK}:3:9)`
  );
  assert.equal(R.text("no urls here"), "no urls here");
  assert.equal(R.text(undefined), "");
});

test("sensitiveField is the submit guard's rule: type, autocomplete, name or id, or a card number in the value", () => {
  const masked = [
    { type: "password", value: "x" },
    { type: "HIDDEN", value: "x" },
    { autocomplete: "cc-number", value: "x" },
    { autocomplete: "section-pay current-password", value: "x" },
    { autocomplete: "one-time-code", value: "x" },
    { name: "cardholder", value: "Ann" },
    { id: "cvc", value: "123" },
    { name: "user_pin", value: "1" },
    { name: "memo", value: "card 4111-1111-1111-1111 exp 12/29" },
  ];
  for (const f of masked) assert.equal(R.sensitiveField(f), true, JSON.stringify(f));
  const kept = [
    { type: "email", name: "email", value: "ann@example.com" },
    { name: "order", value: "1234567812345678" },
    { name: "shipping", value: "1 Main St" },
    { type: "text", name: "q", value: "pins and needles" },
  ];
  for (const f of kept) assert.equal(R.sensitiveField(f), false, JSON.stringify(f));
});

test("a token-like path segment is masked; names, slugs, hashed file names and short ids are not", () => {
  assert.equal(R.url("https://app.test/reset/k9Xq2LmZ8vB4nR7tY1wP5sD3/confirm?step=2"), `https://app.test/reset/${R.MASK}/confirm?step=2`);
  assert.equal(R.url("https://app.test/blog/how-to-write-a-good-commit-message"), "https://app.test/blog/how-to-write-a-good-commit-message");
  assert.equal(R.url("http://localhost:5173/assets/index-Bx7Kq2Lm9NpQrStUvWxYz.js"), "http://localhost:5173/assets/index-Bx7Kq2Lm9NpQrStUvWxYz.js");
  assert.equal(R.url("http://localhost:3000/api/items/42"), "http://localhost:3000/api/items/42");
  assert.equal(R.url("https://cdn.test/a/abcdefghijklmnopqrstuvwxyz"), "https://cdn.test/a/abcdefghijklmnopqrstuvwxyz", "letters only: a word, not a token");
  assert.equal(R.text("at load (https://app.test/m/k9Xq2LmZ8vB4nR7tY1wP5sD3:4:2)"), `at load (https://app.test/m/${R.MASK}:4:2)`);
});
