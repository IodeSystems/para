import { useEffect, useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useProposal, useProposalDiff, useProposalSubmit } from '@/api/proposals'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { Details, Empty, PageTitle, Section, StateChip, StatusChip } from '@/components/Bits'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { ItemCard } from '@/components/stack/ItemCard'
import { DiffBlock, mono, block, type Item, type ProposalData } from '@/components/stack/parts'
import { useDossierDraft, type SaveState } from '@/components/stack/useDossierDraft'
import { answerEmpty, blockIds, parseBlockInfo, sectionId, type Answer } from '@/lib/dossier'
import { UNTRUSTED_WORDS } from '@/lib/proposal'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/proposals/$id')({ component: ProposalPage })

// ProposalPage shows a dossier: the agent's narrative with each item as a card in place. With a
// review:decide key the owner answers on the cards and comments on each section; the page saves a
// draft as they go, and Submit turns the answers into decisions. An older proposal file (no
// dossier blocks) shows its items as cards under the summary.
function ProposalPage() {
  const { id } = Route.useParams()
  const { session } = Route.useRouteContext()
  const q = useProposal(id)
  const decide = canDecide(session)
  return (
    <DataState q={q}>
      {(d) => (
        <>
          <Header d={d} />
          {d.dossier && d.markdown ? (
            <Dossier key={d.proposal.id} d={d} decide={decide} />
          ) : (
            <Legacy d={d} />
          )}
          <Extras d={d} />
        </>
      )}
    </DataState>
  )
}

function Header({ d }: { d: ProposalData }) {
  const p = d.proposal
  return (
    <>
      <PageTitle>{p.title || p.id}</PageTitle>
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
      <UntrustedNote id={p.id} />
      {p.status === 'sent_back' && (
        <Alert severity="info" sx={{ mb: 2 }}>
          Sent back. The agent revises it.
        </Alert>
      )}
    </>
  )
}

// Dossier renders the narrative with the item cards in place, a comment box under each section,
// an overall comment, and the footer with the save state and Submit.
function Dossier({ d, decide }: { d: ProposalData; decide: boolean }) {
  const draft = useDossierDraft(d)
  // A link from the stack names an item (#item-q1): scroll to its card once it is drawn.
  useEffect(() => {
    const hash = window.location.hash
    if (!hash.startsWith('#item-')) return
    document.getElementById(hash.slice(1))?.scrollIntoView({ block: 'start' })
  }, [])
  const submit = useProposalSubmit()
  const [done, setDone] = useState('')
  const byId = new Map(d.items.map((it) => [it.id, it]))
  const bundles = new Map<string, Item[]>()
  for (const it of d.items) {
    if (!it.bundle) continue
    bundles.set(it.bundle, [...(bundles.get(it.bundle) ?? []), it])
  }
  const open = d.items.filter((it) => it.stack === 'proposed')
  const drafted = open.filter((it) => !answerEmpty(draft.answerOf(it.id))).length
  const stale = d.items.filter((it) => it.answer?.stale && !draft.overridden(it.id))
  const fromFile =
    d.items.filter((it) => it.answer?.via === 'file' && it.stack === 'proposed').length +
    (d.response?.via === 'file' && d.response.state === 'draft' ? 1 : 0)
  const replies = (sid: string) => d.replies.filter((r) => r.section === sid)
  // Items with no block in the narrative show at the end, so none is lost.
  const inText = new Set(blockIds(d.markdown ?? ''))
  const rest = d.items.filter((it) => !inText.has(it.id))

  // setAnswer sets one answer. A decision on an item of a bundle is the decision of every item of
  // the bundle: they are decided as one.
  const setAnswer = (it: Item, a: Answer | null) => {
    draft.setAnswer(it.id, a)
    if (!it.bundle || it.kind === 'question') return
    const was = draft.answerOf(it.id)?.decision ?? null
    const now = a?.decision ?? null
    if (was === now) return
    for (const other of bundles.get(it.bundle) ?? []) {
      if (other.id === it.id || other.stack !== 'proposed') continue
      const cur = draft.answerOf(other.id)
      draft.setAnswer(other.id, { ...cur, decision: now === 'edit' ? 'approve' : now })
    }
  }

  const card = (it: Item) => {
    const may = decide && it.stack === 'proposed'
    return (
      <ItemCard
        key={it.id}
        it={it}
        answer={draft.answerOf(it.id)}
        onAnswer={may ? (a) => setAnswer(it, a) : undefined}
        stale={!!it.answer?.stale && !draft.overridden(it.id)}
        bundleSize={it.bundle ? bundles.get(it.bundle)?.length : undefined}
      />
    )
  }

  const doSubmit = async () => {
    setDone('')
    if (!(await draft.flush())) return
    submit.mutate(
      { id: d.proposal.id, revision: d.proposal.revision },
      {
        onSuccess: (r) =>
          setDone(
            r.failed?.length
              ? `Submitted. These failed: ${r.failed.join(', ')}.`
              : 'Submitted. Para ran the approved vault changes; an agent does the rest.',
          ),
      },
    )
  }

  const narrative = (
    <Markdown
      source={d.markdown ?? ''}
      blocks={{
        block: (info) => {
          const ref = parseBlockInfo(info)
          if (!ref) return undefined
          if (ref.block === 'dossier') return null
          const it = byId.get(ref.id)
          if (!it) {
            return (
              <Alert severity="warning" sx={{ my: 1 }}>
                Para has no item {ref.id} on this revision.
              </Alert>
            )
          }
          return card(it)
        },
        section: (title) => {
          const sid = sectionId(title)
          return (
            <SectionComment
              value={draft.sectionOf(sid)}
              onChange={decide ? (v) => draft.setSection(sid, v) : undefined}
              replies={replies(sid).map((r) => r.turn)}
              title={title}
            />
          )
        },
      }}
    />
  )
  return (
    <>
      {!decide && open.length > 0 && (
        <Alert severity="info" sx={{ mb: 2 }}>
          This key can only read. To answer, sign in from a terminal with para serve login.
        </Alert>
      )}
      {stale.length > 0 && (
        <Alert severity="warning" sx={{ mb: 2 }} data-banner="stale">
          The agent changed {stale.length === 1 ? 'an item' : `${stale.length} items`} after you
          answered: {stale.map((it) => it.title || it.id).join(', ')}. Check those answers before
          you submit.
        </Alert>
      )}
      {fromFile > 0 && (
        <Alert severity="info" sx={{ mb: 2 }} data-banner="file">
          Some answers were typed in the file (for example in Obsidian). They are drafts until you
          submit them here. Check them first.
        </Alert>
      )}
      {d.response?.state === 'submitted' && (
        <Alert severity="success" sx={{ mb: 2 }}>
          You submitted your answers
          {d.response.submittedAt ? ` ${humanTime(d.response.submittedAt)}` : ''}.
        </Alert>
      )}
      <Box component="article" aria-label="Dossier">
        {narrative}
      </Box>
      {rest.length > 0 && (
        <Section title="Other items" count={rest.length}>
          {rest.map(card)}
        </Section>
      )}
      <Section title="Overall">
        <SectionComment
          value={draft.overall}
          onChange={decide ? draft.setOverall : undefined}
          replies={replies('overall').map((r) => r.turn)}
          title="the whole dossier"
        />
      </Section>
      {(done || submit.isError) && (
        <Alert severity={submit.isError ? 'error' : 'success'} sx={{ my: 2 }}>
          {submit.isError ? errorText(submit.error) : done}
        </Alert>
      )}
      {decide && (open.length > 0 || draft.state !== 'saved') && (
        <Footer
          drafted={drafted}
          total={open.length}
          state={draft.state}
          error={draft.error}
          onRetry={() => void draft.retry()}
          blocked={
            stale.length > 0
              ? 'Check the stale answers first.'
              : drafted === 0 && !draft.overall.trim()
                ? 'Answer an item first.'
                : ''
          }
          busy={submit.isPending}
          onSubmit={() => void doSubmit()}
        />
      )}
    </>
  )
}

type Turn = { id: string; text: string; by: string }

// SectionComment is the owner's comment on a section, and the agent's replies to it.
function SectionComment({
  value,
  onChange,
  replies,
  title,
}: {
  value: string
  onChange?: (v: string) => void
  replies: Turn[]
  title: string
}) {
  return (
    <Box sx={{ my: 1 }} data-section-comment={title}>
      {onChange ? (
        <TextField
          size="small"
          fullWidth
          multiline
          label={`Comment on ${title}`}
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
      ) : (
        value && (
          <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
            Your comment: {value}
          </Typography>
        )
      )}
      {replies.map((t) => (
        <Typography key={t.id} variant="body2" sx={{ mt: 0.5, overflowWrap: 'anywhere' }}>
          {t.by || 'Agent'} replied: {t.text}
        </Typography>
      ))}
    </Box>
  )
}

const SAVE_WORDS: Record<SaveState, string> = {
  saved: 'Draft saved',
  unsaved: 'Not saved yet',
  saving: 'Saving',
  error: 'Not saved',
}

// Footer stays at the bottom of the screen: how many items have an answer, the save state, and
// Submit.
function Footer({
  drafted,
  total,
  state,
  error,
  onRetry,
  blocked,
  busy,
  onSubmit,
}: {
  drafted: number
  total: number
  state: SaveState
  error: string
  onRetry: () => void
  blocked: string
  busy: boolean
  onSubmit: () => void
}) {
  return (
    <Paper
      elevation={6}
      role="region"
      aria-label="Submit answers"
      sx={{
        position: 'sticky',
        bottom: { xs: 64, sm: 8 },
        mt: 2,
        p: 1.5,
        zIndex: 2,
      }}
    >
      <Stack direction="row" spacing={2} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
        <Typography variant="body2" data-drafted={`${drafted}/${total}`}>
          {drafted} of {total} answered
        </Typography>
        <Typography
          variant="body2"
          color={state === 'error' ? 'error' : 'text.secondary'}
          aria-live="polite"
          data-save={state}
        >
          {SAVE_WORDS[state]}
          {error ? `: ${error}` : ''}
        </Typography>
        {state === 'error' && (
          <Button size="small" onClick={onRetry}>
            Read again and retry
          </Button>
        )}
        <Box sx={{ flexGrow: 1 }} />
        {blocked && (
          <Typography variant="caption" color="text.secondary">
            {blocked}
          </Typography>
        )}
        <Button
          variant="contained"
          disabled={busy || !!blocked || state === 'saving'}
          onClick={onSubmit}
        >
          Submit
        </Button>
      </Stack>
    </Paper>
  )
}

// Legacy shows an older proposal file: its items as cards, to decide in the chat.
function Legacy({ d }: { d: ProposalData }) {
  const pending = d.items.filter((it) => it.stack === 'proposed')
  const decided = d.items.filter((it) => it.stack !== 'proposed')
  return (
    <>
      <Section title="To decide" count={pending.length}>
        <Stack spacing={1.5}>
          <Groups items={d.items} />
          {pending.map((it) => (
            <ItemCard key={it.id} it={it} answer={(it.answer ?? null) as Answer | null} />
          ))}
          {d.items.length === 0 && <Empty>This revision has no items.</Empty>}
          {d.items.length > 0 && pending.length === 0 && <Empty>Nothing left to decide.</Empty>}
        </Stack>
      </Section>
      {decided.length > 0 && (
        <Section title="Decided" count={decided.length}>
          <Stack spacing={1.5}>
            {decided.map((it) => (
              <ItemCard key={it.id} it={it} answer={(it.answer ?? null) as Answer | null} />
            ))}
          </Stack>
        </Section>
      )}
    </>
  )
}

// Groups names the groups on this revision.
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

// Extras is the rest of the page: changes since the last revision, history, source, and the
// command to open it from a terminal.
function Extras({ d }: { d: ProposalData }) {
  const id = d.proposal.id
  return (
    <>
      {d.proposal.revision > 1 && <DiffSection id={id} />}
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
      {d.source.id && (
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
      )}
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
