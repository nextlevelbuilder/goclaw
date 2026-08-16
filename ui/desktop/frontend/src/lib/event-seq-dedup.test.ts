import { describe, expect, it } from 'vitest'
import { shouldProcessRunEvent, runSeqKey, type RunSeqMap } from './event-seq-dedup'

describe('event seq dedup', () => {
  it('drops duplicate seq for the same run', () => {
    const seen: RunSeqMap = new Map()
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'run.started', 1, false)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 2, true)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 2, true)).toBe(false)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'run.started', 1, true)).toBe(false)
    expect(seen.get('sess-1:run-1')).toBe(2)
  })

  it('processes out-of-order new seqs (gap) and advances lastSeq', () => {
    const seen: RunSeqMap = new Map()
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'run.started', 1, false)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 2, true)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 4, true)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 3, true)).toBe(false)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 5, true)).toBe(true)
    expect(seen.get('sess-1:run-1')).toBe(5)
  })

  it('processes frames without seq as before (old-server compat)', () => {
    const seen: RunSeqMap = new Map()
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', undefined, true)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 0, true)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'thinking', undefined, true)).toBe(true)
    expect(seen.size).toBe(0)
  })

  it('processes terminal events and drops duplicate terminal replay', () => {
    const seen: RunSeqMap = new Map()
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'run.started', 1, false)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 2, true)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'run.completed', 3, true)).toBe(true)
    expect(seen.get('sess-1:run-1')).toBe(3)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'run.completed', 3, true)).toBe(false)
  })

  it('keys runs per session so colliding run ids never mix', () => {
    const seen: RunSeqMap = new Map()
    expect(shouldProcessRunEvent(seen, 'run-9', 'sess-A', 'run.started', 1, false)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-9', 'sess-B', 'run.started', 1, false)).toBe(true)
    expect(seen.get('sess-A:run-9')).toBe(1)
    expect(seen.get('sess-B:run-9')).toBe(1)
  })

  it('does not track seq seen before the run started (reconnect replay)', () => {
    const seen: RunSeqMap = new Map()
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 5, false)).toBe(true)
    expect(seen.size).toBe(0)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'run.started', 1, false)).toBe(true)
    expect(seen.get('sess-1:run-1')).toBe(1)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 2, true)).toBe(true)
    expect(shouldProcessRunEvent(seen, 'run-1', 'sess-1', 'chunk', 2, true)).toBe(false)
  })

  it('runSeqKey falls back to runId when sessionKey is missing', () => {
    expect(runSeqKey('run-1')).toBe('run-1')
    expect(runSeqKey('run-1', 'sess-1')).toBe('sess-1:run-1')
  })
})
