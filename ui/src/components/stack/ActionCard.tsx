import Box from '@mui/material/Box'
import TextField from '@mui/material/TextField'
import { block, Row, type CardProps } from '@/components/stack/parts'

// ActionCard shows an act outside para: where, what, and how. With "Edit, then approve" the owner
// changes the words the agent uses.
export function ActionCard({ it, answer, onAnswer }: CardProps) {
  const editing = answer?.decision === 'edit'
  return (
    <Box aria-label="The action">
      <Row label="Target" text={it.target} />
      <Row label="People" text={(it.people ?? []).join(', ')} />
      <Row label="Kind" text={it.subtype} />
      <Row label="What" text={it.what} />
      <Row label="Changes" text={it.previous} />
      <Row label="How" text={it.how} />
      <Row label="Impact" text={it.impact} />
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
        (it.body || it.diff) && (
          <Box component="pre" aria-label="Body" sx={block}>
            {editing ? answer?.body : it.body || it.diff}
          </Box>
        )
      )}
    </Box>
  )
}
