import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import Stack from '@mui/material/Stack'
import { useThemeItems, type ThemeItemRow } from '@/api/theme'
import { ItemActions } from '@/components/AgendaActions'
import { Section } from '@/components/Bits'
import { BulkList, SelectBox } from '@/components/BulkSelect'
import { DocLink } from '@/components/DocLink'
import { PartOfLinks } from '@/components/PartOf'
import { rowKind } from '@/lib/agenda'

// ItemsUnder is "Items under this" on a goal, obligation, parent task, or tag page: the open items
// that hang on it (theme_items), each with why and what else it is part of. Select picks several
// for one decision (Later, Done, Drop, Reopen); Open opens one. hide leaves out rows the page lists
// already.
export function ItemsUnder({
  theme,
  hide,
  title = 'Items under this',
}: {
  theme: string
  hide?: (x: ThemeItemRow) => boolean
  title?: string
}) {
  const q = useThemeItems(theme)
  const rows = (q.data?.items ?? []).filter((x) => !hide?.(x))
  return (
    <Section title={title} count={rows.length} hideEmpty>
      <BulkList ids={rows.map((x) => x.id)}>
        <List dense disablePadding data-items-under={theme}>
          {rows.map((x) => (
            <ListItem key={x.id} disableGutters divider sx={{ display: 'block' }}>
              <Stack direction="row" spacing={1} sx={{ alignItems: 'flex-start' }}>
                <SelectBox id={x.id} title={x.title} />
                <ListItemText
                  primary={<DocLink id={x.id}>{x.title || x.id}</DocLink>}
                  secondary={
                    <>
                      {x.whyText}
                      {x.life === 'later' && ' - put off'}
                      <br />
                      <PartOfLinks parts={x.partOf} omit={q.data?.theme} />
                    </>
                  }
                  slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
                />
                <ItemActions
                  x={{ id: x.id, title: x.title, kind: rowKind(x.type, x.kind) }}
                  deferred={x.life === 'later'}
                />
              </Stack>
            </ListItem>
          ))}
        </List>
      </BulkList>
    </Section>
  )
}

// underGoalOnly: a task or question whose only tie to the page is its goal; the goal page lists
// those in its own Tasks and Questions.
export const underGoalOnly = (x: ThemeItemRow) =>
  (x.type === 'task' || x.type === 'question') && x.why.length === 1 && x.why[0] === 'goal'
