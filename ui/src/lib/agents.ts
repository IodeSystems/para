import { humanTime } from '@/lib/when'
import type { Tone } from '@/lib/status'

// Connected agents (agent_list): the MCP sessions on this machine, how para can push to each, and
// what each holds.

// pushChip says whether para can wake the session: a channel event (para mcp --channel) starts a
// turn; a plain message reaches it only while it works.
export function pushChip(push: readonly string[]): { label: string; tone: Tone } {
  return push.includes('channel')
    ? { label: 'can be woken', tone: 'good' }
    : { label: 'message only', tone: 'quiet' }
}

// clientWords is the client name and version, or "unknown client".
export function clientWords(a: { client?: string | null; version?: string | null }): string {
  return [a.client, a.version].filter(Boolean).join(' ') || 'unknown client'
}

// lastCallWords says when the session last called a tool.
export function lastCallWords(lastCallAt: string | null | undefined, now = Date.now()): string {
  return lastCallAt ? `last call ${humanTime(lastCallAt, now)}` : 'no tool call yet'
}

// noListenerWords is the warning when work is queued and no agent is connected.
export function noListenerWords(queued: number): string {
  return `${queued} queued, and no agent is connected on this machine. Start one, or run /para loop.`
}
