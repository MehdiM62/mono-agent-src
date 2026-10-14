// Where the editor puts things: the camera that fits a workflow into the
// view, and the spot (and connection) for a node added from the palette.
// Pure functions over the canvas's nodes ({ id, x, y, inputs, outputs })
// and edges ({ source, sourcePortIdx, ... }); sizes come from the caller.

export const DEFAULT_CAMERA = { x: 60, y: 60, zoom: 1 }

// fitCamera frames every node in a view of size { width, height }, never
// zooming in past maxZoom (100%) nor out past minZoom. An empty canvas or
// a view not laid out yet (hidden page) keeps the default camera.
export function fitCamera(nodes, view, { nodeW, nodeH, pad = 60, minZoom = 0.25, maxZoom = 1 } = {}) {
  if (!nodes?.length || !view?.width || !view?.height) return { ...DEFAULT_CAMERA }
  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
  for (const n of nodes) {
    minX = Math.min(minX, n.x)
    minY = Math.min(minY, n.y)
    maxX = Math.max(maxX, n.x + nodeW)
    maxY = Math.max(maxY, n.y + nodeH(n))
  }
  const w = maxX - minX, h = maxY - minY
  const zoom = Math.max(minZoom, Math.min(maxZoom, (view.width - 2 * pad) / w, (view.height - 2 * pad) / h))
  // Centre the bounds; when they fit at 100%, keep them near the top-left
  // padding rather than floating a small workflow in the middle.
  const x = zoom < maxZoom ? (view.width - w * zoom) / 2 - minX * zoom : pad - minX * zoom
  const y = zoom < maxZoom ? (view.height - h * zoom) / 2 - minY * zoom : pad - minY * zoom
  return { x, y, zoom }
}

const overlaps = (a, b, nodeW, nodeH) =>
  a.x < b.x + nodeW && b.x < a.x + nodeW && a.y < b.y + nodeH(b) && b.y < a.y + nodeH(a)

// placeAfter is the position for a new node to the right of anchor, with a
// gap, moved down until it covers no other node.
export function placeAfter(anchor, nodes, newNode, { nodeW, nodeH, gapX = 80, gapY = 40 }) {
  const pos = { ...newNode, x: anchor.x + nodeW + gapX, y: anchor.y }
  for (let i = 0; i < 50; i++) {
    const hit = nodes.find(n => overlaps(pos, n, nodeW, nodeH))
    if (!hit) break
    pos.y = hit.y + nodeH(hit) + gapY
  }
  return { x: pos.x, y: pos.y }
}

// autoConnect is the edge from anchor's main (first) output to the new
// node's first input, as n8n does, or null when the anchor has no outputs,
// the main output is already connected, or the new node takes no input.
export function autoConnect(anchor, newNode, edges) {
  if (!anchor?.outputs?.length || !newNode?.inputs?.length) return null
  if (edges.some(e => e.source === anchor.id && e.sourcePortIdx === 0)) return null
  return {
    source: anchor.id, sourcePortIdx: 0, sourcePortId: anchor.outputs[0]?.id,
    target: newNode.id, targetPortIdx: 0, targetPortId: newNode.inputs[0]?.id,
  }
}

// panToShow moves camera cam (zoom kept) just enough that the box
// { x, y, w, h } in world units sits inside the view with pad around it.
export function panToShow(cam, box, view, pad = 40) {
  if (!view?.width || !view?.height) return cam
  const shift = (pos, size, offset, span) => {
    const lo = offset + pos * cam.zoom, hi = offset + (pos + size) * cam.zoom
    if (hi > span - pad) return offset - (hi - (span - pad))
    if (lo < pad) return offset + (pad - lo)
    return offset
  }
  const x = shift(box.x, box.w, cam.x, view.width)
  const y = shift(box.y, box.h, cam.y, view.height)
  return x === cam.x && y === cam.y ? cam : { ...cam, x, y }
}
