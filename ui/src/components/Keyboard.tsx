import { useEffect, useEffectEvent } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { noteIdOf, focusNote } from '@/components/stack/parts'
import { actionFor, cardKeyFor, stackKeyFor, type CardKey, type StackKey } from '@/lib/keys'

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

// useStackKeys gives the review stack's keys (STACK_KEYS) to the page while it is shown. Keys do nothing in a text field or while a dialog is open.
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

// useCardKeys gives a dossier page's keys (CARD_KEYS) to the page while it is shown.
export function useCardKeys(onKey: (k: CardKey) => void) {
  const handle = useEffectEvent(onKey)
  useEffect(() => {
    const listen = (e: KeyboardEvent) => {
      const t = e.target instanceof HTMLElement ? e.target : null
      const k = cardKeyFor({
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

const CARD_ACT: Record<CardKey, string> = {
  decline: 'reject',
  skip: 'skip',
  ask: 'ask',
  done: 'done',
}

// cardKey does a card key on the card of one item: it presses the card's own button, unless it is
// pressed already, and puts the cursor in the note. A card with no such button does nothing.
export function cardKey(item: string, k: CardKey) {
  const card = document.querySelector(`[data-item="${CSS.escape(item)}"]`)
  const button = card?.querySelector<HTMLButtonElement>(`[data-act="${CARD_ACT[k]}"]`)
  if (!button) return
  if (button.getAttribute('aria-pressed') !== 'true') button.click()
  if (k !== 'done') focusNote(noteIdOf(item))
  card?.scrollIntoView({ block: 'nearest' })
}
