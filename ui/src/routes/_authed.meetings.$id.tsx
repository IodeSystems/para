import { createFileRoute } from '@tanstack/react-router'
import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Snackbar from '@mui/material/Snackbar'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useMeeting } from '@/api/meetings'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink, RfxLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { CancelDialog, HoldDialog, ScheduleDialog } from '@/components/MeetingDialogs'
import { MeetingChip } from '@/components/MeetingChip'
import { meetingWhen } from '@/lib/meeting'
import { nameList } from '@/lib/names'

export const Route = createFileRoute('/_authed/meetings/$id')({ component: MeetingPage })

type Dialog = '' | 'schedule' | 'cancel' | 'hold'

function MeetingPage() {
  const { id } = Route.useParams()
  const q = useMeeting(id)
  const [dialog, setDialog] = useState<Dialog>('')
  const [toast, setToast] = useState('')
  const close = () => setDialog('')
  return (
    <DataState q={q}>
      {({ meeting: m, body }) => {
        const live = m.status === 'requested' || m.status === 'scheduled'
        return (
          <>
            <PageTitle
              action={
                live ? (
                  <Stack direction="row" spacing={1}>
                    <Button size="small" variant="outlined" onClick={() => setDialog('schedule')}>
                      {m.status === 'scheduled' ? 'Reschedule' : 'Schedule'}
                    </Button>
                    <Button size="small" variant="contained" onClick={() => setDialog('hold')}>
                      Hold
                    </Button>
                    <Button size="small" color="error" onClick={() => setDialog('cancel')}>
                      Cancel
                    </Button>
                  </Stack>
                ) : undefined
              }
            >
              {m.title || id}
            </PageTitle>
            <Stack direction="row" spacing={1} sx={{ mb: 2, alignItems: 'center' }}>
              <MeetingChip status={m.status} />
              <Typography variant="body2" color="text.secondary">
                {meetingWhen(m.start, m.end)}
              </Typography>
            </Stack>
            <Section title="With" count={m.with.length}>
              {m.with.length === 0 ? (
                <Empty>Nobody is named yet.</Empty>
              ) : (
                <Typography variant="body2">{nameList(m.with, m.withNames).join(', ')}</Typography>
              )}
            </Section>
            <Section title="Asks it should settle" count={m.rfx.length} hideEmpty>
              <List dense disablePadding>
                {m.rfx.map((r, i) => (
                  <ListItem key={r} disableGutters>
                    <ListItemText
                      primary={<RfxLink id={r}>{nameList(m.rfx, m.rfxNames)[i]}</RfxLink>}
                    />
                  </ListItem>
                ))}
              </List>
            </Section>
            <Section title="Questions and notes">
              <Markdown source={body} />
            </Section>
            <Typography variant="caption" color="text.secondary">
              <DocLink id={m.id}>Open the document</DocLink>
            </Typography>
            {dialog === 'schedule' && <ScheduleDialog m={m} onClose={close} onDone={setToast} />}
            {dialog === 'cancel' && <CancelDialog m={m} onClose={close} onDone={setToast} />}
            {dialog === 'hold' && (
              <HoldDialog m={m} body={body} onClose={close} onDone={setToast} />
            )}
            <Snackbar
              open={toast !== ''}
              autoHideDuration={7000}
              onClose={() => setToast('')}
              anchorOrigin={{ vertical: 'top', horizontal: 'right' }}
              sx={{ top: { xs: 60, md: 108 } }}
            >
              <Alert severity="success" variant="filled" onClose={() => setToast('')}>
                {toast}
              </Alert>
            </Snackbar>
          </>
        )
      }}
    </DataState>
  )
}
