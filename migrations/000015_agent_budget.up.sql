ALTER TABLE agents ADD COLUMN budget_monthly_cents INTEGER;

CREATE TABLE activity_logs (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    actor_type  VARCHAR(20) NOT NULL,
    actor_id    VARCHAR(255) NOT NULL,
    action      VARCHAR(100) NOT NULL,
    entity_type VARCHAR(50),
    entity_id   VARCHAR(255),
    details     JSONB,
    ip_address  VARCHAR(45),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_activity_logs_actor ON activity_logs (actor_type, actor_id);
CREATE INDEX idx_activity_logs_action ON activity_logs (action);
CREATE INDEX idx_activity_logs_entity ON activity_logs (entity_type, entity_id);
CREATE INDEX idx_activity_logs_created ON activity_logs (created_at DESC);
