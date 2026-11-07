import type { ChatErrorKind } from '../stores/chat-activity-store'

const PROVIDER_RE = /429|rate.?limit|cooldown|cooling|overloaded|quota|too many requests|resource.?exhausted/i
const INTERRUPTED_RE = /interrupt|disconnect|connection|network|socket|stream|eof|timed? ?out|timeout|aborted|reset by peer/i
const DENIED_RE = /denied|not allowed|permission|forbidden|blocked|policy|not permitted|unauthori[sz]ed/i

/** Sorts a `run.failed` error string into the notice the chat shows. */
export function classifyRunError(message: string): ChatErrorKind {
  if (PROVIDER_RE.test(message)) return 'provider'
  if (INTERRUPTED_RE.test(message)) return 'interrupted'
  return 'failed'
}

/** Whether a failed tool call was refused by the tool settings rather than failing on its own. */
export function isToolDenied(error: string | undefined): boolean {
  return !!error && DENIED_RE.test(error)
}
