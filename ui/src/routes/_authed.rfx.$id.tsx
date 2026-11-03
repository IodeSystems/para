import { createFileRoute } from '@tanstack/react-router'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import { LinkButton } from '@/components/links'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useRfx, useRfxTargetState } from '@/api/rfx'
import { useCopyText } from '@/api/doc'
import { Empty, PageTitle, Section, StateChip } from '@/components/Bits'
import { NextMoves } from '@/components/NextMoves'
import { CopyButton } from '@/components/CopyButton'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { Markdown } from '@/components/Markdown'
import { errorText } from '@/client/gqlClient'
import { targetStates } from '@/lib/meeting'
import { plainState } from '@/lib/status'
import { withoutSection } from '@/lib/markdown'
import { nameList, nameOr } from '@/lib/names'
import { dateWords } from '@/lib/when'

export const Route = createFileRoute('/_authed/rfx/$id')({ component: RfxPage })

function RfxPage() {
  const { id } = Route.useParams()
  const q = useRfx(id)
  const copy = useCopyText()
  const setState = useRfxTargetState()
  return (
    <DataState q={q}>
      {(r) => (
        <>
          <PageTitle
            action={
              <Stack direction="row" spacing={1}>
                <LinkButton
                  size="small"
                  variant="contained"
                  to="."
                  search={(p) => ({ ...p, reply: id })}
                >
                  Record reply
                </LinkButton>
              </Stack>
            }
          >
            {r.doc.title || id}
          </PageTitle>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {r.doc.meta.direction === 'in' ? 'Asked of you' : 'You ask'} - {r.doc.meta.status}
            {r.route ? ` - via ${nameOr(r.routeName, r.route)}` : ''}
            {r.doc.meta.nextNudge ? ` - follow up ${dateWords(r.doc.meta.nextNudge)}` : ''}
          </Typography>
          <Section title="Asked of" count={r.targets.length}>
            {r.targets.length === 0 ? (
              <Empty>Nobody is named yet.</Empty>
            ) : (
              <List dense disablePadding>
                {setState.isError && (
                  <Alert severity="error" sx={{ mb: 1 }}>
                    {errorText(setState.error)}
                  </Alert>
                )}
                {r.targets.map((t, i) => (
                  <ListItem key={`${t.to}-${t.medium}`} disableGutters>
                    <ListItemText
                      primary={nameOr(r.targetNames?.[i], t.to)}
                      secondary={[t.medium, t.value, t.sent && `sent ${t.sent}`]
                        .filter(Boolean)
                        .join(' - ')}
                    />
                    <StateChip state={t.state} />
                    <TextField
                      select
                      size="small"
                      label="Set state"
                      value={targetStates(t.medium).includes(t.state) ? t.state : ''}
                      disabled={setState.isPending || r.doc.meta.status === 'closed'}
                      onChange={(e) =>
                        setState.mutate({
                          id,
                          to: t.to,
                          medium: t.medium,
                          state: e.target.value as never,
                        })
                      }
                      sx={{ ml: 1, minWidth: 130 }}
                    >
                      {targetStates(t.medium).map((s) => (
                        <MenuItem key={s} value={s}>
                          {plainState(s)}
                        </MenuItem>
                      ))}
                    </TextField>
                  </ListItem>
                ))}
              </List>
            )}
          </Section>
          {r.doc.meta.direction !== 'in' && r.doc.meta.status === 'open' && <NextMoves id={id} />}
          {(r.chain.length > 0 || r.dependedOnBy.length > 0) && (
            <Section title="Chain" count={r.chain.length}>
              <List dense disablePadding>
                {r.chain.map((n) => (
                  <ListItem key={n.id} disableGutters sx={{ pl: n.depth * 2 }}>
                    <ListItemText
                      primary={<DocLink id={n.id}>{n.title || nameOr('', n.id)}</DocLink>}
                      secondary={`${n.type} - ${n.status}`}
                    />
                  </ListItem>
                ))}
              </List>
              {r.dependedOnBy.length > 0 && (
                <Typography variant="caption" color="text.secondary">
                  Needed by {nameList(r.dependedOnBy, r.dependedOnByNames).join(', ')}
                </Typography>
              )}
            </Section>
          )}
          {(r.draft || r.doc.meta.direction !== 'in') && (
            <Section
              title="Draft"
              action={
                r.draft ? (
                  <CopyButton
                    label="Copy"
                    getText={async () => (await copy.mutateAsync({ id, as: 'plain' })).text}
                  />
                ) : undefined
              }
            >
              {r.draft ? (
                <Box sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{r.draft}</Box>
              ) : (
                <Empty>No message is drafted yet.</Empty>
              )}
            </Section>
          )}
          {withoutSection(r.doc.body, 'Draft') && (
            <Section title="Ask">
              <Markdown source={withoutSection(r.doc.body, 'Draft')} />
            </Section>
          )}
        </>
      )}
    </DataState>
  )
}
