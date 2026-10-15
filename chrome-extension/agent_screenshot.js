/**
 * MonoAgent Bridge — screenshot and emulate (plan C1)
 *
 * What the coder looks at after it changes something: the viewport, the
 * whole page, one element or a rectangle, optionally at another viewport
 * size or colour scheme. Both go through Page.captureScreenshot and the
 * Emulation domain on background.js's shared debugger (agent_cdp.js).
 *
 * Nothing here changes the page's own layout to take a picture: capture.js's
 * full-page capture overrides the device metrics and clears them afterwards,
 * which would also clear an emulation the coder set, so this asks Chrome to
 * paint beyond the viewport instead, and only when the picture needs it.
 *
 * An emulation lasts only while the debugger stays attached, so it HOLDS the
 * tab (agent_cdp.js) until emulate {reset:true}. Cancel on Chrome's
 * debugging banner ends it, as Chrome itself drops the override then.
 *
 * Node-tested against a fake debugger (agent_screenshot.test.mjs).
 */

(function (root) {
  "use strict";

  const MODES = ["viewport", "full", "element", "clip"];
  const FORMATS = ["png", "jpeg"];
  // Same cap as capture.js: Chrome will not paint a taller surface.
  const MAX_PX = 16384;
  // Under the bridge's 32MiB frame limit, leaving room for the envelope
  // (see cdp_proxy.js). Base64 is ASCII, so its length is its size.
  const MAX_DATA = 30 * 1024 * 1024;
  // A tab in the background may never paint; say so instead of hanging.
  const SHOT_TIMEOUT_MS = 30000;
  const SETTLE_MS = 1000;
  const COLOR_SCHEMES = ["", "light", "dark"];
  const REDUCED_MOTION = ["", "reduce", "no-preference"];

  const emulations = new Map(); // tabId -> { metrics, media }

  const cdp = (tabId, method, params) => root.MonoAgentCdp.send(tabId, method, params);

  function withTimeout(promise, ms, message) {
    let timer;
    const late = new Promise((_, reject) => {
      timer = setTimeout(() => reject(new Error(message)), ms);
    });
    return Promise.race([promise, late]).finally(() => clearTimeout(timer));
  }

  /** elementRect finds the first element matching selector, in document CSS pixels. */
  async function elementRect(tabId, selector) {
    const expression = `(() => {
      const el = document.querySelector(${JSON.stringify(selector)});
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return { x: r.left + scrollX, y: r.top + scrollY, width: r.width, height: r.height };
    })()`;
    const res = await cdp(tabId, "Runtime.evaluate", { expression, returnByValue: true });
    if (res && res.exceptionDetails) throw new Error(`screenshot: invalid selector ${selector}`);
    const rect = res && res.result && res.result.value;
    if (!rect) throw new Error(`screenshot: no element matches ${selector}`);
    if (!(rect.width > 0 && rect.height > 0)) throw new Error(`screenshot: ${selector} has no size (hidden?)`);
    return rect;
  }

  function clipOf(c) {
    const ok = c && ["x", "y", "width", "height"].every((k) => Number.isFinite(c[k]));
    if (!ok || c.width <= 0 || c.height <= 0) throw new Error("screenshot: clip needs numbers x, y, width > 0 and height > 0");
    return { x: c.x, y: c.y, width: Math.min(c.width, MAX_PX), height: Math.min(c.height, MAX_PX), scale: 1 };
  }

  /** screenshot answers screenshot: {data, mime, width, height}. */
  async function screenshot(params) {
    const tabId = root.MonoAgentCdp.tabOf(params, "screenshot");
    const mode = params.mode || "viewport";
    if (!MODES.includes(mode)) throw new Error(`screenshot: mode must be one of ${MODES.join(", ")}`);
    const format = params.format || "png";
    if (!FORMATS.includes(format)) throw new Error(`screenshot: format must be png or jpeg`);
    const shot = { format, fromSurface: true };
    if (params.quality !== undefined && params.quality !== null) {
      if (!Number.isInteger(params.quality) || params.quality < 0 || params.quality > 100) {
        throw new Error("screenshot: quality must be 0 to 100");
      }
      if (format === "jpeg") shot.quality = params.quality;
    }

    if (mode !== "viewport") {
      const metrics = await cdp(tabId, "Page.getLayoutMetrics", {});
      let clip;
      if (mode === "full") {
        const size = (metrics && (metrics.cssContentSize || metrics.contentSize)) || {};
        if (!(size.width > 0 && size.height > 0)) throw new Error("screenshot: the page reports no size");
        clip = { x: 0, y: 0, width: Math.min(Math.ceil(size.width), MAX_PX), height: Math.min(Math.ceil(size.height), MAX_PX), scale: 1 };
      } else if (mode === "element") {
        if (!params.selector || typeof params.selector !== "string") throw new Error("screenshot: element mode needs a selector");
        clip = clipOf(await elementRect(tabId, params.selector));
      } else {
        clip = clipOf(params.clip);
      }
      shot.clip = clip;
      // Painting beyond the viewport can relayout the page; ask for it only
      // when the clip is not already on screen.
      const vp = (metrics && metrics.cssVisualViewport) || {};
      const inside =
        Number.isFinite(vp.pageX) &&
        clip.x >= vp.pageX &&
        clip.y >= vp.pageY &&
        clip.x + clip.width <= vp.pageX + vp.clientWidth &&
        clip.y + clip.height <= vp.pageY + vp.clientHeight;
      if (!inside) shot.captureBeyondViewport = true;
    }

    const res = await withTimeout(
      cdp(tabId, "Page.captureScreenshot", shot),
      SHOT_TIMEOUT_MS,
      "screenshot: the tab did not paint in time (a tab in the background may not paint until it is shown)"
    );
    const data = (res && res.data) || "";
    if (!data) throw new Error("screenshot: Chrome returned no image");
    if (data.length > MAX_DATA) {
      throw new Error(`screenshot: the image is too large for the bridge (${data.length} bytes); use format jpeg, or a clip`);
    }
    const mime = format === "jpeg" ? "image/jpeg" : "image/png";
    const size = imageSize(data, mime);
    return { data, mime, width: size.width, height: size.height };
  }

  /**
   * imageSize reads an image's pixel size from its header: PNG's IHDR, or a
   * JPEG's first start-of-frame marker. {0, 0} when it cannot tell.
   */
  function imageSize(base64, mime) {
    // A JPEG's SOF follows its tables, well inside the first 64KiB.
    const head = base64.slice(0, mime === "image/png" ? 44 : 87384);
    let bin;
    try {
      bin = atob(head.slice(0, head.length - (head.length % 4)));
    } catch {
      return { width: 0, height: 0 };
    }
    const b = (i) => bin.charCodeAt(i);
    const u16 = (i) => (b(i) << 8) | b(i + 1);
    if (mime === "image/png") {
      if (bin.length < 24 || bin.slice(12, 16) !== "IHDR") return { width: 0, height: 0 };
      const u32 = (i) => ((b(i) << 24) >>> 0) + (b(i + 1) << 16) + (b(i + 2) << 8) + b(i + 3);
      return { width: u32(16), height: u32(20) };
    }
    let i = 2;
    while (i + 9 < bin.length) {
      if (b(i) !== 0xff) break;
      const marker = b(i + 1);
      const sof = marker >= 0xc0 && marker <= 0xcf && marker !== 0xc4 && marker !== 0xc8 && marker !== 0xcc;
      if (sof) return { width: u16(i + 7), height: u16(i + 5) };
      i += 2 + u16(i + 2);
    }
    return { width: 0, height: 0 };
  }

  /**
   * settle waits until the page has drawn twice, so an override Chrome has
   * just set or cleared is what the page measures when emulate answers (a
   * cleared one otherwise lands a frame later). Capped: a tab in the
   * background may not draw at all.
   */
  async function settle(tabId) {
    const frames = "new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(() => r(true))))";
    await withTimeout(cdp(tabId, "Runtime.evaluate", { expression: frames, awaitPromise: true }), SETTLE_MS, "").catch(() => {});
  }

  function sizeOf(v, name, max) {
    if (v === undefined || v === null) return 0;
    if (!Number.isFinite(v) || v < 0 || v > max) throw new Error(`emulate: ${name} must be 0 to ${max}`);
    return v;
  }

  /**
   * emulate answers emulate. width/height/deviceScaleFactor/mobile set the
   * device metrics override to exactly those values (0: the real one for
   * that dimension); media sets the media features (an empty value clears
   * one). What a call does not name stays as it was; reset clears both.
   */
  async function emulate(params) {
    const tabId = root.MonoAgentCdp.tabOf(params, "emulate");
    if (params.reset === true) {
      await cdp(tabId, "Emulation.clearDeviceMetricsOverride", {});
      await cdp(tabId, "Emulation.setEmulatedMedia", { media: "", features: [] });
      emulations.delete(tabId);
      root.MonoAgentCdp.release(tabId, "emulate");
      await settle(tabId);
      return { ok: true };
    }
    const named = (k) => params[k] !== undefined && params[k] !== null;
    const wantsMetrics = ["width", "height", "deviceScaleFactor", "mobile"].some(named);
    const media = params.media;
    if (!wantsMetrics && !named("media")) {
      throw new Error("emulate: name width, height, deviceScaleFactor, mobile or media, or reset");
    }
    let metrics = null;
    if (wantsMetrics) {
      if (named("mobile") && typeof params.mobile !== "boolean") throw new Error("emulate: mobile must be true or false");
      metrics = {
        width: Math.round(sizeOf(params.width, "width", 10000)),
        height: Math.round(sizeOf(params.height, "height", 10000)),
        deviceScaleFactor: sizeOf(params.deviceScaleFactor, "deviceScaleFactor", 10),
        mobile: params.mobile === true,
      };
    }
    let features = null;
    if (named("media")) {
      if (typeof media !== "object" || Array.isArray(media)) throw new Error("emulate: media must be an object");
      const scheme = media.prefersColorScheme || "";
      const motion = media.prefersReducedMotion || "";
      if (!COLOR_SCHEMES.includes(scheme)) throw new Error("emulate: prefersColorScheme must be light or dark");
      if (!REDUCED_MOTION.includes(motion)) throw new Error("emulate: prefersReducedMotion must be reduce or no-preference");
      features = [
        { name: "prefers-color-scheme", value: scheme },
        { name: "prefers-reduced-motion", value: motion },
      ];
    }

    const st = emulations.get(tabId) || { metrics: null, media: false };
    root.MonoAgentCdp.hold(tabId, "emulate");
    try {
      if (metrics) {
        await cdp(tabId, "Emulation.setDeviceMetricsOverride", metrics);
        st.metrics = metrics;
      }
      if (features) {
        await cdp(tabId, "Emulation.setEmulatedMedia", { features });
        st.media = features.some((f) => f.value);
      }
    } finally {
      if (st.metrics || st.media) emulations.set(tabId, st);
      else {
        emulations.delete(tabId);
        root.MonoAgentCdp.release(tabId, "emulate");
      }
    }
    await settle(tabId);
    return { ok: true };
  }

  root.MonoAgentCdp.onLost((tabId) => emulations.delete(tabId));

  root.MonoAgentShot = { screenshot, emulate, imageSize };
})(globalThis);
