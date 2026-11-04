import { createFileRoute } from '@tanstack/react-router'
import FormControlLabel from '@mui/material/FormControlLabel'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import Switch from '@mui/material/Switch'
import { useDocTracked } from '@/api/docs'
import { CHIP_ROW, Empty, PageTitle, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { TextLink } from '@/components/links'
import { humanTime } from '@/lib/when'

export const Route = createFileRoute('/_authed/docs/')({
  validateSearch: (s: Record<string, unknown>): { unseen?: boolean } => ({
    unseen: s.unseen === true || s.unseen === 'true' ? true : undefined,
  }),
  component: DocsPage,
})

// DocsPage lists tracked documents: the ones with versions you have not seen first, with the
// count of open comments.
function DocsPage() {
  const { unseen } = Route.useSearch()
  const navigate = Route.useNavigate()
  const q = useDocTracked(unseen ?? false)
  return (
    <>
      <PageTitle subtitle="Remote documents you follow. Agents read them and store each version; you read what changed and mark it seen.">
        Docs
      </PageTitle>
      <Stack direction="row" spacing={2} sx={{ mb: 2, alignItems: 'center' }}>
        <FormControlLabel
          control={
            <Switch
              checked={unseen ?? false}
              onChange={(e) =>
                void navigate({
                  search: (p) => ({ ...p, unseen: e.target.checked || undefined }),
                  replace: true,
                })
              }
            />
          }
          label="Only with changes not seen"
        />
      </Stack>
      <DataState q={q}>
        {(d) =>
          d.docs.length === 0 ? (
            <Empty>No tracked documents.</Empty>
          ) : (
            <List disablePadding aria-label="Tracked documents">
              {d.docs.map((x) => (
                <ListItem key={x.id} disableGutters divider sx={CHIP_ROW} data-doc={x.id}>
                  <ListItemText
                    primary={
                      <TextLink to="/docs/$id" params={{ id: x.id }} underline="hover">
                        {x.title || x.id}
                      </TextLink>
                    }
                    secondary={[
                      x.system,
                      `version ${x.latest}`,
                      x.lastSeen > 0 ? `seen to ${x.lastSeen}` : 'not seen',
                      x.checked && `read ${humanTime(x.checked)}`,
                    ]
                      .filter(Boolean)
                      .join(' - ')}
                    slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                  />
                  {x.unseen > 0 && (
                    <StatusChip
                      tone="attn"
                      label={`${x.unseen} new version${x.unseen === 1 ? '' : 's'}`}
                    />
                  )}
                  {x.openComments > 0 && (
                    <StatusChip
                      tone="wait"
                      label={`${x.openComments} open comment${x.openComments === 1 ? '' : 's'}`}
                    />
                  )}
                  {x.status !== 'watching' && <StateChip state={x.status} />}
                </ListItem>
              ))}
            </List>
          )
        }
      </DataState>
    </>
  )
}
