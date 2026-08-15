export const reasoningDeliveryOptions = [
  { value: 'streaming_only', label: 'Streaming only' },
  { value: 'always_bubbles', label: 'Always as bubbles' },
  { value: 'off', label: 'Off' },
]

export function resolveReasoningDeliveryValue(config: Record<string, unknown> | undefined): string {
  const mode = typeof config?.reasoning_delivery === 'string' ? config.reasoning_delivery : ''
  if (mode === 'streaming_only' || mode === 'always_bubbles' || mode === 'off') return mode
  return config?.reasoning_stream === false ? 'off' : 'streaming_only'
}

export function normalizeReasoningDeliveryConfig<T extends Record<string, unknown>>(config: T): T {
  const next = { ...config } as Record<string, unknown>
  if (next.reasoning_delivery !== undefined || next.reasoning_stream !== undefined) {
    next.reasoning_delivery = resolveReasoningDeliveryValue(next)
    delete next.reasoning_stream
  }
  return next as T
}
