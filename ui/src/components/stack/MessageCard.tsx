import Box from '@mui/material/Box'
import TextField from '@mui/material/TextField'
import { CopyButton } from '@/components/CopyButton'
import { TextLink } from '@/components/links'
import { ioTarget } from '@/lib/io'
import { block, Row, type CardProps } from '@/components/stack/parts'

// MessageCard shows a message as the thing itself: who it goes to and what it says. With "Edit,
// then approve" the owner changes the subject and the body; the agent sends the owner's words.
// A message that names an io draft shows the draft's text, and an edit is written to the draft.
export function MessageCard({ it, answer, onAnswer }: CardProps) {
  const editing = answer?.decision === 'edit'
  return (
    <Box aria-label="The message">
      <Row label="To" text={it.target} />
      <Row label="People" text={(it.people ?? []).join(', ')} />
      <Row label="Channel" text={it.channel} />
      {it.io && (
        <Box sx={{ typography: 'body2' }}>
          <Box component="span" sx={{ color: 'text.secondary' }}>
            Draft:{' '}
          </Box>
          <TextLink to="/io/$id" params={{ id: ioTarget(it.io) || it.io }} underline="hover">
            {ioTarget(it.io) || it.io}
          </TextLink>
        </Box>
      )}
      {editing && onAnswer ? (
        <>
          <TextField
            size="small"
            fullWidth
            label="Subject of your version"
            value={answer?.subject ?? ''}
            onChange={(e) => onAnswer({ ...answer, subject: e.target.value })}
            sx={{ my: 1 }}
          />
          <TextField
            fullWidth
            multiline
            minRows={3}
            label="Body of your version"
            value={answer?.body ?? ''}
            onChange={(e) => onAnswer({ ...answer, body: e.target.value })}
          />
        </>
      ) : (
        <>
          <Row label="Subject" text={editing ? answer?.subject : it.subject} />
          {(it.body || it.diff) && (
            <Box component="pre" aria-label="Message body" sx={block}>
              {editing ? answer?.body : it.body || it.diff}
            </Box>
          )}
        </>
      )}
      {it.state === 'approved' && it.body && (
        <CopyButton getText={() => it.body ?? ''} label="Copy text" />
      )}
    </Box>
  )
}
