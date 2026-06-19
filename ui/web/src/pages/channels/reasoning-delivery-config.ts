export const REASONING_DELIVERY_VALUES = ["streaming_only", "always_bubbles", "off"] as const;

export type ReasoningDeliveryMode = (typeof REASONING_DELIVERY_VALUES)[number];

export const reasoningDeliveryOptions: { value: ReasoningDeliveryMode; label: string }[] = [
  { value: "streaming_only", label: "Streaming only" },
  { value: "always_bubbles", label: "Always as bubbles" },
  { value: "off", label: "Off" },
];

export function resolveReasoningDeliveryValue(config: Record<string, unknown> | undefined): ReasoningDeliveryMode {
  const mode = typeof config?.reasoning_delivery === "string" ? config.reasoning_delivery : "";
  if (isReasoningDeliveryMode(mode)) return mode;
  return config?.reasoning_stream === false ? "off" : "streaming_only";
}

export function normalizeReasoningDeliveryConfig<T extends Record<string, unknown>>(config: T): T {
  const next = { ...config } as Record<string, unknown>;
  if (next.reasoning_delivery !== undefined || next.reasoning_stream !== undefined) {
    next.reasoning_delivery = resolveReasoningDeliveryValue(next);
    delete next.reasoning_stream;
  }
  return next as T;
}

function isReasoningDeliveryMode(value: string): value is ReasoningDeliveryMode {
  return (REASONING_DELIVERY_VALUES as readonly string[]).includes(value);
}
