import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useWho } from '@/api/people'
import { nameOr } from '@/lib/names'
import { dateWords } from '@/lib/when'
import { Empty, PageTitle, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink, RfxLink } from '@/components/DocLink'

export const Route = createFileRoute('/_authed/people/$id')({ component: Person })

function Person() {
  const { id } = Route.useParams()
  const q = useWho(id)
  return (
    <DataState q={q}>
      {(w) => {
        const p = w.person
        if (!p) {
          return (
            <>
              <PageTitle>{id}</PageTitle>
              <Empty>No such person.</Empty>
            </>
          )
        }
        return (
          <>
            <PageTitle>{p.name}</PageTitle>
            <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
              {[p.org, p.role?.title, p.team?.title].filter(Boolean).join(' - ')}
            </Typography>
            <Stack direction="row" sx={{ mb: 2, flexWrap: 'wrap', gap: 0.5 }}>
              {p.aliases.map((a) => (
                <StatusChip key={a} tone="quiet" label={a} />
              ))}
              {p.groups.map((g) => (
                <StatusChip key={g.id} tone="quiet" label={g.title || nameOr('', g.id)} />
              ))}
            </Stack>
            {p.contacts.length + p.openRfx.length + p.interactions.length + p.notes.length ===
              0 && <Empty>Nothing recorded about this person yet.</Empty>}
            <Section title="Contact" count={p.contacts.length} hideEmpty>
              {p.contacts.length === 0 ? (
                <Empty>No contact method recorded.</Empty>
              ) : (
                <List dense disablePadding>
                  {p.contacts.map((c) => (
                    <ListItem key={`${c.method}-${c.value}`} disableGutters>
                      <ListItemText
                        primary={c.value}
                        secondary={[c.method, c.label, c.preferred && 'preferred']
                          .filter(Boolean)
                          .join(' - ')}
                      />
                    </ListItem>
                  ))}
                </List>
              )}
            </Section>
            <Section title="Waiting on them" count={p.openRfx.length} hideEmpty>
              {p.openRfx.length === 0 ? (
                <Empty>No open request.</Empty>
              ) : (
                <List dense disablePadding>
                  {p.openRfx.map((r) => (
                    <ListItem key={r.id} disableGutters>
                      <ListItemText primary={<RfxLink id={r.id}>{r.title || r.id}</RfxLink>} />
                    </ListItem>
                  ))}
                </List>
              )}
            </Section>
            <Section title="Recent contact" count={p.interactions.length} hideEmpty>
              {p.interactions.length === 0 ? (
                <Empty>No interaction logged.</Empty>
              ) : (
                <List dense disablePadding>
                  {p.interactions.map((i) => (
                    <ListItem key={i.id} disableGutters>
                      <ListItemText
                        primary={<DocLink id={i.id}>{i.title || i.id}</DocLink>}
                        secondary={`${dateWords(i.date)} - ${i.medium}`}
                      />
                    </ListItem>
                  ))}
                </List>
              )}
            </Section>
            {p.notes.length > 0 && (
              <Section title="Notes" count={p.notes.length}>
                <List dense disablePadding>
                  {p.notes.map((n) => (
                    <ListItem key={n} disableGutters>
                      <ListItemText primary={n} sx={{ overflowWrap: 'anywhere' }} />
                    </ListItem>
                  ))}
                </List>
              </Section>
            )}
          </>
        )
      }}
    </DataState>
  )
}
