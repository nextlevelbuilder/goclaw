// Webhook registry + delivery-history types. Mirrors internal/store/webhook_store.go
// (WebhookData / webhookCallResp) and internal/http/webhooks_admin.go request shapes.

export type WebhookKind = "llm" | "message";

export interface WebhookData {
  id: string;
  tenant_id: string;
  agent_id?: string;
  name: string;
  kind: WebhookKind;
  secret_prefix: string;
  scopes: string[];
  channel_id?: string;
  rate_limit_per_min: number;
  ip_allowlist: string[];
  require_hmac: boolean;
  localhost_only: boolean;
  revoked: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
  last_used_at?: string;
}

// Create payload — POST /v1/webhooks.
export interface WebhookCreateInput {
  name: string;
  kind: WebhookKind;
  agent_id?: string;
  channel_id?: string;
  rate_limit_per_min?: number;
  ip_allowlist?: string[];
  require_hmac?: boolean;
  localhost_only?: boolean;
}

// PATCH /v1/webhooks/{id} — kind/agent_id are immutable after create.
export interface WebhookUpdateInput {
  name?: string;
  channel_id?: string;
  rate_limit_per_min?: number;
  ip_allowlist?: string[];
  require_hmac?: boolean;
  localhost_only?: boolean;
}

// Response from create — secret + hmac_signing_key shown ONCE.
export interface WebhookCreateResponse extends WebhookData {
  secret: string;
  hmac_signing_key: string;
}

// Response from POST /v1/webhooks/{id}/rotate — secret shown ONCE.
export interface WebhookRotateResponse {
  id: string;
  secret: string;
  hmac_signing_key: string;
  secret_prefix: string;
}

// Delivery-history record — GET /v1/webhooks/{id}/calls (webhookCallResp DTO).
export interface WebhookCallData {
  id: string;
  delivery_id: string;
  mode: "sync" | "async";
  status: "queued" | "running" | "done" | "failed" | "dead";
  attempts: number;
  next_attempt_at?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  last_error?: string;
  response?: string;
}

// Full delivery detail — GET /v1/webhooks/{id}/calls/{callId} (webhookCallDetailResp DTO).
export interface WebhookCallDetail {
  id: string;
  webhook_id: string;
  agent_id?: string;
  delivery_id: string;
  idempotency_key?: string;
  mode: "sync" | "async";
  status: "queued" | "running" | "done" | "failed" | "dead";
  callback_url?: string;
  attempts: number;
  next_attempt_at?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
  last_error?: string;
  request_payload?: string;
  response?: string;
}

// POST /v1/webhooks/{id}/test request — fields used depend on webhook kind.
export interface WebhookTestInput {
  // llm
  input?: string;
  model?: string;
  // message
  channel_name?: string;
  chat_id?: string;
  content?: string;
  media_url?: string;
  media_caption?: string;
  fallback_to_text?: boolean;
}

export interface WebhookTestLLMResult {
  call_id: string;
  agent_id: string;
  output: string;
  finish_reason: string;
  usage?: { prompt_tokens: number; completion_tokens: number; total_tokens: number };
}

export interface WebhookTestMessageResult {
  call_id: string;
  status: string;
  channel_name: string;
  chat_id: string;
  warning?: string;
}

export type WebhookTestResult = WebhookTestLLMResult | WebhookTestMessageResult;

// Paginated list envelope — matches {items,total,limit,offset} from the admin handlers.
export interface Paginated<T> {
  items: T[];
  total: number;
  limit: number;
  offset: number;
}
