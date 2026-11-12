import Box from '@mui/material/Box'
import Typography from '@mui/material/Typography'
import { useDelegationRun } from '@/api/io'

export type VoiceItem = {
  id: string
  direction: string
  state: string
  voice?: string | null
  agent?: string | null
  stamp?: string | null
  allowedBy?: string | null
}

// voiceWords is whose words an outgoing message goes out as: "As you" (you approve it, sent in
// your name, no stamp) or "For you, stamped: <rule>" (an agent sends it without your review,
// under a rule of your .para/delegation.yaml, with a stamp that says so).
export function voiceWords(x: Pick<VoiceItem, 'voice' | 'allowedBy'>): string {
  if (x.voice !== 'for_me') return 'As you'
  return `For you, stamped: ${x.allowedBy || 'no rule allows it'}`
}

// VoiceNote shows the voice of an outgoing draft and, for a for_me draft, the stamp at the end
// of its text and the delegation rule that would let the agent send it (or why none does).
// Approve makes it as you: the stamp comes off.
export function VoiceNote({ item }: { item: VoiceItem }) {
  const forMe = item.voice === 'for_me'
  const open = item.state === 'draft' || item.state === 'ready'
  const run = useDelegationRun(item.id, forMe && open)
  if (item.direction !== 'out') return null
  const d = run.data
  return (
    <Box sx={{ mb: 1 }} data-voice={item.voice ?? 'as_me'}>
      <Typography variant="body2">
        <b>{voiceWords(item)}</b>
        {forMe && item.agent ? ` (sent by agent ${item.agent})` : ''}
      </Typography>
      {forMe && item.stamp && (
        <Typography variant="body2" color="text.secondary" data-stamp>
          Stamp: {item.stamp}
        </Typography>
      )}
      {forMe && open && d && (
        <Typography variant="body2" color="text.secondary" data-delegation>
          {d.rule
            ? `The agent may send it without your review: ${d.rule}. Approve as Me sends it as you instead (the stamp comes off).`
            : !d.file
              ? 'You have no delegation rules (.para/delegation.yaml): it waits for your approval. Approve lets the agent send it for you, with the stamp; Approve as Me sends it as you, without the stamp.'
              : `No delegation rule allows it${d.misses[0] ? ` (rule ${d.misses[0].rule}: ${d.misses[0].why})` : ''}: it waits for your approval. Approve lets the agent send it for you, with the stamp; Approve as Me sends it as you, without the stamp.`}
        </Typography>
      )}
    </Box>
  )
}
