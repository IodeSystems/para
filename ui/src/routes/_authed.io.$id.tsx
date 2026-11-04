import { createFileRoute } from '@tanstack/react-router'
import Link from '@mui/material/Link'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import type { ReactNode } from 'react'
import { useIoItem } from '@/api/io'
import { PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { ioParty, ioTarget } from '@/lib/io'
import { exactTime, humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/io/$id')({ component: IoItemPage })

// One io item: read only. Agents file, send, and drop it; the owner decides in dossiers.
function IoItemPage() {
  const { id } = Route.useParams()
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
          { k: 'When', v: when },
          { k: 'Subject', v: item.subject },
          { k: 'Link', v: link },
          { k: 'Expects', v: expects },
          { k: 'Answers', v: thread },
          { k: item.state === 'sent' ? 'Became' : 'Filed as', v: became },
          { k: 'Dropped because', v: item.reason },
        ]
        return (
          <>
            <PageTitle
              action={<StateChip state={item.state} />}
              subtitle={
                out
                  ? 'A message to send. An agent sends it with its own tools after you approve it in a dossier.'
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
            <Section title="Message">
              <Markdown source={doc.body} />
            </Section>
          </>
        )
      }}
    </DataState>
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
