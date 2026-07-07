ALTER TABLE channel_memory_extraction_items
  DROP CONSTRAINT IF EXISTS channel_memory_extraction_items_tenant_channel_hash_key;

ALTER TABLE channel_memory_extraction_items
  ADD CONSTRAINT channel_memory_extraction_items_tenant_id_run_id_item_hash_key
  UNIQUE (tenant_id, run_id, item_hash);
