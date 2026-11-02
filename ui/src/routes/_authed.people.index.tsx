import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import TextField from '@mui/material/TextField'
import { TextLink } from '@/components/links'
import { usePeople, useWho } from '@/api/people'
import { Empty, PageTitle } from '@/components/Bits'
import { DataState } from '@/components/DataState'

export const Route = createFileRoute('/_authed/people/')({
  validateSearch: (s: Record<string, unknown>): { q?: string } => ({
    q: typeof s.q === 'string' && s.q !== '' ? s.q : undefined,
  }),
  component: People,
})

function People() {
  const { q = '' } = Route.useSearch()
  const navigate = Route.useNavigate()
  const [text, setText] = useState(q)
  const all = usePeople()
  const who = useWho(q)
  return (
    <>
      <PageTitle>People</PageTitle>
      <TextField
        label="Who (name, alias, email, handle)"
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          void navigate({
            search: (p) => ({ ...p, q: e.target.value || undefined }),
            replace: true,
          })
        }}
        fullWidth
        type="search"
        sx={{ mb: 2 }}
      />
      {q !== '' && (
        <DataState q={who}>
          {(w) =>
            w.person ? (
              <List>
                <ListItem disableGutters>
                  <ListItemText
                    primary={
                      <TextLink to="/people/$id" params={{ id: w.person.id }}>
                        {w.person.name}
                      </TextLink>
                    }
                    secondary={w.person.org}
                  />
                </ListItem>
              </List>
            ) : w.candidates.length > 0 ? (
              <List>
                {w.candidates.map((c) => (
                  <ListItem key={c.id} disableGutters>
                    <ListItemText
                      primary={
                        <TextLink to="/people/$id" params={{ id: c.id }}>
                          {c.title || c.id}
                        </TextLink>
                      }
                    />
                  </ListItem>
                ))}
              </List>
            ) : (
              <Empty>Nobody matches.</Empty>
            )
          }
        </DataState>
      )}
      {q === '' && (
        <DataState q={all}>
          {(people) =>
            people.length === 0 ? (
              <Empty>No people yet.</Empty>
            ) : (
              <List>
                {people.map((p) => (
                  <ListItem key={p.id} disableGutters divider>
                    <ListItemText
                      primary={
                        <TextLink to="/people/$id" params={{ id: p.id }}>
                          {p.title || p.id}
                        </TextLink>
                      }
                    />
                  </ListItem>
                ))}
              </List>
            )
          }
        </DataState>
      )}
    </>
  )
}
