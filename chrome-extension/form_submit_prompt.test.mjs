// Decision 4 for the form filler (form_bridge.js): a filled form is never
// submitted for the person. Once a fill changed a field, the page shows the
// "review the form and press Submit yourself" banner (submit_prompt, through
// MonoCoderBridge.promptUserToSubmit); a fill that changed nothing shows
// nothing; and nothing anywhere submits or clicks.
// `CHROME_PATH=/nonexistent node --test chrome-extension/form_submit_prompt.test.mjs`

import test from "node:test";
import assert from "node:assert/strict";
import { loadExtensionScripts } from "./test_helpers.mjs";

const FIELDS = [
  { id: "m1", label: "Full name", name: "name", type: "text", filled: false },
  { id: "m2", label: "Passport number", name: "passport", type: "text", filled: false },
];

function setup({ fields = FIELDS, values = [{ id: "m1", value: "Sam Doe" }], applied = 1, withCoder = true } = {}) {
  const record = { scripts: [], prompts: [], asked: [] };
  const chrome = {
    tabs: { query: async () => [{ id: 42, windowId: 7 }], get: async (id) => ({ id, windowId: 7, url: "https://visa.example/apply" }) },
    storage: { local: { get: async () => ({}) } },
    scripting: {
      executeScript: async ({ target, func, files, args }) => {
        const source = func ? String(func) : "";
        record.scripts.push({ target, files, source, args });
        if (files) return [{ result: undefined }];
        if (source.includes("scan()")) return [{ result: { url: "https://visa.example/apply", title: "Apply", fields } }];
        if (source.includes("apply(")) return [{ result: { filled: args && args[0] && args[0].length ? applied : 0 } }];
        return [{ result: undefined }];
      },
    },
  };
  const scripts = ["chat_context.js", "chat_core.js", ...(withCoder ? ["coder_bridge.js"] : []), "form_bridge.js"];
  const sandbox = loadExtensionScripts(scripts, { chrome });
  sandbox.MonoSummaryAI = { sticky: async () => ({}) };
  sandbox.MonoAsk = {
    request: async (method, params) => {
      record.asked.push({ method, params });
      return { values };
    },
  };
  sandbox.MonoAgentSubmitPrompt = {
    show: async (params) => {
      record.prompts.push(params);
      return { shown: true, submitSelector: "#go" };
    },
  };
  return { F: sandbox.MonoForm, record };
}

const noSubmit = (record) => {
  for (const s of record.scripts) {
    assert.ok(!/\.submit\(|requestSubmit|\.click\(/.test(s.source), `a script submits or clicks: ${s.source.slice(0, 80)}`);
  }
};

test("a fill that changed a field asks the person to submit, once, in that tab", async () => {
  const { F, record } = setup();
  const out = await F.fill({ profile: "p-work", windowId: 7 });
  assert.deepEqual([out.ok, out.filled], [true, 1]);
  assert.deepEqual(record.prompts, [{ tabId: 42, message: "Review the form and press Submit yourself" }]);
  noSubmit(record);
});

test("a fill that changed nothing shows no banner", async () => {
  const none = setup({ values: [] });
  assert.equal((await none.F.fill({ profile: "p-work" })).filled, 0);
  assert.deepEqual(none.record.prompts, [], "nothing was answered");

  const refused = setup({ applied: 0 });
  assert.equal((await refused.F.fill({ profile: "p-work" })).filled, 0);
  assert.deepEqual(refused.record.prompts, [], "the page took none of the answers");

  const empty = setup({ fields: [] });
  assert.equal((await empty.F.fill({ profile: "p-work" })).none, true);
  assert.deepEqual(empty.record.prompts, [], "a page with no fields");
  for (const t of [none, refused, empty]) noSubmit(t.record);
});

test("without the coder bridge loaded the fill still works and asks nothing", async () => {
  const { F, record } = setup({ withCoder: false });
  const out = await F.fill({ profile: "p-work" });
  assert.deepEqual([out.ok, out.filled], [true, 1]);
  assert.deepEqual(record.prompts, []);
  noSubmit(record);
});

// The command line's way in (bridge command form_run, `browser fill`): it
// names its tab, may ask the model to write open answers, reports how many it
// wrote, and still never submits.
test("form_run fills the named tab, passes generate on, and counts what the model wrote", async () => {
  const { F, record } = setup({ values: [{ id: "m1", value: "Sam Doe" }, { id: "m2", value: "About me", generated: true }], applied: 2 });
  const out = await F.run({ profile: "p-work", tabId: 99, what: "fill", generate: true });
  assert.deepEqual([out.ok, out.filled, out.generated], [true, 2, 1]);
  assert.equal(record.asked[0].params.generate, true);
  assert.ok(record.scripts.every((s) => !s.target || s.target.tabId === 99), "every script ran in the named tab");
  noSubmit(record);
});

test("form_run without a tab, or with an unknown mode, is refused", async () => {
  const { F } = setup();
  await assert.rejects(() => F.run({ profile: "p-work", what: "fill" }), /needs a tab/);
  await assert.rejects(() => F.run({ profile: "p-work", tabId: 1, what: "card" }), /fill or files/);
});

test("without generate the request carries none", async () => {
  const { F, record } = setup();
  await F.run({ profile: "p-work", tabId: 99, what: "fill" });
  assert.equal("generate" in record.asked[0].params, false);
});
