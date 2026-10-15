// Raw design-token tallies for the whole page, read in its main world:
// background colors by sampled visible area, text and border colors by
// estimated painted area, font stacks and sizes by character count, spacing,
// radii and shadows by use, :root custom properties and media queries.
// Called as (fn)(opts); Go normalizes, ranks and caps what comes back.
(function (opts) {
  "use strict";
  const vw = innerWidth;
  const vh = innerHeight;
  const tally = (m, k, n) => {
    if (k) m[k] = (m[k] || 0) + n;
  };
  const transparent = (c) => !c || c === "transparent" || /^rgba\(.*,\s*0\)$/.test(c) || /\/\s*0\)$/.test(c);
  const styles = new Map();
  const cs = (n) => {
    let s = styles.get(n);
    if (!s) {
      s = getComputedStyle(n);
      styles.set(n, s);
    }
    return s;
  };
  const visible = (n) =>
    !n.checkVisibility ||
    n.checkVisibility({ checkOpacity: true, checkVisibilityCSS: true, opacityProperty: true, visibilityProperty: true });

  // Backgrounds: a grid of points over the viewport, each counted for the
  // first opaque background under it, so a color scores what is really seen.
  const palette = { background: {}, text: {}, border: {} };
  let canvas = "rgb(255, 255, 255)";
  for (const n of [document.documentElement, document.body]) {
    if (n && !transparent(cs(n).backgroundColor)) {
      canvas = cs(n).backgroundColor;
      break;
    }
  }
  const bgOf = new Map();
  const backgroundOf = (start) => {
    if (bgOf.has(start)) return bgOf.get(start);
    let c = canvas;
    for (let n = start; n && n.nodeType === 1; n = n.parentElement) {
      const b = cs(n).backgroundColor;
      if (!transparent(b)) {
        c = b;
        break;
      }
    }
    bgOf.set(start, c);
    return c;
  };
  const cols = 48;
  const rows = 32;
  const cell = (vw / cols) * (vh / rows);
  for (let i = 0; i < cols; i++) {
    for (let j = 0; j < rows; j++) {
      const e = document.elementFromPoint(((i + 0.5) * vw) / cols, ((j + 0.5) * vh) / rows);
      tally(palette.background, e ? backgroundOf(e) : canvas, cell);
    }
  }

  // Text: estimated glyph area per color; characters per font and size.
  const fonts = {};
  const scale = {};
  let textNodes = 0;
  if (document.body) {
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    for (let t = walker.nextNode(); t && textNodes < opts.maxTextNodes; t = walker.nextNode()) {
      const p = t.parentElement;
      const chars = t.nodeValue.trim().length;
      if (!p || !chars || p.closest("script,style,noscript,template") || !visible(p)) continue;
      textNodes++;
      const s = cs(p);
      const size = parseFloat(s.fontSize) || 0;
      tally(palette.text, s.color, chars * size * size * 0.5);
      tally(fonts, s.fontFamily, chars);
      const step = (scale[s.fontSize] = scale[s.fontSize] || { chars: 0, weights: {}, lineHeights: {} });
      step.chars += chars;
      tally(step.weights, s.fontWeight, chars);
      tally(step.lineHeights, s.lineHeight, chars);
    }
  }

  // Borders, spacing, radii and shadows over the first visible elements.
  const spacing = {};
  const radii = {};
  const shadows = {};
  let elements = 0;
  const all = document.body ? document.body.getElementsByTagName("*") : [];
  for (let i = 0; i < all.length && elements < opts.maxElements; i++) {
    const n = all[i];
    if (!visible(n)) continue;
    elements++;
    const s = cs(n);
    const r = n.getBoundingClientRect();
    for (const [side, len] of [["top", r.width], ["right", r.height], ["bottom", r.width], ["left", r.height]]) {
      const w = parseFloat(s.getPropertyValue("border-" + side + "-width")) || 0;
      const st = s.getPropertyValue("border-" + side + "-style");
      const c = s.getPropertyValue("border-" + side + "-color");
      if (w > 0 && st !== "none" && st !== "hidden" && !transparent(c)) tally(palette.border, c, w * len);
    }
    for (const p of ["margin", "padding"]) {
      for (const side of ["top", "right", "bottom", "left"]) {
        const v = s.getPropertyValue(p + "-" + side);
        if (v && v !== "0px" && v !== "auto") tally(spacing, v, 1);
      }
    }
    for (const p of ["row-gap", "column-gap"]) {
      const v = s.getPropertyValue(p);
      if (v && v !== "normal" && v !== "0px") tally(spacing, v, 1);
    }
    for (const c of ["top-left", "top-right", "bottom-right", "bottom-left"]) {
      const v = s.getPropertyValue("border-" + c + "-radius");
      if (v && v !== "0px") tally(radii, v, 1);
    }
    if (s.boxShadow && s.boxShadow !== "none") tally(shadows, s.boxShadow, 1);
  }

  // :root custom properties, as the document resolves them.
  const rootVars = {};
  const root = getComputedStyle(document.documentElement);
  for (let i = 0, k = 0; i < root.length && k < opts.maxVars; i++) {
    if (root[i].startsWith("--")) {
      rootVars[root[i]] = root.getPropertyValue(root[i]).trim();
      k++;
    }
  }

  // Media queries: every condition and how many rules it holds.
  const media = {};
  let inaccessible = 0;
  let scanned = 0;
  const visit = (rules) => {
    for (const rule of rules) {
      if (++scanned > 20000) return;
      if (typeof CSSMediaRule !== "undefined" && rule instanceof CSSMediaRule) {
        tally(media, rule.conditionText || rule.media.mediaText, rule.cssRules.length);
      }
      if (rule.cssRules) visit(rule.cssRules);
    }
  };
  for (const sheet of Array.from(document.styleSheets).concat(Array.from(document.adoptedStyleSheets || []))) {
    try {
      visit(sheet.cssRules || []);
    } catch (e) {
      inaccessible++;
    }
  }

  return {
    url: location.origin + location.pathname,
    viewport: { width: vw, height: vh, deviceScaleFactor: devicePixelRatio },
    palette,
    fonts,
    scale,
    spacing,
    radii,
    shadows,
    rootVars,
    media,
    elements,
    textNodes,
    inaccessibleSheets: inaccessible,
  };
})
