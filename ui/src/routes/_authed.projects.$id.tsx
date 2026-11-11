import { createFileRoute } from '@tanstack/react-router'
import Typography from '@mui/material/Typography'
import { useSeenOnOpen } from '@/api/requests'
import { useProject } from '@/api/areas'
import Stack from '@mui/material/Stack'
import { ItemLog } from '@/components/ItemLog'
import { AgentLooking } from '@/components/FindContext'
import { PageTitle, Section, StateChip } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { ItemContext } from '@/components/ItemContext'
import { DoneWhen } from '@/components/DoneWhen'
import { GoalBrief } from '@/components/GoalBrief'
import { PointerLinks } from '@/components/PointerLinks'
import { TextLink } from '@/components/links'
import { Markdown } from '@/components/Markdown'
import { nameOr } from '@/lib/names'
import { QuestionRows } from '@/components/Questions'
import { TaskRows } from '@/components/TaskRows'
import { ItemsUnder, underGoalOnly } from '@/components/ItemsUnder'
import { ConceptLinks } from '@/components/AreaLinks'
import { RulesInEffect } from '@/components/RulesInEffect'
import { Assumptions, Risks } from '@/components/RisksAssumptions'
import { MergedFrom, NotesEditor } from '@/components/NotesEditor'

export const Route = createFileRoute('/_authed/projects/$id')({ component: ProjectPage })

function ProjectPage() {
  const { id } = Route.useParams()
  // Opening the item, or the agent work itself, marks its finished agent work seen.
  useSeenOnOpen(id)
  const q = useProject(id)
  return (
    <DataState q={q}>
      {(d) => (
        <>
          <PageTitle
            pin={{ id }}
            action={
              <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                <AgentLooking id={id} />
                <StateChip state={d.project.status} />
              </Stack>
            }
          >
            {d.project.title || id}
          </PageTitle>
          <ItemLog id={id} composer />
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {d.project.owner ? `owner ${nameOr('', d.project.owner)}` : 'no owner'}
            {d.project.area && (
              <>
                {' - project '}
                <TextLink to="/areas/$id" params={{ id: d.project.area }} underline="hover">
                  {nameOr(d.project.areaName, d.project.area)}
                </TextLink>
              </>
            )}
            {d.project.concepts.length > 0 && (
              <>
                {' - concepts '}
                <ConceptLinks ids={d.project.concepts} />
              </>
            )}
          </Typography>
          <ItemContext id={id} />
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
            {d.lastAligned
              ? `last checked against sources ${d.lastAligned}`
              : 'not checked against sources yet'}
          </Typography>
          <DoneWhen value={d.project.doneWhen} prompt="No done-when yet." />
          <GoalBrief brief={d.project.brief} />
          <Risks risks={d.project.brief?.risks} />
          <Assumptions list={d.assumptions} />
          <PointerLinks links={d.project.links} />
          <RulesInEffect rules={d.rules} />
          <Section title="Tasks" count={d.tasks.length} hideEmpty>
            <TaskRows tasks={d.tasks} omit={id} />
          </Section>
          <ItemsUnder theme={id} hide={underGoalOnly} />
          <Section title="Questions" count={d.questions.length} hideEmpty>
            <QuestionRows list={d.questions} />
          </Section>
          {d.body.trim() !== '' && (
            <Section title="About">
              <Markdown source={d.body} />
            </Section>
          )}
          <MergedFrom id={id} />
          <NotesEditor id={id} />
        </>
      )}
    </DataState>
  )
}
