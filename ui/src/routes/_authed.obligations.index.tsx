import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import FormControlLabel from '@mui/material/FormControlLabel'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Switch from '@mui/material/Switch'
import { useMeetings, useObligations } from '@/api/meetings'
import { CHIP_ROW, Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { LinkButton, TextLink } from '@/components/links'
import { AgentLooking } from '@/components/FindContext'
import { MeetingChip } from '@/components/MeetingChip'
import { meetingWhen } from '@/lib/meeting'
import { joinNames, nameList } from '@/lib/names'

export const Route = createFileRoute('/_authed/obligations/')({ component: Obligations })

function Obligations() {
  const [all, setAll] = useState(false)
  const meetings = useMeetings(all)
  const owed = useObligations(all)
  const meetingIds = new Set((meetings.data ?? []).map((m) => m.id))
  return (
    <>
      <PageTitle
        subtitle="What you owe or must attend: meetings you asked for or booked (booked first, by start time), and what you owe people."
        action={
          <LinkButton size="small" to="/agenda">
            Agenda
          </LinkButton>
        }
      >
        Obligations
      </PageTitle>
      <FormControlLabel
        sx={{ mb: 1 }}
        control={<Switch checked={all} onChange={(e) => setAll(e.target.checked)} />}
        label="Show closed"
      />
      <Section title="Meetings" count={meetings.data?.length}>
        <DataState q={meetings}>
          {(items) =>
            items.length === 0 ? (
              <Empty>{all ? 'No meetings.' : 'No requested or scheduled meetings.'}</Empty>
            ) : (
              <List disablePadding>
                {items.map((m) => (
                  <ListItem key={m.id} disableGutters divider sx={CHIP_ROW}>
                    <ListItemText
                      primary={
                        <TextLink to="/obligations/$id" params={{ id: m.id }} underline="hover">
                          {m.title || m.id}
                        </TextLink>
                      }
                      secondary={[
                        meetingWhen(m.start, m.end),
                        m.with.length > 0 && `with ${joinNames(nameList(m.with, m.withNames))}`,
                      ]
                        .filter(Boolean)
                        .join(' - ')}
                      slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                    />
                    <AgentLooking id={m.id} />
                    <MeetingChip status={m.status} />
                  </ListItem>
                ))}
              </List>
            )
          }
        </DataState>
      </Section>
      <Section title="Owed" count={(owed.data ?? []).filter((r) => !meetingIds.has(r.id)).length}>
        <DataState q={owed}>
          {(rows) => {
            const list = rows.filter((r) => !meetingIds.has(r.id))
            return list.length === 0 ? (
              <Empty>Nothing owed.</Empty>
            ) : (
              <List disablePadding data-owed-list>
                {list.map((r) => (
                  <ListItem key={r.id} disableGutters divider data-obligation-row={r.id}>
                    <ListItemText
                      primary={
                        <TextLink to="/obligations/$id" params={{ id: r.id }} underline="hover">
                          {r.title || r.id}
                        </TextLink>
                      }
                      secondary={[r.life !== 'open' && r.life, r.reason]
                        .filter(Boolean)
                        .join(' - ')}
                    />
                    <AgentLooking id={r.id} />
                  </ListItem>
                ))}
              </List>
            )
          }}
        </DataState>
      </Section>
    </>
  )
}
