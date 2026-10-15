/**
 * MonoAgent Bridge — submit_prompt (plan C1, decision 4)
 *
 * When the extension has filled a form for the person, it never submits it:
 * it asks them to. This shows a banner at the top of the page ("Review and
 * press Submit yourself") and draws a ring around the form's submit control,
 * then gets out of the way: the banner leaves when they submit, dismiss it,
 * or leave the page. Nothing here clicks, submits or changes the form.
 *
 * Like highlight_panel.js, the banner lives in a CLOSED shadow root and its
 * one button answers only a trusted click, so the page can neither find its
 * contents nor drive it. The ring is drawn in that same root over the
 * control, not by restyling the page's element.
 *
 * showPrompt is injected by chrome.scripting into the tab's top frame, in
 * the extension's isolated world, so it is self-contained. Browser-tested in
 * agent_submit.browser.test.mjs.
 */

(function (root) {
  "use strict";

  /** showPrompt(selector, message) → {shown, submitSelector?} or {error}. */
  function showPrompt(selector, message) {
    const ID = "monoagent-submit-prompt";
    const SUBMIT = 'button:not([type]), button[type="submit"], input[type="submit"], input[type="image"]';
    const WORDS = /^(submit|send|continue|next|apply|sign ?up|register|save|pay|place order|order|confirm|book|check ?out|create)\b/i;

    const previous = globalThis.MonoAgentSubmitPromptState;
    if (previous && typeof previous.remove === "function") previous.remove();

    let target = null;
    if (selector) {
      try {
        target = document.querySelector(selector);
      } catch {
        return { error: `submit_prompt: invalid selector ${selector}` };
      }
      if (!target) return { error: `submit_prompt: no element matches ${selector}` };
    }

    const visible = (el) => !!el && el.getClientRects().length > 0;
    const isSubmit = (el) => !!el && typeof el.matches === "function" && el.matches(SUBMIT);
    const inForm = (form) => (form ? Array.from(form.elements || []).find(isSubmit) || null : null);
    const byText = (scope) =>
      Array.from(scope.querySelectorAll('button, [role="button"], input[type="button"]')).find(
        (el) => visible(el) && WORDS.test(String(el.textContent || el.value || el.getAttribute("aria-label") || "").trim())
      ) || null;

    function findControl() {
      if (target) {
        if (isSubmit(target)) return target;
        const form = target.tagName === "FORM" ? target : target.closest("form") || target.querySelector("form");
        return inForm(form) || byText(target) || (form ? byText(form) : null);
      }
      // The form the person is in, else the first form with a control on screen.
      const active = document.activeElement;
      const forms = [active && active.closest ? active.closest("form") : null, ...Array.from(document.forms)];
      for (const form of forms) {
        const c = inForm(form);
        if (visible(c)) return c;
      }
      return byText(document.body || document.documentElement);
    }

    function selectorOf(el) {
      const esc = (s) => (window.CSS && CSS.escape ? CSS.escape(s) : String(s).replace(/[^\w-]/g, "\\$&"));
      const parts = [];
      let node = el;
      while (node && node.nodeType === 1 && parts.length < 32) {
        if (node.id && document.querySelectorAll("#" + esc(node.id)).length === 1) {
          parts.unshift("#" + esc(node.id));
          break;
        }
        const parent = node.parentElement;
        let part = node.tagName.toLowerCase();
        if (parent) {
          const same = Array.from(parent.children).filter((c) => c.tagName === node.tagName);
          if (same.length > 1) part += `:nth-of-type(${same.indexOf(node) + 1})`;
        }
        parts.unshift(part);
        node = parent;
      }
      return parts.join(" > ");
    }

    const control = findControl();
    const text = String(message || "").trim().slice(0, 300) || "Review and press Submit yourself";

    const host = document.createElement("div");
    host.id = ID;
    host.style.cssText = "all:initial;position:fixed;inset:0;pointer-events:none;z-index:2147483647";
    const shadow = host.attachShadow({ mode: "closed" });
    const style = document.createElement("style");
    style.textContent = `
      .bar{position:fixed;top:12px;left:50%;transform:translateX(-50%);max-width:min(560px,calc(100vw - 32px));
        display:flex;align-items:center;gap:12px;padding:10px 12px 10px 16px;border-radius:10px;
        background:#1f2430;color:#f5f6f8;font:14px/1.4 system-ui,sans-serif;box-shadow:0 6px 24px rgba(0,0,0,.35);
        pointer-events:auto}
      .bar.low{top:auto;bottom:12px}
      .tag{font-size:11px;letter-spacing:.04em;text-transform:uppercase;color:#f5b546;white-space:nowrap}
      .msg{flex:1}
      button{all:unset;cursor:pointer;padding:4px 10px;border-radius:6px;background:#343b4a;color:#f5f6f8;font:13px system-ui,sans-serif}
      button:focus-visible{outline:2px solid #f5b546;outline-offset:2px}
      .ring{position:fixed;border:3px solid #f5b546;border-radius:8px;box-shadow:0 0 0 4px rgba(245,181,70,.35);
        animation:pulse 1.6s ease-in-out infinite}
      @keyframes pulse{50%{box-shadow:0 0 0 9px rgba(245,181,70,.12)}}
      @media (prefers-reduced-motion: reduce){.ring{animation:none}}`;
    const bar = document.createElement("div");
    bar.className = "bar";
    bar.setAttribute("role", "status");
    const tag = document.createElement("span");
    tag.className = "tag";
    tag.textContent = "MonoAgent";
    const msg = document.createElement("span");
    msg.className = "msg";
    msg.textContent = text;
    const close = document.createElement("button");
    close.type = "button";
    close.textContent = "Dismiss";
    bar.append(tag, msg, close);
    const ring = document.createElement("div");
    ring.className = "ring";
    ring.hidden = !control;
    shadow.append(style, bar, ring);
    (document.body || document.documentElement).appendChild(host);

    const TOP_BAND = 80; // the banner's height and margin, with room to spare
    let frame = 0;
    function place() {
      if (!control || !control.isConnected || !visible(control)) {
        ring.hidden = true;
      } else {
        const r = control.getBoundingClientRect();
        ring.hidden = false;
        Object.assign(ring.style, { left: `${r.left - 6}px`, top: `${r.top - 6}px`, width: `${r.width + 6}px`, height: `${r.height + 6}px` });
        // A control near the top would sit under the banner, out of reach.
        bar.classList.toggle("low", r.top < TOP_BAND);
      }
      frame = requestAnimationFrame(place);
    }

    function remove() {
      cancelAnimationFrame(frame);
      host.remove();
      window.removeEventListener("submit", remove, true);
      window.removeEventListener("pagehide", remove, true);
      if (globalThis.MonoAgentSubmitPromptState === state) globalThis.MonoAgentSubmitPromptState = null;
    }
    const state = { remove };
    globalThis.MonoAgentSubmitPromptState = state;
    // The person submitted (or the page did): the request has been answered.
    window.addEventListener("submit", remove, true);
    window.addEventListener("pagehide", remove, true);
    close.addEventListener("click", (event) => {
      if (event.isTrusted === true) remove();
    });

    if (control) {
      control.scrollIntoView({ block: "center", behavior: "smooth" });
      place();
    }
    const out = { shown: true };
    if (control) out.submitSelector = selectorOf(control);
    return out;
  }

  /** show answers submit_prompt. */
  async function show(params) {
    const tabId = root.MonoAgentCdp.tabOf(params, "submit_prompt");
    if (params.selector !== undefined && typeof params.selector !== "string") throw new Error("submit_prompt: selector must be a string");
    const message = typeof params.message === "string" ? params.message : "";
    const [res] = await chrome.scripting.executeScript({
      target: { tabId },
      func: showPrompt,
      args: [params.selector || "", message],
    });
    const out = res && res.result;
    if (!out) throw new Error("submit_prompt: the page did not answer");
    if (out.error) throw new Error(out.error);
    return out;
  }

  root.MonoAgentSubmitPrompt = { show, showPrompt };
})(globalThis);
