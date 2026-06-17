ALTER TABLE skill_user_grants
    DROP CONSTRAINT IF EXISTS skill_user_grants_skill_id_user_id_tenant_id_key;

ALTER TABLE skill_user_grants
    ADD CONSTRAINT skill_user_grants_skill_id_user_id_key
    UNIQUE (skill_id, user_id);
