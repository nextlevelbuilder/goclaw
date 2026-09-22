-- OAuth OA instances must be removed before running an older gateway binary.
-- Restore the legacy Bot API type without touching encrypted credentials.
UPDATE channel_instances SET channel_type = 'zalo_oa' WHERE channel_type = 'zalo_bot';
