import { useState } from 'react'
import { useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Box from '@mui/material/Box'
import Chip from '@mui/material/Chip'
import CheckIcon from '@mui/icons-material/Check'
import { agentUnsend, closeItem, useAgentSend } from '@/api/composer'
import { readNotes, saveNotes } from '@/api/notes'
import { canDecide } from '@/client/session'
import { uploadAttachment } from '@/client/upload'
import { errorText } from '@/client/gqlClient'
import { useItemActions } from '@/components/AgendaActions'
import { Composer, type ComposerAction } from '@/components/Composer'
import { useItemDraft } from '@/components/ItemDraft'
import { useToasts } from '@/components/ToastStack'
import { investigable, isClosed, rowKind } from '@/lib/agenda'
import { closable, composerPrompt, notesWithEntry, type ItemMeta } from '@/lib/composer'

// ItemComposer is the composer at the foot of every item's page. Send to agent queues agent work
// with the owner's words and files (agent_send); the agent formats them into the item's notes and
// updates what they touch. Save as notes writes them into the notes now, below what is there. With
// Mark done on, Save as notes closes a task or a meeting at once: that is Done with a note, as the
// Done button near the box does. Investigate adds them to the notes and asks an agent to fill in
// details, check whether it is already done, and recheck its assumptions. It needs review:decide:
// without it the page only reads. onSent runs after a send to an agent, or a save that closed the
// item: the item left the owner's hands. Undo of that send runs onBack.

const today = () => {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export function ItemComposer({
  id,
  title,
  meta,
  onSent,
  onBack,
}: {
  id: string
  title: string
  meta: ItemMeta
  onSent?: () => void
  onBack?: () => void
}) {
  const { session } = useRouteContext({ from: '/_authed' })
  const toasts = useToasts()
  const qc = useQueryClient()
  const send = useAgentSend()
  const draft = useItemDraft(id)
  const kind = rowKind(meta.type, meta.kind)
  const closed = isClosed(meta.status)
  const item = useItemActions({ id, title, kind }, closed, false, (gone) =>
    gone ? onSent?.() : onBack?.(),
  )
  const [done, setDone] = useState(false)
  // now is when the page opened: the prompt does not change while the owner types.
  const [now] = useState(() => new Date())
  if (!canDecide(session)) return null
  const name = title || id
  const close = closable(meta)

  // upload stores each file; a failure stops and says which file.
  const upload = async (files: File[]) => {
    const out = []
    for (const f of files) out.push(await uploadAttachment(id, f))
    return out
  }
  const fail = (what: string, e: unknown) => {
    toasts.push({ text: `Could not ${what} ${name}: ${errorText(e)}`, tone: 'error' })
    return false
  }

  const toAgent = async (text: string, files: File[]) => {
    try {
      const atts = await upload(files)
      // Mark done on an item the owner can close closes it now. Waiting for an agent left it at the
      // top of the agenda.
      const closeNow = done && close
      if (closeNow) await closeItem(id)
      const res = await send.mutateAsync({
        id,
        text: text || undefined,
        attachments: atts.length ? atts.map((a) => a.path) : undefined,
        done: (done && !close) || undefined,
      })
      toasts.push({
        text: closeNow ? `Closed, and sent to agent: ${name}` : `Sent to agent: ${name}`,
        note: res.note ?? undefined,
        undo: async () => {
          await agentUnsend(res.request)
          onBack?.()
          await qc.invalidateQueries()
        },
      })
      setDone(false)
      onSent?.()
      return true
    } catch (e) {
      return fail('send', e)
    }
  }

  const asNotes = async (text: string, files: File[]) => {
    // Save as notes with Mark done is Done with a note: the same call and Undo as the Done button.
    if (done && close) {
      const ok = await item.done(text, files)
      if (ok) setDone(false)
      return ok
    }
    try {
      const atts = await upload(files)
      const before = await readNotes(id)
      const md = notesWithEntry(
        before.md,
        today(),
        text,
        atts.map((a) => a.embed),
      )
      const saved = await saveNotes(id, before.hash, md)
      await qc.invalidateQueries()
      toasts.push({
        text: `Saved as notes: ${name}`,
        undo: async () => {
          const now = await readNotes(id)
          if (now.hash !== saved.hash) throw new Error('the notes changed since; edit them by hand')
          await saveNotes(id, now.hash, before.md)
          await qc.invalidateQueries()
        },
      })
      return true
    } catch (e) {
      return fail('save notes on', e)
    }
  }

  const actions: ComposerAction[] = [
    {
      label: done && close ? 'Save as notes and close' : 'Save as notes',
      hint:
        done && !close
          ? 'Only an agent can close this: send it, or turn Mark done off'
          : 'Add to the notes now, below what is there',
      disabled: done && !close,
      run: asNotes,
    },
  ]
  if (!closed && investigable(kind))
    actions.push({
      label: 'Investigate',
      hint: 'Add to the notes, and ask an agent to fill in details, check if it is done, and recheck its assumptions',
      run: (text, files) => item.look(text, files),
    })

  return (
    <Box
      data-item-composer={id}
      sx={{
        position: 'sticky',
        bottom: { xs: 'calc(64px + env(safe-area-inset-bottom))', sm: 16 },
        zIndex: 2,
        mt: 3,
      }}
    >
      <Composer
        label={`Tell the agent about ${name}`}
        placeholder="What happened, or what to do about this"
        prompt={composerPrompt(meta, now)}
        onSend={toAgent}
        actions={actions}
        draft={draft}
        chips={
          <Chip
            size="small"
            label="Mark done"
            icon={done ? <CheckIcon /> : undefined}
            color={done ? 'primary' : 'default'}
            variant={done ? 'filled' : 'outlined'}
            aria-pressed={done}
            onClick={() => setDone((v) => !v)}
            data-composer-done
          />
        }
      />
    </Box>
  )
}
