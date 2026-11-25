export type TextInputBehaviorTarget = HTMLInputElement | HTMLTextAreaElement

export type TextInputBehaviorOptions = {
  autocomplete?: 'preserve' | 'off'
}

const TEXT_INPUT_SELECTOR = 'input, textarea'
const SKIP_INPUT_TYPES = new Set([
  'button',
  'checkbox',
  'color',
  'file',
  'hidden',
  'image',
  'radio',
  'range',
  'reset',
  'submit',
])

function isElement(value: unknown): value is Element {
  return typeof Element !== 'undefined' && value instanceof Element
}

function isDocument(value: unknown): value is Document {
  return typeof Document !== 'undefined' && value instanceof Document
}

function isTextInputTarget(value: unknown): value is TextInputBehaviorTarget {
  if (typeof HTMLTextAreaElement !== 'undefined' && value instanceof HTMLTextAreaElement) return true
  if (typeof HTMLInputElement === 'undefined' || !(value instanceof HTMLInputElement)) return false
  return !SKIP_INPUT_TYPES.has(String(value.type || 'text').toLowerCase())
}

function inputBehaviorDisabled(element: Element) {
  return element.closest('[data-mzgui-text-input-behavior="off"]') != null
}

function textInputCandidates(root: ParentNode): Element[] {
  const base: Element[] = []
  if (isElement(root) && root.matches(TEXT_INPUT_SELECTOR)) base.push(root)
  return base.concat(Array.from(root.querySelectorAll(TEXT_INPUT_SELECTOR)))
}

export function normalizeTextInputElement(
  element: TextInputBehaviorTarget,
  options: TextInputBehaviorOptions = {},
) {
  if (inputBehaviorDisabled(element)) return false
  element.setAttribute('autocorrect', 'off')
  element.setAttribute('autocapitalize', 'off')
  element.setAttribute('spellcheck', 'false')
  if (options.autocomplete !== 'preserve' && !element.hasAttribute('autocomplete')) {
    element.setAttribute('autocomplete', 'off')
  }
  return true
}

export function normalizeTextInputBehavior(
  root: ParentNode | null | undefined = typeof document === 'undefined' ? undefined : document.body,
  options: TextInputBehaviorOptions = {},
) {
  if (!root) return 0
  let normalized = 0
  for (const element of textInputCandidates(root)) {
    if (!isTextInputTarget(element)) continue
    if (normalizeTextInputElement(element, options)) normalized += 1
  }
  return normalized
}

export function installTextInputBehaviorObserver(
  root: ParentNode | null | undefined = typeof document === 'undefined' ? undefined : document.body,
  options: TextInputBehaviorOptions = {},
) {
  if (!root) return () => {}
  normalizeTextInputBehavior(root, options)
  const owner = isDocument(root) ? root : root.ownerDocument
  const Observer = owner?.defaultView?.MutationObserver
  if (!Observer) return () => {}
  const observer = new Observer((mutations) => {
    for (const mutation of mutations) {
      mutation.addedNodes.forEach((node) => {
        if (isElement(node)) normalizeTextInputBehavior(node, options)
      })
    }
  })
  observer.observe(root, { childList: true, subtree: true })
  return () => observer.disconnect()
}
