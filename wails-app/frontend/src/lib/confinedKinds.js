// "Fix with coder" (failed-run-fix) and "Discuss with coder" (recording-analyze)
// start task kinds that read untrusted text (node outputs, recorded pages) and
// still run as unconfined turns. Their buttons stay hidden until those kinds
// are confined like the Apply task; flip this when they are.
export const UNCONFINED_KIND_BUTTONS = false
