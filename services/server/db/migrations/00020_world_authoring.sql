-- +goose Up
ALTER TABLE worlds
    ADD COLUMN draft_source ENUM('manual', 'ai') NOT NULL DEFAULT 'manual' AFTER setting_json,
    ADD COLUMN human_confirmed_version BIGINT NULL AFTER version,
    ADD COLUMN human_confirmed_fields JSON NULL AFTER human_confirmed_version,
    ADD COLUMN human_confirmed_at DATETIME(6) NULL AFTER human_confirmed_fields,
    ADD COLUMN published_at DATETIME(6) NULL AFTER human_confirmed_at,
    ADD COLUMN unpublished_at DATETIME(6) NULL AFTER published_at,
    ADD CONSTRAINT chk_worlds_human_confirmation_version
        CHECK (human_confirmed_version IS NULL OR human_confirmed_version > 0),
    ADD KEY idx_worlds_owner_authoring (owner_id, status, updated_at),
    ADD KEY idx_worlds_publication (visibility, status, published_at);

UPDATE worlds
SET published_at = updated_at
WHERE visibility = 'public' AND status = 'published' AND published_at IS NULL;

-- +goose Down
ALTER TABLE worlds
    DROP KEY idx_worlds_publication,
    DROP KEY idx_worlds_owner_authoring,
    DROP CHECK chk_worlds_human_confirmation_version,
    DROP COLUMN unpublished_at,
    DROP COLUMN published_at,
    DROP COLUMN human_confirmed_at,
    DROP COLUMN human_confirmed_fields,
    DROP COLUMN human_confirmed_version,
    DROP COLUMN draft_source;
