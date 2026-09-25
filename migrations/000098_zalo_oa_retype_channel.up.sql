-- Reserve zalo_oa for Official Account OAuth v4. Existing rows are Bot API
-- instances; preserve their names, credentials, config, and tenant bindings.
UPDATE channel_instances SET channel_type = 'zalo_bot' WHERE channel_type = 'zalo_oa';
