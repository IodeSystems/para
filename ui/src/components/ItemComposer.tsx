import { useState } from 'react'
import { useNavigate, useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Box from '@mui/material/Box'
import { agentUnsend } from '@/api/composer'
import { useThreadPost } from '@/api/thread'
import { readNotes, saveNotes } from '@/api/notes'
import { localStamp } from '@/api/today'
import { canDecide } from '@/client/session'
import { uploadAttachment } from '@/client/upload'
import { errorText } from '@/client/gqlClient'
import { useQuestion, useQuestionReview } from '@/api/questions'
import { useItemActions } from '@/components/AgendaActions'
import { Composer, type ComposerAction } from '@/components/Composer'
import { useItemDraft } from '@/components/ItemDraft'
import { useToasts } from '@/components/ToastStack'
import { useThemeReview } from '@/components/ThemeReview'
import { investigable, isClosed, rowKind } from '@/lib/agenda'
import {
  answerState,
  composerPrompt,
  notable,
  notesWithEntry,
  openQuestion,
  type ItemMeta,
} from '@/lib/composer'
import { requestEditTurn, TURN_UNDO_MS } from '@/lib/threadEdit'

// ItemComposer is the composer at the foot of every item's page. Send to agent posts a turn on the
// item's thread (thread_post) with a kind, the owner's words and files; the agent formats them into the item's notes and
// updates what they touch. Save as notes writes them into the notes now, below what is there. Done,
// the button near the box, closes the item and adds the box's words as a note. Investigate adds them to the notes and asks an agent to fill in
// details, check whether it is already done, and recheck its assumptions. It needs review:decide:
// without it the page only reads. onSent runs after a send to an agent, or a save that closed the
// item: the item left the owner's hands. Undo of that send runs onBack.

export function ItemComposer({
  id,
  title,
  meta,
  onSent,
  onBack,
  stickyOnPhone = true,
}: {
  id: string
  title: string
  meta: ItemMeta
  onSent?: () => void
  onBack?: () => void
  // stickyOnPhone false leaves the composer in the page flow on a phone, so a thread above it is not
  // covered by it (the Next card).
  stickyOnPhone?: boolean
}) {
  const { session } = useRouteContext({ from: '/_authed' })
  const toasts = useToasts()
  const navigate = useNavigate()
  const qc = useQueryClient()
  const send = useThreadPost()
  const draft = useItemDraft(id)
  const kind = rowKind(meta.type, meta.kind)
  const closed = isClosed(meta.status)
  const item = useItemActions({ id, title, kind }, closed, false, (gone) =>
    gone ? onSent?.() : onBack?.(),
  )
  // An open question is answered from its box: a picked choice, or the words.
  const asking = openQuestion(meta)
  const qn = useQuestion(id, asking).data?.question
  const review = useQuestionReview()
  const themeReview = useThemeReview()
  const [choice, setChoice] = useState('')
  const options = asking ? (qn?.options ?? []) : []
  // now is when the page opened: the prompt does not change while the owner types.
  const [now] = useState(() => new Date())
  if (!canDecide(session)) return null
  const name = title || id

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
      const res = await send.mutateAsync({
        id,
        text,
        attachments: atts.length ? atts.map((a) => a.path) : undefined,
      })
      toasts.push({
        text: `Sent to agent: ${name}`,
        note: res.note ?? undefined,
        target: { id },
        // Edit and Undo while the turn is queued: the toast stays about ten seconds.
        dwell: TURN_UNDO_MS,
        edit: () => {
          void navigate({ to: '/docs/$id', params: { id } })
          requestEditTurn(res.request)
        },
        undo: async () => {
          await agentUnsend(res.request)
          onBack?.()
          await qc.invalidateQueries()
        },
      })
      onSent?.()
      return true
    } catch (e) {
      return fail('send', e)
    }
  }

  const asNotes = async (text: string, files: File[]) => {
    try {
      const atts = await upload(files)
      const before = await readNotes(id)
      const md = notesWithEntry(
        before.md,
        localStamp(),
        text,
        atts.map((a) => a.embed),
      )
      const saved = await saveNotes(id, before.hash, md)
      await qc.invalidateQueries()
      toasts.push({
        text: `Saved as notes: ${name}`,
        target: { id },
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

  const answer = async (text: string) => {
    try {
      await review.mutateAsync(
        choice
          ? { id, decision: 'answer', choices: [choice], note: text || undefined }
          : { id, decision: 'answer', ownWords: text },
      )
      toasts.push({
        text: `Answered: ${name}`,
        note: 'An answer cannot be undone here.',
        target: { id },
      })
      setChoice('')
      await qc.invalidateQueries()
      onSent?.()
      // What waited on the question may be moot now: the owner reviews it (only drops and moves).
      void themeReview({ theme: id, title: name, end: { kind: 'closed' } }).catch(() => false)
      return true
    } catch (e) {
      return fail('answer', e)
    }
  }
  const decline = async (text: string) => {
    try {
      await review.mutateAsync({ id, decision: 'decline', note: text })
      toasts.push({ text: `Declined: ${name}`, target: { id } })
      await qc.invalidateQueries()
      onSent?.()
      return true
    } catch (e) {
      return fail('decline', e)
    }
  }

  const can = answerState(draft?.text ?? '', choice)
  const actions: ComposerAction[] = notable(meta)
    ? [
        {
          label: 'Save as notes',
          hint: 'Add to the notes now, below what is there',
          run: asNotes,
        },
      ]
    : []
  if (asking)
    actions.unshift(
      {
        label: 'Answer',
        hint: can.answer ? 'Answer the question with this' : 'Write the answer, or pick a choice',
        disabled: !can.answer,
        run: answer,
      },
      {
        label: 'Decline',
        hint: can.decline ? 'Decline the question; the words are the reason' : 'Write why first',
        disabled: !can.decline,
        run: decline,
      },
    )
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
        position: { xs: stickyOnPhone ? 'sticky' : 'static', sm: 'sticky' },
        // On a phone it sits just above the bottom bar, clear of the home indicator.
        bottom: { xs: 'calc(72px + env(safe-area-inset-bottom))', sm: 16 },
        zIndex: 2,
        mt: 3,
        // Hidden while a Done-with-words popover covers it (AgendaActions).
        'body[data-composer-covered] &': { visibility: 'hidden' },
      }}
    >
      <Composer
        label={`Tell the agent about ${name}`}
        placeholder="What happened, or what to do about this"
        prompt={composerPrompt(meta, now)}
        onSend={toAgent}
        actions={actions}
        draft={draft}
        actionsReady={asking && choice !== ''}
        choicesTitle="Tell the agent"
        choices={[
          {
            key: 'answer',
            title: 'Answer',
            options: options.map((o) => ({ value: o, label: o })),
            value: choice,
            onChange: setChoice,
            empty: 'Pick an answer, or write it in the box',
          },
        ]}
      />
    </Box>
  )
}
