/**
 * MonoAgent Bridge — the full side panel shows only to someone signed in
 *
 * Loaded first, in sidepanel.html's head. The page stays invisible until the
 * worker says the person is signed in (gate.js); signed out, or signed out
 * later, it is replaced by the sign-in screen (signin.html). The worker
 * refuses everything this page's scripts ask for meanwhile, so this is what
 * the person sees, not what protects anything.
 */

(function () {
  "use strict";

  if (typeof chrome === "undefined" || !chrome.runtime || !chrome.runtime.sendMessage) return;
  const root = document.documentElement;
  root.style.visibility = "hidden";

  function decide(status) {
    if (!status || status.signedIn !== true) {
      location.replace("signin.html");
      return;
    }
    root.style.visibility = "";
  }

  chrome.runtime.sendMessage({ type: "get_status" }, (status) => {
    void chrome.runtime.lastError; // no worker answer: signed out until it says otherwise
    decide(status);
  });
  chrome.runtime.onMessage.addListener((msg) => {
    if (msg && msg.type === "status" && msg.signedIn !== true) decide(msg);
  });
})();
