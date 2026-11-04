import Box from '@mui/material/Box'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import MenuItem from '@mui/material/MenuItem'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useDocComments } from '@/api/docs'
import { StatusChip } from '@/components/Bits'
import { DocDiff } from '@/components/DocDiff'
import { TextLink } from '@/components/links'
import { Row, type CardProps } from '@/components/stack/parts'
import { COMMENT_TARGETS, commentState } from '@/lib/docs'
import type { CommentAnswer } from '@/lib/dossier'

// UpstreamCard shows the change of a tracked document between two versions and the comments the
// agent lists. Approve marks the change seen; for each comment the owner picks what it becomes
// (a commitment, an Ask, a question) or closes it. Para does it on submit.
export function UpstreamCard({ it, answer, onAnswer }: CardProps) {
  const id = (it.target ?? '').replace(/^\[\[|\]\]$/g, '')
  const comments = useDocComments(id)
  const listed = new Set(it.comments ?? [])
  const shown = (comments.data?.comments ?? []).filter((c) => listed.has(c.id))
  const picked = new Map((answer?.comments ?? []).map((c) => [c.id, c]))
  const pick = (cid: string, to: string) => {
    if (!onAnswer) return
    const rest = (answer?.comments ?? []).filter((c) => c.id !== cid)
    const next: CommentAnswer[] = to ? [...rest, { id: cid, to }] : rest
    // A comment answer goes with approve: picking one approves the change.
    onAnswer({ ...answer, decision: 'approve', comments: next })
  }
  return (
    <Box>
      {id && (
        <Typography variant="body2" sx={{ mb: 1 }}>
          <TextLink to="/docs/$id" params={{ id }} underline="hover">
            {id}
          </TextLink>
          {' - '}
          {it.from ? `version ${it.from} to ${it.to}` : `up to version ${it.to}`}
        </Typography>
      )}
      {id && <DocDiff id={id} from={it.from ?? 0} to={it.to ?? 0} />}
      {shown.length > 0 && (
        <>
          <Typography variant="caption" color="text.secondary" component="div" sx={{ mt: 1 }}>
            Comments
          </Typography>
          <List dense disablePadding aria-label="Comments to settle">
            {shown.map((c) => {
              const a = picked.get(c.id)
              return (
                <ListItem
                  key={c.id}
                  disableGutters
                  divider
                  sx={{ display: 'block' }}
                  data-comment-id={c.id}
                >
                  <Typography variant="body2" component="div">
                    <b>{c.by.replace(/^\[\[|\]\]$/g, '')}</b>: {c.text}{' '}
                    <StatusChip tone={c.resolution ? 'good' : 'wait'} label={commentState(c)} />
                  </Typography>
                  {c.quote && (
                    <Typography
                      variant="caption"
                      color="text.secondary"
                      component="div"
                      sx={{ borderLeft: 3, borderColor: 'divider', pl: 1, my: 0.5 }}
                    >
                      {c.quote}
                    </Typography>
                  )}
                  {onAnswer && !c.resolution ? (
                    <TextField
                      select
                      size="small"
                      label={`What ${c.id} becomes`}
                      value={a?.to ?? ''}
                      onChange={(e) => pick(c.id, e.target.value)}
                      sx={{ minWidth: 220, mt: 0.5 }}
                    >
                      <MenuItem value="">Leave it open</MenuItem>
                      {COMMENT_TARGETS.map((t) => (
                        <MenuItem key={t.value} value={t.value}>
                          {t.words}
                        </MenuItem>
                      ))}
                    </TextField>
                  ) : (
                    a && <Row label="You chose" text={a.to} />
                  )}
                </ListItem>
              )
            })}
          </List>
        </>
      )}
    </Box>
  )
}
