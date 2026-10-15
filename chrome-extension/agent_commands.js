/**
 * MonoAgent Bridge — the coder's browser commands (plan C1)
 *
 * One entry point for background.js's dispatch: the command types the
 * coder's `monoagentcli browser …` sends, each answered by its agent_*.js
 * module. The Go side's typed helpers are internal/extension/page_agent.go.
 *
 * Load order (background.js): agent_cdp.js first, as every other module
 * registers with it when it loads; this file last.
 */

(function (root) {
  "use strict";

  const TYPES = [
    "list_tabs",
    "tab_group",
    "screenshot",
    "emulate",
    "watch_tab",
    "read_console",
    "read_network",
    "a11y_tree",
    "submit_guard",
    "submit_prompt",
  ];

  /** install hands agent_cdp.js the debugger helpers and starts the submit guard's listeners. */
  function install(deps) {
    root.MonoAgentCdp.install(deps);
    root.MonoAgentSubmitGuard.install();
  }

  async function handle(type, params) {
    const p = params || {};
    switch (type) {
      case "list_tabs":
        return root.MonoAgentTabs.listTabs();
      case "tab_group":
        return root.MonoAgentTabs.tabGroup(p);
      case "screenshot":
        return root.MonoAgentShot.screenshot(p);
      case "emulate":
        return root.MonoAgentShot.emulate(p);
      case "watch_tab":
        return root.MonoAgentWatch.watch(p);
      case "read_console":
        return root.MonoAgentWatch.readConsole(p);
      case "read_network":
        return root.MonoAgentWatch.readNetwork(p);
      case "a11y_tree":
        return root.MonoAgentA11y.tree(p);
      case "submit_guard":
        return root.MonoAgentSubmitGuard.handle(p);
      case "submit_prompt":
        return root.MonoAgentSubmitPrompt.show(p);
      default:
        throw new Error(`Unknown command type: ${type}`);
    }
  }

  root.MonoAgentCommands = { TYPES, install, handle };
})(globalThis);
