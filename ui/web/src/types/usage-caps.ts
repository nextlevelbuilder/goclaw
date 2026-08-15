export interface UsagePricingFields {
  input?: string;
  output?: string;
  cache_read?: string;
  cache_write?: string;
  reasoning?: string;
  request?: string;
  image?: string;
  web_search?: string;
}

export interface UsageCapPolicy {
  id: string;
  tenant_id: string;
  agent_id?: string;
  provider_id?: string;
  provider_type?: string;
  model_id?: string;
  window: "hour" | "day" | "week" | "month";
  max_tokens?: number;
  max_cost_micros?: number;
  source?: "manual" | "agent_budget_monthly_cents";
  enabled: boolean;
  priority: number;
  created_at: string;
  updated_at: string;
}

export interface UsageCapUtilization {
  policy: UsageCapPolicy;
  window_start: string;
  window_end: string;
  used_tokens: number;
  reserved_tokens: number;
  used_cost_micros: number;
  reserved_cost_micros: number;
}

export interface UsageCapEvent {
  id: string;
  policy_id?: string;
  reservation_key?: string;
  decision: "allow" | "block" | "reconcile" | "skip";
  reason?: string;
  estimated_tokens: number;
  estimated_cost_micros: number;
  actual_tokens: number;
  actual_cost_micros: number;
  created_at: string;
}

export interface PricingCatalogEntry {
  id: string;
  model_id: string;
  canonical_model_id?: string;
  pricing: UsagePricingFields;
  synced_at: string;
}

export interface PricingOverride {
  id: string;
  provider_id: string;
  provider_type: string;
  model_id: string;
  pricing: UsagePricingFields;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}
