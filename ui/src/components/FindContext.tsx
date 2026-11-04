import Typography from '@mui/material/Typography'
import { StatusChip } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { useLooking, useReplies, useWorking } from '@/api/requests'

// AgentLooking is the chip a row shows while open agent work looks into the item. When that work
// has finished, it shows "Agent replied" instead.
export function AgentLooking({ id }: { id: string }) {
  const looking = useLooking()
  return looking.has(id) ? (
    <StatusChip tone="good" label="Agent looking" />
  ) : (
    <AgentReplied id={id} />
  )
}

// AgentReplied is the chip of an item the agent replied about, with the agent's note.
export function AgentReplied({ id }: { id: string }) {
  const reply = useReplies().get(id)
  return reply ? <StatusChip tone="good" label="Agent replied" /> : null
}

// AgentReplyNote is the reply of the agent on an item page: the resolution note, and a link to
// the agent work.
export function AgentReplyNote({ id }: { id: string }) {
  const reply = useReplies().get(id)
  if (!reply) return null
  return (
    <Typography variant="body2" sx={{ mb: 2, overflowWrap: 'anywhere' }}>
      <strong>Agent replied:</strong> {reply.reply}{' '}
      <DocLink id={reply.request}>Open the agent work</DocLink>
    </Typography>
  )
}

// AgentWorking is the chip a task row shows while delegated agent work serves it.
export function AgentWorking({ id }: { id: string }) {
  const working = useWorking()
  return working.has(id) ? <StatusChip tone="good" label="Agent working" /> : null
}
