-- Channel groups directory: stores known groups across all channel types.
-- Used by allow_from picker, managers tab, and contacts page.
CREATE TABLE IF NOT EXISTS channel_groups (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id        UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001',
    channel_type     VARCHAR(50)  NOT NULL,
    channel_instance VARCHAR(255),
    group_id         VARCHAR(255) NOT NULL,
    group_name       VARCHAR(255),
    avatar_url       TEXT,
    member_count     INT          DEFAULT 0,
    first_seen_at    TIMESTAMPTZ  DEFAULT NOW(),
    last_seen_at     TIMESTAMPTZ  DEFAULT NOW(),
    UNIQUE(tenant_id, channel_type, group_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_groups_channel_type ON channel_groups (tenant_id, channel_type);
CREATE INDEX IF NOT EXISTS idx_channel_groups_name ON channel_groups (group_name);
