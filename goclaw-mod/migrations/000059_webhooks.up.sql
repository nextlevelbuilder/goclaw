-- Webhook registry + call audit log.
-- tenant_id on every row — all queries must include WHERE tenant_id = $N.
-- secret_hash stores SHA-256 hex; raw secret returned only once on create (phase-04).

-- ============================================================
-- Table: webhooks  (registry)
-- ============================================================
CREATE TABLE webhooks (
    id                  uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id           uuid        NOT NULL,
    agent_id            uuid        REFERENCES agents(id) ON DELETE SET NULL,
    name                text        NOT NULL,
    kind                text        NOT NULL CHECK (kind IN ('llm', 'message')),
    secret_prefix       text,
    secret_hash         text        NOT NULL,
    scopes              text[]      NOT NULL DEFAULT '{}',
    channel_id          uuid,
    rate_limit_per_min  int         NOT NULL DEFAULT 60,
    ip_allowlist        text[]      NOT NULL DEFAULT '{}',
    require_hmac        boolean     NOT NULL DEFAULT false,
    localhost_only      boolean     NOT NULL DEFAULT false,
    revoked             boolean     NOT NULL DEFAULT false,
    created_by          text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    last_used_at        timestamptz
);

CREATE INDEX idx_webhooks_tenant          ON webhooks (tenant_id);
CREATE INDEX idx_webhooks_tenant_agent    ON webhooks (tenant_id, agent_id);
CREATE UNIQUE INDEX uq_webhooks_secret    ON webhooks (secret_hash) WHERE revoked = false;

-- ============================================================
-- Table: webhook_calls  (audit + async state)
-- ============================================================
CREATE TABLE webhook_calls (
    id               uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        uuid        NOT NULL,
    webhook_id       uuid        NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    agent_id         uuid,
    idempotency_key  text,
    mode             text        NOT NULL CHECK (mode IN ('sync', 'async')),
    callback_url     text,
    status           text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'done', 'failed', 'dead')),
    attempts         int         NOT NULL DEFAULT 0,
    delivery_id      uuid        NOT NULL DEFAULT gen_random_uuid(),
    next_attempt_at  timestamptz,
    started_at       timestamptz,
    request_payload  jsonb,
    response         jsonb,
    last_error       text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    completed_at     timestamptz
);

CREATE INDEX idx_webhook_calls_tenant_created   ON webhook_calls (tenant_id, created_at DESC);
CREATE INDEX idx_webhook_calls_status_attempt   ON webhook_calls (status, next_attempt_at);
CREATE UNIQUE INDEX uq_webhook_calls_idempotency
    ON webhook_calls (webhook_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
