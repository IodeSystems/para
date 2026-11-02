import { StateChip } from '@/components/Bits'
import type { Tone } from '@/lib/status'

const TONES: Record<string, Tone> = {
  requested: 'attn',
  scheduled: 'wait',
  held: 'good',
  cancelled: 'quiet',
}

// MeetingChip shows a meeting status with the tone it has everywhere.
export function MeetingChip({ status }: { status: string }) {
  return <StateChip state={status} tone={TONES[status] ?? 'quiet'} />
}
