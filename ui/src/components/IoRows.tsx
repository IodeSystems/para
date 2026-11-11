import type { ReactNode } from 'react'
import Link from '@mui/material/Link'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import type { useIoList } from '@/api/io'
import { CHIP_ROW, StateChip, StatusChip } from '@/components/Bits'
import { TextLink } from '@/components/links'
import { PinButton } from '@/components/PinButton'
import { voiceWords } from '@/components/Voice'
import { ioParty } from '@/lib/io'
import { humanTime } from '@/lib/when'

export type IoRow = NonNullable<ReturnType<typeof useIoList>['data']>['items'][number]

export function IoRows({
  items,
  extra,
  lead,
}: {
  items: IoRow[]
  // extra renders under a row's text (made chips); lead renders before it (a checkbox).
  extra?: (x: IoRow) => ReactNode
  lead?: (x: IoRow) => ReactNode
}) {
  return (
    <List disablePadding>
      {items.map((x) => {
        const who = x.direction === 'in' ? ioParty(x.from) : x.to.map(ioParty).join(', ')
        return (
          <ListItem key={x.id} disableGutters divider sx={CHIP_ROW}>
            {lead?.(x)}
            <ListItemText
              primary={
                <TextLink to="/io/$id" params={{ id: x.id }} underline="hover">
                  {x.title || 'Untitled message'}
                </TextLink>
              }
              secondary={[
                who && (x.direction === 'in' ? `from ${who}` : `to ${who}`),
                x.medium,
                humanTime(x.at || x.created),
                x.direction === 'in' && x.needsYouReason,
                x.voice === 'for_me' && voiceWords(x),
                x.legacy && 'old inbox capture: run para vault upgrade',
              ]
                .filter(Boolean)
                .join(' - ')}
              slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
            />
            {extra?.(x)}
            {x.ref && (
              <Link href={x.ref} target="_blank" rel="noreferrer" variant="body2" sx={{ mr: 1 }}>
                link
              </Link>
            )}
            {x.unverifiable && <StatusChip tone="attn" label="no way to verify" />}
            <StateChip state={x.state} />
            <PinButton id={x.id} />
          </ListItem>
        )
      })}
    </List>
  )
}
