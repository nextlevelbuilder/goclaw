UPDATE cron_jobs SET payload = jsonb_set(
  jsonb_set(
    jsonb_set(
      jsonb_set(payload, '{deliver}', to_jsonb(deliver)),
      '{channel}', to_jsonb(deliver_channel)
    ),
    '{to}', to_jsonb(deliver_to)
  ),
  '{wake_heartbeat}', to_jsonb(wake_heartbeat)
);

ALTER TABLE cron_jobs DROP COLUMN stateless;
ALTER TABLE cron_jobs DROP COLUMN deliver;
ALTER TABLE cron_jobs DROP COLUMN deliver_channel;
ALTER TABLE cron_jobs DROP COLUMN deliver_to;
ALTER TABLE cron_jobs DROP COLUMN wake_heartbeat;
