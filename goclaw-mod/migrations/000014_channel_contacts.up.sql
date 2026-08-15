-- Channel contacts: auto-collected user info from all channels.
-- Global (not per-agent). Used for contact selector, future RBAC, analytics.
CREATE TABLE IF NOT EXISTS channel_contacts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_type     VARCHAR(50) NOT NULL,
    channel_instance VARCHAR(255),
    sender_id        VARCHAR(255) NOT NULL,
    user_id          VARCHAR(255),
    display_name     VARCHAR(255),
    username         VARCHAR(255),
    avatar_url       TEXT,
    peer_kind        VARCHAR(20),
    metadata         JSONB DEFAULT '{}',
    merged_id        UUID,
    first_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (channel_type, sender_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_contacts_instance ON channel_contacts(channel_instance) WHERE channel_instance IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_channel_contacts_merged ON channel_contacts(merged_id) WHERE merged_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_channel_contacts_search ON channel_contacts(display_name, username);
