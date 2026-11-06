/** Per-model catalogue entry from GET /v1/providers/capabilities. */
export interface ProviderModelCapability {
  /** Full `<provider>/<model>` identity as served by the gateway. */
  id: string
  label?: string
  context_window?: number
  max_tokens?: number
  thinking_levels?: string[]
  default_thinking_level?: string
  supports_fast_mode?: boolean
  capabilities?: {
    tool_calling?: boolean
    vision?: boolean
    stream_with_tools?: boolean
    cache_control?: boolean
  }
  cost?: { input?: number; output?: number }
  /** True when the gateway could not refresh this entry from upstream. */
  stale?: boolean
}

/** Provider entry from GET /v1/providers/capabilities (Viewer role). */
export interface ProviderCapability {
  /** Provider name, e.g. "groq" — the value stored in agent configs. */
  id: string
  provider_id?: string
  label?: string
  wire_api?: string
  auth_kind?: string
  model_source?: string
  default_model_id?: string
  models?: ProviderModelCapability[]
}

export interface ProviderData {
  id: string
  name: string
  display_name?: string
  provider_type: string
  api_base?: string
  api_key?: string
  enabled: boolean
  settings?: Record<string, unknown>
  created_at?: string
  updated_at?: string
  // --- Provider subsystem rework: transport / auth / catalogue metadata ---
  /** Transport family, e.g. `openai-completions`, `anthropic-messages`, `cli-delegated`. */
  wire_api?: string
  /** Credential kind, e.g. `api_key`, `oauth_browser`, `service_account`, `cli_delegated`, `none`. */
  auth_kind?: string
  /** Where the model catalogue came from: `bundled`, `discovered`, or a stale marker. */
  model_source?: string
  /** Default `<provider>/<model>` identity for this provider. */
  default_model_id?: string
  /** Per-model catalogue (populated from the capabilities endpoint). */
  models?: ProviderModelCapability[]
}

export interface ProviderInput {
  name: string
  display_name?: string
  provider_type: string
  api_base?: string
  api_key?: string
  enabled?: boolean
  settings?: Record<string, unknown>
}

// --- Provider health and usage (GET /v1/providers/{id}/health, /usage-detail, /v1/usage/*) ---

/** Runtime failover classes served by the gateway; anything else maps to `unknown`. */
export const PROVIDER_ERROR_CLASSES = [
  'rate_limit',
  'overloaded',
  'timeout',
  'auth',
  'auth_permanent',
  'billing',
  'format',
  'model_not_found',
  'content_policy',
  'server_error',
  'context_overflow',
  'unknown',
] as const

export type ProviderErrorClass = (typeof PROVIDER_ERROR_CLASSES)[number]

/** GET /v1/providers/{id}/health */
export interface ProviderHealth {
  provider_id: string
  /** Provider name (not the UUID). */
  provider: string
  enabled: boolean
  consecutive_failures: number
  cooling_down: boolean
  /** RFC3339, or null when no cooldown was ever set. */
  cooldown_until: string | null
  /** Empty string when the provider has not failed. */
  last_error_class: string
  error_counts: Record<string, number>
  last_probe_at: string | null
  updated_at: string | null
  max_cooldown_seconds: number
}

export interface ProviderUsageErrorItem {
  /** RFC3339 time of the newest failed call in the group. */
  at: string
  error_class: string
  count: number
}

/** GET /v1/providers/{id}/usage-detail */
export interface ProviderUsageDetail {
  provider: string
  calls: number
  errors: number
  latency_ms: { p50: number; p95: number }
  /** Newest first. */
  recent_errors: ProviderUsageErrorItem[]
}

/** One period of GET /v1/usage/summary. */
export interface UsageSummaryPeriod {
  requests: number
  input_tokens: number
  output_tokens: number
  cost: number
  unique_users: number
  errors: number
  llm_calls: number
  tool_calls: number
  avg_duration_ms: number
}

/** GET /v1/usage/summary */
export interface UsageSummary {
  current: UsageSummaryPeriod
  previous: UsageSummaryPeriod
}

/** One point of GET /v1/usage/timeseries (only the fields this UI reads). */
export interface UsageTimeSeriesPoint {
  /** RFC3339 bucket start (UTC midnight when group_by=day). */
  bucket_time: string
  input_tokens: number
  output_tokens: number
  total_cost: number
  request_count: number
  error_count: number
}

export type UsageRange = '24h' | '7d' | '30d'

/** Everything the Usage tab renders for one provider and range. */
export interface ProviderUsageBundle {
  detail: ProviderUsageDetail
  summary: UsageSummaryPeriod
  points: UsageTimeSeriesPoint[]
}
