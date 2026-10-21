// The unpacked extension in a headless Chromium, for browser tests that
// must go through Chrome's own loader (chrome.scripting, the service
// worker) rather than evaluating the scripts. Its own profile directory
// and a random DevTools port; no pairing token, so the extension never
// dials a bridge -- the user's bridge on 9222 is never touched.
//
// Since the extension pairs itself (autopair.js), a fresh browser DOES ask a
// bridge on 9222/9323 for a token. So the browser loads a copy of the
// extension whose autopair.js asks no default port: none at all, or only
// `bridgePort` when a test brings a private bridge of its own.

import { spawn } from "node:child_process";
import { cp, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const HERE = dirname(fileURLToPath(import.meta.url));
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/** A flat-session CDP client over the browser socket. */
export class Cdp {
  constructor(ws) {
    this.ws = ws;
    this.id = 0;
    this.pending = new Map();
    ws.addEventListener("message", (e) => {
      const m = JSON.parse(e.data);
      const p = m.id && this.pending.get(m.id);
      if (!p) return;
      this.pending.delete(m.id);
      if (m.error) p.reject(new Error(m.error.message));
      else p.resolve(m.result);
    });
  }
  send(method, params = {}, sessionId) {
    const id = ++this.id;
    this.ws.send(JSON.stringify({ id, method, params, sessionId }));
    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      setTimeout(() => this.pending.delete(id) && reject(new Error(`${method} timed out`)), 15000);
    });
  }
  async evaluate(sessionId, expression) {
    const r = await this.send("Runtime.evaluate", { expression, awaitPromise: true, returnByValue: true }, sessionId);
    if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);
    return r.result.value;
  }
}

/** extensionCopy copies the extension into dir, asking only `ports` for a pairing. */
async function extensionCopy(dir, ports) {
  const to = join(dir, "extension");
  await cp(HERE, to, { recursive: true, filter: (src) => !/\.test\.mjs$|[\\/]fixtures$/.test(src) });
  const file = join(to, "autopair.js");
  const src = await readFile(file, "utf8");
  const patched = src.replace(/const PORTS = \[[^\]]*\];/, `const PORTS = ${JSON.stringify(ports)};`);
  if (patched === src && JSON.stringify(ports) !== "[9222,9323]") throw new Error("autopair.js has no PORTS line to patch");
  await writeFile(file, patched);
  return to;
}

async function launchOnce(binary, { bridgePort = 0 } = {}) {
  const dir = await mkdtemp(join(tmpdir(), "mono-rec-ext-"));
  const extDir = await extensionCopy(dir, bridgePort ? [bridgePort] : []);
  const child = spawn(
    binary,
    [
      ...(process.env.MONO_CHROME_NO_SANDBOX === "1" ? ["--no-sandbox"] : []),
      "--headless=new",
      "--remote-debugging-port=0",
      `--user-data-dir=${dir}`,
      `--load-extension=${extDir}`,
      `--disable-extensions-except=${extDir}`,
      "--no-first-run",
      "--no-default-browser-check",
      "--disable-gpu",
      "--window-size=1200,900",
      "about:blank",
    ],
    { stdio: ["ignore", "ignore", "pipe"], detached: true }
  );
  let stderr = "";
  child.stderr.on("data", (c) => (stderr += String(c)));
  let port = 0;
  let path = "";
  for (let i = 0; i < 400 && !port; i++) {
    try {
      [port, path] = (await readFile(join(dir, "DevToolsActivePort"), "utf8")).split("\n");
      port = Number(port);
    } catch {
      await sleep(50);
    }
  }
  if (!port) throw new Error(`no DevTools port\n${stderr.slice(-1000)}`);
  const ws = new WebSocket(`ws://127.0.0.1:${port}${path.trim()}`);
  await new Promise((res, rej) => {
    ws.addEventListener("open", res, { once: true });
    ws.addEventListener("error", () => rej(new Error("devtools socket failed")), { once: true });
  });
  const close = async () => {
    try {
      ws.close();
    } catch {}
    try {
      process.kill(-child.pid, "SIGKILL");
    } catch {}
    await sleep(300);
    await rm(dir, { recursive: true, force: true }).catch(() => {});
  };
  return { cdp: new Cdp(ws), close };
}

// A loaded machine (several browsers starting at once under node --test)
// can take a while to bring up the extension's service worker.
export const WORKER_START_MS = 30000;

/**
 * start launches the browser and waits for the extension's service worker:
 * {cdp, close, worker: {id, targetId} | null}. A browser whose worker does
 * not appear within WORKER_START_MS is closed and launched once more. If
 * the last launch still has no worker, that browser is returned with
 * worker null rather than thrown: a test that needs the worker then FAILS
 * (the extension is broken), where a thrown error would make the suite
 * skip as if there were no Chrome.
 */
export async function start(binary, { attempts = 2, workerMs = WORKER_START_MS, bridgePort = 0 } = {}) {
  for (let attempt = 1; ; attempt++) {
    const b = await launchOnce(binary, { bridgePort });
    try {
      b.worker = await extensionWorker(b.cdp, workerMs);
      return b;
    } catch (err) {
      if (attempt >= attempts) {
        b.worker = null;
        b.workerError = `${err.message} (after ${attempts} launches)`;
        return b;
      }
      await b.close();
    }
  }
}

/**
 * extensionWorker waits for the extension's service worker and returns its
 * id and target, once its script has run. Chrome lists the worker's target
 * before it evaluates background.js, and a test that evaluated in it at once
 * found none of the worker's globals ("gate is not defined"), more often the
 * busier the machine.
 */
export async function extensionWorker(cdp, timeoutMs = WORKER_START_MS) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    const { targetInfos } = await cdp.send("Target.getTargets");
    const sw = targetInfos.find((t) => t.type === "service_worker" && t.url.endsWith("/background.js"));
    if (sw && (await scriptHasRun(cdp, sw.targetId))) return { id: new URL(sw.url).host, targetId: sw.targetId };
    await sleep(100);
  }
  throw new Error(`the extension's service worker did not start within ${timeoutMs}ms`);
}

/**
 * scriptHasRun asks the worker whether background.js has run. The script
 * runs top to bottom in one task, so once `gate` (near its top) exists,
 * everything it sets up at load exists too.
 */
async function scriptHasRun(cdp, targetId) {
  let sessionId = "";
  try {
    ({ sessionId } = await cdp.send("Target.attachToTarget", { targetId, flatten: true }));
    return await cdp.evaluate(sessionId, `typeof gate !== "undefined"`);
  } catch {
    return false; // the worker went away between the listing and the question
  } finally {
    if (sessionId) await cdp.send("Target.detachFromTarget", { sessionId }).catch(() => {});
  }
}

/** attach opens a flat session on a target (created from `url` when given). */
export async function attach(cdp, { url, targetId }) {
  const id = targetId || (await cdp.send("Target.createTarget", { url })).targetId;
  const { sessionId } = await cdp.send("Target.attachToTarget", { targetId: id, flatten: true });
  await cdp.send("Runtime.enable", {}, sessionId);
  return sessionId;
}
