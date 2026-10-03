-- +goose Up
CREATE TABLE email_delivery_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email_hash BINARY(32) NOT NULL,
    purpose VARCHAR(40) NOT NULL,
    provider_msg_id VARCHAR(200) NULL,
    accepted TINYINT(1) NOT NULL,
    result VARCHAR(80) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_email_delivery_logs_hash_created (email_hash, created_at)
) ENGINE=InnoDB;

CREATE TABLE registration_risk_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    email_hash BINARY(32) NOT NULL,
    ip_prefix_hash BINARY(32) NOT NULL,
    risk_cookie_hash BINARY(32) NULL,
    asn VARCHAR(40) NULL,
    turnstile_result VARCHAR(40) NULL,
    risk_level ENUM('low', 'medium', 'high') NOT NULL,
    action ENUM('allow', 'allow_without_grant', 'challenge', 'manual_review', 'block') NOT NULL,
    grant_granted TINYINT(1) NOT NULL DEFAULT 0,
    expires_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_registration_risk_events_created (created_at),
    KEY idx_registration_risk_events_cookie (risk_cookie_hash, created_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE IF EXISTS registration_risk_events;
DROP TABLE IF EXISTS email_delivery_logs;
