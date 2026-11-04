import Box from '@mui/material/Box'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { lineDiff } from '@/lib/diff'
import { DiffBlock, Row, type CardProps } from '@/components/stack/parts'

// DocEditCard shows an edit to a document as a diff of the old text and the new text. With
// "Edit, then approve" the owner writes the new text.
export function DocEditCard({ it, answer, onAnswer }: CardProps) {
  const editing = answer?.decision === 'edit'
  const after = editing ? (answer?.new ?? '') : (it.new ?? '')
  return (
    <Box>
      <Row label="Document" text={it.target || it.for} />
      <Row label="Where" text={it.range} />
      <Typography variant="caption" color="text.secondary" component="div">
        {editing ? 'Your new text against the old text' : 'The change'}
      </Typography>
      <DiffBlock lines={lineDiff(it.old ?? '', after)} label="Before and after" />
      {editing && onAnswer && (
        <TextField
          fullWidth
          multiline
          minRows={3}
          label="Your new text"
          value={answer?.new ?? ''}
          onChange={(e) => onAnswer({ ...answer, new: e.target.value })}
          sx={{ mt: 1 }}
        />
      )}
    </Box>
  )
}
