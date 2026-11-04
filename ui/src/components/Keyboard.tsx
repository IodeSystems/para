import { useEffect, useEffectEvent } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { actionFor, stackKeyFor, type StackKey } from '@/lib/keys'

// dialogOpen says a dialog or a menu is open: its own keys rule.
const dialogOpen = () => !!document.querySelector('[role="dialog"], .MuiDrawer-root')

// Global shortcuts: digits switch tabs, / searches, ? shows help. Help opens as a URL overlay
// (WEB-10), so Back closes it.
export function Keyboard() {
  const navigate = useNavigate()
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const t = e.target instanceof HTMLElement ? e.target : null
      const a = actionFor({
        key: e.key,
        ctrlKey: e.ctrlKey,
        metaKey: e.metaKey,
        altKey: e.altKey,
        target: t,
      })
      if (!a) return
      // A dialog or menu is open: its own keys rule, except that ? and Esc still work.
      if (dialogOpen() && a.kind !== 'help') return
      e.preventDefault()
      switch (a.kind) {
        case 'tab':
          void navigate({ href: a.to })
          break
        case 'search': {
          // Already on Search: put the cursor back in the field.
          const field = document.getElementById('search-input')
          if (field) field.focus()
          else void navigate({ to: '/search' })
          break
        }
        case 'help':
          void navigate({ to: '.', search: (prev) => ({ ...prev, help: true }) })
          break
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [navigate])
  return null
}

// useStackKeys gives the review stack's keys (j, k, a, r, e, c, x, A, o) to the page while it is
// shown. Keys do nothing in a text field or while a dialog is open.
export function useStackKeys(onKey: (k: StackKey) => void) {
  const handle = useEffectEvent(onKey)
  useEffect(() => {
    const listen = (e: KeyboardEvent) => {
      const t = e.target instanceof HTMLElement ? e.target : null
      const k = stackKeyFor({
        key: e.key,
        ctrlKey: e.ctrlKey,
        metaKey: e.metaKey,
        altKey: e.altKey,
        target: t,
      })
      if (!k || dialogOpen()) return
      e.preventDefault()
      handle(k)
    }
    window.addEventListener('keydown', listen)
    return () => window.removeEventListener('keydown', listen)
  }, [])
}
