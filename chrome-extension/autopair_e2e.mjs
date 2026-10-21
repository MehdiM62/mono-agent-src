/**
 * End to end: a freshly installed extension in a real (headless) Chromium
 * against a real bridge. Not a *.test.mjs: it needs a bridge that
 * internal/accountsmoke/autopair_browser_test.go starts (the real binary, a
 * scratch HOME, a private port) and signs in or out as this script asks.
 *
 *   node autopair_e2e.mjs signed-in  <bridge-port>
 *   node autopair_e2e.mjs signed-out <bridge-port>
 *
 * signed-in:  the side panel shows the normal panel, never the sign-in
 *             screen, and the browser holds a pairing token it was handed.
 * signed-out: the side panel shows the sign-in screen and, after "SIGNED_OUT_OK"
 *             is printed and the driver prints "go" on stdin (it signs in),
 *             flips to the normal panel by itself.
 *
 * Prints "OK" and exits 0 on success; a failure exits 1 with the reason.
 * SHOTS=<dir> saves screenshots.
 */

import { writeFile } from "node:fs/promises";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { findChrome } from "./browser_harness.mjs";
import { attach, sleep, start } from "./extension_harness.mjs";

const [mode, portArg] = process.argv.slice(2);
const port = Number(portArg);
if (!["signed-in", "signed-out"].includes(mode) || !port) {
  console.error("usage: autopair_e2e.mjs signed-in|signed-out <bridge-port>");
  process.exit(2);
}
const CHROME = findChrome();
if (!CHROME) {
  console.log("SKIP no Chrome");
  process.exit(0);
}

const fail = (msg) => {
  console.error("FAIL " + msg);
  throw new Error(msg);
};
const browser = await start(CHROME, { bridgePort: port });
let code = 0;
try {
  if (!browser.worker) fail(browser.workerError);
  const { cdp } = browser;
  const sw = await attach(cdp, { targetId: browser.worker.targetId });
  const swEval = (expr) => cdp.evaluate(sw, expr);
  const { targetId } = await cdp.send("Target.createTarget", { url: `chrome-extension://${browser.worker.id}/sidepanel.html` });
  const panel = (await cdp.send("Target.attachToTarget", { targetId, flatten: true })).sessionId;
  await cdp.send("Runtime.enable", {}, panel);
  await cdp.send("Page.enable", {}, panel);
  await cdp.send("Emulation.setDeviceMetricsOverride", { width: 400, height: 760, deviceScaleFactor: 1, mobile: false }, panel);
  const ev = (expr) => cdp.evaluate(panel, expr);
  const until = async (expr, what, ms = 30000) => {
    for (let t = 0; t < ms; t += 100) {
      try {
        if (await ev(expr)) return;
      } catch {
        // the document is being replaced
      }
      await sleep(100);
    }
    fail(`timed out waiting for ${what}`);
  };
  const shot = async (name) => {
    if (!process.env.SHOTS) return;
    const { data } = await cdp.send("Page.captureScreenshot", { format: "png" }, panel);
    await writeFile(join(process.env.SHOTS, name), Buffer.from(data, "base64"));
  };
  const stored = async () => JSON.parse(await swEval(`chrome.storage.local.get(["pairingToken","pairedWsUrl"]).then((o) => JSON.stringify(o))`));
  const paired = async () => {
    const o = await stored();
    if (!/^[0-9a-f]{64}$/.test(o.pairingToken || "")) fail(`no pairing token was stored: ${JSON.stringify(Object.keys(o))}`);
    if (o.pairedWsUrl !== `ws://127.0.0.1:${port}/monoagent`) fail(`paired to ${o.pairedWsUrl}`);
  };
  const normalPanel = `location.pathname === "/sidepanel.html" && document.readyState === "complete"`;

  if (mode === "signed-in") {
    // Never on the sign-in screen, from the first moment to a settled normal panel.
    let sawSignIn = false;
    let settled = 0;
    for (let t = 0; t < 30000 && settled < 15; t += 200) {
      try {
        const path = await ev(`location.pathname`);
        if (path === "/signin.html") sawSignIn = true;
        const ok = await ev(`${normalPanel} && document.documentElement.style.visibility === ""`);
        settled = ok ? settled + 1 : 0;
      } catch {
        settled = 0;
      }
      await sleep(200);
    }
    if (settled < 15) fail("the normal panel never settled");
    await paired();
    if (!(await swEval(`connectionStatus === "connected" && gate.signedIn()`))) fail("the worker is not connected and signed in");
    await shot("autopair-signed-in.png");
    if (sawSignIn) console.error("NOTE the sign-in screen showed briefly while the browser paired");
  } else {
    await until(`location.pathname === "/signin.html" && document.readyState === "complete"`, "the sign-in screen");
    await until(`/updates automatically/.test(document.getElementById("signin-state").textContent)`, "the line saying the window updates automatically");
    const screen = JSON.parse(await ev(`JSON.stringify({
      state: document.getElementById("signin-state").textContent,
      tokenFieldHidden: document.getElementById("signin-pairing").hidden,
      pairLineHidden: document.getElementById("signin-pair").hidden,
    })`));
    if (!/Sign in in the MonoAgent app or run monoagentcli account login\. This window updates automatically\./.test(screen.state)) fail("wrong copy: " + screen.state);
    if (!screen.tokenFieldHidden || !screen.pairLineHidden) fail("signed out with the bridge up must not ask for a token");
    const o = await stored();
    if (o.pairingToken) fail("a token was handed out while signed out");
    await shot("autopair-signed-out.png");
    console.log("SIGNED_OUT_OK");
    await new Promise((res) => createInterface({ input: process.stdin }).once("line", res));
    await until(`${normalPanel} && document.documentElement.style.visibility === ""`, "the panel to flip to the normal one by itself", 45000);
    await paired();
    await shot("autopair-flipped.png");
  }
  console.log("OK");
} catch (e) {
  console.error("FAIL " + e.message);
  code = 1;
} finally {
  await browser.close();
}
process.exit(code);
