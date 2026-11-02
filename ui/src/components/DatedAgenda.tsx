import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import { useAgenda, useCalendarExport } from '@/api/meetings'
import { Empty, Section, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { RowMenu } from '@/components/RowMenu'
import { AgentLooking, findContextItem } from '@/components/FindContext'
import { useLooking } from '@/api/requests'
import { errorText } from '@/client/gqlClient'
import { downloadText } from '@/lib/download'
import { meetingWhen, viewerZone } from '@/lib/meeting'
import { joinNames, nameList } from '@/lib/names'
import { dateWords } from '@/lib/when'

// DatedAgenda is the Agenda grouped by date: dated items by day, with the row actions of their kind.
const KIND_WORDS: Record<string, string> = {
  meeting: 'meeting',
  due: 'due',
  nudge: 'follow up',
  exception: 'review',
}

export function DatedAgenda() {
  const [days, setDays] = useState(7)
  const zone = viewerZone()
  const q = useAgenda(days, zone)
  const looking = useLooking()
  const exp = useCalendarExport()
  const [said, setSaid] = useState('')
  const doExport = () =>
    exp.mutate(
      { days, zone },
      {
        onSuccess: (r) => {
          const saved = downloadText(`para-agenda.${r.ext}`, r.text, 'text/calendar')
          setSaid(
            saved
              ? `Calendar exported: ${r.events} event${r.events === 1 ? '' : 's'} saved as para-agenda.${r.ext}.`
              : `Calendar rendered with ${r.events} events, but this browser could not save the file.`,
          )
        },
      },
    )
  return (
    <>
      <Stack
        direction="row"
        spacing={1}
        sx={{ mb: 2, alignItems: 'center', flexWrap: 'wrap' }}
        useFlexGap
      >
        <TextField
          select
          size="small"
          label="Days"
          value={days}
          onChange={(e) => setDays(Number(e.target.value))}
          sx={{ minWidth: 120 }}
        >
          {[1, 3, 7, 14, 30].map((n) => (
            <MenuItem key={n} value={n}>
              {n === 1 ? 'Today' : `${n} days`}
            </MenuItem>
          ))}
        </TextField>
        <Button variant="outlined" size="small" disabled={exp.isPending} onClick={doExport}>
          Export calendar
        </Button>
      </Stack>
      {exp.isError && (
        <Alert severity="error" sx={{ mb: 2 }}>
          {errorText(exp.error)}
        </Alert>
      )}
      {said && (
        <Alert severity="success" onClose={() => setSaid('')} sx={{ mb: 2 }}>
          {said}
        </Alert>
      )}
      <DataState q={q}>
        {(a) => {
          const dates = [...new Set(a.items.map((i) => i.date))]
          return a.items.length === 0 ? (
            <Empty>
              Nothing is dated from {dateWords(a.from)} to {dateWords(a.to)}.
            </Empty>
          ) : (
            <>
              {dates.map((d) => {
                const items = a.items.filter((i) => i.date === d)
                return (
                  <Section key={d} title={dateWords(d)} count={items.length}>
                    <List dense disablePadding>
                      {items.map((i) => (
                        <ListItem key={`${i.kind}-${i.id}`} disableGutters>
                          <ListItemText
                            primary={
                              i.kind === 'meeting' ? (
                                <TextLink
                                  to="/meetings/$id"
                                  params={{ id: i.id }}
                                  underline="hover"
                                >
                                  {i.title}
                                </TextLink>
                              ) : (
                                <DocLink id={i.id}>{i.title}</DocLink>
                              )
                            }
                            secondary={[
                              i.kind === 'meeting' ? meetingWhen(i.at, i.until) : i.time,
                              i.detail,
                              i.why,
                              i.who.length > 0 && `with ${joinNames(nameList(i.who, i.whoNames))}`,
                              i.projectName && `goal ${i.projectName}`,
                            ]
                              .filter(Boolean)
                              .join(' - ')}
                            slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                          />
                          <StatusChip
                            tone={i.overdue ? 'bad' : 'quiet'}
                            label={i.overdue ? 'overdue' : (KIND_WORDS[i.kind] ?? i.kind)}
                          />
                          <AgentLooking id={i.id} />
                          {(i.kind === 'meeting' || i.kind === 'nudge') && (
                            <RowMenu
                              label={`More actions for ${i.title}`}
                              always
                              items={[findContextItem(i.id, looking.has(i.id))]}
                            />
                          )}
                          {i.kind === 'due' && (
                            <RowMenu
                              label={`More actions for ${i.title}`}
                              always
                              items={[
                                findContextItem(i.id, looking.has(i.id)),
                                {
                                  label: 'Done',
                                  hint: 'Mark this task finished',
                                  search: { done: i.id },
                                },
                                {
                                  label: 'Later',
                                  hint: 'Put this off. It leaves the Agenda and next',
                                  search: { later: i.id },
                                },
                                {
                                  label: 'Cut a dependency',
                                  hint: 'Let it go ahead without something it waits on',
                                  search: { cut: i.id },
                                },
                              ]}
                            />
                          )}
                        </ListItem>
                      ))}
                    </List>
                  </Section>
                )
              })}
            </>
          )
        }}
      </DataState>
    </>
  )
}
