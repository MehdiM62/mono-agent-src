// Where an element comes from in the page's source code, read from what
// dev builds leave on the DOM: inspector attributes (code-inspector-plugin,
// react-dev-inspector, vite-plugin-vue-inspector, Sentry's component
// annotation), React fibers, Vue component instances and Svelte's
// __svelte_meta; else class, id, test-id and text hints to search for.
// Called as (fn)(el, opts); every candidate carries a confidence and the
// best one wins.
(function (el, opts) {
  "use strict";
  const MAX_UP = 25;
  const candidates = [];
  const hints = [];
  const hint = (kind, value) => {
    if (value && hints.length < 24 && !hints.some((h) => h.kind === kind && h.value === value)) hints.push({ kind, value });
  };
  const ancestors = [];
  for (let n = el; n && n.nodeType === 1 && ancestors.length < MAX_UP; n = n.parentElement) ancestors.push(n);
  const decay = (base, d) => Math.max(0.4, Math.round((base - 0.05 * d) * 100) / 100);

  // "path:line:column[:tag]" — a Windows drive letter keeps its colon.
  const parseLoc = (s) => {
    const m = /^(.*?):(\d+):(\d+)(?::[^:\\/]*)?$/.exec(s) || /^(.*?):(\d+)$/.exec(s);
    return m ? { file: m[1], line: +m[2], column: m[3] ? +m[3] : 0 } : { file: s, line: 0, column: 0 };
  };

  ancestors.forEach((n, d) => {
    const a = (k) => n.getAttribute(k);
    if (a("data-insp-path")) {
      candidates.push(Object.assign({ via: "data-insp-path", confidence: decay(0.95, d) }, parseLoc(a("data-insp-path"))));
    }
    if (a("data-v-inspector")) {
      candidates.push(Object.assign({ framework: "vue", via: "data-v-inspector", confidence: decay(0.95, d) }, parseLoc(a("data-v-inspector"))));
    }
    const rel = a("data-inspector-relative-path") || a("data-inspector-path");
    if (rel) {
      candidates.push({
        framework: "react",
        via: "data-inspector",
        file: rel,
        line: +a("data-inspector-line") || 0,
        column: +a("data-inspector-column") || 0,
        confidence: decay(0.95, d),
      });
    }
    if (a("data-sentry-source-file") || a("data-sentry-component")) {
      candidates.push({
        framework: "react",
        via: "data-sentry",
        file: a("data-sentry-source-file") || "",
        component: a("data-sentry-component") || "",
        confidence: decay(0.7, d),
      });
    }
  });

  // React: the host fiber's _debugSource is where its JSX was written; the
  // owner chain names the components that rendered it.
  const nameOf = (t, depth) => {
    if (!t || (depth || 0) > 3) return "";
    if (typeof t === "function") return t.displayName || t.name || "";
    if (typeof t === "object") return t.displayName || nameOf(t.render, (depth || 0) + 1) || nameOf(t.type, (depth || 0) + 1);
    return "";
  };
  const STACK_SKIP = /node_modules|react-dom|react\.development|react-jsx|scheduler|chunk-[A-Z0-9]{6,}/i;
  for (let d = 0; d < ancestors.length; d++) {
    const n = ancestors[d];
    const key = Object.keys(n).find((k) => k.startsWith("__reactFiber$") || k.startsWith("__reactInternalInstance$"));
    if (!key) continue;
    const fiber = n[key];
    const components = [];
    for (let o = fiber._debugOwner, i = 0; o && i < 20; o = o._debugOwner, i++) {
      const nm = nameOf(o.type);
      if (nm && components.indexOf(nm) < 0) components.push(nm);
    }
    for (let f = fiber.return, i = 0; f && i < 60 && components.length < 8; f = f.return, i++) {
      const nm = typeof f.type === "string" ? "" : nameOf(f.type);
      if (nm && components.indexOf(nm) < 0) components.push(nm);
    }
    const c = { framework: "react", components, component: components[0] || "" };
    let src = null;
    let hops = 0;
    for (let f = fiber; f && hops < 60; f = f.return, hops++) {
      if (f._debugSource) {
        src = f._debugSource;
        break;
      }
    }
    if (src) {
      Object.assign(c, { via: "react-debug-source", file: src.fileName, line: src.lineNumber || 0, column: src.columnNumber || 0 });
      c.confidence = decay(hops === 0 ? 0.95 : 0.8, d);
    } else if (fiber._debugStack && fiber._debugStack.stack) {
      // React 19 dropped _debugSource; the JSX call site is in this stack.
      const frame = fiber._debugStack.stack.split("\n").slice(1).find((l) => /:\d+:\d+\)?\s*$/.test(l) && !STACK_SKIP.test(l));
      const m = frame && /(?:\(|at\s+)([^()\s]+):(\d+):(\d+)\)?\s*$/.exec(frame);
      if (m) Object.assign(c, { via: "react-debug-stack", file: m[1], line: +m[2], column: +m[3] });
      c.confidence = decay(m ? 0.6 : 0.4, d);
    } else {
      c.via = "react-fiber";
      c.confidence = decay(components.length ? 0.45 : 0.3, d);
    }
    candidates.push(c);
    break;
  }

  // Vue 3 (__vueParentComponent) and Vue 2 (__vue__): the component file.
  for (let d = 0; d < ancestors.length; d++) {
    const n = ancestors[d];
    const inst = n.__vueParentComponent;
    const vm = n.__vue__;
    if (!inst && !vm) continue;
    const components = [];
    let file = "";
    if (inst) {
      for (let i = inst, k = 0; i && k < 20; i = i.parent, k++) {
        const t = i.type || {};
        const nm = t.name || t.__name || "";
        if (nm && components.indexOf(nm) < 0) components.push(nm);
        if (!file && t.__file) file = t.__file;
      }
    } else {
      for (let v = vm, k = 0; v && k < 20; v = v.$parent, k++) {
        const o = v.$options || {};
        const nm = o.name || o._componentTag || "";
        if (nm && components.indexOf(nm) < 0) components.push(nm);
        if (!file && o.__file) file = o.__file;
      }
    }
    candidates.push({
      framework: "vue",
      via: inst ? "vue-component" : "vue2-component",
      file,
      components,
      component: components[0] || "",
      confidence: decay(file ? 0.75 : 0.4, d),
    });
    break;
  }

  // Svelte dev builds put the element's own location on it.
  for (let d = 0; d < ancestors.length; d++) {
    const meta = ancestors[d].__svelte_meta;
    if (!meta || !meta.loc) continue;
    candidates.push({
      framework: "svelte",
      via: "svelte-meta",
      file: meta.loc.file || "",
      line: meta.loc.line || 0,
      column: meta.loc.column || 0,
      confidence: decay(0.9, d),
    });
    break;
  }

  // Angular dev mode names the owning component; it knows no file.
  try {
    const ng = window.ng;
    const owner = ng && ng.getOwningComponent && ng.getOwningComponent(el);
    const name = owner && owner.constructor && owner.constructor.name;
    if (name) candidates.push({ framework: "angular", via: "angular-dev", component: name, components: [name], confidence: 0.4 });
  } catch (e) {
    // not an Angular dev build
  }

  // Hints to search the code for when nothing better is there.
  const CSS_MODULE = /^([A-Z][A-Za-z0-9]*)_([A-Za-z0-9-]+)__[A-Za-z0-9_-]{4,}$/;
  for (const n of ancestors.slice(0, 3)) {
    for (const c of Array.from(n.classList).slice(0, 8)) {
      const m = CSS_MODULE.exec(c);
      if (m) {
        hint("css-module", c);
        if (!candidates.some((x) => x.via === "css-module")) {
          candidates.push({ via: "css-module", component: m[1], components: [m[1]], confidence: 0.3 });
        }
      } else if (!/^(css|sc|jsx|svelte)-[A-Za-z0-9]{5,}$/.test(c)) {
        hint("class", c);
      }
    }
  }
  if (el.id) hint("id", el.id);
  const testId = el.closest("[data-testid],[data-test-id],[data-test],[data-cy]");
  if (testId) {
    hint("test-id", testId.getAttribute("data-testid") || testId.getAttribute("data-test-id") || testId.getAttribute("data-test") || testId.getAttribute("data-cy"));
  }
  const text = (el.innerText || el.textContent || "").replace(/\s+/g, " ").trim();
  if (text) hint("text", text.length > opts.maxText ? text.slice(0, opts.maxText) : text);
  hint("tag", el.localName);

  candidates.sort((a, b) => b.confidence - a.confidence);
  const best = Object.assign({ framework: "", component: "", components: [], file: "", line: 0, column: 0, confidence: 0.1, via: "hints" }, candidates[0] || {});
  // A file from one source and component names from another, when the best lacks them.
  for (const c of candidates.slice(1)) {
    if (!best.framework && c.framework) best.framework = c.framework;
    if (!best.component && c.component) {
      best.component = c.component;
      best.components = c.components || [c.component];
    }
  }
  best.hints = hints;
  return best;
})
