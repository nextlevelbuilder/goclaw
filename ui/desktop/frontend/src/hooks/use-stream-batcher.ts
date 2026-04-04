import { useRef, useCallback } from 'react'

/**
 * RAF-based stream batcher — prevents 100+ setState calls/sec during streaming.
 * Buffers text and flushes once per animation frame.
 */
export function useStreamBatcher(onFlush: (text: string) => void) {
  const bufferRef = useRef('')
  const rafRef = useRef(0)

  const append = useCallback(
    (text: string) => {
      bufferRef.current += text
      if (!rafRef.current) {
        rafRef.current = requestAnimationFrame(() => {
          onFlush(bufferRef.current)
          bufferRef.current = ''
          rafRef.current = 0
        })
      }
    },
    [onFlush],
  )

  const flush = useCallback(() => {
    if (rafRef.current) {
      cancelAnimationFrame(rafRef.current)
      rafRef.current = 0
    }
    if (bufferRef.current) {
      onFlush(bufferRef.current)
      bufferRef.current = ''
    }
  }, [onFlush])

  return { append, flush }
}
