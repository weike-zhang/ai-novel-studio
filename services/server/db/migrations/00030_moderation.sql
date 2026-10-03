-- +goose Up
CREATE TABLE moderation_decisions (
    id BINARY(16) PRIMARY KEY,
    content_type VARCHAR(40) NOT NULL,
    content_id BINARY(16) NOT NULL,
    content_version BIGINT NOT NULL,
    category VARCHAR(80) NOT NULL,
    action ENUM('allow', 'allow_with_label', 'revise', 'manual_review', 'block') NOT NULL,
    reason_code VARCHAR(80) NOT NULL,
    rule_version VARCHAR(40) NOT NULL,
    source VARCHAR(40) NOT NULL,
    content_hash BINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_moderation_decisions_world FOREIGN KEY (content_id) REFERENCES worlds(id),
    KEY idx_moderation_world_version (content_id, content_version),
    KEY idx_moderation_action_created (action, created_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE moderation_decisions;
