import { useMemo, useState } from 'react'
import { createFileRoute, useRouteContext } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import Button from '@mui/material/Button'
import Checkbox from '@mui/material/Checkbox'
import Stack from '@mui/material/Stack'
import Tab from '@mui/material/Tab'
import Tabs from '@mui/material/Tabs'
import Typography from '@mui/material/Typography'
import { ioReview, useIoList } from '@/api/io'
import { latestReceipt, revertReceipt } from '@/api/receipts'
import { canDecide } from '@/client/session'
import { errorText } from '@/client/gqlClient'
import { Empty, PageTitle } from '@/components/Bits'
import { CaptureButton } from '@/components/CaptureButton'
import { DataState } from '@/components/DataState'
import { IoMade } from '@/components/IoMade'
import { IoRows, type IoRow } from '@/components/IoRows'
import { useToasts } from '@/components/ToastStack'
import { splitInput, type InputTab } from '@/lib/input'

const TABS: { key: InputTab; label: string; empty: string; help: string }[] = [
  {
    key: 'needs',
    label: 'Needs you',
    empty: 'Nothing waits on you.',
    help: 'Incoming messages that expect your response.',
  },
  {
    key: 'changed',
    label: 'Changed para',
    empty: 'No agent changed para from unreviewed input.',
    help: 'Incoming messages an agent already acted on, and you have not reviewed.',
  },
  {
    key: 'rest',
    label: 'The rest',
    empty: 'Nothing else unreviewed.',
    help: 'Unreviewed digest: no reply expected, nothing made.',
  },
  {
    key: 'reviewed',
    label: 'Reviewed',
    empty: 'Nothing reviewed yet.',
    help: 'Recently reviewed input.',
  },
]

const REVIEWED_SHOWN = 50

export const Route = createFileRoute('/_authed/input')({
  validateSearch: (s: Record<string, unknown>): { tab?: InputTab } => ({
    tab: s.tab === 'changed' || s.tab === 'rest' || s.tab === 'reviewed' ? s.tab : undefined,
  }),
  component: InputPage,
})

function InputPage() {
  const { tab } = Route.useSearch()
  const navigate = Route.useNavigate()
  const { session } = useRouteContext({ from: '/_authed' })
  const decide = canDecide(session)
  const toasts = useToasts()
  const qc = useQueryClient()
  const q = useIoList('in', true)
  const [selected, setSelected] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const cur = tab ?? 'needs'
  const spec = TABS.find((t) => t.key === cur) ?? TABS[0]

  const split = useMemo(
    // all=true brings filed items too: a filed one shows only once reviewed; a dropped one never.
    () =>
      splitInput(
        (q.data?.items ?? []).filter(
          (x) =>
            x.state !== 'dropped' && (!!x.reviewed || x.state === 'unread' || x.state === 'read'),
        ),
      ),
    [q.data],
  )

  const review = async (ids: string[]) => {
    if (ids.length === 0) return
    setBusy(true)
    try {
      await ioReview(ids)
      // io_review replies with ids and time only; its one receipt is the newest of the op.
      const receipt = await latestReceipt('io_review').catch(() => null)
      setSelected((s) => s.filter((id) => !ids.includes(id)))
      toasts.push({
        text: ids.length === 1 ? 'Marked reviewed' : `Marked ${ids.length} reviewed`,
        target: ids.length === 1 ? { id: ids[0], kind: 'io' } : undefined,
        undo: receipt
          ? async () => {
              await revertReceipt(receipt)
              await qc.invalidateQueries()
            }
          : undefined,
      })
      await qc.invalidateQueries()
    } catch (e) {
      toasts.push({ text: `Could not mark reviewed: ${errorText(e)}`, tone: 'error' })
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageTitle
        subtitle="Input you have not reviewed, and what it created. Reviewing records that you looked; it changes nothing else."
        action={<CaptureButton />}
      >
        Input
      </PageTitle>
      <Tabs
        value={cur}
        onChange={(_e, v: InputTab) => {
          setSelected([])
          void navigate({ search: { tab: v === 'needs' ? undefined : v }, replace: true })
        }}
        variant="scrollable"
        sx={{ mb: 2 }}
      >
        {TABS.map((t) => (
          <Tab
            key={t.key}
            value={t.key}
            data-tab={t.key}
            label={q.data ? `${t.label} (${split[t.key].length})` : t.label}
          />
        ))}
      </Tabs>
      <DataState q={q}>
        {() => {
          const list: IoRow[] =
            cur === 'reviewed' ? split.reviewed.slice(0, REVIEWED_SHOWN) : split[cur]
          const reviewable = cur !== 'reviewed' && decide
          return (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {spec.help}
              </Typography>
              {reviewable && list.length > 0 && (
                <Stack direction="row" spacing={1} sx={{ mb: 1, alignItems: 'center' }}>
                  <Checkbox
                    size="small"
                    aria-label="Select all"
                    checked={list.every((x) => selected.includes(x.id))}
                    indeterminate={
                      selected.length > 0 && !list.every((x) => selected.includes(x.id))
                    }
                    onChange={(e) => setSelected(e.target.checked ? list.map((x) => x.id) : [])}
                  />
                  <Button
                    size="small"
                    variant="contained"
                    data-act="mark-reviewed"
                    disabled={busy || selected.length === 0}
                    onClick={() => void review(selected)}
                  >
                    {selected.length > 0 ? `Mark reviewed (${selected.length})` : 'Mark reviewed'}
                  </Button>
                </Stack>
              )}
              {list.length === 0 ? (
                <Empty>{spec.empty}</Empty>
              ) : (
                <IoRows
                  items={list}
                  lead={
                    reviewable
                      ? (x) => (
                          <Checkbox
                            size="small"
                            aria-label={`Select ${x.title || x.id}`}
                            checked={selected.includes(x.id)}
                            onChange={(e) =>
                              setSelected((s) =>
                                e.target.checked ? [...s, x.id] : s.filter((id) => id !== x.id),
                              )
                            }
                          />
                        )
                      : undefined
                  }
                  extra={(x) => (
                    <>
                      {(x.made ?? 0) > 0 && <IoMade id={x.id} />}
                      {reviewable && (
                        <Button
                          size="small"
                          data-act="review-row"
                          disabled={busy}
                          onClick={() => void review([x.id])}
                        >
                          Mark reviewed
                        </Button>
                      )}
                    </>
                  )}
                />
              )}
            </>
          )
        }}
      </DataState>
    </>
  )
}
