import { createFileRoute } from '@tanstack/react-router'
import Typography from '@mui/material/Typography'
import { useSeenOnOpen } from '@/api/requests'
import { useGoal } from '@/api/areas'
import Stack from '@mui/material/Stack'
import { ItemLayout } from '@/components/ItemLayout'
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

export const Route = createFileRoute('/_authed/goals/$id')({ component: GoalPage })

function GoalPage() {
  const { id } = Route.useParams()
  // Opening the item, or the agent work itself, marks its finished agent work seen.
  useSeenOnOpen(id)
  const q = useGoal(id)
  return (
    <DataState q={q}>
      {(d) => (
        <ItemLayout id={id} composer>
          <PageTitle
            pin={{ id }}
            action={
              <Stack direction="row" spacing={0.5} sx={{ alignItems: 'center' }}>
                <AgentLooking id={id} />
                <StateChip state={d.goal.status} />
              </Stack>
            }
          >
            {d.goal.title || id}
          </PageTitle>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
            {d.goal.owner ? `owner ${nameOr('', d.goal.owner)}` : 'no owner'}
            {d.goal.area && (
              <>
                {' - area '}
                <TextLink to="/areas/$id" params={{ id: d.goal.area }} underline="hover">
                  {nameOr(d.goal.areaName, d.goal.area)}
                </TextLink>
              </>
            )}
            {d.goal.project && (
              <>
                {' - part of '}
                <TextLink to="/projects/$id" params={{ id: d.goal.project }} underline="hover">
                  {nameOr(d.goal.projectName, d.goal.project)}
                </TextLink>
              </>
            )}
            {d.goal.resumeWhen && ` - parked until ${d.goal.resumeWhen}`}
            {d.goal.concepts.length > 0 && (
              <>
                {' - concepts '}
                <ConceptLinks ids={d.goal.concepts} />
              </>
            )}
          </Typography>
          <ItemContext id={id} />
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
            {d.lastAligned
              ? `last checked against sources ${d.lastAligned}`
              : 'not checked against sources yet'}
          </Typography>
          <DoneWhen value={d.goal.doneWhen} prompt="No done-when yet." />
          <GoalBrief brief={d.goal.brief} />
          <Risks risks={d.goal.brief?.risks} />
          <Assumptions list={d.assumptions} />
          <PointerLinks links={d.goal.links} />
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
        </ItemLayout>
      )}
    </DataState>
  )
}
