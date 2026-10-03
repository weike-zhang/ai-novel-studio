-- +goose Up
ALTER TABLE ai_jobs
    ADD COLUMN lease_owner VARCHAR(96) NULL AFTER input_version,
    ADD COLUMN lease_expires_at DATETIME(6) NULL AFTER lease_owner,
    ADD KEY idx_ai_jobs_lease (status, lease_expires_at);

ALTER TABLE outbox_events
    ADD COLUMN publish_attempts INT NOT NULL DEFAULT 0 AFTER payload_json,
    ADD COLUMN publish_claimed_by VARCHAR(96) NULL AFTER publish_attempts,
    ADD COLUMN publish_claimed_at DATETIME(6) NULL AFTER publish_claimed_by,
    ADD COLUMN last_error VARCHAR(500) NULL AFTER publish_claimed_at,
    ADD KEY idx_outbox_claim (published_at, publish_claimed_at, created_at);

-- +goose Down
ALTER TABLE outbox_events
    DROP KEY idx_outbox_claim,
    DROP COLUMN last_error,
    DROP COLUMN publish_claimed_at,
    DROP COLUMN publish_claimed_by,
    DROP COLUMN publish_attempts;

ALTER TABLE ai_jobs
    DROP KEY idx_ai_jobs_lease,
    DROP COLUMN lease_expires_at,
    DROP COLUMN lease_owner;
