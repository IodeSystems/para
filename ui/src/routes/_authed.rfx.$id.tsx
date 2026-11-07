import { createFileRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Typography from '@mui/material/Typography'
import { useSeenOnOpen } from '@/api/requests'
import { useRfx } from '@/api/rfx'
import { useCopyText } from '@/api/doc'
import { Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { NextMoves } from '@/components/NextMoves'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { ItemContext } from '@/components/ItemContext'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { withoutSection } from '@/lib/markdown'
import { nameList, nameOr } from '@/lib/names'
import { dateWords } from '@/lib/when'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'
import { ItemComposer } from '@/components/ItemComposer'
import { ItemDraftProvider } from '@/components/ItemDraft'

export const Route = createFileRoute('/_authed/rfx/$id')({ component: RfxPage })

function RfxPage() {
  const { id } = Route.useParams()
  // Opening the item, or the agent work itself, marks its finished agent work seen.
  useSeenOnOpen(id)
  const q = useRfx(id)
  const copy = useCopyText()
  return (
    <DataState q={q}>
      {(r) => (
        <ItemDraftProvider id={r.doc.id}>
          <PageTitle>{r.doc.title || id}</PageTitle>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {r.doc.meta.direction === 'in' ? 'Asked of you' : 'You ask'} - {r.doc.meta.status}
            {r.route ? ` - via ${nameOr(r.routeName, r.route)}` : ''}
            {r.doc.meta.nextNudge ? ` - follow up ${dateWords(r.doc.meta.nextNudge)}` : ''}
          </Typography>
          <ItemContext id={id} />
          <Section title="Asked of" count={r.targets.length}>
            {r.targets.length === 0 ? (
              <Empty>Nobody is named yet.</Empty>
            ) : (
              <List dense disablePadding>
                {r.targets.map((t, i) => (
                  <ListItem key={`${t.to}-${t.medium}`} disableGutters>
                    <ListItemText
                      primary={nameOr(r.targetNames?.[i], t.to)}
                      secondary={[t.medium, t.value, t.sent && `sent ${t.sent}`]
                        .filter(Boolean)
                        .join(' - ')}
                    />
                    <StateChip state={t.state} />
                  </ListItem>
                ))}
              </List>
            )}
          </Section>
          {r.doc.meta.direction !== 'in' && r.doc.meta.status === 'open' && <NextMoves id={id} />}
          {(r.chain.length > 0 || r.dependedOnBy.length > 0) && (
            <Section title="Chain" count={r.chain.length}>
              <List dense disablePadding>
                {r.chain.map((n) => (
                  <ListItem key={n.id} disableGutters sx={{ pl: n.depth * 2 }}>
                    <ListItemText
                      primary={<DocLink id={n.id}>{n.title || nameOr('', n.id)}</DocLink>}
                      secondary={`${n.type} - ${n.status}`}
                    />
                  </ListItem>
                ))}
              </List>
              {r.dependedOnBy.length > 0 && (
                <Typography variant="caption" color="text.secondary">
                  Needed by {nameList(r.dependedOnBy, r.dependedOnByNames).join(', ')}
                </Typography>
              )}
            </Section>
          )}
          {(r.draft || r.doc.meta.direction !== 'in') && (
            <Section
              title="Draft"
              action={
                r.draft ? (
                  <CopyButton
                    label="Copy"
                    getText={async () => (await copy.mutateAsync({ id, as: 'plain' })).text}
                  />
                ) : undefined
              }
            >
              {r.draft ? (
                <Box sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{r.draft}</Box>
              ) : (
                <Empty>No message is drafted yet.</Empty>
              )}
            </Section>
          )}
          {withoutSection(r.doc.body, 'Draft') && (
            <Section title="Ask">
              <Markdown source={withoutSection(r.doc.body, 'Draft')} />
            </Section>
          )}
          <MergedFrom id={id} />
          <NotesEditor id={id} />
          <ItemComposer
            id={r.doc.id}
            title={r.doc.title}
            meta={{ type: 'rfx', status: r.doc.meta.status }}
          />
        </ItemDraftProvider>
      )}
    </DataState>
  )
}
