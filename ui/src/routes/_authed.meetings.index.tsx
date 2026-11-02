import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import FormControlLabel from '@mui/material/FormControlLabel'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Switch from '@mui/material/Switch'
import { useMeetings } from '@/api/meetings'
import { CHIP_ROW, Empty, PageTitle } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { LinkButton, TextLink } from '@/components/links'
import { RowMenu } from '@/components/RowMenu'
import { AgentLooking, findContextItem } from '@/components/FindContext'
import { useLooking } from '@/api/requests'
import { MeetingChip } from '@/components/MeetingChip'
import { meetingWhen } from '@/lib/meeting'
import { joinNames, nameList } from '@/lib/names'

export const Route = createFileRoute('/_authed/meetings/')({ component: Meetings })

function Meetings() {
  const looking = useLooking()
  const [all, setAll] = useState(false)
  const q = useMeetings(all)
  return (
    <>
      <PageTitle
        subtitle="Meetings you asked for or booked. Booked ones come first, by start time."
        action={
          <LinkButton size="small" to="/agenda">
            Agenda
          </LinkButton>
        }
      >
        Meetings
      </PageTitle>
      <FormControlLabel
        sx={{ mb: 1 }}
        control={<Switch checked={all} onChange={(e) => setAll(e.target.checked)} />}
        label="Show held and cancelled"
      />
      <DataState q={q}>
        {(items) =>
          items.length === 0 ? (
            <Empty>{all ? 'No meetings.' : 'No requested or scheduled meetings.'}</Empty>
          ) : (
            <List disablePadding>
              {items.map((m) => (
                <ListItem key={m.id} disableGutters divider sx={CHIP_ROW}>
                  <ListItemText
                    primary={
                      <TextLink to="/meetings/$id" params={{ id: m.id }} underline="hover">
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
                  <RowMenu
                    label={`More actions for ${m.title || m.id}`}
                    always
                    items={[findContextItem(m.id, looking.has(m.id))]}
                  />
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
