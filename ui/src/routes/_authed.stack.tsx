import { useEffect, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
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
import { useProposals, useStack, useStackDecide, type StackFilter } from '@/api/proposals'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { Empty, PageTitle, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { LinkButton, TextLink } from '@/components/links'
import { useStackKeys } from '@/components/Keyboard'
import type { StackKey } from '@/lib/keys'
import {
  canStackDecide,
  kindWords,
  nextItem,
  rowKey,
  STACK_STATES,
  stackLines,
  stackTone,
  stackWords,
  type Line,
} from '@/lib/stack'

const KINDS = ['question', 'para', 'message', 'action', 'doc_edit', 'upstream'] as const

const text = (v: unknown) => (typeof v === 'string' && v !== '' ? v : undefined)

export const Route = createFileRoute('/_authed/stack')({
  validateSearch: (s: Record<string, unknown>): StackFilter => ({
    state: STACK_STATES.find((x) => x === s.state),
    kind: KINDS.find((x) => x === s.kind),
    group: text(s.group),
    proposal: text(s.proposal),
    actor: text(s.actor),
  }),
  component: StackPage,
})

type Data = NonNullable<ReturnType<typeof useStack>['data']>
type Row = Data['pages'][number]['rows'][number]

// StackPage is the review stack: every item of every current dossier, by stack state. With a
// review:decide key the owner approves or rejects items and groups here; a question opens in its
// dossier, where it is answered.
function StackPage() {
  const f = Route.useSearch()
  const { session } = Route.useRouteContext()
  const decide = canDecide(session)
  const q = useStack(f)
  return (
    <>
      <PageTitle subtitle="Every item agents asked you to decide, by dossier and group. Questions open in their dossier.">
        Stack
      </PageTitle>
      <Filters f={f} counts={q.data?.pages[0]?.counts ?? []} />
      <DataState q={q}>{(d) => <StackList d={d} decide={decide} q={q} />}</DataState>
    </>
  )
}

function Filters({ f, counts }: { f: StackFilter; counts: { state: string; count: number }[] }) {
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
          label="all open"
          color={f.state ? 'default' : 'primary'}
          variant={f.state ? 'outlined' : 'filled'}
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

function StackList({ d, decide, q }: { d: Data; decide: boolean; q: ReturnType<typeof useStack> }) {
  const navigate = useNavigate()
  const act = useStackDecide()
  const rows = d.pages.flatMap((p) => p.rows)
  const groups = d.pages[0]?.groups ?? []
  const total = d.pages[0]?.total ?? rows.length
  const lines = stackLines(rows, groups)
  const [cursor, setCursor] = useState<string | null>(null)
  const [picked, setPicked] = useState<Set<string>>(() => new Set())
  const byKey = new Map(rows.map((r) => [rowKey(r), r]))
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

  const decideRows = (list: Row[], decision: 'approve' | 'reject') => {
    const ok = list.filter(canStackDecide)
    if (!decide || ok.length === 0) return
    act.mutate(
      { items: ok.map((r) => ({ proposal: r.proposal, item: r.item })), decision },
      { onSuccess: () => setPicked(new Set()) },
    )
  }
  const approveGroup = (proposal: string, group: string) => {
    if (!decide) return
    act.mutate({ proposal, group, decision: 'approve' })
  }
  const openRow = (r: Row) =>
    void navigate({
      to: '/proposals/$id',
      params: { id: r.proposal },
      hash: `item-${r.item}`,
    })
  const move = (step: 1 | -1) => {
    const to = nextItem(lines, cursor, step)
    setCursor(to)
    const at = lines.findIndex((l) => l.key === to)
    if (at >= 0) v.scrollToIndex(at, { align: 'auto' })
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
        if (row?.kind === 'question' && picked.size === 0) return openRow(row)
        return decideRows(chosen, 'approve')
      case 'reject':
        return decideRows(chosen, 'reject')
      case 'edit':
      case 'comment':
      case 'open':
        return row ? openRow(row) : undefined
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

  if (lines.length === 0) return <Empty>Nothing here. No agent waits on you for this.</Empty>
  const pickedRows = [...picked].flatMap((x) => byKey.get(x) ?? [])
  return (
    <>
      {act.isError && (
        <Alert severity="error" sx={{ mb: 1 }}>
          {errorText(act.error)}
        </Alert>
      )}
      <Stack direction="row" spacing={1} sx={{ mb: 1, alignItems: 'center', flexWrap: 'wrap' }}>
        <Typography variant="body2" color="text.secondary" sx={{ flexGrow: 1 }}>
          {total} {total === 1 ? 'item' : 'items'}. Keys: j and k move, a approves, r rejects, x
          selects, A approves the group, o opens.
        </Typography>
        {decide && pickedRows.length > 0 && (
          <>
            <Button
              size="small"
              color="success"
              variant="contained"
              disabled={act.isPending}
              onClick={() => decideRows(pickedRows, 'approve')}
            >
              Approve {pickedRows.length} selected
            </Button>
            <Button
              size="small"
              color="error"
              disabled={act.isPending}
              onClick={() => decideRows(pickedRows, 'reject')}
            >
              Reject selected
            </Button>
          </>
        )}
      </Stack>
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
              }}
            >
              <LineView
                line={line}
                decide={decide}
                busy={act.isPending}
                cursor={cursor}
                picked={picked}
                onPick={(r) => {
                  setCursor(rowKey(r))
                  setPicked((p) => {
                    const n = new Set(p)
                    if (n.has(rowKey(r))) n.delete(rowKey(r))
                    else n.add(rowKey(r))
                    return n
                  })
                }}
                onDecide={(r, dec) => decideRows([r], dec)}
                onGroup={approveGroup}
                onFocusRow={(r) => setCursor(rowKey(r))}
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
  onPick,
  onDecide,
  onGroup,
  onFocusRow,
}: {
  line: Line<Row>
  decide: boolean
  busy: boolean
  cursor: string | null
  picked: Set<string>
  onPick: (r: Row) => void
  onDecide: (r: Row, d: 'approve' | 'reject') => void
  onGroup: (proposal: string, group: string) => void
  onFocusRow: (r: Row) => void
}) {
  if (line.type === 'dossier') {
    return (
      <Box
        sx={{ pt: 2, pb: 0.5, borderBottom: 1, borderColor: 'divider' }}
        data-dossier={line.proposal}
      >
        <Typography variant="subtitle1" component="h2">
          <TextLink to="/proposals/$id" params={{ id: line.proposal }} underline="hover">
            {line.title || line.proposal}
          </TextLink>{' '}
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
  return (
    <Stack
      direction="row"
      spacing={1}
      data-row={key}
      data-stack={r.stack}
      data-kind={r.kind}
      aria-current={here || undefined}
      onClick={() => onFocusRow(r)}
      sx={{
        pl: r.group ? 3 : 1,
        pr: 1,
        py: 0.75,
        alignItems: 'center',
        flexWrap: { xs: 'wrap', sm: 'nowrap' },
        borderLeft: 3,
        borderColor: here ? 'primary.main' : 'transparent',
        bgcolor: here ? 'action.selected' : undefined,
      }}
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
          <TextLink
            to="/proposals/$id"
            params={{ id: r.proposal }}
            hash={`item-${r.item}`}
            underline="hover"
          >
            {r.summary}
          </TextLink>
        </Typography>
        <Stack direction="row" spacing={0.5} useFlexGap sx={{ flexWrap: 'wrap', mt: 0.25 }}>
          <StatusChip tone="quiet" label={kindWords(r.kind)} />
          <StatusChip tone={stackTone(r.stack)} label={stackWords(r.stack)} />
          {r.state !== 'pending' && <StateChip state={r.state} />}
          {r.drafted && <StatusChip tone="wait" label="drafted" />}
          {r.bundle && <StatusChip tone="quiet" label={`bundle ${r.bundle}`} />}
          <Typography variant="caption" color="text.secondary">
            by {r.actor}
            {r.claimedBy ? `, ${r.claimedBy} has it` : ''}
          </Typography>
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
            >
              {r.reason}
            </Typography>
          )}
        </Stack>
      </Box>
      {decide && canStackDecide(r) && (
        <Stack direction="row" spacing={0.5}>
          <Button
            size="small"
            color="success"
            variant="outlined"
            disabled={busy}
            onClick={() => onDecide(r, 'approve')}
          >
            Approve
          </Button>
          <Button size="small" color="error" disabled={busy} onClick={() => onDecide(r, 'reject')}>
            Reject
          </Button>
        </Stack>
      )}
      {decide && r.kind === 'question' && r.stack === 'proposed' && (
        <LinkButton
          size="small"
          to="/proposals/$id"
          params={{ id: r.proposal }}
          hash={`item-${r.item}`}
        >
          Answer
        </LinkButton>
      )}
    </Stack>
  )
}
