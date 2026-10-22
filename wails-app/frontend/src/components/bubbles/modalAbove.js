// The expanded bubble overlay sits at z-index 9601, above the pages' own
// dialogs (z 9000-9500) so a bubble opened from one is visible and clickable.
export const BUBBLE_OVERLAY_Z = 9601

// layerOf is the z-index of the positioned layer el sits in (null when none is found).
function layerOf(el) {
  for (let cur = el; cur && cur !== document.documentElement; cur = cur.parentElement) {
    const cs = getComputedStyle(cur)
    const z = parseInt(cs.zIndex, 10)
    if (cs.position !== 'static' && Number.isFinite(z)) return z
  }
  return null
}

// isModalOpenAbove reports whether a modal dialog is open on top of the
// bubble overlay (a ConfirmDialog, z 10001). A page dialog underneath it does
// not count (a dialog whose layer cannot be read does): Escape and a backdrop click still collapse the bubble first.
export function isModalOpenAbove() {
  return [...document.querySelectorAll('[aria-modal="true"]')].some(el => { const z = layerOf(el); return z === null || z > BUBBLE_OVERLAY_Z })
}
