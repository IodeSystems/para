import { useRef, useState } from 'react'
import { createFileRoute, useNavigate } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Tooltip from '@mui/material/Tooltip'
import Typography from '@mui/material/Typography'
import { useApprove, useProposal, useProposalDiff } from '@/api/proposals'
import { errorText } from '@/client/gqlClient'
import { Details, Empty, PageTitle, Section, StateChip, StatusChip } from '@/components/Bits'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { LinkButton, TextLink } from '@/components/links'
import { RowMenu, type RowMenuItem } from '@/components/RowMenu'
import { diffKind, EXTERNAL_PROMISE, UNTRUSTED_WORDS } from '@/lib/proposal'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/proposals/$id')({ component: ProposalPage })

const mono = { fontFamily: 'ui-monospace, monospace', fontSize: 13 } as const

function ProposalPage() {
  const { id } = Route.useParams()
  const { session } = Route.useRouteContext()
  const q = useProposal(id)
  const canReview = session.admin || session.scopes.includes('proposal:review')
  return (
    <DataState q={q}>
      {(d) => {
        const p = d.proposal
        return (
          <>
            <PageTitle>{p.title || id}</PageTitle>
            <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 2, flexWrap: 'wrap' }}>
              <StateChip state={p.status} />
              <StatusChip tone="quiet" label={`revision ${p.revision}`} />
              <StatusChip tone="quiet" label={`by ${p.proposedBy}`} />
              <StatusChip tone="quiet" label={`${p.pending} of ${p.itemCount} to decide`} />
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
            {!canReview && (
              <Alert severity="warning" sx={{ mb: 2 }}>
                This session cannot decide items: it lacks the proposal:review scope. Sign in with a
                key made by{' '}
                <code>
                  para key create reviewer --scope vault:read --scope vault:write --scope
                  proposal:review --print-url
                </code>
                .
              </Alert>
            )}
            <UntrustedNote />
            {p.status === 'sent_back' && (
              <Alert severity="info" sx={{ mb: 2 }}>
                Sent back. The agent revises it and files a new revision. Wake an agent with{' '}
                <code>{d.reviewCommand.replace(/^para review/, 'para proposal wait')}</code>.
              </Alert>
            )}
            <Section title="Items" count={d.items.length}>
              <Stack spacing={1.5}>
                {d.items.map((it) => (
                  <ItemCard
                    key={it.id}
                    proposalId={id}
                    it={it}
                    notes={d.annotations.filter((a) => a.item === it.id)}
                    canReview={canReview}
                  />
                ))}
                {d.items.length === 0 && <Empty>This revision has no items.</Empty>}
              </Stack>
            </Section>
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
              {d.source.error ? (
                <Alert severity="warning">The source could not be read: {d.source.error}</Alert>
              ) : (
                <Box
                  component="pre"
                  tabIndex={0}
                  aria-label="Source text (untrusted)"
                  sx={{
                    ...mono,
                    m: 0,
                    p: 1,
                    bgcolor: 'action.hover',
                    borderRadius: 1,
                    whiteSpace: 'pre-wrap',
                    overflowWrap: 'anywhere',
                    maxHeight: 320,
                    overflow: 'auto',
                  }}
                >
                  {d.source.body}
                </Box>
              )}
            </Section>
            <Details summary="Wake or resume an agent">
              <Stack spacing={1}>
                <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
                  <Typography sx={{ ...mono, overflowWrap: 'anywhere', flexGrow: 1 }}>
                    {d.reviewCommand}
                  </Typography>
                  <CopyButton getText={() => d.reviewCommand} label="Copy command" />
                </Stack>
                {d.reviewUrl && (
                  <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                    {d.reviewUrl}
                  </Typography>
                )}
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
type Note = ProposalData['annotations'][number]

const NOTE_KEY = 'para.untrustedNoteSeen'

// UntrustedNote explains once per browser why evidence is marked. Dismissing it is remembered.
function UntrustedNote() {
  // Shown on the first proposal a browser opens, then never again: opening it counts as reading it.
  const [seen, setSeen] = useState(() => {
    try {
      const was = window.localStorage.getItem(NOTE_KEY) === '1'
      if (!was) window.localStorage.setItem(NOTE_KEY, '1')
      return was
    } catch {
      return true
    }
  })
  if (seen) return null
  return (
    <Alert severity="info" sx={{ mb: 2 }} onClose={() => setSeen(true)}>
      {UNTRUSTED_WORDS} Evidence is marked <strong>untrusted</strong> on every item.
    </Alert>
  )
}

function ItemCard({
  proposalId,
  it,
  notes,
  canReview,
}: {
  proposalId: string
  it: Item
  notes: Note[]
  canReview: boolean
}) {
  const approve = useApprove()
  const navigate = useNavigate()
  const body = useRef<HTMLDivElement>(null)
  const picked = useRef('')
  // selectedQuote is the text selected inside this card, trimmed.
  const selectedQuote = () => {
    const sel = window.getSelection()
    const root = body.current
    if (!sel || sel.isCollapsed || !root || !sel.anchorNode || !root.contains(sel.anchorNode))
      return ''
    return sel.toString().trim()
  }
  const can = (a: string) => canReview && it.actions.includes(a)
  const external = it.kind === 'external'
  const input: unknown = it.input
  const decided = it.state === 'approved' || it.state === 'failed'
  // Send back is one action. With edits or notes they are the message; otherwise a reason is asked.
  const sendBack = can('send_back') || can('rework')
  // One primary per item: Approve, or Send back when the item was edited or has an open note.
  const primary = can('approve') ? 'approve' : sendBack ? 'send_back' : ''
  const more: RowMenuItem[] = [
    ...(sendBack && primary !== 'send_back'
      ? [
          {
            label: 'Send back',
            hint: 'Return it to the agent, with a reason',
            search: { decide: `sendback:${it.id}` },
          },
        ]
      : []),
    ...(can('needs_first')
      ? [
          {
            label: 'Needs first',
            hint: 'Hold this until something else is done',
            search: { decide: `hold:${it.id}` },
          },
        ]
      : []),
    ...(can('edit')
      ? [
          {
            label: 'Edit',
            hint: 'Change the text yourself before approving',
            search: { edit: it.id },
          },
        ]
      : []),
    ...(can('annotate')
      ? [
          {
            label: 'Note',
            hint: 'Attach a note to the words you selected',
            onSelect: () => {
              const quote = picked.current || selectedQuote()
              picked.current = ''
              void navigate({
                to: '.',
                search: (p) => ({ ...p, note: it.id, quote: quote || undefined }),
              })
            },
          },
        ]
      : []),
  ]
  return (
    <Paper variant="outlined" sx={{ p: 1.5 }} data-item={it.id}>
      <Typography variant="subtitle2" component="h3" sx={{ mb: 0.5, overflowWrap: 'anywhere' }}>
        {it.summary}
      </Typography>
      <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 1, flexWrap: 'wrap' }}>
        <StatusChip tone="quiet" label={external ? 'message to send' : 'change in the vault'} />
        <StateChip state={it.state} />
        {it.edited && <StatusChip tone="wait" label="edited by you" />}
        {it.openAnnotations > 0 && (
          <StatusChip
            tone="attn"
            label={`${it.openAnnotations} open ${it.openAnnotations === 1 ? 'note' : 'notes'}`}
          />
        )}
        {it.heldBy && <StatusChip tone="attn" label="needs something first" />}
      </Stack>
      <Box ref={body}>
        {external ? (
          <>
            <Typography variant="body2" sx={{ mb: 0.5 }}>
              {it.change.join('. ')}
            </Typography>
            <Box
              component="pre"
              sx={{
                ...mono,
                my: 1,
                p: 1,
                bgcolor: 'action.hover',
                borderRadius: 1,
                whiteSpace: 'pre-wrap',
                overflowWrap: 'anywhere',
              }}
            >
              {it.draft}
            </Box>
            {it.state === 'pending' && (
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
                {EXTERNAL_PROMISE}
              </Typography>
            )}
          </>
        ) : (
          <>
            <Typography variant="overline" color="text.secondary" component="div">
              The exact change
            </Typography>
            <Box component="ul" aria-label="Exact change" sx={{ m: 0, mb: 1, pl: 2.5 }}>
              {it.change.map((line) => (
                <Typography
                  component="li"
                  variant="body2"
                  key={line}
                  sx={{ overflowWrap: 'anywhere' }}
                >
                  {line}
                </Typography>
              ))}
            </Box>
          </>
        )}
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
      </Box>
      {it.reason && (
        <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
          Reason: {it.reason}
        </Typography>
      )}
      {decided && (it.result || it.state === 'failed') && (
        <Alert severity={it.state === 'failed' ? 'error' : 'success'} sx={{ my: 1 }}>
          {it.state === 'failed' ? (
            it.result
          ) : it.resultName ? (
            <>
              Done. {external ? '' : 'Made '}
              <DocLink id={it.result ?? ''}>{it.resultName}</DocLink>.
            </>
          ) : (
            <>Done. {it.result}</>
          )}
        </Alert>
      )}
      {it.state === 'approved' && external && (
        <Alert
          severity="success"
          sx={{ my: 1 }}
          action={<CopyButton getText={() => it.draft ?? ''} label="Copy draft" />}
        >
          Approved. Not sent yet.
        </Alert>
      )}
      {it.state === 'approved' && !external && !it.result && (
        <Alert severity="success" sx={{ my: 1 }}>
          Approved.
        </Alert>
      )}
      <Details summary="Details">
        {it.approvedHash && (
          <Typography variant="caption" component="div">
            Approved as hash <code>{it.approvedHash}</code>
          </Typography>
        )}
        <Typography variant="caption" component="div">
          Approval covers this exact text. Hash <code>{it.hash}</code>
        </Typography>
        {!external && (
          <>
            <Typography variant="caption" component="div">
              {it.op}
            </Typography>
            <Box
              component="pre"
              sx={{
                ...mono,
                my: 0.5,
                p: 1,
                bgcolor: 'action.hover',
                borderRadius: 1,
                overflowX: 'auto',
              }}
            >
              {JSON.stringify(input ?? {}, null, 2)}
            </Box>
          </>
        )}
      </Details>
      {notes.length > 0 && (
        <List dense disablePadding sx={{ my: 1 }}>
          {notes.map((n) => (
            <ListItem key={n.id} disableGutters sx={{ display: 'block' }}>
              <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                <em>&ldquo;{n.quote}&rdquo;</em>
                {!n.anchored && ' (no longer in the item)'}
              </Typography>
              <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
                {n.note} <StateChip state={n.state} />
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
      {approve.isError && (
        <Alert severity="error" sx={{ my: 1 }}>
          {errorText(approve.error)}
        </Alert>
      )}
      {it.actions.length > 0 && canReview && (
        <Stack direction="row" spacing={1} sx={{ mt: 1, alignItems: 'center' }}>
          {primary === 'approve' && (
            <Button
              size="small"
              variant="contained"
              disabled={approve.isPending}
              onClick={() => approve.mutate({ id: proposalId, items: [it.id] })}
            >
              {it.edited ? 'Approve as edited' : 'Approve'}
            </Button>
          )}
          {primary === 'send_back' && (
            <LinkButton
              size="small"
              variant="contained"
              to="."
              search={(p) => ({ ...p, decide: `sendback:${it.id}` })}
            >
              Send back
            </LinkButton>
          )}
          {can('deny') && (
            <LinkButton
              size="small"
              variant="outlined"
              color="inherit"
              to="."
              search={(p) => ({ ...p, decide: `deny:${it.id}` })}
            >
              Deny
            </LinkButton>
          )}
          <RowMenu
            items={more}
            label="More decisions for this item"
            text="More"
            beforeOpen={() => (picked.current = selectedQuote())}
          />
        </Stack>
      )}
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
                        <Box
                          component="pre"
                          sx={{ ...mono, m: 0, overflowX: 'auto', whiteSpace: 'pre-wrap' }}
                        >
                          {f.lines.map((l, n) => {
                            const k = diffKind(l)
                            return (
                              <Box
                                component="span"
                                key={n}
                                sx={{
                                  display: 'block',
                                  color:
                                    k === 'add'
                                      ? 'success.main'
                                      : k === 'del'
                                        ? 'error.main'
                                        : 'text.secondary',
                                }}
                              >
                                {l}
                              </Box>
                            )
                          })}
                        </Box>
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
