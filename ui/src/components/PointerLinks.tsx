import Link from '@mui/material/Link'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { Empty, Section } from '@/components/Bits'

type Pointer = { kind: string; label: string; target: string }

// PointerLinks lists where plans and docs live. Para holds the pointer, not the document.
export function PointerLinks({ links }: { links: Pointer[] | null | undefined }) {
  const list = links ?? []
  return (
    <Section title="Links" count={list.length}>
      {list.length === 0 ? (
        <Empty>No link. Para points to plans and docs; it does not hold them.</Empty>
      ) : (
        <List dense disablePadding>
          {list.map((l) => (
            <ListItem key={`${l.kind}-${l.target}`} disableGutters>
              <ListItemText
                primary={
                  /^https?:\/\//.test(l.target) ? (
                    <Link href={l.target} target="_blank" rel="noreferrer noopener">
                      {l.label || l.target}
                    </Link>
                  ) : (
                    l.label || l.target
                  )
                }
                secondary={`${l.kind} - ${l.target}`}
                slotProps={{ secondary: { sx: { overflowWrap: 'anywhere' } } }}
              />
            </ListItem>
          ))}
        </List>
      )}
    </Section>
  )
}
