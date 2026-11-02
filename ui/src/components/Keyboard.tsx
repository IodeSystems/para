import { useEffect } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { actionFor } from '@/lib/keys'

// Global shortcuts: digits switch tabs, / searches, c captures, ? shows help. Capture and help
// open as URL overlays (WEB-10), so Back closes them.
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
      e.preventDefault()
      switch (a.kind) {
        case 'tab':
          void navigate({ href: a.to })
          break
        case 'search':
          void navigate({ to: '/search' })
          break
        case 'capture':
          void navigate({ to: '.', search: (prev) => ({ ...prev, capture: true }) })
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
