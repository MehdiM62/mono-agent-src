/**
 * MonoAgent Bridge — the armed tab's bar and toast (plan C1a, review re-check)
 *
 * The submit guard cannot tell the person's own submit from an agent's (a
 * click an agent sends through the debugger is as trusted as a real one),
 * so while a tab is armed the person is told, in the page:
 *
 *   - a small bar, bottom right, for as long as the tab is armed: "An agent
 *     is using this tab — form submits wait for your approval in MonoAgent",
 *     with a "Stop agent guard" button that disarms the tab and lets go of
 *     the agent's watch and emulation (agent_commands.js release; a later
 *     agent command arms it again);
 *   - a toast each time a submit is held: "This submit is waiting for
 *     approval in MonoAgent".
 *
 * Like submit_prompt's banner, both live in a CLOSED shadow root, and the
 * button answers only a trusted click, so page script can neither read nor
 * press them. A page that removes or hides the bar's host gets it back.
 * What this cannot stop: an agent clicking the button through the debugger,
 * which is the plan's stated limit (a full-access agent can reach the bridge
 * anyway).
 *
 * The three functions are injected by chrome.scripting into the tab's top
 * frame, in the extension's isolated world (agent_submit_guard.js), so each
 * is self-contained; they share state through one isolated-world global.
 * Browser-tested in agent_commands.browser.test.mjs.
 */

(function (root) {
  "use strict";

  /** show puts the bar up (once) and keeps it up. */
  function show() {
    const KEY = "MonoAgentGuardBarState";
    const existing = globalThis[KEY];
    if (existing && existing.host && existing.host.isConnected) return true;
    if (existing && existing.stop) existing.stop();

    const host = document.createElement("div");
    host.id = "monoagent-guard-bar";
    // !important: a page stylesheet's !important cannot hide it either.
    host.style.cssText = [
      "all:initial", "position:fixed", "right:12px", "bottom:12px", "z-index:2147483647",
      "display:block", "visibility:visible", "opacity:1",
    ].map((d) => `${d} !important`).join(";");
    // As the browser writes it back, to tell a page's change from our own.
    const HOST_STYLE = host.style.cssText;
    const shadow = host.attachShadow({ mode: "closed" });
    const style = document.createElement("style");
    style.textContent = `
      .bar{display:flex;align-items:center;gap:10px;max-width:min(440px,calc(100vw - 24px));padding:8px 8px 8px 12px;
        border-radius:10px;background:#1f2430;color:#f5f6f8;font:13px/1.4 system-ui,sans-serif;box-shadow:0 6px 24px rgba(0,0,0,.35)}
      .dot{flex:none;width:8px;height:8px;border-radius:50%;background:#f5b546}
      .msg{flex:1}
      button{all:unset;cursor:pointer;white-space:nowrap;padding:4px 10px;border-radius:6px;background:#343b4a;color:#f5f6f8;font:12px system-ui,sans-serif}
      button:focus-visible{outline:2px solid #f5b546;outline-offset:2px}
      .toast{position:absolute;right:0;bottom:calc(100% + 8px);max-width:min(440px,calc(100vw - 24px));padding:8px 12px;border-radius:10px;
        background:#f5b546;color:#1f2430;font:13px/1.4 system-ui,sans-serif;box-shadow:0 6px 24px rgba(0,0,0,.25)}`;
    const bar = document.createElement("div");
    bar.className = "bar";
    bar.setAttribute("role", "status");
    const dot = document.createElement("span");
    dot.className = "dot";
    const msg = document.createElement("span");
    msg.className = "msg";
    msg.textContent = "An agent is using this tab — form submits wait for your approval in MonoAgent";
    const stopButton = document.createElement("button");
    stopButton.type = "button";
    stopButton.textContent = "Stop agent guard";
    bar.append(dot, msg, stopButton);
    const toast = document.createElement("div");
    toast.className = "toast";
    toast.setAttribute("role", "alert");
    toast.hidden = true;
    shadow.append(style, toast, bar);

    const mount = () => {
      const parent = document.body || document.documentElement;
      if (parent) parent.appendChild(host);
    };
    mount();
    // A page that takes the bar away, or hides it, gets it back.
    const watch = new MutationObserver(() => {
      if (!host.isConnected) mount();
      if (host.style.cssText !== HOST_STYLE) host.style.cssText = HOST_STYLE;
    });
    watch.observe(document, { childList: true, subtree: true });
    watch.observe(host, { attributes: true });

    let toastTimer = 0;
    const state = {
      host,
      // For the extension's own eyes (this isolated world): the page cannot reach them.
      parts: { msg, toast, stopButton },
      toast(text) {
        toast.textContent = text;
        toast.hidden = false;
        clearTimeout(toastTimer);
        toastTimer = setTimeout(() => (toast.hidden = true), 6000);
      },
      stop() {
        watch.disconnect();
        clearTimeout(toastTimer);
        host.remove();
        if (globalThis[KEY] === state) globalThis[KEY] = null;
      },
    };
    globalThis[KEY] = state;
    stopButton.addEventListener("click", (event) => {
      if (event.isTrusted !== true) return;
      stopButton.disabled = true;
      try {
        chrome.runtime.sendMessage({ type: "agent_guard_stop" }).catch(() => {});
      } catch {
        // the extension reloaded; nothing is armed by this worker any more
        state.stop();
      }
    });
    return true;
  }

  /** toast says a submit is held, on the bar (show it first). */
  function toast() {
    const state = globalThis.MonoAgentGuardBarState;
    if (!state || !state.host || !state.host.isConnected) return false;
    state.toast("This submit is waiting for approval in MonoAgent");
    return true;
  }

  /** hide takes the bar down: the tab is no longer armed. */
  function hide() {
    const state = globalThis.MonoAgentGuardBarState;
    if (state && state.stop) state.stop();
    return true;
  }

  root.MonoAgentGuardBar = { show, toast, hide };
})(globalThis);
