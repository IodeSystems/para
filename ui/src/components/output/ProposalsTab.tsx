import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import FormControlLabel from '@mui/material/FormControlLabel'
import Stack from '@mui/material/Stack'
import Switch from '@mui/material/Switch'
import Typography from '@mui/material/Typography'
import { useIoItems } from '@/api/io'
import { useApproveProposals, useIoProposals, type ProposalGroup } from '@/api/proposals'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { Empty, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { ProposalCard } from '@/components/output/ProposalCard'
import { useToasts } from '@/components/ToastStack'
import { batchPairs, orderGroups, refusalsFor, selectedDraft } from '@/lib/proposals'
import { plural } from '@/lib/plural'
import { useRouteContext } from '@tanstack/react-router'

// ProposalsTab is what waits for the owner to let go out: each proposal is the preview of an outward
// action (a message, a PR, a ticket, a deploy, a comment). A group is a batch; a set holds variations
// of one proposal. Needs-owner first.
export function ProposalsTab() {
  const [all, setAll] = useState(false)
  const q = useIoProposals(all)
  return (
    <>
      <FormControlLabel
        sx={{ mb: 1 }}
        control={<Switch size="small" checked={all} onChange={(e) => setAll(e.target.checked)} />}
        label="Include sent, dropped, and closed"
      />
      <DataState q={q}>
        {(d) =>
          d.groups.length === 0 ? (
            <Empty>Nothing waits for you to approve. An agent shows a proposal here before it sends anything.</Empty>
          ) : (
            <>
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1.5 }}>
                {plural(d.count, 'proposal')}
                {d.needsYou > 0 ? `, ${d.needsYou} need you` : ''}.
              </Typography>
              {orderGroups(d.groups).map((g) => (
                <Group key={g.group} g={g} />
              ))}
            </>
          )
        }
      </DataState>
    </>
  )
}

function Group({ g }: { g: ProposalGroup }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const decide = canDecide(session)
  const [chosen, setChosen] = useState<Record<string, string>>({})
  const [refusals, setRefusals] = useState<{ byId: Record<string, string>; all?: string }>({
    byId: {},
  })
  const approve = useApproveProposals()
  const toasts = useToasts()
  const batch = g.sets.length > 1
  // The drafts rendered now: one per set, the tab it shows.
  const shown = g.sets.flatMap((s) => {
    const d = selectedDraft(s, chosen[s.root])
    return d ? [d.id] : []
  })
  const items = useIoItems(shown)
  const hashes: Record<string, string | undefined> = {}
  shown.forEach((id, i) => {
    hashes[id] = items[i]?.data?.item.draftHash ?? undefined
  })
  const { pairs, skipped } = batchPairs(g.sets, chosen, hashes)
  const run = async () => {
    setRefusals({ byId: {} })
    try {
      await approve.mutateAsync(pairs)
      toasts.push({ text: `Approved ${plural(pairs.length, 'draft')}`, note: 'An agent sends them.' })
    } catch (e) {
      setRefusals(
        refusalsFor(
          pairs.map((p) => p.id),
          errorText(e),
        ),
      )
    }
  }
  return (
    <Section
      title={batch ? `Batch: ${g.group}` : (g.sets[0]?.drafts[0]?.title ?? g.group)}
      count={batch ? g.sets.length : undefined}
      action={
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          {g.needsYou && <StatusChip tone="attn" label="Needs you" />}
          {batch && decide && (
            <Button
              size="small"
              variant="contained"
              color="success"
              data-act="approve-all"
              disabled={approve.isPending || pairs.length === 0}
              onClick={() => void run()}
            >
              {`Approve all shown (${pairs.length})`}
            </Button>
          )}
        </Stack>
      }
    >
      {skipped > 0 && (
        <Typography variant="caption" color="text.secondary" component="div">
          {plural(skipped, 'draft')} still loading, left out for now.
        </Typography>
      )}
      {refusals.all && (
        <Alert severity="error" sx={{ mb: 1 }} data-refusal-all>
          Nothing was approved: {refusals.all}
        </Alert>
      )}
      {g.sets.map((s) => {
        const d = selectedDraft(s, chosen[s.root])
        return (
          <ProposalCard
            key={s.root}
            set={s}
            chosen={d?.id}
            onChoose={(id) => setChosen((c) => ({ ...c, [s.root]: id }))}
            refusal={d ? refusals.byId[d.id] : undefined}
            decide={decide}
          />
        )
      })}
    </Section>
  )
}
