import { useEffect, useEffectEvent, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useProposalDraft, type DraftBody } from '@/api/proposals'
import { errorText } from '@/client/gqlClient'
import { answerEmpty, answerInput, stringMap, type Answer } from '@/lib/dossier'
import type { ProposalData } from '@/components/stack/parts'

// AUTOSAVE_MS is how long the page waits after the last change before it saves the draft.
export const AUTOSAVE_MS = 1500

// Changes are the owner's edits not saved yet. An item set to null clears its answer.
type Changes = {
  items: Record<string, Answer | null>
  sections: Record<string, string>
  overall?: string
}

const NONE: Changes = { items: {}, sections: {} }

const dirty = (c: Changes) =>
  Object.keys(c.items).length > 0 || Object.keys(c.sections).length > 0 || c.overall !== undefined

export type SaveState = 'saved' | 'unsaved' | 'saving' | 'error'

// useDossierDraft keeps the owner's answers on a dossier page and saves them as a draft about
// 1.5 s after the last change. What shows is the change not saved yet, else the saved answer. A
// change is dropped only after the page has read the saved dossier back, so the screen does not
// jump back to the old answer.
export function useDossierDraft(d: ProposalData) {
  const id = d.proposal.id
  const qc = useQueryClient()
  const save = useProposalDraft()
  const [changes, setChanges] = useState<Changes>(NONE)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const saved = new Map(d.items.map((it) => [it.id, (it.answer ?? null) as Answer | null]))
  const savedSections = stringMap(d.response?.sections)

  const answerOf = (item: string): Answer | null =>
    item in changes.items ? (changes.items[item] ?? null) : (saved.get(item) ?? null)
  const sectionOf = (sid: string): string => changes.sections[sid] ?? savedSections[sid] ?? ''
  const overall = changes.overall ?? d.response?.overall ?? ''

  const setAnswer = (item: string, a: Answer | null) =>
    setChanges((c) => ({ ...c, items: { ...c.items, [item]: answerEmpty(a) ? null : a } }))
  const setSection = (sid: string, text: string) =>
    setChanges((c) => ({ ...c, sections: { ...c.sections, [sid]: text } }))
  const setOverall = (text: string) => setChanges((c) => ({ ...c, overall: text }))

  // flush saves every change now. It gives true when nothing is left to save.
  const flush = async (): Promise<boolean> => {
    const sent = changes
    if (!dirty(sent)) return true
    if (busy) return false
    setBusy(true)
    const body: DraftBody = {
      id,
      revision: d.proposal.revision,
      items: Object.entries(sent.items).map(([item, a]) => ({ item, answer: answerInput(a) })),
      sections: Object.entries(sent.sections).map(([section, comment]) => ({ section, comment })),
      overall: sent.overall,
    }
    try {
      await save.mutateAsync(body)
      await qc.refetchQueries({ queryKey: ['proposal', id], exact: true })
      // Keep what changed while the save ran; drop what was saved.
      setChanges((c) => {
        const items = { ...c.items }
        for (const [k, v] of Object.entries(sent.items)) if (items[k] === v) delete items[k]
        const sections = { ...c.sections }
        for (const [k, v] of Object.entries(sent.sections))
          if (sections[k] === v) delete sections[k]
        return { items, sections, overall: c.overall === sent.overall ? undefined : c.overall }
      })
      setError('')
      return true
    } catch (e) {
      setError(errorText(e))
      return false
    } finally {
      setBusy(false)
    }
  }
  const onTimer = useEffectEvent(() => void flush())

  useEffect(() => {
    if (!dirty(changes) || busy || error) return
    const t = window.setTimeout(onTimer, AUTOSAVE_MS)
    return () => window.clearTimeout(t)
  }, [changes, busy, error])

  const state: SaveState = busy ? 'saving' : error ? 'error' : dirty(changes) ? 'unsaved' : 'saved'
  return {
    answerOf,
    // overridden says the owner changed this answer here and it is not saved yet.
    overridden: (item: string) => item in changes.items,
    setAnswer,
    sectionOf,
    setSection,
    overall,
    setOverall,
    state,
    error,
    // retry reads the dossier again and saves the changes on the new revision.
    retry: async () => {
      await qc.refetchQueries({ queryKey: ['proposal', id], exact: true })
      setError('')
    },
    flush,
  }
}
