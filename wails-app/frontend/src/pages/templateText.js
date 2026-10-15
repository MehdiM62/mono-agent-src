// templateBlurb is the text a template card shows a GUI user: the
// template's `summary` when the CLI gives one, else its description without
// the sentences written for terminal users ("Run with: workflow run <id>
// --input '{…}'", "monoagentcli login …").
const CLI_SENTENCE = /Run with:|--input|monoagentcli/

export function templateBlurb(t) {
  const summary = String(t?.summary || '').trim()
  if (summary) return summary
  const desc = String(t?.description || '').trim()
  // A sentence ends at . ! or ? followed by a capital — so "e.g. a" and
  // "robots.txt)" stay inside theirs.
  return desc.split(/(?<=[.!?])\s+(?=[A-Z])/).map(s => s.trim()).filter(s => s && !CLI_SENTENCE.test(s)).join(' ')
}
