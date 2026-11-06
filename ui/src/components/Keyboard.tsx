import { useEffect, useEffectEvent } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { OMNI_FOCUS } from '@/components/OmniBar'
import { actionFor, nextKeyFor, type NextKey } from '@/lib/keys'

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
        case 'search':
          window.dispatchEvent(new Event(OMNI_FOCUS))
          break
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

// useNextKeys gives the Next page's keys (NEXT_KEYS) to the page while it is shown.
export function useNextKeys(onKey: (k: NextKey) => void) {
  const handle = useEffectEvent(onKey)
  useEffect(() => {
    const listen = (e: KeyboardEvent) => {
      const t = e.target instanceof HTMLElement ? e.target : null
      const k = nextKeyFor({
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
