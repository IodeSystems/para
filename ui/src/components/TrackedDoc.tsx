import { useState } from 'react'
import Alert from '@mui/material/Alert'
import Box from '@mui/material/Box'
import Button from '@mui/material/Button'
import Link from '@mui/material/Link'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import MenuItem from '@mui/material/MenuItem'
import Stack from '@mui/material/Stack'
import TextField from '@mui/material/TextField'
import Typography from '@mui/material/Typography'
import { useDocComments, useDocSeen, useDocShow, type DocComment } from '@/api/docs'
import { errorText } from '@/client/gqlClient'
import { Empty, Section, StateChip, StatusChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocDiff, PickRow, VersionPick } from '@/components/DocDiff'
import { DocLink } from '@/components/DocLink'
import { TextLink } from '@/components/links'
import { block } from '@/components/stack/parts'
import { commentState, markOf, runs } from '@/lib/docs'
import { exactTime, humanTime } from '@/lib/when'

// TrackedDoc is the part of a tracked document's page that other documents lack: what changed
// since you last looked, the text of any version with the comments' quotes marked, the comments,
// and every version. With a review:decide key (decide) the owner marks the change seen.
export function TrackedDoc({ id, decide }: { id: string; decide: boolean }) {
  const [version, setVersion] = useState(0)
  const q = useDocShow(id, version)
  const comments = useDocComments(id)
  const seen = useDocSeen()
  return (
    <DataState q={q}>
      {(d) => {
        const doc = d.doc
        return (
          <>
            <Stack
              direction="row"
              useFlexGap
              sx={{ alignItems: 'center', flexWrap: 'wrap', gap: 1, mb: 1 }}
            >
              <StatusChip tone="quiet" label={doc.system} />
              {doc.status !== 'watching' && <StateChip state={doc.status} />}
              {doc.unseen > 0 && (
                <StatusChip
                  tone="attn"
                  label={`${doc.unseen} new version${doc.unseen === 1 ? '' : 's'}`}
                />
              )}
              {decide && doc.unseen > 0 && (
                <Button
                  variant="contained"
                  size="small"
                  disabled={seen.isPending}
                  onClick={() => seen.mutate({ id: doc.id, version: doc.latest })}
                >
                  Mark seen
                </Button>
              )}
            </Stack>
            {d.note && (
              <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
                {d.note}
              </Typography>
            )}
            {seen.error && (
              <Alert severity="error" sx={{ mb: 2 }}>
                {errorText(seen.error)}
              </Alert>
            )}
            <Typography variant="body2" sx={{ mb: 2, overflowWrap: 'anywhere' }}>
              <Link href={doc.url} target="_blank" rel="noreferrer noopener">
                {doc.url}
              </Link>
              {doc.checked && (
                <span title={exactTime(doc.checked)}> - read {humanTime(doc.checked)}</span>
              )}
              {doc.project && (
                <>
                  {' '}
                  - <DocLink id={doc.project} />
                </>
              )}
            </Typography>
            <Changes id={doc.id} latest={doc.latest} lastSeen={doc.lastSeen} />
            <Section title="Text">
              <PickRow>
                <VersionPick
                  label="Version"
                  value={d.version}
                  latest={doc.latest}
                  onChange={setVersion}
                />
              </PickRow>
              <MarkedText
                text={d.text}
                version={d.version}
                comments={comments.data?.comments ?? []}
              />
            </Section>
            <Section title="Comments" count={comments.data?.open}>
              <Comments list={comments.data?.comments ?? []} />
            </Section>
            <Section title="Versions" count={d.versions.length}>
              <List dense disablePadding aria-label="Versions">
                {[...d.versions].reverse().map((v) => (
                  <ListItem key={v.version} disableGutters divider>
                    <ListItemText
                      primary={
                        <>
                          Version {v.version}
                          {v.version === 1 && ' (the original)'}
                          {v.proposal && (
                            <>
                              {' '}
                              - after the edit{' '}
                              <TextLink to="/docs/$id" params={{ id: v.proposal }} underline="hover">
                                {v.item || v.proposal}
                              </TextLink>
                            </>
                          )}
                        </>
                      }
                      secondary={[
                        humanTime(v.fetchedAt),
                        v.fetchedBy && `read by ${v.fetchedBy}`,
                        v.fidelity !== 'exact' && v.fidelity,
                        v.hash.slice(0, 19),
                      ]
                        .filter(Boolean)
                        .join(' - ')}
                    />
                    {v.seen ? (
                      <StatusChip tone="good" label="seen" />
                    ) : (
                      <StatusChip tone="attn" label="not seen" />
                    )}
                  </ListItem>
                ))}
              </List>
            </Section>
          </>
        )
      }}
    </DataState>
  )
}

// Changes compares two versions; by default the last one you saw and the latest.
function Changes({ id, latest, lastSeen }: { id: string; latest: number; lastSeen: number }) {
  const [from, setFrom] = useState(-1)
  const [to, setTo] = useState(0)
  const [unit, setUnit] = useState('line')
  const toV = to || latest
  const fromV = from >= 0 ? from : lastSeen >= toV ? toV - 1 : lastSeen
  return (
    <Section title={lastSeen >= latest ? 'Last change' : 'What changed since you looked'}>
      <PickRow>
        <VersionPick label="From" value={fromV} latest={latest} zero="Nothing" onChange={setFrom} />
        <VersionPick label="To" value={toV} latest={latest} onChange={setTo} />
        <TextField
          select
          size="small"
          label="By"
          value={unit}
          onChange={(e) => setUnit(e.target.value)}
          sx={{ minWidth: 120 }}
        >
          <MenuItem value="line">Line</MenuItem>
          <MenuItem value="paragraph">Paragraph</MenuItem>
        </TextField>
      </PickRow>
      {fromV >= toV ? (
        <Empty>Pick a From version before To.</Empty>
      ) : (
        <DocDiff id={id} from={fromV} to={toV} unit={unit} />
      )}
    </Section>
  )
}

// MarkedText shows a version's text with each comment's quote marked.
function MarkedText({
  text,
  version,
  comments,
}: {
  text: string
  version: number
  comments: DocComment[]
}) {
  const marks = comments.flatMap((c) => {
    const m = markOf(c, version)
    return m ? [m] : []
  })
  return (
    <Box component="pre" sx={{ ...block, maxHeight: 480, overflowY: 'auto' }} aria-label="Text">
      {runs(text, marks).map((r, i) =>
        r.ids.length > 0 ? (
          <Box
            component="mark"
            key={i}
            data-comment={r.ids.join(' ')}
            title={`Comment ${r.ids.join(', ')}`}
            sx={{ bgcolor: 'warning.light', color: 'inherit', borderRadius: 0.5 }}
          >
            {r.text}
          </Box>
        ) : (
          <span key={i}>{r.text}</span>
        ),
      )}
    </Box>
  )
}

function Comments({ list }: { list: DocComment[] }) {
  if (list.length === 0) return <Empty>No comments.</Empty>
  return (
    <List disablePadding aria-label="Comments">
      {list.map((c) => (
        <ListItem
          key={c.id}
          disableGutters
          divider
          sx={{ display: 'block' }}
          data-comment-id={c.id}
        >
          <Stack
            direction="row"
            spacing={1}
            useFlexGap
            sx={{ alignItems: 'center', flexWrap: 'wrap' }}
          >
            <Typography variant="subtitle2" component="span">
              {c.id} - {c.by.replace(/^\[\[|\]\]$/g, '')}
            </Typography>
            <StatusChip
              tone={c.resolution ? 'good' : c.lost ? 'attn' : 'wait'}
              label={commentState(c)}
            />
            <Typography variant="caption" color="text.secondary">
              {humanTime(c.at)} - on version {c.version}
            </Typography>
          </Stack>
          {c.quote && (
            <Typography
              variant="body2"
              color="text.secondary"
              sx={{ borderLeft: 3, borderColor: 'divider', pl: 1, my: 0.5, whiteSpace: 'pre-wrap' }}
            >
              {c.quote}
            </Typography>
          )}
          <Typography variant="body2" sx={{ whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>
            {c.text}
          </Typography>
          <Typography variant="caption" color="text.secondary" component="div">
            {c.url && (
              <Link href={c.url} target="_blank" rel="noreferrer noopener" sx={{ mr: 1 }}>
                Open the comment
              </Link>
            )}
            {c.became && (
              <>
                Became{' '}
                <DocLink id={c.became.replace(/^\[\[|\]\]$/g, '')}>
                  {c.becameTitle || undefined}
                </DocLink>
                {c.becameClosed ? ' (closed)' : ''}
              </>
            )}
            {c.note && ` ${c.note}`}
          </Typography>
        </ListItem>
      ))}
    </List>
  )
}
