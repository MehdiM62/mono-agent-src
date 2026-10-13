// The node palette is split in two: the actions of the installed web
// automations, and every other node. Which section a node belongs to comes
// from `node palette` (each node's `section`); the palette only groups.

export const WEB_AUTOMATION = 'web_automation'

// paletteSectionOf is a category's section, from its nodes.
export function paletteSectionOf(nodes) {
  return Array.isArray(nodes) && nodes.some(n => n?.section === WEB_AUTOMATION) ? WEB_AUTOMATION : 'nodes'
}

// paletteCategoryLabel is the automation's name for a web automation
// category, '' for a built-in one.
export function paletteCategoryLabel(nodes) {
  return (Array.isArray(nodes) && nodes.find(n => n?.category_label)?.category_label) || ''
}

// splitPaletteSections groups categories into the two sections, web
// automations first (sorted by name) and then the rest in their given order.
// Both sections are always returned, so an empty one can say so.
export function splitPaletteSections(categories) {
  const web = [], rest = []
  for (const c of categories || []) (c.section === WEB_AUTOMATION ? web : rest).push(c)
  web.sort((a, b) => String(a.label).localeCompare(String(b.label)))
  return [
    { id: WEB_AUTOMATION, label: 'Web automations', categories: web },
    { id: 'nodes', label: 'Nodes', categories: rest },
  ]
}

export const DEPRECATED = 'deprecated'
const DEPRECATED_LABEL = /\(deprecated\)/i

// arrangePaletteCategories orders the built-in categories for a first edit:
// triggers first (every workflow starts with one), deprecated nodes pulled
// out of their categories into one collapsed "Deprecated" group at the end
// (still there for old workflows, out of the way of new ones), and a SERVICE
// node that shares its name with a node elsewhere labelled "(API)" so the two
// distinct node types can be told apart. Nodes keep their own category.
export function arrangePaletteCategories(categories) {
  const deprecated = []
  const cats = []
  for (const c of categories || []) {
    const keep = []
    for (const n of c.nodes || []) {
      const node = { ...n, category: n.category || c.id }
      if (DEPRECATED_LABEL.test(node.label || '')) deprecated.push(node)
      else keep.push(node)
    }
    if (keep.length) cats.push({ ...c, nodes: keep })
  }
  const otherLabels = new Set()
  for (const c of cats) if (c.id !== 'service') for (const n of c.nodes) otherLabels.add(String(n.label).toLowerCase())
  const named = cats.map(c => (c.id !== 'service' ? c : {
    ...c,
    nodes: c.nodes.map(n => (otherLabels.has(String(n.label).toLowerCase()) ? { ...n, label: `${n.label} (API)` } : n)),
  }))
  const triggers = named.filter(c => c.id === 'triggers')
  const rest = named.filter(c => c.id !== 'triggers')
  const out = [...triggers, ...rest]
  if (deprecated.length) out.push({ id: DEPRECATED, label: 'DEPRECATED', section: 'nodes', collapsedByDefault: true, nodes: deprecated })
  return out
}
