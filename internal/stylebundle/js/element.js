// What a style bundle reads in the page's main world about one element,
// beyond what the CSS domain answers: its rect, its parent's computed style
// and its tag's browser defaults (for pruning), the subtree for element.html,
// a child summary, the media rules that touch it and the @font-face rules of
// the families it uses. Called as (fn)(el, opts); returns plain data. The
// subtree carries the page's text: Go replaces it with placeholders before
// anything is written.
(function (el, opts) {
  "use strict";
  const MAX_TEXT = 2000;
  const MAX_RULES = 20000;
  const XHTML = "http://www.w3.org/1999/xhtml";
  const SVG = "http://www.w3.org/2000/svg";

  // Computed longhands the declaration does not enumerate but CDP lists.
  const UNLISTED = ["background-position-x", "background-position-y", "page"];
  function styleMap(cs) {
    const out = {};
    for (let i = 0; i < cs.length; i++) {
      const name = cs[i];
      if (!name.startsWith("--")) out[name] = cs.getPropertyValue(name);
    }
    for (const name of UNLISTED) out[name] = cs.getPropertyValue(name);
    return out;
  }

  // A fresh element of the same tag in an empty frame: the browser's
  // defaults, with none of the page's styles.
  function tagDefaults(tag, ns) {
    const frame = document.createElement("iframe");
    frame.setAttribute("aria-hidden", "true");
    frame.style.cssText = "position:absolute;left:-10000px;top:0;width:0;height:0;border:0;visibility:hidden";
    (document.body || document.documentElement).appendChild(frame);
    try {
      const doc = frame.contentDocument;
      try {
        doc.open();
        doc.write("<!doctype html><html><head></head><body></body></html>");
        doc.close();
      } catch (e) {
        // Trusted Types refuse document.write; about:blank's quirks mode is close enough.
      }
      let host = doc.body;
      if (ns === SVG && tag !== "svg") {
        host = doc.createElementNS(SVG, "svg");
        doc.body.appendChild(host);
      }
      const probe = ns && ns !== XHTML ? doc.createElementNS(ns, tag) : doc.createElement(tag);
      host.appendChild(probe);
      return styleMap(frame.contentWindow.getComputedStyle(probe));
    } catch (e) {
      return null;
    } finally {
      frame.remove();
    }
  }

  let budget = opts.maxNodes;
  let truncated = false;
  const SKIP_CONTENT = { script: 1, style: 1, noscript: 1, template: 1 };
  function serialize(node) {
    if (budget <= 0) {
      truncated = true;
      return null;
    }
    if (node.nodeType === 3) {
      const s = node.nodeValue;
      if (!s) return null;
      budget--;
      return s.length > MAX_TEXT ? { x: s.slice(0, MAX_TEXT), n: s.length } : { x: s };
    }
    if (node.nodeType !== 1) return null;
    budget--;
    const out = { t: node.localName, a: [] };
    for (const at of node.attributes) out.a.push([at.name, at.value]);
    if (!SKIP_CONTENT[node.localName]) {
      const kids = [];
      for (const ch of node.childNodes) {
        const s = serialize(ch);
        if (s) kids.push(s);
      }
      if (kids.length) out.c = kids;
    }
    return out;
  }

  function ownTextLength(node) {
    let n = 0;
    for (const ch of node.childNodes) if (ch.nodeType === 3) n += ch.nodeValue.trim().length;
    return n;
  }

  function childSummary() {
    const nodes = [];
    (function walk(node, depth) {
      for (const ch of node.children) {
        if (nodes.length >= opts.maxChildren) return;
        const r = ch.getBoundingClientRect();
        const entry = {
          depth,
          tag: ch.localName,
          classes: Array.from(ch.classList).slice(0, 12),
          display: getComputedStyle(ch).display,
          width: Math.round(r.width * 100) / 100,
          height: Math.round(r.height * 100) / 100,
          children: ch.children.length,
          textLength: ownTextLength(ch),
        };
        if (ch.id) entry.id = ch.id;
        if (ch.getAttribute("role")) entry.role = ch.getAttribute("role");
        nodes.push(entry);
        walk(ch, depth + 1);
      }
    })(el, 1);
    const total = el.getElementsByTagName("*").length;
    return { total, direct: el.children.length, nodes, truncated: total > nodes.length };
  }

  // A rule for :hover or ::before still says which element it styles.
  const STATEFUL = /::?(hover|focus-visible|focus-within|focus|active|visited|target|before|after|marker|placeholder|selection|first-line|first-letter|backdrop)\b/g;
  function touches(sel) {
    const loose = sel.replace(STATEFUL, "").trim() || "*";
    for (const s of [sel, loose]) {
      try {
        if (el.matches(s)) return "element";
        if (el.querySelector(s)) return "descendant";
      } catch (e) {
        // a selector this browser cannot parse touches nothing
      }
    }
    return "";
  }

  let inaccessible = 0;
  const sheets = Array.from(document.styleSheets).concat(Array.from(document.adoptedStyleSheets || []));

  function breakpoints() {
    const found = new Map();
    let scanned = 0;
    function visit(rules, conds) {
      for (const rule of rules) {
        if (++scanned > MAX_RULES) return;
        if (typeof CSSMediaRule !== "undefined" && rule instanceof CSSMediaRule) {
          visit(rule.cssRules, conds.concat([{ type: "media", text: rule.conditionText || rule.media.mediaText }]));
        } else if (typeof CSSContainerRule !== "undefined" && rule instanceof CSSContainerRule) {
          visit(rule.cssRules, conds.concat([{ type: "container", text: rule.conditionText }]));
        } else if (rule.cssRules && !(typeof CSSStyleRule !== "undefined" && rule instanceof CSSStyleRule)) {
          visit(rule.cssRules, conds); // @supports, @layer, @scope
        } else if (conds.length && rule.selectorText) {
          const target = touches(rule.selectorText);
          if (!target) continue;
          const cond = conds[conds.length - 1];
          const text = conds.filter((c) => c.type === cond.type).map((c) => c.text).join(" and ");
          const key = cond.type + "\u0000" + text;
          let bp = found.get(key);
          if (!bp) {
            bp = { type: cond.type, condition: text, target, selectors: [], properties: [] };
            if (cond.type === "media") {
              try {
                bp.active = matchMedia(text).matches;
              } catch (e) {
                // unparseable media text: activity unknown
              }
            }
            found.set(key, bp);
          }
          if (target === "element") bp.target = "element";
          if (bp.selectors.length < 10 && bp.selectors.indexOf(rule.selectorText) < 0) bp.selectors.push(rule.selectorText);
          for (let i = 0; i < rule.style.length && bp.properties.length < 40; i++) {
            if (bp.properties.indexOf(rule.style[i]) < 0) bp.properties.push(rule.style[i]);
          }
        }
      }
    }
    for (const sheet of sheets) {
      let rules;
      try {
        rules = sheet.cssRules;
      } catch (e) {
        inaccessible++;
        continue;
      }
      if (rules) visit(rules, []);
    }
    return Array.from(found.values());
  }

  function familyKey(f) {
    return f.trim().replace(/^["']|["']$/g, "").toLowerCase();
  }

  function fontFaces() {
    const used = new Set();
    const nodes = [el].concat(Array.from(el.getElementsByTagName("*")).slice(0, 300));
    for (const n of nodes) {
      for (const f of getComputedStyle(n).fontFamily.split(",")) used.add(familyKey(f));
    }
    const faces = [];
    const seen = new Set();
    const add = (face) => {
      const key = [familyKey(face.family), face.weight || "", face.style || "", face.src || ""].join("|");
      if (seen.has(key)) return;
      seen.add(key);
      faces.push(face);
    };
    (function visit(rules) {
      for (const rule of rules) {
        if (typeof CSSFontFaceRule !== "undefined" && rule instanceof CSSFontFaceRule) {
          const st = rule.style;
          const family = st.getPropertyValue("font-family");
          if (!used.has(familyKey(family))) continue;
          add({
            family: family.trim().replace(/^["']|["']$/g, ""),
            src: st.getPropertyValue("src"),
            weight: st.getPropertyValue("font-weight"),
            style: st.getPropertyValue("font-style"),
            stretch: st.getPropertyValue("font-stretch"),
            display: st.getPropertyValue("font-display"),
            unicodeRange: st.getPropertyValue("unicode-range"),
          });
        } else if (rule.cssRules) {
          visit(rule.cssRules);
        }
      }
    })(
      sheets.flatMap((s) => {
        try {
          return Array.from(s.cssRules || []);
        } catch (e) {
          return [];
        }
      })
    );
    // FontFace objects also cover sheets this page may not read (cross-origin).
    if (document.fonts) {
      for (const f of document.fonts) {
        if (!used.has(familyKey(f.family))) continue;
        const family = f.family.replace(/^["']|["']$/g, "");
        if (faces.some((x) => familyKey(x.family) === familyKey(family) && x.weight === f.weight && x.style === f.style)) continue;
        add({ family, weight: f.weight, style: f.style, stretch: f.stretch, display: f.display, unicodeRange: f.unicodeRange, status: f.status });
      }
    }
    return faces;
  }

  const r = el.getBoundingClientRect();
  return {
    url: location.origin + location.pathname,
    tag: el.localName,
    rect: { x: r.left + scrollX, y: r.top + scrollY, width: r.width, height: r.height },
    viewport: { width: innerWidth, height: innerHeight, deviceScaleFactor: devicePixelRatio },
    centerOrigin: el.offsetWidth === undefined ? "" : el.offsetWidth / 2 + "px " + el.offsetHeight / 2 + "px",
    parent: el.parentElement ? styleMap(getComputedStyle(el.parentElement)) : null,
    defaults: tagDefaults(el.localName, el.namespaceURI),
    tree: serialize(el),
    treeTruncated: truncated,
    children: childSummary(),
    breakpoints: breakpoints(),
    fontFaces: fontFaces(),
    inaccessibleSheets: inaccessible,
  };
})
