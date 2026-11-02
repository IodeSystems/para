import { useEffect, useRef, useSyncExternalStore } from 'react'
import { useOpenProposals } from '@/api/proposals'
import { useQuestions } from '@/api/questions'
import { useRequests } from '@/api/requests'
import { useIncidents } from '@/api/areas'
import {
  noticeText,
  notifyEnabled,
  notifySupported,
  rises,
  workNeedsYou,
  zeroCounts,
  type Counts,
} from '@/lib/notify'

// Browser notifications from live pushes. A push invalidates the queries (client/live.ts); this
// hook compares the counts the queries give with the last ones and notifies on growth. The first
// load sets the baseline, so opening the app never notifies for what was already waiting.
export function useNotifications(vault: string): void {
  const on = useSyncExternalStore(
    (cb) => {
      window.addEventListener('para-notify', cb)
      return () => window.removeEventListener('para-notify', cb)
    },
    () => notifyEnabled(vault) && notifySupported() && Notification.permission === 'granted',
  )
  const { open, isSuccess: pOk } = useOpenProposals()
  const q = useQuestions()
  const r = useRequests()
  const inc = useIncidents()
  const ready = pOk && q.isSuccess && r.isSuccess && inc.isSuccess
  const prev = useRef<Counts | null>(null)
  const proposals = open.length
  const answers = (q.data ?? []).filter((x) => x.status === 'open' && !!x.proposedAnswer).length
  const work = (r.data ?? []).filter((x) => workNeedsYou(x.resolution)).length
  const incidents = (inc.data ?? []).filter((x) => x.status === 'open').length

  useEffect(() => {
    if (!ready) return
    const next: Counts = {
      ...zeroCounts(),
      proposal: proposals,
      answer: answers,
      work,
      incident: incidents,
    }
    const was = prev.current
    prev.current = next
    if (!on || !was) return
    for (const n of rises(was, next)) {
      try {
        new Notification('para', { body: noticeText(n), tag: `para-${vault}-${n.kind}` })
      } catch {
        // Some browsers only allow notifications through a service worker; skip.
      }
    }
  }, [ready, on, vault, proposals, answers, work, incidents])
}
