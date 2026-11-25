/** W116-G663：对照旧版焦点环 Focus 再对照。旧版无独立焦点环控件，仅为 token/outline。不含产品语义。不 VoiceOver。不改业务 RPC。 */
export const focusRingClassName = 'mzgui-focus-ring'

const tabbableSelector = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

export function getTabbableElements(root: ParentNode | null): HTMLElement[] {
  if (!root) {
    return []
  }
  return Array.from(root.querySelectorAll<HTMLElement>(tabbableSelector)).filter((element) => {
    if (element.hasAttribute('disabled') || element.getAttribute('aria-hidden') === 'true') {
      return false
    }
    return element.tabIndex >= 0 && (element.offsetWidth > 0 || element.offsetHeight > 0 || element.getClientRects().length > 0)
  })
}

export function trapFocus(event: KeyboardEvent, root: ParentNode | null) {
  if (event.key !== 'Tab') {
    return
  }
  const items = getTabbableElements(root)
  if (items.length === 0) {
    event.preventDefault()
    return
  }
  const first = items[0]
  const last = items[items.length - 1]
  const active = document.activeElement
  if (event.shiftKey && (active === first || !root || (active instanceof Node && !root.contains(active)))) {
    event.preventDefault()
    last.focus()
    return
  }
  if (!event.shiftKey && active === last) {
    event.preventDefault()
    first.focus()
  }
}

export function rememberFocus(): HTMLElement | null {
  return document.activeElement instanceof HTMLElement ? document.activeElement : null
}

export function cycleIndex(current: number, delta: number, length: number): number {
  if (length <= 0) {
    return -1
  }
  if (current < 0) {
    return delta >= 0 ? 0 : length - 1
  }
  return (current + delta + length * 8) % length
}

export function pageIndex(current: number, deltaPages: number, length: number, pageSize = 10): number {
  if (length <= 0) {
    return -1
  }
  const size = pageSize > 0 ? Math.floor(pageSize) : 1
  const start = current < 0 ? 0 : current
  return Math.min(length - 1, Math.max(0, start + deltaPages * size))
}

export function typeaheadIndex(labels: string[], current: number, key: string): number {
  if (labels.length === 0 || key.length !== 1 || key === ' ') {
    return -1
  }
  const needle = key.toLowerCase()
  const start = current < 0 ? 0 : current + 1
  for (let offset = 0; offset < labels.length; offset++) {
    const index = (start + offset) % labels.length
    const label = labels[index].trim().toLowerCase()
    if (label !== '' && label.startsWith(needle)) {
      return index
    }
  }
  return -1
}
