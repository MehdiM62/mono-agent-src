// Pure helpers for the Info page. The field catalog (`profile info fields`)
// is the only list of fields; nothing here names a field.

// groupCatalog -> [{ id, label, fields }] in the catalog's group order, with
// each group's fields in catalog order. Groups with no fields are dropped, and
// fields naming an unknown group are kept in a trailing group of their own.
export function groupCatalog(catalog) {
  const groups = (catalog?.groups || []).map(g => ({ id: g.id, label: g.label, fields: [] }))
  const byId = new Map(groups.map(g => [g.id, g]))
  for (const f of catalog?.fields || []) {
    let g = byId.get(f.group)
    if (!g) {
      g = { id: f.group, label: f.group, fields: [] }
      byId.set(f.group, g)
      groups.push(g)
    }
    g.fields.push(f)
  }
  return groups.filter(g => g.fields.length > 0)
}

export function entryMap(entries) {
  return new Map((entries || []).map(e => [e.key, e]))
}

export const hasValue = (entry) => !!entry && entry.value !== ''

// leftEmpty counts the fields an AI fill could still fill: photos and the
// card are never AI-filled, so they do not count.
export function leftEmpty(catalog, entries) {
  const map = entryMap(entries)
  return (catalog?.fields || [])
    .filter(f => f.kind !== 'photo' && !f.card && !hasValue(map.get(f.key))).length
}

export function errorText(e) {
  const m = (e && e.message) || String(e ?? '')
  return m.replace(/^Error:\s*/i, '').trim() || 'unknown error'
}

export const isPdfDataUrl = (u) => typeof u === 'string' && u.startsWith('data:application/pdf')
