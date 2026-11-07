import { useState } from 'react'
import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import type { ReactNode } from 'react'
import { useIOApprove, useIODone, useIoItem } from '@/api/io'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { DraftEditor } from '@/components/DraftEditor'
import { DoneForm } from '@/components/stack/parts'
import { ioParty, ioTarget } from '@/lib/io'
import { exactTime, humanTime } from '@/lib/when'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'
import { VoiceNote, voiceWords } from '@/components/Voice'

export const Route = createFileRoute('/_authed/io/$id')({ component: IoItemPage })

// One io item. Agents file, send, and drop it; the owner decides in dossiers. With a
// review:decide key the owner may mark an outbox draft sent by themself (Done by me).
function IoItemPage() {
  const { id } = Route.useParams()
  const { session } = Route.useRouteContext()
  const decide = canDecide(session)
  const q = useIoItem(id)
  return (
    <DataState q={q}>
      {({ doc, item }) => {
        const out = item.direction === 'out'
        const from = item.from ? <Party p={item.from} /> : ''
        const to = item.to.length > 0 ? <Parties list={item.to} /> : ''
        const when = item.at ? <span title={exactTime(item.at)}>{humanTime(item.at)}</span> : ''
        const link = item.ref ? (
          <Link href={item.ref} target="_blank" rel="noreferrer" sx={{ overflowWrap: 'anywhere' }}>
            {item.ref}
          </Link>
        ) : item.verify ? (
          item.verify
        ) : item.unverifiable ? (
          <Box component="span" sx={{ color: 'warning.main' }} data-unverifiable>
            No way to verify: no link to the message. Ask the agent for its permalink.
          </Box>
        ) : (
          ''
        )
        const expects =
          item.expects === 'response'
            ? `a response: sending opens an Ask${item.route ? ` (route ${item.route})` : ''}`
            : item.expects === 'none'
              ? 'no response: sending logs an interaction'
              : ''
        const thread = item.thread ? <DocLink id={item.thread} /> : ''
        const became = item.became ? <DocLink id={item.became} /> : ''
        const rows: { k: string; v: ReactNode }[] = [
          { k: out ? 'To' : 'From', v: out ? to : from },
          { k: out ? 'From' : 'To', v: out ? from : to },
          { k: 'Medium', v: item.medium },
          { k: 'Voice', v: out ? voiceWords(item) : '' },
          { k: 'Kind', v: item.kind },
          { k: 'When', v: when },
          { k: 'Subject', v: item.subject },
          { k: 'Link', v: link },
          { k: 'Expects', v: expects },
          { k: 'Answers', v: thread },
          { k: item.state === 'sent' ? 'Became' : 'Filed as', v: became },
          { k: 'Dropped because', v: item.reason },
          {
            k: 'Triage',
            v: item.triage
              ? `${item.triage === 'digest' ? 'digest' : 'needs you'} by ${item.triageBy ?? 'a rule'}`
              : '',
          },
          {
            k: 'Linked to',
            v:
              item.links.length > 0 ? (
                <>
                  {item.links.map((l, i) => (
                    <span key={l}>
                      {i > 0 && ', '}
                      <DocLink id={l} />
                    </span>
                  ))}
                </>
              ) : (
                ''
              ),
          },
        ]
        return (
          <>
            <PageTitle
              action={<StateChip state={item.state} />}
              subtitle={
                out
                  ? 'A message to send. Approve its text here, or edit or drop it; an agent sends it with its own tools after you approve.'
                  : 'A message that came in. An agent files it into a task, question, Ask, note, or interaction.'
              }
            >
              {doc.title || id}
            </PageTitle>
            {item.legacy && (
              <Typography variant="body2" color="warning.main" sx={{ mb: 2 }}>
                An inbox capture from before io. Run para vault upgrade to move it to io/.
              </Typography>
            )}
            <Stack component="dl" spacing={0.5} sx={{ m: 0, mb: 2 }}>
              {rows
                .filter(({ v }) => v !== '' && v !== null && v !== undefined)
                .map(({ k, v }) => (
                  <Stack key={k} direction="row" spacing={1}>
                    <Typography
                      component="dt"
                      variant="body2"
                      color="text.secondary"
                      sx={{ minWidth: 120 }}
                    >
                      {k}
                    </Typography>
                    <Typography
                      component="dd"
                      variant="body2"
                      sx={{ m: 0, overflowWrap: 'anywhere' }}
                    >
                      {v}
                    </Typography>
                  </Stack>
                ))}
            </Stack>
            {out && <VoiceNote item={item} />}
            {decide && out && (item.state === 'draft' || item.state === 'ready') && (
              <Approve
                id={item.id}
                approved={item.state === 'ready'}
                forMe={item.voice === 'for_me'}
              />
            )}
            {decide && out && (
              <SentByMe
                id={item.id}
                text={doc.body}
                open={item.state === 'draft' || item.state === 'ready'}
              />
            )}
            <Section title="Message">
              {decide && out && (item.state === 'draft' || item.state === 'ready') ? (
                <EditableMessage id={item.id} text={doc.body} approved={item.state === 'ready'} />
              ) : (
                <Markdown source={doc.body} />
              )}
            </Section>
            <MergedFrom id={id} />
            <NotesEditor id={id} />
          </>
        )
      }}
    </DataState>
  )
}

// EditableMessage shows an open outgoing draft's text with Edit for the owner. Save keeps the
// text; "and approve" approves it in the same click.
function EditableMessage({ id, text, approved }: { id: string; text: string; approved: boolean }) {
  const [editing, setEditing] = useState(false)
  if (editing) {
    return (
      <DraftEditor
        key={text}
        id={id}
        text={text}
        approved={approved}
        onSaved={() => setEditing(false)}
        onCancel={() => setEditing(false)}
      />
    )
  }
  return (
    <>
      <Markdown source={text} />
      <Button size="small" data-act="edit" onClick={() => setEditing(true)}>
        Edit text
      </Button>
    </>
  )
}

function Party({ p }: { p: string | null | undefined }) {
  const id = ioTarget(p)
  if (id) return <DocLink id={id}>{ioParty(p)}</DocLink>
  return <>{p ?? ''}</>
}

function Parties({ list }: { list: readonly string[] }) {
  if (list.length === 0) return null
  return (
    <>
      {list.map((p, i) => (
        <span key={p}>
          {i > 0 && ', '}
          <Party p={p} />
        </span>
      ))}
    </>
  )
}

// Approve is the owner's approval of an outgoing draft: its exact text, as it is now, becomes ready
// to send. An edit after it makes it a draft again. approved says the draft is approved already.
// A for_me draft becomes as you: its stamp comes off, and you approve the text without it.
function Approve({ id, approved, forMe }: { id: string; approved: boolean; forMe: boolean }) {
  const approve = useIOApprove()
  if (approve.isSuccess || approved) {
    return (
      <Alert severity="success" sx={{ mb: 2 }} data-approved>
        Approved. An agent sends it with its own tools and reports it sent.
      </Alert>
    )
  }
  return (
    <Box sx={{ mb: 1 }}>
      <Button
        size="small"
        variant="contained"
        color="success"
        data-act="approve"
        disabled={approve.isPending}
        onClick={() => approve.mutate(id)}
      >
        {forMe ? 'Approve as you' : 'Approve'}
      </Button>
      {approve.isError && (
        <Typography variant="body2" color="error" sx={{ mt: 0.5 }}>
          {errorText(approve.error)}
        </Typography>
      )}
    </Box>
  )
}

// SentByMe is Done by me on an outbox draft: the owner sent it themself. Para marks it sent and
// files it as an agent's send would. open says the draft is not sent yet.
function SentByMe({ id, text, open: unsent }: { id: string; text: string; open: boolean }) {
  const [open, setOpen] = useState(false)
  const done = useIODone()
  if (done.isSuccess) {
    return (
      <Alert severity="success" sx={{ mb: 2 }}>
        Marked sent by you.
      </Alert>
    )
  }
  if (!unsent) return null
  return (
    <Box sx={{ mb: 2 }}>
      <Button
        size="small"
        variant={open ? 'contained' : 'outlined'}
        data-act="done"
        onClick={() => setOpen((x) => !x)}
      >
        Done by me
      </Button>
      {open && (
        <DoneForm
          start={text}
          textLabel="What you sent (optional)"
          withNote={false}
          busy={done.isPending}
          error={done.isError ? errorText(done.error) : undefined}
          onCancel={() => setOpen(false)}
          onConfirm={(f) =>
            done.mutate({
              id,
              url: f.url.trim() || undefined,
              locator: f.locator.trim() || undefined,
              text: f.text.trim() && f.text !== text ? f.text : undefined,
            })
          }
        />
      )}
    </Box>
  )
}
