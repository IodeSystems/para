import { createFileRoute } from '@tanstack/react-router'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useHealth } from '@/api/health'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { dateWords } from '@/lib/when'

export const Route = createFileRoute('/_authed/health')({ component: Health })

function Health() {
  const q = useHealth()
  return (
    <>
      <PageTitle subtitle="Gaps in the vault, by goal. Ask your agent in the chat to fix or dismiss one.">
        Health
      </PageTitle>
      <DataState q={q}>
        {(h) =>
          h.groups.length === 0 ? (
            <Empty>Nothing to flag.{h.dismissed > 0 ? ` ${h.dismissed} dismissed.` : ''}</Empty>
          ) : (
            <>
              {h.groups.map((g) => (
                <Section
                  key={g.goal}
                  title={g.due ? `${g.title} (due ${dateWords(g.due)})` : g.title}
                  count={g.findings.length}
                >
                  <List dense disablePadding>
                    {g.findings.map((f) => (
                      <ListItem key={`${f.kind}-${f.id}`} disableGutters>
                        <ListItemText
                          primary={<DocLink id={f.id}>{f.title || f.id}</DocLink>}
                          secondary={`${f.label}: ${f.message}`}
                          slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                        />
                      </ListItem>
                    ))}
                  </List>
                </Section>
              ))}
              {h.dismissed > 0 && <Empty>{h.dismissed} dismissed.</Empty>}
            </>
          )
        }
      </DataState>
    </>
  )
}
