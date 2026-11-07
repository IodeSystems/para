import { useQuestions } from '@/api/questions'
import { Empty, Section } from '@/components/Bits'
import { DataState } from '@/components/DataState'
import { DoneQuestions, QuestionRows } from '@/components/Questions'

// QuestionsView is the Agenda's Questions filter: every open question, blocking first, then the
// rest, then those put off, and the ones answered lately.
export function QuestionsView() {
  const q = useQuestions({ deferred: true })
  return (
    <DataState q={q}>
      {(list) => {
        // Blocking: something waits on it, or an answer waits to be checked. Other: the rest.
        // Later: put off by you; it still blocks what depends on it.
        const blocking = list.filter((x) => !x.deferred && (x.blocking > 0 || x.hasProposal))
        const other = list.filter((x) => !x.deferred && x.blocking === 0 && !x.hasProposal)
        const later = list.filter((x) => x.deferred)
        const rows = (items: typeof list) => <QuestionRows list={items} />
        return (
          <>
            {list.length === 0 && <Empty>No open question.</Empty>}
            <Section title="Blocking" count={blocking.length} hideEmpty>
              {rows(blocking)}
            </Section>
            <Section title="Other" count={other.length} hideEmpty>
              {rows(other)}
            </Section>
            <Section title="Later" count={later.length} hideEmpty>
              {rows(later)}
            </Section>
            <DoneQuestions />
          </>
        )
      }}
    </DataState>
  )
}
