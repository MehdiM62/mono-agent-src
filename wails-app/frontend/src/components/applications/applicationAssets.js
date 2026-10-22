import * as WailsApp from '../../wailsjs/go/main/App'

/**
 * Normalizes Contract B ApplicationAssets ensuring array fields and sub-objects exist safely.
 */
export function normalizeApplicationAssets(raw) {
  if (!raw || typeof raw !== 'object') {
    return {
      posting: null,
      documents: [],
      fill: null,
      people: [],
      messages: [],
      tasks: [],
      task: null,
      links: [],
    }
  }

  // Handle case where backend returns { assets: { ... } } or raw is already { posting, documents, ... }
  const data = raw.assets && typeof raw.assets === 'object' ? raw.assets : raw

  return {
    posting: data.posting ? {
      url: data.posting.url || '',
      capturedText: data.posting.capturedText || data.posting.captured_text || '',
      capturedAt: data.posting.capturedAt || data.posting.captured_at || '',
      deadline: data.posting.deadline || '',
    } : null,
    documents: Array.isArray(data.documents) ? data.documents.map(d => ({
      id: d.id || '',
      filename: d.filename || '',
      kind: d.kind || 'other',
      source: d.source || 'upload',
      format: d.format || '',
      sizeBytes: d.sizeBytes != null ? d.sizeBytes : (d.size_bytes != null ? d.size_bytes : 0),
      createdAt: d.createdAt || d.created_at || '',
      attached: Boolean(d.attached),
      path: d.path || '',
    })) : [],
    fill: data.fill ? {
      status: data.fill.status || 'none',
      screenshot: data.fill.screenshot || '',
      updatedAt: data.fill.updatedAt || data.fill.updated_at || '',
      fields: Array.isArray(data.fill.fields) ? data.fill.fields.map(f => ({
        label: f.label || '',
        value: f.value != null ? String(f.value) : '',
        source: f.source || '',
        generated: Boolean(f.generated),
      })) : [],
      note: data.fill.note || '',
    } : null,
    people: Array.isArray(data.people) ? data.people.map(p => ({
      id: p.id || '',
      name: p.name || '',
      role: p.role || '',
      email: p.email || '',
    })) : [],
    messages: Array.isArray(data.messages) ? data.messages.map(m => ({
      id: m.id || '',
      channel: m.channel || '',
      direction: m.direction || 'in',
      subject: m.subject || '',
      snippet: m.snippet || '',
      at: m.at || '',
    })) : [],
    tasks: Array.isArray(data.tasks) ? data.tasks.map(t => ({
      id: t.id || '',
      title: t.title || '',
      status: t.status || '',
    })) : [],
    task: data.task ? {
      taskId: data.task.taskId || data.task.task_id || '',
      conversationId: data.task.conversationId || data.task.conversation_id || '',
      status: data.task.status || '',
    } : null,
    links: Array.isArray(data.links) ? data.links : [],
  }
}

/**
 * Safely fetches application assets from Wails.
 * If the Go binding GetApplicationAssets does not exist yet, returns null without crashing.
 */
export async function fetchApplicationAssets(id) {
  if (!id) return null

  // Guard: missing binding returns null safely (empty state, no crash)
  if (typeof WailsApp?.GetApplicationAssets !== 'function') {
    return null
  }

  try {
    const raw = await WailsApp.GetApplicationAssets(id)
    return normalizeApplicationAssets(raw)
  } catch (err) {
    // If backend rejects or throws, return normalized empty object or null
    console.warn('GetApplicationAssets failed for application', id, err)
    return null
  }
}
