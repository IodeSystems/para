import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { Section, StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { needsOwner, sentStatus } from '@/lib/sent'

// WaitingOnYou is agent work that ended and waits for the owner, so the needs-you view of the
// stack lists it above the items: a turn that ended stuck, blocked, or needs clarification (the
// owner answers in the chat, or with para request respond), and a result not yet seen (opening the
// work marks it seen).
export function WaitingOnYou({
  items,
}: {
  items: {
    request: string
    title: string
    item?: string | null
    itemName?: string | null
    note?: string | null
    status: string
    resolution?: string | null
    summary?: string | null
    queuedAt: string
  }[]
}) {
  if (items.length === 0) return null
  return (
    <Section title="Agent work waiting on you" count={items.length}>
      <List dense disablePadding aria-label="Agent work waiting on you">
        {items.map((it) => {
          const s = sentStatus(it)
          const respond = it.status === 'done' && needsOwner(it.resolution)
          return (
            <ListItem key={it.request} disableGutters data-waits={it.request}>
              <ListItemText
                primary={
                  <>
                    <DocLink id={it.request}>{it.title || 'Agent work'}</DocLink>
                    {it.item && (
                      <>
                        {' about '}
                        <DocLink id={it.item}>{it.itemName || it.item}</DocLink>
                      </>
                    )}
                  </>
                }
                secondary={[
                  it.summary && `Agent: ${it.summary}`,
                  it.note && `You asked: ${it.note}`,
                  respond
                    ? `Answer in the chat, or: para request respond ${it.request} clarify "<answer>"`
                    : 'Result to check: opening it marks it seen',
                ]
                  .filter(Boolean)
                  .join(' - ')}
                sx={{ overflowWrap: 'anywhere' }}
              />
              <StatusChip tone={s.tone} label={s.label} />
            </ListItem>
          )
        })}
      </List>
    </Section>
  )
}
