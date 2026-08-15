-- K6: store raw webhook secret encrypted at rest (AES-256-GCM via GOCLAW_ENCRYPTION_KEY).
-- encrypted_secret holds crypto.Encrypt(raw_secret, encKey) — never the raw bytes.
-- secret_hash is retained for bearer-token lookup (globally unique index).
-- HMAC signing uses decrypted encrypted_secret (raw bytes), not hex(secret_hash).
-- Existing webhooks (feature not shipped to prod) have encrypted_secret = '' → require rotation.
ALTER TABLE webhooks ADD COLUMN encrypted_secret TEXT NOT NULL DEFAULT '';
