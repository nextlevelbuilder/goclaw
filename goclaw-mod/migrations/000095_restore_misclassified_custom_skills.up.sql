-- Restore custom skills previously converted to system skills by an unscoped
-- bundled-skill seed lookup. The faulty seed wrote bundled content into the
-- next managed version; startup validates the preceding version before using
-- it as recovery data. Genuine bundled skills always use owner_id='system'.
-- Visibility and lifecycle state cannot
-- be recovered from the overwritten row, so default to the non-public,
-- non-active state until the owner explicitly reviews the repaired skill.
UPDATE skills
SET is_system = false,
    visibility = 'private',
    status = 'archived',
    frontmatter = COALESCE(frontmatter, '{}'::jsonb) ||
        '{"_goclaw_recovery":"bundled_slug_collision"}'::jsonb,
    version = GREATEST(version - 1, 1),
    file_path = regexp_replace(
        file_path,
        '/[0-9]+$',
        '/' || GREATEST(version - 1, 1)::text
    ),
    updated_at = NOW()
WHERE is_system = true
  AND owner_id <> 'system';
