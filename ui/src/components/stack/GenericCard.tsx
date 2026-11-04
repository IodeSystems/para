import Box from '@mui/material/Box'
import { block, Row, type CardProps } from '@/components/stack/parts'

// GenericCard shows an item of a kind this app has no card for (upstream, or a kind added
// later): every field the agent wrote, as plain text.
export function GenericCard({ it }: CardProps) {
  const fields: unknown = it.fields
  return (
    <Box>
      <Row label="Target" text={it.target} />
      <Row label="What" text={it.what} />
      <Row label="How" text={it.how} />
      <Row label="Impact" text={it.impact} />
      {it.body && (
        <Box component="pre" sx={block}>
          {it.body}
        </Box>
      )}
      {fields && typeof fields === 'object' && Object.keys(fields).length > 0 ? (
        <Box component="pre" sx={block}>
          {JSON.stringify(fields, null, 2)}
        </Box>
      ) : null}
    </Box>
  )
}
