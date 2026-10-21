/**
 * MonoAgent Bridge — pictures for the chat (issue #941)
 *
 * The side panel's chat reads a page's text. A page drawn on a canvas, or
 * words inside images, have none, so two things can put a picture in front of
 * the model, each only by the person's choice:
 *
 *   look      the model asks (propose_page_look); the card's Run takes a
 *             screenshot of the tab the card was made for. `page_look_capture`
 *             {tab:{tabId,url}} -> {ok:true, image:{mime,data,width,height,bytes}, url}
 *                                 | {ok:false, code, error}
 *             codes: invalid, page_changed, not_visible, not_allowed, failed
 *   attach    the person picks image files in the panel.
 *
 * Both end as `images` ([{mime, data(base64)}]) on chat.send, which the Go
 * side checks again (internal/extension/chat_images.go) and hands to the
 * runner. The rules kept here, whatever the panel sent:
 *
 *   - at most MAX_IMAGES a message, MAX_BYTES each after shrinking (longest
 *     side MAX_EDGE, JPEG), PNG/JPEG/GIF/WebP by their own first bytes;
 *   - a screenshot is never taken of a page that is not http(s), nor of one on
 *     the person's own network (private_net.js isPrivateHost: a router, a NAS,
 *     a dev server), nor of a tab that is not the one showing, nor of a tab
 *     that is no longer on the origin the card was made for;
 *   - a screenshot can show what the page text skips, so the panel shows it to
 *     the person before it first leaves the browser in a chat (the card).
 *
 * Runs in the worker (the capture) and in the panel (shrinking attachments).
 * After private_net.js and agent_redact.js (the page address shown to the
 * person has its secrets masked).
 */

(function (root) {
  "use strict";

  const MAX_IMAGES = 3;
  const MAX_BYTES = 2 * 1024 * 1024;
  const MAX_EDGE = 1600;
  // A file bigger than this is refused before the browser decodes it.
  const MAX_FILE = 20 * 1024 * 1024;
  const MIN_EDGE = 320;
  const MIMES = ["image/png", "image/jpeg", "image/gif", "image/webp"];
  const QUALITIES = [0.85, 0.7, 0.55, 0.4];

  const answer = (code, error) => ({ ok: false, code, error });

  /** kindOf reads the image type from the first bytes of base64 data; "" when it is none of MIMES. */
  function kindOf(base64) {
    let head;
    try {
      head = atob(String(base64).slice(0, 16));
    } catch {
      return "";
    }
    const b = (i) => head.charCodeAt(i);
    if (head.startsWith("\x89PNG\r\n\x1a\n")) return "image/png";
    if (b(0) === 0xff && b(1) === 0xd8 && b(2) === 0xff) return "image/jpeg";
    if (head.startsWith("GIF87a") || head.startsWith("GIF89a")) return "image/gif";
    if (head.startsWith("RIFF") && head.slice(8, 12) === "WEBP") return "image/webp";
    return "";
  }

  /**
   * checkImages returns the images of a send as the bridge takes them, or
   * throws why not: a list of at most MAX_IMAGES {mime, data} whose bytes are
   * what they say and at most MAX_BYTES each.
   */
  function checkImages(list) {
    if (list === undefined || list === null) return [];
    if (!Array.isArray(list)) throw new Error("images must be a list");
    if (list.length > MAX_IMAGES) throw new Error(`at most ${MAX_IMAGES} images go with a message`);
    return list.map((img, i) => {
      const data = img && typeof img.data === "string" ? img.data : "";
      if (!data) throw new Error(`image ${i + 1} has no data`);
      // Base64 is 4 characters for 3 bytes.
      const padding = data.endsWith("==") ? 2 : data.endsWith("=") ? 1 : 0;
      if (Math.floor((data.length * 3) / 4) - padding > MAX_BYTES) throw new Error(`image ${i + 1} is over ${MAX_BYTES / 1024 / 1024} MB`);
      const mime = kindOf(data);
      if (!mime || (img.mime && img.mime !== mime)) throw new Error(`image ${i + 1} is not a PNG, JPEG, GIF or WebP picture`);
      return { mime, data };
    });
  }

  /** whyNot says why a page may not be captured; "" when it may. */
  function whyNot(url) {
    let u;
    try {
      u = new URL(url);
    } catch {
      return "that page has no address I can check";
    }
    if (u.protocol !== "http:" && u.protocol !== "https:") return "only web pages (http or https) can be looked at";
    if (root.MonoPrivateNet && root.MonoPrivateNet.isPrivateHost(u.hostname)) {
      return `${u.hostname} is on a private network: pages there are never captured`;
    }
    return "";
  }

  const originOf = (url) => {
    try {
      const u = new URL(url);
      return u.protocol === "http:" || u.protocol === "https:" ? u.origin : "";
    } catch {
      return "";
    }
  };

  const blobToBase64 = async (blob) => {
    const bytes = new Uint8Array(await blob.arrayBuffer());
    let bin = "";
    for (let i = 0; i < bytes.length; i += 0x8000) bin += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
    return btoa(bin);
  };

  /**
   * shrink draws a picture (a Blob) at most MAX_EDGE on its longest side as a
   * JPEG, and lowers quality, then size, until it is at most MAX_BYTES.
   * env: {createImageBitmap(blob), canvas(w, h) -> OffscreenCanvas-like}; a
   * picture that cannot be made small enough, or read, throws.
   * -> {mime, data, width, height, bytes}
   */
  async function shrink(blob, env) {
    if (blob && blob.size > MAX_FILE) throw new Error(`that file is over ${MAX_FILE / 1024 / 1024} MB`);
    let bitmap;
    try {
      bitmap = await env.createImageBitmap(blob);
    } catch {
      throw new Error("that file is not a picture this browser can read");
    }
    try {
      let scale = Math.min(1, MAX_EDGE / Math.max(bitmap.width, bitmap.height));
      for (;;) {
        const width = Math.max(1, Math.round(bitmap.width * scale));
        const height = Math.max(1, Math.round(bitmap.height * scale));
        const canvas = env.canvas(width, height);
        const ctx = canvas.getContext("2d");
        // JPEG has no alpha: a transparent page would turn black.
        ctx.fillStyle = "#fff";
        ctx.fillRect(0, 0, width, height);
        ctx.drawImage(bitmap, 0, 0, width, height);
        for (const quality of QUALITIES) {
          const out = await canvas.convertToBlob({ type: "image/jpeg", quality });
          if (out.size <= MAX_BYTES) return { mime: "image/jpeg", data: await blobToBase64(out), width, height, bytes: out.size };
        }
        if (Math.max(width, height) <= MIN_EDGE) throw new Error("that picture cannot be made small enough to send");
        scale *= 0.7;
      }
    } finally {
      if (bitmap.close) bitmap.close();
    }
  }

  const realEnv = () => ({
    createImageBitmap: (b) => root.createImageBitmap(b),
    canvas: (w, h) => new root.OffscreenCanvas(w, h),
  });

  /**
   * capture takes the screenshot a look card asked for. deps (tests): chrome,
   * env. The tab must be the one showing in its window (captureVisibleTab
   * takes what is on screen), on the card's origin before and after.
   */
  async function capture(msg, deps) {
    const c = (deps && deps.chrome) || root.chrome;
    const tab = (msg && msg.tab) || {};
    if (!Number.isInteger(tab.tabId) || tab.tabId <= 0) return answer("invalid", "there is no page to look at");
    const was = originOf(tab.url);
    if (!was) return answer("not_allowed", "only web pages (http or https) can be looked at");
    let live;
    try {
      live = await c.tabs.get(tab.tabId);
    } catch {
      return answer("page_changed", "the page changed: that tab is gone");
    }
    if (originOf(live && live.url) !== was) return answer("page_changed", "the page changed since this was suggested");
    const refused = whyNot(live.url);
    if (refused) return answer("not_allowed", refused);
    if (!live.active) return answer("not_visible", "that tab is not the one showing: switch to it, then press the button again");
    let image;
    try {
      // The tab, its address and its window, read again just before and just after the picture:
      // what is on screen is that tab's page only if none of them moved in between.
      const same = (t) => t && t.id === live.id && t.url === live.url && t.windowId === live.windowId && t.active === true;
      if (!same(await c.tabs.get(tab.tabId))) return answer("page_changed", "the page changed just before the picture was taken");
      const dataUrl = await c.tabs.captureVisibleTab(live.windowId, { format: "jpeg", quality: 90 });
      if (!same(await c.tabs.get(tab.tabId))) return answer("page_changed", "the page changed while the picture was taken");
      const blob = await (await ((deps && deps.fetch) || root.fetch)(dataUrl)).blob();
      image = await shrink(blob, (deps && deps.env) || realEnv());
    } catch (err) {
      return answer("failed", (err && err.message) || String(err));
    }
    const redact = root.MonoAgentRedact;
    return { ok: true, image, url: redact ? redact.url(live.url) : live.url };
  }

  /** install answers page_look_capture from this extension's own pages (the side panel) only. */
  function install() {
    root.chrome.runtime.onMessage.addListener((msg, sender, respond) => {
      if (!msg || msg.type !== "page_look_capture") return false;
      const base = root.chrome.runtime.getURL ? root.chrome.runtime.getURL("") : "";
      if (!sender || sender.id !== root.chrome.runtime.id || !base || !String(sender.url || "").startsWith(base)) return false;
      capture(msg).then(respond, (err) => respond(answer("failed", (err && err.message) || String(err))));
      return true;
    });
  }

  root.MonoChatLook = { MAX_IMAGES, MAX_BYTES, MAX_EDGE, MAX_FILE, MIMES, kindOf, checkImages, whyNot, shrink, capture, install, realEnv };
})(globalThis);
