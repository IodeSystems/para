import { useEffect, useRef, useState, type KeyboardEvent } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router'
import { useWindowVirtualizer } from '@tanstack/react-virtual'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Chip from '@mui/material/Chip'
import LinearProgress from '@mui/material/LinearProgress'
import Link from '@mui/material/Link'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import {
  useProposal,
  useProposalDone,
  useProposalSubmit,
  useProposals,
  useStack,
  stackReturn,
  useStackDecide,
  useStackLater,
  useStackUndo,
  useTags,
  type DoneBody,
  type StackFilter,
} from '@/api/proposals'
import { useQuestion, useQuestionReview, type QuestionReviewBody } from '@/api/questions'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { Empty, PageTitle, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { useToasts } from '@/components/ToastStack'
import { Composer } from '@/components/Composer'
import { uploadAttachment } from '@/client/upload'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { useStackKeys } from '@/components/Keyboard'
import { ItemCard } from '@/components/stack/ItemCard'
import { LaterButton } from '@/components/stack/LaterButton'
import { WaitingOnYou } from '@/components/stack/WaitingOnYou'
import { QuestionCard } from '@/components/stack/QuestionCard'
import { DoneForm, RefsField, ReviewStateChip, type ProposalData } from '@/components/stack/parts'
import { useDossierDraft } from '@/components/stack/useDossierDraft'
import { answerEmpty, stringMap, type Answer } from '@/lib/dossier'
import {
  appliedCount,
  canUndo,
  failText,
  nextFocus,
  toastText,
  undoAll,
  undoBodies,
  whyNoUndo,
  type Verb,
} from '@/lib/stackToast'
import { prefersReducedMotion } from '@/lib/toasts'
import type { StackKey } from '@/lib/keys'
import { noteLabel, noteProblem } from '@/lib/review'
import {
  canLater,
  canStackDecide,
  inView,
  kindWords,
  laterChip,
  nextItem,
  rowActions,
  rowKey,
  STACK_STATES,
  stackLines,
  stackTone,
  stackWords,
  topicLabel,
  type Line,
} from '@/lib/stack'

const KINDS = ['question', 'para', 'message', 'action', 'doc_edit', 'upstream'] as const

const text = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined)

// StackSearch is the stack's filter. view=sent was the Asked of agents tab: it is /requests now,
// and old links land there.
type StackSearch = StackFilter & { view?: 'sent' }

export const Route = createFileRoute('/_authed/stack')({
  beforeLoad: ({ search }) => {
    if (search.view === 'sent') throw redirect({ to: '/requests', replace: true })
  },
  validateSearch: (s: Record<string, unknown>): StackSearch => ({
    state: STACK_STATES.find((x) => x === s.state),
    kind: KINDS.find((x) => x === s.kind),
    group: text(s.group),
    proposal: text(s.proposal),
    actor: text(s.actor),
    tag: text(s.tag),
    view: s.view === 'sent' ? 'sent' : undefined,
  }),
  component: StackPage,
})

type Data = NonNullable<ReturnType<typeof useStack>['data']>
type Row = Data['pages'][number]['rows'][number]
type Submitted = { items: { item: string; state: string; undo?: string | null }[] }

// StackPage is the review stack: every item of every current dossier and every open vault
// question, by stack state. With a review:decide key the owner approves, declines, skips, asks an
// agent, or marks done by me, here. A row opens in place as the same card the dossier shows.
function StackPage() {
  const { view: _view, ...f } = Route.useSearch()
  const { session } = Route.useRouteContext()
  const decide = canDecide(session)
  // skipped are the rows this visit skipped. A skip is navigation, stored nowhere: the to-decide
  // view hides them until the owner opens the stack again. Kept in memory, so a reload is a new
  // visit.
  const [skipped, setSkipped] = useState<ReadonlySet<string>>(() => new Set())
  return (
    <>
      <PageTitle subtitle="What agents asked you to decide, by dossier and group, and the open questions in the vault. A row opens here as its card; what you answered leaves the list.">
        Stack
      </PageTitle>
      <StackView f={f} decide={decide} skipped={skipped} setSkipped={setSkipped} />
    </>
  )
}

type SkipProps = {
  skipped: ReadonlySet<string>
  setSkipped: (f: (s: ReadonlySet<string>) => ReadonlySet<string>) => void
}

function StackView({
  f,
  decide,
  skipped,
  setSkipped,
}: { f: StackFilter; decide: boolean } & SkipProps) {
  const q = useStack(f)
  const page = q.data?.pages[0]
  return (
    <>
      <Filters f={f} counts={page?.counts ?? []} needsYou={page?.needsYou ?? 0} />
      {!f.state && <WaitingOnYou items={page?.requests ?? []} />}
      <DataState q={q}>
        {(d) => <StackList d={d} decide={decide} q={q} skipped={skipped} setSkipped={setSkipped} />}
      </DataState>
    </>
  )
}

// Topics are the stack's saved views: one chip per tag in use, with what waits in it.
function Topics({ tag, onTag }: { tag?: string; onTag: (t: string | undefined) => void }) {
  const q = useTags()
  const tags = (q.data ?? []).filter((t) => t.stack + t.later > 0 || t.tag === tag)
  if (tags.length === 0) return null
  return (
    <Stack
      direction="row"
      spacing={1}
      useFlexGap
      sx={{ flexWrap: 'wrap', mb: 1.5 }}
      role="group"
      aria-label="Topics"
    >
      {tags.map((t) => {
        const on = tag === t.tag
        return (
          <Chip
            key={t.tag}
            data-topic={t.tag}
            label={topicLabel(t)}
            color={on ? 'secondary' : 'default'}
            variant={on ? 'filled' : 'outlined'}
            aria-pressed={on}
            onClick={() => onTag(on ? undefined : t.tag)}
          />
        )
      })}
    </Stack>
  )
}

function Filters({
  f,
  counts,
  needsYou,
}: {
  f: StackFilter
  counts: { state: string; count: number }[]
  needsYou: number
}) {
  const navigate = useNavigate({ from: '/stack' })
  const set = (p: Partial<StackFilter>) =>
    void navigate({ search: (prev) => ({ ...prev, ...p }), replace: true })
  const proposals = useProposals()
  return (
    <Box sx={{ mb: 2 }}>
      <Stack
        direction="row"
        spacing={1}
        useFlexGap
        sx={{ flexWrap: 'wrap', mb: 1.5 }}
        role="group"
        aria-label="Show by state"
      >
        <Chip
          data-state-chip="needs-you"
          label={`${needsYou} need you`}
          color={f.state ? 'default' : 'primary'}
          variant={f.state ? 'outlined' : 'filled'}
          aria-pressed={!f.state}
          onClick={() => set({ state: undefined })}
        />
        {STACK_STATES.map((s) => {
          const n = counts.find((c) => c.state === s)?.count ?? 0
          const on = f.state === s
          return (
            <Chip
              key={s}
              data-state-chip={s}
              label={`${n} ${stackWords(s)}`}
              color={on ? 'primary' : 'default'}
              variant={on ? 'filled' : 'outlined'}
              aria-pressed={on}
              onClick={() => set({ state: on ? undefined : s })}
            />
          )
        })}
      </Stack>
      <Topics tag={f.tag} onTag={(t) => set({ tag: t })} />
      <Stack direction={{ xs: 'column', sm: 'row' }} spacing={1}>
        <TextField
          select
          size="small"
          label="Filter by kind"
          value={f.kind ?? ''}
          onChange={(e) => set({ kind: e.target.value || undefined })}
          sx={{ minWidth: 160 }}
        >
          <MenuItem value="">every kind</MenuItem>
          {KINDS.map((k) => (
            <MenuItem key={k} value={k}>
              {kindWords(k)}
            </MenuItem>
          ))}
        </TextField>
        <TextField
          select
          size="small"
          label="Filter by dossier"
          value={f.proposal ?? ''}
          onChange={(e) => set({ proposal: e.target.value || undefined })}
          sx={{ minWidth: 200 }}
        >
          <MenuItem value="">every dossier</MenuItem>
          {(proposals.data ?? [])
            .filter((p) => p.status === 'open' || p.id === f.proposal)
            .map((p) => (
              <MenuItem key={p.id} value={p.id}>
                {p.title || p.id}
              </MenuItem>
            ))}
        </TextField>
        <FilterText label="Filter by group" value={f.group} onSet={(v) => set({ group: v })} />
        <FilterText label="Filter by agent" value={f.actor} onSet={(v) => set({ actor: v })} />
      </Stack>
    </Box>
  )
}

// FilterText applies its text when the field loses focus or on Enter, not on each key.
function FilterText({
  label,
  value,
  onSet,
}: {
  label: string
  value?: string
  onSet: (v: string | undefined) => void
}) {
  return (
    <TextField
      key={value ?? ''}
      size="small"
      label={label}
      defaultValue={value ?? ''}
      onBlur={(e) => onSet(e.target.value.trim() || undefined)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
      }}
    />
  )
}

// Act is a row action that needs a form: a note, refs, or where the result is.
type Act = 'decline' | 'skip' | 'ask' | 'done'

const ACT_WORDS: Record<Act, string> = {
  decline: 'Decline',
  skip: 'Skip',
  ask: 'Send to agent',
  done: 'Done by me',
}

// LEAVE_MS is how long a card fades before the list closes the gap.
const LEAVE_MS = 180

type Applied = { undo?: string | null }[]

function StackList({
  d,
  decide,
  q,
  skipped,
  setSkipped,
}: {
  d: Data
  decide: boolean
  q: ReturnType<typeof useStack>
} & SkipProps) {
  const act = useStackDecide()
  const review = useQuestionReview()
  const later = useStackLater()
  const proposalDone = useProposalDone()
  const undoCall = useStackUndo()
  const toasts = useToasts()
  const qc = useQueryClient()
  const f = Route.useSearch()
  const [leaving, setLeaving] = useState<Set<string>>(() => new Set())
  // gone are the rows the owner acted on: they stay out of the to-decide view at once, without waiting
  // for the refetch, and come back on Undo or when the request fails. A state filter other than
  // "all open" and "to decide" shows them where they now belong.
  const [gone, setGone] = useState<Set<string>>(() => new Set())
  const [sliding, setSliding] = useState(false)
  const [focusKey, setFocusKey] = useState<string | null>(null)
  const [pending, setPending] = useState(0)
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  const hides = !f.state || f.state === 'proposed'
  const allRows = inView(
    d.pages.flatMap((p) => p.rows),
    skipped,
    rowKey,
    !f.state,
  )
  const rows = hides ? allRows.filter((r) => !gone.has(rowKey(r))) : allRows
  const groups = d.pages[0]?.groups ?? []
  const total = Math.max(0, (d.pages[0]?.total ?? allRows.length) - (hides ? gone.size : 0))
  const lines = stackLines(rows, groups)
  const [cursor, setCursor] = useState<string | null>(null)
  const [picked, setPicked] = useState<Set<string>>(() => new Set())
  // form is the row whose action form is open, and the action. A row key of '' is the selection.
  const [form, setForm] = useState<{ key: string; act: Act } | null>(null)
  // opened is the row shown in place as its card.
  const [opened, setOpened] = useState<string | null>(null)
  const byKey = new Map(allRows.map((r) => [rowKey(r), r]))
  const v = useWindowVirtualizer({
    count: lines.length,
    estimateSize: (i) => (lines[i]?.type === 'item' ? 76 : 48),
    overscan: 8,
  })
  const items = v.getVirtualItems()
  const last = items[items.length - 1]?.index ?? 0
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = q
  useEffect(() => {
    if (last >= lines.length - 5 && hasNextPage && !isFetchingNextPage) void fetchNextPage()
  }, [last, lines.length, hasNextPage, isFetchingNextPage, fetchNextPage])
  const timerMap = timers.current
  useEffect(() => () => timerMap.forEach(clearTimeout), [timerMap])

  // Focus and scroll go to the card that took the place of the one that left.
  useEffect(() => {
    if (!focusKey) return
    const at = lines.findIndex((l) => l.key === focusKey)
    if (at >= 0) v.scrollToIndex(at, { align: 'auto' })
    const id = requestAnimationFrame(() => {
      document
        .querySelector<HTMLElement>(`[data-row="${CSS.escape(focusKey)}"]`)
        ?.focus({ preventScroll: true })
      setFocusKey(null)
    })
    return () => cancelAnimationFrame(id)
  })

  // leave takes rows out of the view: they fade, then the list slides up, and the next card takes
  // the cursor and focus.
  const leave = (keys: string[]) => {
    const order = lines.flatMap((l) => (l.type === 'item' ? [l.key] : []))
    const next = nextFocus(order, new Set(keys))
    const quick = prefersReducedMotion()
    setSliding(!quick)
    setLeaving((s) => new Set([...s, ...keys]))
    const t = setTimeout(
      () => {
        setGone((g) => new Set([...g, ...keys]))
        setLeaving((s) => new Set([...s].filter((k) => !keys.includes(k))))
        setCursor(next)
        setFocusKey(next)
        setTimeout(() => setSliding(false), 400)
      },
      quick ? 0 : LEAVE_MS,
    )
    for (const k of keys) timerMap.set(k, t)
  }
  // restore brings rows back: the request failed, or the owner undid it.
  const restore = (keys: string[]) => {
    for (const k of keys) {
      const t = timerMap.get(k)
      if (t) clearTimeout(t)
      timerMap.delete(k)
    }
    setLeaving((s) => new Set([...s].filter((k) => !keys.includes(k))))
    setGone((g) => new Set([...g].filter((k) => !keys.includes(k))))
    setSliding(!prefersReducedMotion())
    setCursor(keys[0] ?? null)
  }

  // perform does one action on rows: the cards leave at once, the request runs, and a toast says
  // what was done, with Undo when the server can take it back. A failure brings the cards back.
  const perform = async (
    verb: Verb,
    list: Row[],
    run: () => Promise<{ applied: Applied; undo?: () => Promise<unknown>; failed?: boolean }>,
  ) => {
    const keys = list.map(rowKey)
    const titles = list.map((r) => r.summary)
    leave(keys)
    setPending((n) => n + 1)
    try {
      const res = await run()
      if (res.failed) {
        restore(keys)
        toasts.push({ text: toastText(verb, titles) + ' failed', tone: 'error' })
        return
      }
      toasts.push({
        text: toastText(verb, titles, appliedCount(res.applied)),
        undo:
          res.undo && canUndo(res.applied)
            ? async () => {
                await res.undo?.()
                restore(keys)
              }
            : undefined,
        note: res.undo && canUndo(res.applied) ? undefined : whyNoUndo(verb),
      })
    } catch (e) {
      restore(keys)
      toasts.push({ text: failText(verb, titles, errorText(e)), tone: 'error' })
    } finally {
      setPending((n) => n - 1)
    }
  }

  const approveRows = (list: Row[]) => {
    const ok = list.filter(canStackDecide)
    if (!decide || ok.length === 0) return
    setPicked(new Set())
    void perform('approve', ok, async () => {
      const res = await act.mutateAsync({
        items: ok.map((r) => ({ proposal: r.proposal, item: r.item })),
        decision: 'approve',
      })
      const applied = res.proposals.flatMap((p) => p.items)
      return {
        applied,
        failed: applied.some((i) => i.state === 'failed'),
        undo: () =>
          undoAll(
            undoBodies(
              ok.map((r) => ({ proposal: r.proposal, item: r.item })),
              [],
            ),
            (b) => undoCall.mutateAsync(b),
          ),
      }
    })
  }
  // skipRows moves past rows for this visit: nothing is sent or stored. Undo brings them back.
  const skipRows = (list: Row[]) => {
    const ok = list.filter((r) => rowActions(r).skip)
    if (ok.length === 0) return
    const keys = ok.map(rowKey)
    setForm(null)
    setPicked(new Set())
    leave(keys)
    setSkipped((s) => new Set([...s, ...keys]))
    toasts.push({
      text: toastText(
        'skip',
        ok.map((r) => r.summary),
      ),
      undo: () => {
        setSkipped((s) => new Set([...s].filter((k) => !keys.includes(k))))
        restore(keys)
        return Promise.resolve()
      },
    })
  }
  // decideRows declines or sends to an agent rows with the owner's note. Vault questions go through
  // question_review, one at a time; dossier items in one stack_decide.
  const decideRows = (
    list: Row[],
    a: Exclude<Act, 'done' | 'skip'>,
    note: string,
    refs: string[],
  ) => {
    if (!decide) return
    const ok = list.filter((r) => rowActions(r)[a])
    if (ok.length === 0) return
    const items = ok.filter((r) => !r.question)
    const questions = ok.filter((r) => r.question)
    const decision = a === 'decline' ? 'reject' : a
    setForm(null)
    setPicked(new Set())
    void perform(a, ok, async () => {
      const applied: Applied = []
      let requests: (string | null | undefined)[] = []
      const asked: { id: string; request?: string | null }[] = []
      if (items.length > 0) {
        const res = await act.mutateAsync({
          items: items.map((r) => ({ proposal: r.proposal, item: r.item })),
          decision,
          comment: note || undefined,
          refs: a === 'ask' && refs.length ? refs : undefined,
        })
        applied.push(...res.proposals.flatMap((p) => p.items))
        requests = res.requests ?? []
      }
      for (const r of questions) {
        const res = await review.mutateAsync({
          id: r.question ?? r.item,
          decision: a,
          note: note || undefined,
          refs: a === 'ask' && refs.length ? refs : undefined,
        })
        applied.push({ undo: res.undo })
        asked.push({ id: r.question ?? r.item, request: res.request })
      }
      return {
        applied,
        undo: () =>
          undoAll(
            undoBodies(
              items.map((r) => ({ proposal: r.proposal, item: r.item })),
              asked,
              requests,
            ),
            (b) => undoCall.mutateAsync(b),
          ),
      }
    })
  }
  // laterRows puts rows off until a date. Undo in the toast brings them back now.
  const laterRows = (list: Row[], until: string) => {
    if (!decide) return
    const ok = list.filter(canLater)
    if (ok.length === 0) return
    setPicked(new Set())
    const items = ok.filter((r) => !r.question).map((r) => ({ proposal: r.proposal, item: r.item }))
    const questions = ok.flatMap((r) => (r.question ? [r.question] : []))
    const body = {
      items: items.length ? items : undefined,
      questions: questions.length ? questions : undefined,
    }
    void perform('later', ok, async () => {
      const res = await later.mutateAsync({ ...body, until })
      return {
        applied: res.changed.map(() => ({ undo: 'reset' })),
        failed: res.changed.length === 0,
        undo: () => stackReturn(body),
      }
    })
  }
  // bringRows brings put-off rows back now (stack_return). They stay where they are, now to
  // decide; Undo in the toast puts them off again, to the same date.
  const bringRows = (list: Row[]) => {
    const ok = list.filter((r) => r.stack === 'later')
    if (!decide || ok.length === 0) return
    const body = {
      items: ok.filter((r) => !r.question).map((r) => ({ proposal: r.proposal, item: r.item })),
      questions: ok.flatMap((r) => (r.question ? [r.question] : [])),
    }
    const call = {
      items: body.items.length ? body.items : undefined,
      questions: body.questions.length ? body.questions : undefined,
    }
    const titles = ok.map((r) => r.summary)
    setPending((n) => n + 1)
    void stackReturn(call)
      .then((res) => {
        void qc.invalidateQueries()
        if (res.changed.length === 0) {
          toasts.push({ text: failText('return', titles, 'nothing changed'), tone: 'error' })
          return
        }
        toasts.push({
          text: toastText('return', titles),
          undo: async () => {
            const until = ok[0]?.deferUntil
            await later.mutateAsync({
              ...call,
              until: until && until !== 'someday' ? until : undefined,
            })
          },
        })
      })
      .catch((e: unknown) =>
        toasts.push({ text: failText('return', titles, errorText(e)), tone: 'error' }),
      )
      .finally(() => setPending((n) => n - 1))
  }
  // markDone is Done by me on a row. It takes effect at once and cannot be undone.
  const markDone = (r: Row, body: Parameters<typeof proposalDone.mutate>[0]) => {
    setForm(null)
    void perform('done', [r], async () => {
      await proposalDone.mutateAsync(body)
      return { applied: [] }
    })
  }
  // answerQuestion records the owner's answer to a vault question. It cannot be undone here.
  const answerQuestion = (r: Row, body: Parameters<typeof review.mutate>[0]) => {
    setOpened(null)
    void perform('answer', [r], async () => {
      await review.mutateAsync(body)
      return { applied: [] }
    })
  }
  // submitted is a dossier submit from a row card: the items it decided leave the view.
  const submitted = (
    r: Row,
    res: { items: { item: string; state: string; undo?: string | null }[] },
  ) => {
    const left = allRows.filter(
      (x) =>
        x.proposal === r.proposal &&
        res.items.some((i) => i.item === x.item && i.state !== 'pending'),
    )
    if (left.length === 0) return
    const keys = left.map(rowKey)
    setOpened(null)
    leave(keys)
    toasts.push({
      text: toastText(
        'submit',
        left.map((x) => x.summary),
        appliedCount(res.items),
      ),
      undo: canUndo(res.items)
        ? async () => {
            await undoCall.mutateAsync({
              items: left.map((x) => ({ proposal: x.proposal, item: x.item })),
            })
            restore(keys)
          }
        : undefined,
      note: canUndo(res.items) ? undefined : whyNoUndo('submit'),
    })
  }
  const approveGroup = (proposal: string, group: string) => {
    if (!decide) return
    const list = allRows.filter(
      (r) =>
        r.proposal === proposal && r.group === group && canStackDecide(r) && !gone.has(rowKey(r)),
    )
    void perform('approve', list, async () => {
      const res = await act.mutateAsync({ proposal, group, decision: 'approve' })
      const applied = res.proposals.flatMap((p) => p.items)
      return {
        applied,
        failed: applied.some((i) => i.state === 'failed'),
        undo: () =>
          undoCall.mutateAsync({
            items: res.proposals.flatMap((p) =>
              p.items.map((i) => ({ proposal: p.id, item: i.item })),
            ),
          }),
      }
    })
  }
  const toggleOpen = (r: Row, only?: boolean) =>
    setOpened((o) => (o === rowKey(r) && !only ? null : rowKey(r)))
  const move = (step: 1 | -1) => {
    const to = nextItem(lines, cursor, step)
    setCursor(to)
    const at = lines.findIndex((l) => l.key === to)
    if (at >= 0) v.scrollToIndex(at, { align: 'auto' })
  }
  // openForm opens an action form on the selection, or else on the row under the cursor.
  const openForm = (a: Act, row: Row | undefined) => {
    if (!decide) return
    // A skip needs no words: it moves on at once.
    if (a === 'skip') {
      const chosen =
        picked.size > 0 ? [...picked].flatMap((x) => byKey.get(x) ?? []) : row ? [row] : []
      return skipRows(chosen)
    }
    if (a !== 'done' && picked.size > 0) return setForm({ key: '', act: a })
    if (row && rowActions(row)[a]) setForm({ key: rowKey(row), act: a })
  }

  useStackKeys((k: StackKey) => {
    const row = cursor ? byKey.get(cursor) : undefined
    const chosen =
      picked.size > 0 ? [...picked].flatMap((x) => byKey.get(x) ?? []) : row ? [row] : []
    switch (k) {
      case 'down':
        return move(1)
      case 'up':
        return move(-1)
      case 'approve':
        if (row?.kind === 'question' && picked.size === 0) return toggleOpen(row, true)
        return approveRows(chosen)
      case 'decline':
      case 'skip':
      case 'ask':
      case 'done':
        return openForm(k, row)
      case 'edit':
      case 'comment':
        return row ? toggleOpen(row, true) : undefined
      case 'open':
        return row ? toggleOpen(row) : undefined
      case 'select':
        if (!row || !decide) return
        return setPicked((p) => {
          const n = new Set(p)
          if (n.has(rowKey(row))) n.delete(rowKey(row))
          else n.add(rowKey(row))
          return n
        })
      case 'approveGroup':
        if (row?.group) approveGroup(row.proposal, row.group)
        return
    }
  })

  if (lines.length === 0)
    return (
      <Empty>
        {f.state
          ? 'Nothing here.'
          : 'Nothing needs you. What you answered, sent to an agent, or put off is under the state chips.'}
      </Empty>
    )
  const pickedRows = [...picked].flatMap((x) => byKey.get(x) ?? [])
  const busy = pending > 0
  return (
    <>
      <Stack direction="row" spacing={1} sx={{ mb: 1, alignItems: 'center', flexWrap: 'wrap' }}>
        <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1 }}>
          {total} {total === 1 ? 'item' : 'items'}. Keys: j and k move, a approves, d declines, s
          skips, g asks an agent, m marks done by me, o opens the card, x selects, A approves the
          group. ? shows them all.
        </Typography>
        {decide && pickedRows.length > 0 && (
          <>
            <Button
              size="small"
              color="success"
              variant="contained"
              disabled={busy}
              onClick={() => approveRows(pickedRows)}
            >
              Approve {pickedRows.length} selected
            </Button>
            <Button
              size="small"
              color="error"
              disabled={busy}
              onClick={() => openForm('decline', undefined)}
            >
              Decline selected
            </Button>
            <Button size="small" disabled={busy} onClick={() => openForm('skip', undefined)}>
              Skip selected
            </Button>
            <LaterButton disabled={busy} onLater={(u) => laterRows(pickedRows, u)} />
            <Button size="small" disabled={busy} onClick={() => openForm('ask', undefined)}>
              Send to agent about selected
            </Button>
          </>
        )}
      </Stack>
      {form?.key === '' && form.act !== 'done' && (
        <NoteForm
          act={form.act}
          count={pickedRows.length}
          busy={busy}
          attachTo={pickedRows[0] ? (pickedRows[0].question ?? pickedRows[0].proposal) : undefined}
          onCancel={() => setForm(null)}
          onConfirm={(note, refs) => {
            const a = form.act
            if (a !== 'skip' && a !== 'done') decideRows(pickedRows, a, note, refs)
          }}
        />
      )}
      <Box
        role="list"
        aria-label="Review stack"
        sx={{ position: 'relative', height: v.getTotalSize() }}
      >
        {items.map((vi) => {
          const line = lines[vi.index]
          if (!line) return null
          return (
            <Box
              key={line.key}
              data-index={vi.index}
              ref={v.measureElement}
              role="listitem"
              sx={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                transform: `translateY(${vi.start - v.options.scrollMargin}px)`,
                // The rows below slide up when a card leaves; scrolling and first paint do not animate.
                transition: sliding ? 'transform 220ms ease, opacity 180ms ease' : undefined,
                opacity: leaving.has(line.key) ? 0 : 1,
                pointerEvents: leaving.has(line.key) ? 'none' : undefined,
                '@media (prefers-reduced-motion: reduce)': { transition: 'none' },
              }}
            >
              <LineView
                line={line}
                decide={decide}
                busy={busy}
                cursor={cursor}
                picked={picked}
                form={form && form.key === line.key ? form.act : null}
                opened={opened === line.key}
                onPick={(r) => {
                  setCursor(rowKey(r))
                  setPicked((p) => {
                    const n = new Set(p)
                    if (n.has(rowKey(r))) n.delete(rowKey(r))
                    else n.add(rowKey(r))
                    return n
                  })
                }}
                onApprove={(r) => approveRows([r])}
                onForm={(r, a) => {
                  setCursor(rowKey(r))
                  if (a === 'skip') return skipRows([r])
                  setForm(a ? { key: rowKey(r), act: a } : null)
                }}
                onDecide={(r, a, note, refs) => a !== 'skip' && decideRows([r], a, note, refs)}
                onLater={(r, until) => laterRows([r], until)}
                onBring={(r) => bringRows([r])}
                onOpen={(r) => {
                  setCursor(rowKey(r))
                  toggleOpen(r)
                }}
                onGroup={approveGroup}
                onFocusRow={(r) => setCursor(rowKey(r))}
                onDoneBy={markDone}
                onAnswer={answerQuestion}
                onSubmitted={submitted}
              />
            </Box>
          )
        })}
      </Box>
      {q.isFetchingNextPage && <LinearProgress aria-label="Loading more" sx={{ mt: 1 }} />}
    </>
  )
}

function LineView({
  line,
  decide,
  busy,
  cursor,
  picked,
  form,
  opened,
  onPick,
  onApprove,
  onForm,
  onDecide,
  onLater,
  onBring,
  onOpen,
  onGroup,
  onFocusRow,
  onDoneBy,
  onAnswer,
  onSubmitted,
}: {
  line: Line<Row>
  decide: boolean
  busy: boolean
  cursor: string | null
  picked: Set<string>
  form: Act | null
  opened: boolean
  onPick: (r: Row) => void
  onApprove: (r: Row) => void
  onForm: (r: Row, a: Act | null) => void
  onDecide: (r: Row, a: Exclude<Act, 'done'>, note: string, refs: string[]) => void
  onLater: (r: Row, until: string) => void
  onBring: (r: Row) => void
  onOpen: (r: Row) => void
  onGroup: (proposal: string, group: string) => void
  onFocusRow: (r: Row) => void
  onDoneBy: (r: Row, body: DoneBody) => void
  onAnswer: (r: Row, body: QuestionReviewBody) => void
  onSubmitted: (r: Row, res: Submitted) => void
}) {
  if (line.type === 'dossier') {
    return (
      <Box
        sx={{ pt: 2, pb: 0.5, borderBottom: 1, borderColor: 'divider' }}
        data-dossier={line.proposal}
      >
        <Typography variant="subtitle1" component="h2">
          {line.proposal ? (
            <TextLink to="/proposals/$id" params={{ id: line.proposal }} underline="hover">
              {line.title || line.proposal}
            </TextLink>
          ) : (
            line.title
          )}{' '}
          <Typography component="span" variant="body2" color="text.secondary">
            {line.count} {line.count === 1 ? 'item' : 'items'}
          </Typography>
        </Typography>
      </Box>
    )
  }
  if (line.type === 'group') {
    const pct = line.total ? Math.round((100 * line.done) / line.total) : 0
    return (
      <Stack
        direction="row"
        spacing={1.5}
        sx={{ pl: 1, py: 1, alignItems: 'center' }}
        data-group={line.group}
      >
        <Typography variant="body2" sx={{ minWidth: 120, overflowWrap: 'anywhere' }}>
          Group {line.group}
        </Typography>
        <LinearProgress
          variant="determinate"
          value={pct}
          aria-label={`Group ${line.group}: ${line.done} of ${line.total} done`}
          sx={{ flexGrow: 1, maxWidth: 240 }}
        />
        <Typography variant="caption" color="text.secondary">
          {line.done} of {line.total} done
        </Typography>
        {decide && line.proposed > 0 && (
          <Button
            size="small"
            variant="outlined"
            color="success"
            disabled={busy}
            onClick={() => onGroup(line.proposal, line.group)}
          >
            Approve group ({line.proposed})
          </Button>
        )}
      </Stack>
    )
  }
  const r = line.row
  const key = rowKey(r)
  const here = cursor === key
  const can = decide ? rowActions(r) : null
  const actButton = (a: Act, color?: 'error') =>
    can?.[a] && (
      <Button
        size="small"
        color={color}
        data-act={a}
        variant={form === a ? 'contained' : 'text'}
        aria-pressed={form === a}
        disabled={busy}
        onClick={() => onForm(r, form === a ? null : a)}
      >
        {ACT_WORDS[a]}
      </Button>
    )
  return (
    <Box
      data-row={key}
      data-stack={r.stack}
      data-state={r.state}
      data-kind={r.kind}
      data-question={r.question || undefined}
      aria-current={here || undefined}
      tabIndex={-1}
      onClick={() => onFocusRow(r)}
      sx={{
        outline: 'none',
        pl: r.group ? 3 : 1,
        pr: 1,
        py: 0.75,
        borderLeft: 3,
        borderColor: here ? 'primary.main' : 'transparent',
        bgcolor: here ? 'action.selected' : undefined,
      }}
    >
      <Stack
        direction="row"
        spacing={1}
        sx={{ alignItems: 'center', flexWrap: { xs: 'wrap', sm: 'nowrap' } }}
      >
        {decide && canStackDecide(r) && (
          <Checkbox
            size="small"
            checked={picked.has(key)}
            onChange={() => onPick(r)}
            slotProps={{ input: { 'aria-label': `Select ${r.summary}` } }}
          />
        )}
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
            {r.question ? (
              <DocLink id={r.question}>{r.summary}</DocLink>
            ) : (
              <TextLink
                to="/proposals/$id"
                params={{ id: r.proposal }}
                hash={`item-${r.item}`}
                underline="hover"
              >
                {r.summary}
              </TextLink>
            )}
          </Typography>
          <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.25 }}>
            <StatusChip
              tone="quiet"
              label={r.question ? 'question in the vault' : kindWords(r.kind)}
            />
            <StatusChip tone={stackTone(r.stack)} label={stackWords(r.stack)} />
            {r.state === 'skipped' || r.state === 'sent_back' ? (
              <ReviewStateChip state={r.state} />
            ) : (
              r.state !== 'pending' && <StateChip state={r.state} />
            )}
            {r.drafted && <StatusChip tone="wait" label="drafted" />}
            {r.proposed && <StatusChip tone="wait" label="an agent proposed an answer" />}
            {r.bundle && <StatusChip tone="quiet" label={`bundle ${r.bundle}`} />}
            {r.deferUntil && r.stack === 'later' && (
              <StatusChip tone="wait" label={laterChip(r.deferUntil)} />
            )}
            {(r.tags ?? []).map((t) => (
              <StatusChip key={t} tone="quiet" label={`#${t}`} />
            ))}
            {r.actor && (
              <Typography variant="caption" color="text.secondary">
                by {r.actor}
                {r.claimedBy ? `, ${r.claimedBy} has it` : ''}
              </Typography>
            )}
            {r.url && (
              <Link variant="caption" href={r.url} target="_blank" rel="noreferrer noopener">
                result
              </Link>
            )}
            {r.result && r.stack === 'done' && !r.url && (
              <Typography variant="caption">
                <DocLink id={r.result}>made {r.result}</DocLink>
              </Typography>
            )}
            {r.receipt && (
              <Typography variant="caption" color="text.secondary">
                receipt {r.receipt}
              </Typography>
            )}
            {r.reason && (
              <Typography
                variant="caption"
                color={r.stack === 'failed' ? 'error' : 'text.secondary'}
                sx={{ overflowWrap: 'anywhere' }}
                data-reason
              >
                {r.state === 'sent_back' ? 'You asked: ' : ''}
                {r.reason}
              </Typography>
            )}
            {(r.refs ?? []).length > 0 && (
              <Typography variant="caption" color="text.secondary">
                read: {(r.refs ?? []).join(', ')}
              </Typography>
            )}
            {r.request && (
              <Typography variant="caption">
                <DocLink id={r.request}>agent work</DocLink>
              </Typography>
            )}
          </Stack>
        </Box>
        <Stack
          direction="row"
          spacing={0.5}
          useFlexGap
          sx={{ flexWrap: 'wrap', justifyContent: 'flex-end' }}
        >
          {can?.approve && (
            <Button
              size="small"
              color="success"
              variant="outlined"
              data-act="approve"
              disabled={busy}
              onClick={() => onApprove(r)}
            >
              Approve
            </Button>
          )}
          {actButton('decline', 'error')}
          {actButton('skip')}
          {decide && canLater(r) && (
            <LaterButton disabled={busy} onLater={(until) => onLater(r, until)} />
          )}
          {actButton('ask')}
          {actButton('done')}
          {can?.bring && (
            <Button
              size="small"
              variant="outlined"
              data-act="bring"
              disabled={busy}
              onClick={() => onBring(r)}
            >
              Bring back
            </Button>
          )}
          <Button size="small" data-act="open" aria-expanded={opened} onClick={() => onOpen(r)}>
            {opened
              ? 'Close'
              : decide && r.kind === 'question' && r.stack === 'proposed'
                ? 'Answer'
                : 'Open'}
          </Button>
        </Stack>
      </Stack>
      {form && form !== 'done' && (
        <NoteForm
          act={form}
          busy={busy}
          attachTo={r.question ?? r.proposal}
          onCancel={() => onForm(r, null)}
          onConfirm={(note, refs) => onDecide(r, form, note, refs)}
        />
      )}
      {form === 'done' && (
        <RowDone r={r} onClose={() => onForm(r, null)} onDone={(body) => onDoneBy(r, body)} />
      )}
      {opened &&
        (r.question ? (
          <VaultQuestion r={r} decide={decide} onAnswer={(body) => onAnswer(r, body)} />
        ) : (
          <RowCard r={r} decide={decide} onSubmitted={(res) => onSubmitted(r, res)} />
        ))}
    </Box>
  )
}

// NoteForm is the note a row action takes: required to decline or ask, optional to skip. With
// Send to agent is the composer: the note, files it embeds, and documents for the agent to read.
// Enter confirms, Esc cancels.
function NoteForm({
  act,
  count,
  busy,
  attachTo,
  onCancel,
  onConfirm,
}: {
  act: Exclude<Act, 'done'>
  count?: number
  busy: boolean
  // attachTo is the item files attached to the note are stored under (a dossier or a question).
  attachTo?: string
  onCancel: () => void
  onConfirm: (note: string, refs: string[]) => void
}) {
  const [note, setNote] = useState('')
  const [refs, setRefs] = useState<string[]>([])
  const [tried, setTried] = useState(false)
  const toasts = useToasts()
  if (act === 'ask') {
    // Send to agent is the composer: the note, files the note embeds, and documents to read.
    const send = async (text: string, files: File[]) => {
      try {
        const embeds = []
        for (const f of files) {
          if (!attachTo) throw new Error('nothing to attach the file to')
          embeds.push((await uploadAttachment(attachTo, f)).embed)
        }
        onConfirm([text, ...embeds].filter(Boolean).join('\n\n'), refs)
        return true
      } catch (e) {
        toasts.push({ text: `Could not attach: ${errorText(e)}`, tone: 'error' })
        return false
      }
    }
    return (
      <Box
        data-note-form={act}
        role="group"
        aria-label={ACT_WORDS[act]}
        onClick={(e) => e.stopPropagation()}
        sx={{ my: 1, ml: 1 }}
      >
        <Composer
          label={noteLabel('ask')}
          placeholder={count ? `Note for the agent about ${count} selected` : 'Note for the agent'}
          autoFocus
          files={!!attachTo}
          onSend={busy ? () => Promise.resolve(false) : send}
          onCancel={onCancel}
          chips={<RefsField refs={refs} onRefs={setRefs} />}
        />
      </Box>
    )
  }
  const decision = act === 'decline' ? 'reject' : act
  const problem = noteProblem(decision, note)
  const confirm = () => {
    setTried(true)
    if (!problem) onConfirm(note.trim(), refs)
  }
  const keys = (e: KeyboardEvent) => {
    if (e.key === 'Escape') onCancel()
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      confirm()
    }
  }
  return (
    <Box
      data-note-form={act}
      role="group"
      aria-label={ACT_WORDS[act]}
      onClick={(e) => e.stopPropagation()}
      sx={{ my: 1, ml: 1, p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}
    >
      <TextField
        size="small"
        fullWidth
        multiline
        autoFocus
        label={noteLabel(decision)}
        value={note}
        error={tried && problem !== ''}
        helperText={problem || undefined}
        onChange={(e) => setNote(e.target.value)}
        onKeyDown={keys}
      />
      <Stack direction="row" spacing={1} sx={{ mt: 1, alignItems: 'center' }}>
        <Button
          size="small"
          variant="contained"
          color={act === 'decline' ? 'error' : 'primary'}
          disabled={busy || problem !== ''}
          onClick={confirm}
        >
          {ACT_WORDS[act]}
          {count ? ` ${count} selected` : ''}
        </Button>
        <Button size="small" onClick={onCancel}>
          Cancel
        </Button>
      </Stack>
    </Box>
  )
}

// RowDone is Done by me on a row: the owner did the item themself.
function RowDone({
  r,
  onClose,
  onDone,
}: {
  r: Row
  onClose: () => void
  onDone: (body: DoneBody) => void
}) {
  return (
    <Box onClick={(e) => e.stopPropagation()} sx={{ ml: 1 }}>
      <DoneForm
        start=""
        textLabel={
          r.kind === 'doc_edit'
            ? 'The document after your edit (optional)'
            : 'What you sent, if not the draft (optional)'
        }
        withNote
        busy={false}
        onCancel={onClose}
        onConfirm={(f) =>
          onDone({
            id: r.proposal,
            item: r.item,
            url: f.url.trim() || undefined,
            locator: f.locator.trim() || undefined,
            text: f.text.trim() ? f.text : undefined,
            note: f.note.trim() || undefined,
          })
        }
      />
    </Box>
  )
}

// RowCard shows a row in place as the card of its dossier item, the same card the dossier page
// shows. The owner's answer saves as a draft of the dossier; Submit submits the dossier's answers.
function RowCard({
  r,
  decide,
  onSubmitted,
}: {
  r: Row
  decide: boolean
  onSubmitted: (res: Submitted) => void
}) {
  const q = useProposal(r.proposal)
  return (
    <Box onClick={(e) => e.stopPropagation()} sx={{ ml: 1 }}>
      <DataState q={q}>
        {(d) => <DossierItem d={d} item={r.item} decide={decide} onSubmitted={onSubmitted} />}
      </DataState>
    </Box>
  )
}

function DossierItem({
  d,
  item,
  decide,
  onSubmitted,
}: {
  d: ProposalData
  item: string
  decide: boolean
  onSubmitted: (res: Submitted) => void
}) {
  const draft = useDossierDraft(d)
  const submit = useProposalSubmit()
  const [said, setSaid] = useState('')
  const it = d.items.find((x) => x.id === item)
  if (!it) return <Alert severity="warning">This dossier has no item {item} now.</Alert>
  const may = decide && it.stack === 'proposed'
  const drafted = d.items.filter(
    (x) => x.stack === 'proposed' && !answerEmpty(draft.answerOf(x.id)),
  ).length
  const problem = draft.problems.map((p) => p.problem).join(' ')
  const doSubmit = async () => {
    setSaid('')
    if (!(await draft.flush())) return
    submit.mutate(
      { id: d.proposal.id, revision: d.proposal.revision },
      {
        onSuccess: (x) => {
          setSaid(
            x.failed?.length ? `Submitted. These failed: ${x.failed.join(', ')}.` : 'Submitted.',
          )
          onSubmitted(x)
        },
      },
    )
  }
  return (
    <>
      <ItemCard
        it={it}
        answer={draft.answerOf(it.id)}
        onAnswer={may ? (a) => draft.setAnswer(it.id, a) : undefined}
        stale={!!it.answer?.stale && !draft.overridden(it.id)}
        proposal={decide ? d.proposal.id : undefined}
      />
      {(said || submit.isError || draft.error) && (
        <Alert severity={submit.isError || draft.error ? 'error' : 'success'} sx={{ mb: 1 }}>
          {submit.isError ? errorText(submit.error) : draft.error || said}
        </Alert>
      )}
      {may && (
        <Stack
          direction="row"
          spacing={1}
          useFlexGap
          sx={{ mb: 1, alignItems: 'center', flexWrap: 'wrap' }}
        >
          <Button
            size="small"
            variant="contained"
            disabled={
              submit.isPending || draft.state === 'saving' || drafted === 0 || problem !== ''
            }
            onClick={() => void doSubmit()}
          >
            Submit this dossier ({drafted} answered)
          </Button>
          <Typography variant="caption" color="text.secondary" data-save={draft.state}>
            {problem ||
              'Submit decides every answer you drafted in this dossier. Open the dossier to see them all.'}
          </Typography>
        </Stack>
      )}
    </>
  )
}

// VaultQuestion is an open vault question in place: the same question card the dossier shows. The
// owner picks or edits options, adds one, or answers in their own words, with an optional note;
// Answer records it at once. Decline, Skip, and Send to agent are on the row.
function VaultQuestion({
  r,
  decide,
  onAnswer,
}: {
  r: Row
  decide: boolean
  onAnswer: (body: QuestionReviewBody) => void
}) {
  const q = useQuestion(r.question ?? r.item)
  const [answer, setAnswer] = useState<Answer | null>(null)
  return (
    <Box
      onClick={(e) => e.stopPropagation()}
      sx={{ ml: 1, my: 1 }}
      data-vault-question={r.question}
    >
      <DataState q={q}>
        {({ question: x }) => {
          const choices = answer?.choices ?? []
          const own = (answer?.ownWords ?? '').trim()
          return (
            <Box sx={{ p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1 }}>
              {x.context && (
                <Typography variant="body2" sx={{ mb: 1, overflowWrap: 'anywhere' }}>
                  {x.context}
                </Typography>
              )}
              {x.hasProposal && x.proposedAnswer && (
                <Typography variant="body2" sx={{ mb: 1, overflowWrap: 'anywhere' }}>
                  {x.proposedBy || 'An agent'} proposed: {x.proposedChoice || x.proposedAnswer}
                </Typography>
              )}
              <QuestionCard
                it={{
                  options: x.options,
                  multi: false,
                  recommended: x.recommended,
                  optionDetails: x.optionDetails,
                }}
                answer={answer}
                onAnswer={decide ? setAnswer : undefined}
              />
              {decide && (
                <>
                  <TextField
                    size="small"
                    fullWidth
                    multiline
                    label="Note, kept as the reason (optional)"
                    value={answer?.comment ?? ''}
                    onChange={(e) => setAnswer({ ...answer, comment: e.target.value })}
                    sx={{ mt: 1 }}
                  />
                  <Button
                    size="small"
                    variant="contained"
                    data-act="answer"
                    sx={{ mt: 1 }}
                    disabled={choices.length === 0 && own === ''}
                    onClick={() => {
                      const edited = stringMap(answer?.editedOptions)
                      onAnswer({
                        id: x.id,
                        decision: 'answer',
                        choices,
                        addedOptions: answer?.addedOptions ?? undefined,
                        editedOptions: Object.keys(edited).length ? edited : undefined,
                        ownWords: own || undefined,
                        note: (answer?.comment ?? '').trim() || undefined,
                      })
                    }}
                  >
                    Answer
                  </Button>
                </>
              )}
            </Box>
          )
        }}
      </DataState>
    </Box>
  )
}
