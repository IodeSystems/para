import { createFileRoute } from '@tanstack/react-router'
import Box from '@mui/material/Box'
import Link from '@mui/material/Link'
import Typography from '@mui/material/Typography'
import { AnnotationCard } from '@/components/Annotation'
import List from '@mui/material/List'
import ListItem from '@mui/material/ListItem'
import ListItemText from '@mui/material/ListItemText'
import { useArea } from '@/api/areas'
import { PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DocLink } from '@/components/DocLink'
import { IncidentRows } from '@/components/IncidentRows'
import { PointerLinks } from '@/components/PointerLinks'
import { TextLink } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { nameOr } from '@/lib/names'
import { QuestionRows } from '@/components/Questions'
import { TaskRows } from '@/components/TaskRows'
import { ConceptLinks } from '@/components/AreaLinks'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'
import { ItemComposer } from '@/components/ItemComposer'
import { ItemDraftProvider } from '@/components/ItemDraft'

export const Route = createFileRoute('/_authed/areas/$id')({ component: AreaPage })

function AreaPage() {
  const { id } = Route.useParams()
  const q = useArea(id)
  return (
    <DataState q={q}>
      {(d) => (
        <ItemDraftProvider id={d.area.id}>
          <PageTitle action={<StateChip state={d.area.status} />}>{d.area.title || id}</PageTitle>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            {d.area.kind === 'concept' ? 'Concept' : 'Project'}
            {d.area.cadence &&
              ` - review ${d.area.cadence}, last ${d.area.lastReviewed || 'never'}` +
                (d.area.nextReview ? `, next ${d.area.nextReview}` : '')}
            {d.area.concepts.length > 0 && (
              <>
                {' - concepts '}
                <ConceptLinks ids={d.area.concepts} />
              </>
            )}
          </Typography>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
            {d.lastAligned
              ? `last checked against sources ${d.lastAligned}`
              : 'not checked against sources yet'}
          </Typography>
          <PointerLinks links={d.area.links} />
          <Section title="Goals" count={d.projects.length} hideEmpty>
            {
              <List dense disablePadding>
                {d.projects.map((p) => (
                  <ListItem key={p.id} disableGutters>
                    <ListItemText
                      primary={
                        <TextLink to="/projects/$id" params={{ id: p.id }} underline="hover">
                          {p.title || p.id}
                        </TextLink>
                      }
                      secondary={p.owner ? `owner ${nameOr('', p.owner)}` : undefined}
                    />
                    <StateChip state={p.status} />
                  </ListItem>
                ))}
              </List>
            }
          </Section>
          <Section title="Drawn on by" count={d.drawnOnBy.length} hideEmpty>
            <List dense disablePadding>
              {d.drawnOnBy.map((u) => (
                <ListItem key={u.id} disableGutters>
                  <ListItemText
                    primary={
                      u.type === 'area' ? (
                        <TextLink to="/areas/$id" params={{ id: u.id }} underline="hover">
                          {u.title || u.id}
                        </TextLink>
                      ) : (
                        <TextLink to="/projects/$id" params={{ id: u.id }} underline="hover">
                          {u.title || u.id}
                        </TextLink>
                      )
                    }
                    secondary={u.type === 'area' ? 'Project' : 'Goal'}
                  />
                </ListItem>
              ))}
            </List>
          </Section>
          <Section title="Tasks" count={d.tasks.length} hideEmpty>
            <TaskRows tasks={d.tasks} showProject />
          </Section>
          <Section title="Recurring" count={d.recurring.length} hideEmpty>
            <TaskRows tasks={d.recurring} />
          </Section>
          <Section title="Questions" count={d.questions.length} hideEmpty>
            <QuestionRows list={d.questions} />
          </Section>
          <Section title="Incidents" count={d.incidents.length} hideEmpty>
            <IncidentRows list={d.incidents} />
          </Section>
          {d.notes.length > 0 && (
            <Section title="Notes" count={d.notes.length}>
              <List dense disablePadding>
                {d.notes.map((n) => (
                  <ListItem key={n.id} disableGutters>
                    <ListItemText primary={<DocLink id={n.id}>{n.title || n.id}</DocLink>} />
                  </ListItem>
                ))}
              </List>
            </Section>
          )}
          {d.follows && (
            <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }} data-follows>
              {`Follows a source: ${d.follows.sourceStatus}, ${d.follows.rules} ${d.follows.rules === 1 ? 'rule' : 'rules'}, `}
              {d.follows.verified
                ? `checked by ${d.follows.reviewedBy || 'a person'}`
                : 'summary not checked against the source yet'}
              {d.follows.url && (
                <>
                  {' - '}
                  <Link href={d.follows.url} target="_blank" rel="noreferrer">
                    source
                  </Link>
                </>
              )}
            </Typography>
          )}
          {d.body.trim() !== '' && (
            <Section title={d.follows ? 'Summary' : 'About'}>
              {d.sections.map((s, i) => (
                <Box key={`${i}-${s.heading}`} component="section">
                  {s.heading && (
                    <Typography
                      variant={s.level <= 2 ? 'h6' : 'subtitle1'}
                      component={s.level <= 2 ? 'h3' : 'h4'}
                      sx={{ mt: i > 0 ? 2 : 0, overflowWrap: 'anywhere' }}
                    >
                      {s.heading}
                    </Typography>
                  )}
                  {s.parts.map((p, j) =>
                    p.annotation ? (
                      <AnnotationCard key={p.annotation.id} annotation={p.annotation} />
                    ) : (
                      <Markdown key={j} source={p.text ?? ''} />
                    ),
                  )}
                </Box>
              ))}
            </Section>
          )}
          <MergedFrom id={id} />
          <NotesEditor id={id} />
          <ItemComposer id={d.area.id} title={d.area.title} meta={{ type: 'area' }} />
        </ItemDraftProvider>
      )}
    </DataState>
  )
}
