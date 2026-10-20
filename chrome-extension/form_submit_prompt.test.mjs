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
    tabs: { query: async () => [{ id: 42, windowId: 7 }] },
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
