CREATE TABLE skill_evolution_settings (
    tenant_id        UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id         UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    enabled          BOOLEAN NOT NULL DEFAULT false,
    mode             VARCHAR(32) NOT NULL DEFAULT 'suggest_only',
    last_analyzed_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, skill_id),
    CONSTRAINT chk_skill_evolution_mode CHECK (mode IN ('suggest_only', 'auto_analyze'))
);

CREATE INDEX idx_skill_evolution_settings_skill ON skill_evolution_settings(skill_id);

CREATE TABLE skill_usage_metrics (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id          UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    skill_slug        VARCHAR(255) NOT NULL,
    skill_version     INT NOT NULL DEFAULT 1,
    agent_id          UUID REFERENCES agents(id) ON DELETE SET NULL,
    user_id           VARCHAR(255),
    session_key       TEXT,
    trace_id          TEXT,
    invocation_id     TEXT,
    invocation_source VARCHAR(32) NOT NULL DEFAULT 'runtime',
    status            VARCHAR(32) NOT NULL DEFAULT 'started',
    failure_reason    TEXT,
    tool_calls_count  INT NOT NULL DEFAULT 0,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_skill_usage_status CHECK (status IN ('started', 'succeeded', 'failed', 'abandoned'))
);

CREATE INDEX idx_skill_usage_metrics_skill_created ON skill_usage_metrics(skill_id, created_at DESC);
CREATE INDEX idx_skill_usage_metrics_tenant_created ON skill_usage_metrics(tenant_id, created_at DESC);
CREATE INDEX idx_skill_usage_metrics_status ON skill_usage_metrics(skill_id, status, created_at DESC);
CREATE INDEX idx_skill_usage_metrics_invocation ON skill_usage_metrics(invocation_id) WHERE invocation_id IS NOT NULL;

CREATE TABLE skill_improvement_suggestions (
    id                     UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id              UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id               UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    skill_slug             VARCHAR(255) NOT NULL,
    suggestion_type        VARCHAR(64) NOT NULL,
    status                 VARCHAR(32) NOT NULL DEFAULT 'pending',
    reason                 TEXT NOT NULL DEFAULT '',
    evidence               JSONB NOT NULL DEFAULT '{}',
    draft_patch            JSONB NOT NULL DEFAULT '{}',
    target_file            TEXT NOT NULL DEFAULT '',
    created_by_actor_type  VARCHAR(32) NOT NULL DEFAULT '',
    created_by_actor_id    VARCHAR(255) NOT NULL DEFAULT '',
    reviewed_by_actor_type VARCHAR(32) NOT NULL DEFAULT '',
    reviewed_by_actor_id   VARCHAR(255) NOT NULL DEFAULT '',
    reviewed_at            TIMESTAMPTZ,
    applied_version        INT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_skill_suggestion_status CHECK (status IN ('pending', 'approved', 'rejected', 'applied'))
);

CREATE INDEX idx_skill_suggestions_skill_status_created ON skill_improvement_suggestions(skill_id, status, created_at DESC);
CREATE INDEX idx_skill_suggestions_tenant_created ON skill_improvement_suggestions(tenant_id, created_at DESC);

CREATE TABLE skill_versions (
    id                         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id                  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    skill_id                   UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    version                    INT NOT NULL,
    content_hash               VARCHAR(64) NOT NULL DEFAULT '',
    changed_files              JSONB NOT NULL DEFAULT '[]',
    created_by_actor_type      VARCHAR(32) NOT NULL DEFAULT '',
    created_by_actor_id        VARCHAR(255) NOT NULL DEFAULT '',
    created_from_suggestion_id UUID REFERENCES skill_improvement_suggestions(id) ON DELETE SET NULL,
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(skill_id, version)
);

CREATE INDEX idx_skill_versions_tenant_skill ON skill_versions(tenant_id, skill_id, version DESC);

INSERT INTO skill_versions (
    tenant_id, skill_id, version, content_hash, changed_files,
    created_by_actor_type, created_by_actor_id, created_at
)
SELECT tenant_id, id, version, COALESCE(file_hash, ''), '[]'::jsonb,
       'system', 'migration', COALESCE(created_at, NOW())
FROM skills
WHERE status != 'deleted'
ON CONFLICT (skill_id, version) DO NOTHING;
