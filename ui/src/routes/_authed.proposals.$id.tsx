import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { useProposal, useProposalDiff } from '@/api/proposals'
import { Details, Empty, PageTitle, Section, StateChip, StatusChip } from '@/components/Bits'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { diffKind } from '@/lib/diff'
import { DECIDE_HINT, isOutward, stateWords, UNTRUSTED_WORDS } from '@/lib/proposal'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/proposals/$id')({ component: ProposalPage })

const mono = { fontFamily: 'ui-monospace, monospace', fontSize: 13 } as const
const block = {
  ...mono,
  my: 1,
  p: 1,
  bgcolor: 'action.hover',
  borderRadius: 1,
  whiteSpace: 'pre-wrap',
  overflowWrap: 'anywhere',
} as const

// ProposalPage shows everything the owner needs to decide. The decision is made in the chat
// with the agent: this page has no control that writes.
function ProposalPage() {
  const { id } = Route.useParams()
  const q = useProposal(id)
  return (
    <DataState q={q}>
      {(d) => {
        const p = d.proposal
        const pending = d.items.filter((it) => it.state === 'pending')
        const decided = d.items.filter((it) => it.state !== 'pending')
        return (
          <>
            <PageTitle subtitle={DECIDE_HINT}>{p.title || id}</PageTitle>
            <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 2, flexWrap: 'wrap' }}>
              <StateChip state={p.status} />
              <StatusChip tone="quiet" label={`revision ${p.revision}`} />
              <StatusChip tone="quiet" label={`by ${p.proposedBy}`} />
              <StatusChip tone="quiet" label={humanTime(p.created)} />
              <StatusChip
                tone="quiet"
                label={p.pending === 0 ? 'all decided' : `${p.pending} to decide`}
              />
            </Stack>
            {p.error && (
              <Alert severity="error" sx={{ mb: 2 }}>
                This proposal file is broken: {p.error}
              </Alert>
            )}
            {d.supersededBy && (
              <Alert severity="info" sx={{ mb: 2 }}>
                The agent filed a newer revision.{' '}
                <TextLink to="/proposals/$id" params={{ id: d.supersededBy }}>
                  Open the newer revision
                </TextLink>
              </Alert>
            )}
            {(d.tldr || d.context) && (
              <Box sx={{ mb: 2 }}>
                {d.tldr && (
                  <Typography variant="subtitle1" sx={{ overflowWrap: 'anywhere' }} data-tldr>
                    {d.tldr}
                  </Typography>
                )}
                {d.context && (
                  <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                    {d.context}
                  </Typography>
                )}
              </Box>
            )}
            <UntrustedNote id={id} />
            {p.status === 'sent_back' && (
              <Alert severity="info" sx={{ mb: 2 }}>
                Sent back. The agent revises it and files a new revision.
              </Alert>
            )}
            <Section title="To decide in the chat" count={pending.length}>
              <Stack spacing={1.5}>
                <Groups items={d.items} />
                {pending.map((it) => (
                  <ItemCard key={it.id} it={it} />
                ))}
                {d.items.length === 0 && <Empty>This revision has no items.</Empty>}
                {d.items.length > 0 && pending.length === 0 && (
                  <Empty>Nothing left to decide.</Empty>
                )}
              </Stack>
            </Section>
            {decided.length > 0 && (
              <Section title="Decided" count={decided.length}>
                <Stack spacing={1.5}>
                  {decided.map((it) => (
                    <ItemCard key={it.id} it={it} />
                  ))}
                </Stack>
              </Section>
            )}
            {p.revision > 1 && <DiffSection id={id} />}
            <Section title="History" count={d.history.length}>
              <List dense disablePadding>
                {d.history.map((h) => (
                  <ListItem key={h.id} disableGutters>
                    <ListItemText
                      primary={
                        h.id === id ? (
                          <strong>Revision {h.revision} (this page)</strong>
                        ) : (
                          <TextLink to="/proposals/$id" params={{ id: h.id }} underline="hover">
                            Revision {h.revision}
                          </TextLink>
                        )
                      }
                      secondary={`${humanTime(h.created)} - ${h.status.replace(/_/g, ' ')}`}
                    />
                  </ListItem>
                ))}
              </List>
            </Section>
            <Section title="Source">
              <Typography variant="body2" sx={{ mb: 1 }}>
                <DocLink id={d.source.id}>{d.source.title || d.source.id}</DocLink>
              </Typography>
              {d.proposal.sourcePath ? (
                <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                  Path: {d.proposal.sourcePath}
                </Typography>
              ) : null}
              {d.source.error ? (
                <Alert severity="warning">The source could not be read: {d.source.error}</Alert>
              ) : (d.source.body ?? '').trim() === '' ? null : (
                <Box
                  component="pre"
                  tabIndex={0}
                  aria-label="Source text (untrusted)"
                  sx={{ ...block, my: 0, maxHeight: 320, overflow: 'auto' }}
                >
                  {d.source.body}
                </Box>
              )}
            </Section>
            <Details summary="Open it from a terminal">
              <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                <Typography sx={{ ...mono, overflowWrap: 'anywhere', flexGrow: 1 }}>
                  {d.reviewCommand}
                </Typography>
                <CopyButton getText={() => d.reviewCommand} label="Copy command" />
              </Stack>
            </Details>
          </>
        )
      }}
    </DataState>
  )
}

type ProposalData = NonNullable<ReturnType<typeof useProposal>['data']>
type Item = ProposalData['items'][number]
type Claim = { text: string; source: string }

// ID_LIKE matches a document id, which a person does not read.
const ID_LIKE = /^[a-z][a-z_]*-[0-9a-z]{8,}$/

// Groups names the groups and bundles on this revision, so the owner can decide them as one in
// the chat.
function Groups({ items }: { items: Item[] }) {
  const counts = new Map<string, number>()
  for (const it of items) {
    if (it.group) counts.set(it.group, (counts.get(it.group) ?? 0) + 1)
  }
  if (counts.size === 0) return null
  return (
    <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap' }} aria-label="Groups">
      {[...counts].map(([group, n]) => (
        <StatusChip key={group} tone="quiet" label={`group ${group}: ${n}`} />
      ))}
    </Stack>
  )
}

// Row is one labelled line of an item. It shows nothing when there is no text.
function Row({ label, text }: { label: string; text?: string | null }) {
  if (!text) return null
  return (
    <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
      <Box component="span" sx={{ color: 'text.secondary' }}>
        {label}:{' '}
      </Box>
      {text}
    </Typography>
  )
}

function ClaimRows({ label, list }: { label: string; list: Claim[] }) {
  return (
    <>
      {list.map((c) => (
        <Row
          key={c.text}
          label={label}
          text={ID_LIKE.test(c.source) ? c.text : `${c.text} [${c.source}]`}
        />
      ))}
    </>
  )
}

// Weighing is the case for and against an item.
function Weighing({ it }: { it: Item }) {
  return (
    <Box sx={{ my: 1 }}>
      <ClaimRows label="Pro" list={it.pros ?? []} />
      <ClaimRows label="Con" list={it.cons ?? []} />
      {it.cost && <ClaimRows label="Cost" list={[it.cost]} />}
      {it.benefit && <ClaimRows label="Benefit" list={[it.benefit]} />}
      {!!it.confidence && (
        <Row
          label="Confidence"
          text={`${it.confidence}%${it.confidenceBasis ? `, because ${it.confidenceBasis}` : ''}`}
        />
      )}
      {!!it.recommendation && <Row label="Recommended" text={`${it.recommendation}%`} />}
      <Row label="Risk" text={it.risk} />
      <Row label="Reversible" text={[it.reversible, it.reversibleHow].filter(Boolean).join(': ')} />
      <Row label="Group" text={it.group} />
      <Row label="Runs after" text={it.after} />
      <Row label="Bundle" text={it.bundle} />
      <Row label="Choice" text={it.choice} />
      <Row label="For" text={it.for} />
      <Row label="Not a duplicate of" text={it.notDuplicate} />
      <Row label="With results filled in" text={it.filled} />
    </Box>
  )
}

// Outward shows a message or an action as the thing itself: who it goes to, and what it says.
function Outward({ it }: { it: Item }) {
  const message = it.kind === 'message' || it.kind === 'external'
  return (
    <Box aria-label={message ? 'The message' : 'The action'}>
      <Row label={message ? 'To' : 'Target'} text={it.target} />
      <Row label="People" text={(it.people ?? []).join(', ')} />
      <Row label="Channel" text={it.channel} />
      <Row label="Kind" text={it.subtype} />
      <Row label="Subject" text={it.subject} />
      <Row label="Title" text={it.title} />
      <Row label="What" text={it.what} />
      <Row label="Changes" text={it.previous} />
      <Row label="How" text={it.how} />
      <Row label="Impact" text={it.impact} />
      {(it.body || it.diff) && (
        <Box component="pre" aria-label={message ? 'Message body' : 'Body'} sx={block}>
          {it.body || it.diff}
        </Box>
      )}
      {it.state === 'approved' && it.body && (
        <CopyButton getText={() => it.body ?? ''} label="Copy text" />
      )}
    </Box>
  )
}

// VaultChange shows a change in the vault: what the agent says, and para's own preview of the
// document before and after.
function VaultChange({ it }: { it: Item }) {
  const input: unknown = it.input
  return (
    <Box>
      <Typography variant="overline" color="text.secondary" component="div">
        The exact change
      </Typography>
      <Box component="ul" aria-label="Exact change" sx={{ m: 0, mb: 1, pl: 2.5 }}>
        {it.change.map((line) => (
          <Typography component="li" variant="body2" key={line} sx={{ overflowWrap: 'anywhere' }}>
            {line}
          </Typography>
        ))}
      </Box>
      <Row label="Title" text={it.title} />
      <Row label="What" text={it.what} />
      <Row label="How" text={it.how} />
      <Row label="Impact" text={it.impact} />
      {it.preview && <Preview pv={it.preview} />}
      <Details summary="The op and its input">
        <Typography variant="caption" component="div">
          {it.op}
        </Typography>
        <Box
          component="pre"
          sx={{ ...block, my: 0.5, whiteSpace: 'pre' as const, overflowX: 'auto' }}
        >
          {JSON.stringify(input ?? {}, null, 2)}
        </Box>
      </Details>
    </Box>
  )
}

// Preview is para's own account of a vault change: before, after, and what waits on it.
function Preview({ pv }: { pv: NonNullable<Item['preview']> }) {
  return (
    <Box aria-label="Before and after" sx={{ my: 1 }}>
      {pv.error && <Alert severity="warning">This would fail now: {pv.error}</Alert>}
      {pv.changes.map((c) => (
        <Box key={c.doc}>
          <Typography variant="caption" color="text.secondary">
            {c.new ? 'New' : 'Changes'} {c.type}: {c.title || c.doc}
          </Typography>
          <DiffBlock lines={c.lines} />
        </Box>
      ))}
      {pv.downstream.map((x) => (
        <Row key={x.doc} label={x.title || x.doc} text={x.effect} />
      ))}
    </Box>
  )
}

function DiffBlock({ lines }: { lines: string[] }) {
  return (
    <Box component="pre" sx={{ ...mono, m: 0, overflowX: 'auto', whiteSpace: 'pre-wrap' }}>
      {lines.map((l, n) => {
        const k = diffKind(l)
        return (
          <Box
            component="span"
            key={n}
            sx={{
              display: 'block',
              color: k === 'add' ? 'success.main' : k === 'del' ? 'error.main' : 'text.secondary',
            }}
          >
            {l}
          </Box>
        )
      })}
    </Box>
  )
}

// UntrustedNote explains once per proposal why evidence is marked: at the top of the page, never
// on each item. Opening a proposal counts as reading it.
function UntrustedNote({ id }: { id: string }) {
  const [seen] = useState(() => {
    try {
      const key = `para.untrustedNoteSeen.${id}`
      const was = window.localStorage.getItem(key) === '1'
      if (!was) window.localStorage.setItem(key, '1')
      return was
    } catch {
      return true
    }
  })
  if (seen) return null
  return (
    <Alert severity="info" sx={{ mb: 2 }}>
      {UNTRUSTED_WORDS} Evidence is marked <strong>untrusted</strong> on every item.
    </Alert>
  )
}

function ItemCard({ it }: { it: Item }) {
  const outward = isOutward(it.kind)
  const notes = it.thread ?? []
  const dec = it.decided
  return (
    <Paper variant="outlined" sx={{ p: 1.5 }} data-item={it.id}>
      <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5, overflowWrap: 'anywhere' }}>
        {it.summary}
      </Typography>
      <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 1, flexWrap: 'wrap' }}>
        <StatusChip
          tone="quiet"
          label={
            it.kind === 'message' || it.kind === 'external'
              ? 'message to send'
              : outward
                ? 'action outside para'
                : 'change in the vault'
          }
        />
        <StateChip state={it.state} label={stateWords(it.state, it.kind)} />
        {it.edited && <StatusChip tone="wait" label="edited" />}
        {it.heldBy && <StatusChip tone="attn" label={`needs ${it.heldBy} first`} />}
      </Stack>
      {outward ? <Outward it={it} /> : <VaultChange it={it} />}
      <Weighing it={it} />
      <Box sx={{ borderLeft: 3, borderColor: 'divider', pl: 1.5, my: 1 }}>
        <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center' }}>
          <Typography variant="caption" color="text.secondary">
            Evidence{it.evidence.locator ? ` (${it.evidence.locator})` : ''}
          </Typography>
          <Tooltip title={UNTRUSTED_WORDS}>
            <span>
              <StatusChip tone="attn" label="untrusted" />
            </span>
          </Tooltip>
        </Stack>
        <Typography variant="body2" sx={{ fontStyle: 'italic', overflowWrap: 'anywhere' }}>
          {it.evidence.quote}
        </Typography>
      </Box>
      {dec && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          {dec.decision.replace(/_/g, ' ')} by {dec.by}, {humanTime(dec.at)}
          {dec.surface ? ` (${dec.surface})` : ''}{' '}
          {dec.selfReported && <StatusChip tone="wait" label="self-reported" />}
        </Typography>
      )}
      {dec?.ownerSaid && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Owner said: &ldquo;{dec.ownerSaid}&rdquo;
        </Typography>
      )}
      {it.reason && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Reason: {it.reason}
        </Typography>
      )}
      {it.state === 'approved' && outward && !it.execution && (
        <Alert severity="warning" sx={{ my: 1 }}>
          Approved. Not done yet.
        </Alert>
      )}
      {it.state === 'approved' && !outward && !it.result && (
        <Alert severity="info" sx={{ my: 1 }}>
          {it.waits ? `Approved. Runs after ${it.after}.` : 'Approved.'}
        </Alert>
      )}
      {(it.state === 'done' || it.state === 'failed') && (it.result || it.state === 'failed') && (
        <Alert severity={it.state === 'failed' ? 'error' : 'success'} sx={{ my: 1 }}>
          {it.state === 'failed' ? (
            it.result
          ) : it.resultName ? (
            <>
              Done. {outward ? '' : 'Made '}
              <DocLink id={it.result ?? ''}>{it.resultName}</DocLink>.
            </>
          ) : (
            <>Done. {it.result}</>
          )}
        </Alert>
      )}
      {it.execution && (
        <Box aria-label="Execution">
          <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
            {it.execution.result} by {it.execution.by}, {humanTime(it.execution.at)}{' '}
            {it.execution.selfReported ? (
              <StatusChip tone="wait" label="self-reported" />
            ) : (
              <StatusChip tone="good" label="recorded by a hook" />
            )}
          </Typography>
          {it.execution.url && (
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              <a href={it.execution.url} target="_blank" rel="noreferrer noopener">
                {it.execution.url}
              </a>
            </Typography>
          )}
          <Row label="Where" text={it.execution.locator} />
        </Box>
      )}
      {notes.length > 0 && (
        <List dense disablePadding sx={{ my: 1 }} aria-label="Thread">
          {notes.map((n, i) => (
            <ListItem key={n.id ?? i} disableGutters sx={{ display: 'block' }}>
              {n.quote && (
                <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                  <em>&ldquo;{n.quote}&rdquo;</em>
                </Typography>
              )}
              <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                {n.kind.replace(/_/g, ' ')}: {n.text} <StateChip state={n.state} />
              </Typography>
              {n.answer && (
                <Typography variant="body2" color="text.secondary">
                  Agent: {n.answer}
                </Typography>
              )}
            </ListItem>
          ))}
        </List>
      )}
      <Details summary="Hashes">
        {it.approvedHash && (
          <Typography variant="caption" component="div">
            Approved as hash <code>{it.approvedHash}</code>
          </Typography>
        )}
        <Typography variant="caption" component="div">
          An approval covers this exact text. Hash <code>{it.hash}</code>
        </Typography>
      </Details>
    </Paper>
  )
}

function DiffSection({ id }: { id: string }) {
  const q = useProposalDiff(id, true)
  return (
    <Section title="Changes since the last revision">
      <DataState q={q}>
        {(d) =>
          d.items.every((i) => i.change === 'same') ? (
            <Empty>Nothing differs from {d.previous}.</Empty>
          ) : (
            <Stack spacing={1}>
              {d.items
                .filter((i) => i.change !== 'same')
                .map((i) => (
                  <Box key={i.item}>
                    <Typography variant="subtitle2">
                      {i.item}{' '}
                      <StateChip
                        state={i.change === 'removed' ? 'blocked' : 'sent'}
                        label={i.change}
                      />
                    </Typography>
                    {i.fields.map((f) => (
                      <Box key={f.field}>
                        <Typography variant="caption" color="text.secondary">
                          {f.field}
                        </Typography>
                        <DiffBlock lines={f.lines} />
                      </Box>
                    ))}
                  </Box>
                ))}
            </Stack>
          )
        }
      </DataState>
    </Section>
  )
}
