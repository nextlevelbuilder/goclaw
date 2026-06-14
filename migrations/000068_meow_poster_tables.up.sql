-- ============================================================
-- Meow Telegram channel autopilot ("mp_" = meow-poster).
-- PostgreSQL-only: the channel system is gated OFF in Lite/desktop
-- (edition.AllowsChannels()==false for non-standard), so these tables
-- are never created in the SQLite schema. Standard (PG) edition only.
-- Every table carries tenant_id; all queries are tenant-scoped.
-- ============================================================

-- Channel registry: one row per managed Telegram channel.
CREATE TABLE IF NOT EXISTS mp_channels (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    handle        VARCHAR(255) NOT NULL,            -- e.g. "@TonSlotOfficial"
    chat_id       VARCHAR(64),                      -- numeric Telegram chat id, resolved at runtime
    brand_key     VARCHAR(64) NOT NULL,             -- e.g. "ton-slot"
    tz            VARCHAR(64) NOT NULL DEFAULT 'Asia/Seoul',
    has_mascot    BOOLEAN NOT NULL DEFAULT false,
    launched      BOOLEAN NOT NULL DEFAULT false,   -- pre-launch channels never auto-post / pump
    enabled       BOOLEAN NOT NULL DEFAULT true,
    smm_tier      VARCHAR(32) NOT NULL DEFAULT '',  -- engagement tier key (smmcity), '' = none
    smm_caps      JSONB NOT NULL DEFAULT '{}',      -- per-channel engagement caps
    button_set    JSONB NOT NULL DEFAULT '[]',      -- inline buttons (label/url), C9 allowlist
    subs_baseline INTEGER NOT NULL DEFAULT 0,       -- operational subscriber baseline
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, handle),
    -- FK target for child composite (id, tenant_id) references — lets child
    -- rows enforce that their tenant matches this channel's tenant.
    UNIQUE(id, tenant_id)
);

-- Content posts: scheduled / draft / published posts per channel.
CREATE TABLE IF NOT EXISTS mp_content_posts (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_id       UUID NOT NULL,
    scheduled_date   DATE NOT NULL,                 -- the local-calendar slot day
    scheduled_at_utc TIMESTAMPTZ,                   -- resolved fire time (UTC)
    status           VARCHAR(20) NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft','approved','publishing','published','skipped')),
    ko_text          TEXT NOT NULL DEFAULT '',
    en_text          TEXT NOT NULL DEFAULT '',
    image_path       TEXT NOT NULL DEFAULT '',
    buttons          JSONB NOT NULL DEFAULT '[]',
    tg_message_id    BIGINT,                        -- set on successful publish
    tg_link          TEXT NOT NULL DEFAULT '',
    approved_by      VARCHAR(255) NOT NULL DEFAULT '',
    approved_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Composite FK: a post's tenant MUST match its channel's tenant. Blocks
    -- inserting a post under tenant B that points at tenant A's channel.
    FOREIGN KEY (channel_id, tenant_id) REFERENCES mp_channels(id, tenant_id) ON DELETE CASCADE,
    -- FK target for mp_smm_orders composite (post_id, tenant_id).
    UNIQUE(id, tenant_id)
);

-- Exactly-once publish guard: at most one publishing/published post per
-- channel per calendar day (a missed/skipped day can be retried).
CREATE UNIQUE INDEX IF NOT EXISTS idx_mp_posts_publish_once
    ON mp_content_posts(channel_id, scheduled_date)
    WHERE status IN ('publishing','published');

-- Scheduler hot path: due-post lookup by channel/time/status.
CREATE INDEX IF NOT EXISTS idx_mp_posts_due
    ON mp_content_posts(channel_id, scheduled_at_utc, status);

-- Daily subscriber metrics per channel.
CREATE TABLE IF NOT EXISTS mp_channel_metrics (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_id       UUID NOT NULL,
    date             DATE NOT NULL,
    subscriber_count INTEGER NOT NULL DEFAULT 0,
    delta            INTEGER NOT NULL DEFAULT 0,
    stale            BOOLEAN NOT NULL DEFAULT false,   -- count carried from a prior day (probe failed)
    alerted_for_date BOOLEAN NOT NULL DEFAULT false,   -- drop-alert dedup
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Composite FK: metric tenant MUST match its channel's tenant.
    FOREIGN KEY (channel_id, tenant_id) REFERENCES mp_channels(id, tenant_id) ON DELETE CASCADE,
    UNIQUE(channel_id, date)
);

-- smmcity engagement orders, one row per (post, service) — no double-spend.
CREATE TABLE IF NOT EXISTS mp_smm_orders (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    post_id      UUID NOT NULL,
    service_id   VARCHAR(64) NOT NULL,
    qty          INTEGER NOT NULL DEFAULT 0,
    smm_order_id VARCHAR(128) NOT NULL DEFAULT '',
    status       VARCHAR(20) NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending','placed','done','error')),
    cost         DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- Composite FK: order tenant MUST match its post's tenant.
    FOREIGN KEY (post_id, tenant_id) REFERENCES mp_content_posts(id, tenant_id) ON DELETE CASCADE,
    UNIQUE(post_id, service_id)
);

-- Report idempotency ledger: one row per (tenant, period_kind, period_key).
-- tenant_id is part of the key for multi-tenant correctness (each owner
-- tenant gets its own daily/weekly/monthly report ledger).
CREATE TABLE IF NOT EXISTS mp_reports_sent (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    period_kind VARCHAR(16) NOT NULL
                  CHECK (period_kind IN ('daily','weekly','monthly')),
    period_key  VARCHAR(32) NOT NULL,               -- e.g. "2026-06-14", "2026-W24", "2026-06"
    sent_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, period_kind, period_key)
);
