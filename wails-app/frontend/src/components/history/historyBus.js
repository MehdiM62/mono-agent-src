// A tiny open/close bus so the Sidebar, a shortcut and the panel need no
// shared parent state: anything calls openHistory(), the one mounted
// HistoryPanel listens.
const bus = new EventTarget()
export const openHistory = () => bus.dispatchEvent(new Event('open'))
export const toggleHistory = () => bus.dispatchEvent(new Event('toggle'))
export function onHistoryRequest(fn) {
  const o = () => fn('open'), t = () => fn('toggle')
  bus.addEventListener('open', o); bus.addEventListener('toggle', t)
  return () => { bus.removeEventListener('open', o); bus.removeEventListener('toggle', t) }
}
