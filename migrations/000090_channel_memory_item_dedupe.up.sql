DELETE FROM channel_memory_extraction_items i
USING channel_memory_extraction_items keep
WHERE i.tenant_id = keep.tenant_id
  AND i.channel_instance_id = keep.channel_instance_id
  AND i.item_hash = keep.item_hash
  AND i.id <> keep.id
  AND (
    CASE i.status
      WHEN 'written' THEN 5
      WHEN 'approved' THEN 4
      WHEN 'pending_review' THEN 3
      WHEN 'rejected' THEN 2
      WHEN 'deleted' THEN 1
      ELSE 0
    END,
    i.created_at,
    i.id::text
  ) < (
    CASE keep.status
      WHEN 'written' THEN 5
      WHEN 'approved' THEN 4
      WHEN 'pending_review' THEN 3
      WHEN 'rejected' THEN 2
      WHEN 'deleted' THEN 1
      ELSE 0
    END,
    keep.created_at,
    keep.id::text
  );

ALTER TABLE channel_memory_extraction_items
  DROP CONSTRAINT IF EXISTS channel_memory_extraction_items_tenant_id_run_id_item_hash_key;

ALTER TABLE channel_memory_extraction_items
  ADD CONSTRAINT channel_memory_extraction_items_tenant_channel_hash_key
  UNIQUE (tenant_id, channel_instance_id, item_hash);
