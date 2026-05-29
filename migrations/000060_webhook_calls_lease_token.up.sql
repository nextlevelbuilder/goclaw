-- K5: add lease_token to webhook_calls for optimistic-concurrency CAS.
-- ClaimNext sets lease_token = new UUID; UpdateStatus/MarkFailed guard with AND lease_token = $N.
-- ReclaimStale rotates lease_token to NULL so any in-flight CAS fails on next attempt.
ALTER TABLE webhook_calls ADD COLUMN lease_token TEXT;
