import Typography from '@mui/material/Typography'

// DoneWhen shows how we know a task or a goal is done. Without one it says so, quietly: the
// owner sets it in the chat.
export function DoneWhen({ value, prompt }: { value: string | null | undefined; prompt: string }) {
  const text = (value ?? '').trim()
  return text ? (
    <Typography variant="body2" sx={{ mb: 2, overflowWrap: 'anywhere' }}>
      Done when: {text}
    </Typography>
  ) : (
    <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
      {prompt}
    </Typography>
  )
}
