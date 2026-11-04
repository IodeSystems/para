import { useState } from 'react'
import Box from '@mui/material/Box'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { Details } from '@/components/Bits'
import { block, Preview, Row, type CardProps } from '@/components/stack/parts'

// ChangeCard shows a change in the vault: the exact change, and para's own preview of the
// documents before and after. To edit, the owner changes the op's input; the preview shows the
// agent's input.
export function ChangeCard({ it, answer, onAnswer }: CardProps) {
  const input: unknown = it.input
  const editing = answer?.decision === 'edit'
  return (
    <Box>
      <Typography variant="overline" color="text.secondary" component="div">
        The exact change
      </Typography>
      <Box component="ul" aria-label="Exact change" sx={{ m: 0, mb: 1, pl: 2.5 }}>
        {it.change.map((line) => (
          <Typography component="li" variant="body2" key={line} sx={{ overflowWrap: 'anywhere' }}>
            {line}
          </Typography>
        ))}
      </Box>
      <Row label="What" text={it.what} />
      <Row label="How" text={it.how} />
      <Row label="Impact" text={it.impact} />
      {it.preview && <Preview pv={it.preview} />}
      {editing && onAnswer ? (
        <InputEditor
          value={answer?.input ?? input}
          onChange={(v) => onAnswer({ ...answer, input: v })}
        />
      ) : (
        <Details summary="The op and its input">
          <Typography variant="caption" component="div">
            {it.op}
          </Typography>
          <Box
            component="pre"
            sx={{ ...block, my: 0.5, whiteSpace: 'pre' as const, overflowX: 'auto' }}
          >
            {JSON.stringify((editing ? answer?.input : input) ?? {}, null, 2)}
          </Box>
        </Details>
      )}
    </Box>
  )
}

// InputEditor edits the op input as JSON. Text that is not a JSON object is not saved.
function InputEditor({ value, onChange }: { value: unknown; onChange: (v: unknown) => void }) {
  const [text, setText] = useState(() => JSON.stringify(value ?? {}, null, 2))
  const [bad, setBad] = useState('')
  return (
    <TextField
      fullWidth
      multiline
      minRows={4}
      label="Your input for the op (JSON)"
      value={text}
      error={bad !== ''}
      helperText={bad || 'Para runs the op with this input when you submit.'}
      slotProps={{ htmlInput: { style: { fontFamily: 'ui-monospace, monospace', fontSize: 13 } } }}
      onChange={(e) => {
        setText(e.target.value)
        try {
          const v: unknown = JSON.parse(e.target.value)
          if (!v || typeof v !== 'object' || Array.isArray(v)) {
            setBad('Give a JSON object: { ... }')
            return
          }
          setBad('')
          onChange(v)
        } catch {
          setBad('This is not JSON yet. Para keeps the last good input.')
        }
      }}
      sx={{ my: 1 }}
    />
  )
}
