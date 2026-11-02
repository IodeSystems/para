import Chip from '@mui/material/Chip'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { TextLink } from '@/components/links'

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
          <Chip
            size="small"
            label={i.severity}
            color={i.severity === 'sev1' ? 'error' : i.severity === 'sev2' ? 'warning' : 'default'}
            sx={{ mr: 0.5 }}
          />
          <Chip size="small" variant="outlined" label={i.status} />
        </ListItem>
      ))}
    </List>
  )
}
