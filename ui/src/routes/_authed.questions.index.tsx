import { createFileRoute } from '@tanstack/react-router'
import { useQuestions } from '@/api/questions'
import { Empty, PageTitle, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { QuestionRows } from '@/components/Questions'

export const Route = createFileRoute('/_authed/questions/')({ component: Questions })

function Questions() {
  const q = useQuestions()
  return (
    <>
      <PageTitle>Questions</PageTitle>
      <DataState q={q}>
        {(list) => (
          <Section title="Open, the ones that block most first" count={list.length}>
            {list.length === 0 ? <Empty>No open question.</Empty> : <QuestionRows list={list} />}
          </Section>
        )}
      </DataState>
    </>
  )
}
