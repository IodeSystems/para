// copyText puts text on the clipboard. navigator.clipboard exists only in a secure context, and
// para on plain HTTP over a LAN is not one, so fall back to a hidden textarea and execCommand.
// Returns true when the text was copied.
export async function copyText(text: string): Promise<boolean> {
  if (typeof navigator !== 'undefined' && navigator.clipboard && window.isSecureContext) {
    try {
      await navigator.clipboard.writeText(text)
      return true
    } catch {
      // Permission denied or document not focused: use the fallback below.
    }
  }
  return copyWithTextarea(text)
}

function copyWithTextarea(text: string): boolean {
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.setAttribute('aria-hidden', 'true')
  Object.assign(ta.style, { position: 'fixed', top: '0', left: '0', opacity: '0' })
  document.body.appendChild(ta)
  const active = document.activeElement
  ta.focus()
  ta.select()
  ta.setSelectionRange(0, text.length)
  let ok = false
  try {
    ok = document.execCommand('copy')
  } catch {
    ok = false
  }
  ta.remove()
  if (active instanceof HTMLElement) active.focus()
  return ok
}
