// Evolution types matching web UI + Go backend JSON responses

export interface ToolAggregate {
  tool_name: string
  call_count: number
  success_rate: number
  avg_duration_ms: number
}

export interface RetrievalAggregate {
  source: string
  query_count: number
  usage_rate: number
  avg_score: number
}

export interface AggregatedMetrics {
  tool_aggregates: ToolAggregate[] | null
  retrieval_aggregates: RetrievalAggregate[] | null
}

export interface EvolutionSuggestion {
  id: string
  agent_id: string
  suggestion_type: string
  suggestion: string
  rationale: string
  parameters: Record<string, unknown> | null
  status: string
  reviewed_by: string | null
  reviewed_at: string | null
  created_at: string
}

export interface AdaptationGuardrails {
  max_delta_per_cycle: number
  min_data_points: number
  rollback_on_drop_pct: number
  locked_params: string[]
}
