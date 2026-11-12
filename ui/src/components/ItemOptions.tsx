import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'
import { useRouteContext } from '@tanstack/react-router'
import { useQuestionReview, useQuestions } from '@/api/questions'
import { useThreadPost } from '@/api/thread'
import { errorText } from '@/client/gqlClient'
import { canDecide } from '@/client/session'
import { Section } from '@/components/Bits'
import { DocLink } from '@/components/DocLink'
import { useToasts } from '@/components/ToastStack'

// ItemOptions shows the open questions filed about an item (scoped to it, or with about set to
// it) that have options: one button per option, the recommended one marked. A click answers the
// question with that option, then posts "Chose option N: <option>" in the item's thread so the
// agent that filed the options acts on the choice. Without review:decide the buttons are off.
export function ItemOptions({ id }: { id: string }) {
  const { session } = useRouteContext({ from: '/_authed' })
  const decide = canDecide(session)
  const q = useQuestions({ about: id })
  const review = useQuestionReview()
  const post = useThreadPost()
  const toasts = useToasts()
  const list = (q.data ?? []).filter((x) => x.options.length > 0)
  if (list.length === 0) return null
  const choose = async (qid: string, qtitle: string, option: string, n: number) => {
    try {
      await review.mutateAsync({ id: qid, decision: 'answer', choices: [option] })
    } catch (e) {
      toasts.push({ text: `Could not answer: ${errorText(e)}`, tone: 'error' })
      return
    }
    try {
      await post.mutateAsync({ id, text: `Chose option ${n}: ${option}` })
      toasts.push({ text: `Chose: ${option}`, note: qtitle, target: { id } })
    } catch (e) {
      toasts.push({
        text: `Answered, but the agent was not told: ${errorText(e)}`,
        tone: 'error',
      })
    }
  }
  return (
    <Section title="Options" count={list.length}>
      <Stack spacing={2}>
        {list.map((x) => (
          <Box key={x.id} data-item-options={x.id}>
            <Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>
              <DocLink id={x.id}>{x.title || x.id}</DocLink>
            </Typography>
            {x.context && (
              <Typography variant="body2" color="text.secondary" sx={{ overflowWrap: 'anywhere' }}>
                {x.context}
              </Typography>
            )}
            <Stack direction="row" spacing={1} useFlexGap sx={{ flexWrap: 'wrap', mt: 1 }}>
              {x.options.map((o, i) => {
                const rec = o === x.recommended
                return (
                  <Button
                    key={o}
                    size="small"
                    variant={rec ? 'contained' : 'outlined'}
                    data-option={o}
                    data-recommended={rec || undefined}
                    disabled={!decide || review.isPending || post.isPending}
                    onClick={() => void choose(x.id, x.title, o, i + 1)}
                  >
                    {i + 1}. {o}
                    {rec ? ' (recommended)' : ''}
                  </Button>
                )
              })}
            </Stack>
          </Box>
        ))}
      </Stack>
    </Section>
  )
}
