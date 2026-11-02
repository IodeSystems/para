import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { StateChip, StatusChip } from '@/components/Bits'
import { TextLink } from '@/components/links'
import { toneOfSeverity } from '@/lib/status'

type Row = {
  id: string
  title: string
  severity: string
  status: string
  started: string
  ended?: string | null
}

export function IncidentRows({ list }: { list: Row[] }) {
  return (
    <List dense disablePadding>
      {list.map((i) => (
        <ListItem key={i.id} disableGutters>
          <ListItemText
            primary={
              <TextLink to="/incidents/$id" params={{ id: i.id }} underline="hover">
                {i.title || i.id}
              </TextLink>
            }
            secondary={`started ${i.started}${i.ended ? `, ended ${i.ended}` : ''}`}
          />
          <Stack direction="row" spacing={0.5}>
            <StatusChip tone={toneOfSeverity(i.severity)} label={i.severity} />
            <StateChip state={i.status} tone={i.status === 'open' ? 'attn' : undefined} />
          </Stack>
        </ListItem>
      ))}
    </List>
  )
}
