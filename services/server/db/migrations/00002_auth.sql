-- +goose Up
ALTER TABLE users
    ADD COLUMN account_name_normalized VARCHAR(20) CHARACTER SET ascii COLLATE ascii_bin GENERATED ALWAYS AS (LOWER(TRIM(account_name))) STORED AFTER account_name,
    ADD COLUMN password_hash VARCHAR(255) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER account_name_normalized,
    ADD COLUMN email_normalized VARCHAR(320) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER password_hash,
    ADD COLUMN email_ciphertext VARBINARY(1024) NULL AFTER email_normalized,
    ADD COLUMN email_verified_at DATETIME(6) NULL AFTER email_ciphertext,
    ADD COLUMN recovery_code_digest BINARY(32) NULL AFTER email_verified_at,
    ADD COLUMN recovery_code_rotated_at DATETIME(6) NULL AFTER recovery_code_digest,
    ADD COLUMN adult_confirmed_at DATETIME(6) NULL AFTER recovery_code_rotated_at,
    ADD COLUMN terms_version VARCHAR(40) NULL AFTER adult_confirmed_at;

ALTER TABLE users
    ADD UNIQUE KEY uq_users_account_name_normalized (account_name_normalized),
    ADD UNIQUE KEY uq_users_email_normalized (email_normalized);

CREATE TABLE auth_sessions (
    id BINARY(16) PRIMARY KEY,
    user_id BINARY(16) NOT NULL,
    family_id BINARY(16) NOT NULL,
    token_digest BINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    last_used_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    expires_at DATETIME(6) NULL,
    rotated_at DATETIME(6) NULL,
    rotation_grace_ends_at DATETIME(6) NULL,
    revoked_at DATETIME(6) NULL,
    CONSTRAINT fk_auth_sessions_user FOREIGN KEY (user_id) REFERENCES users(id),
    UNIQUE KEY uq_auth_sessions_token_digest (token_digest),
    KEY idx_auth_sessions_user_active (user_id, revoked_at),
    KEY idx_auth_sessions_family (family_id, created_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE IF EXISTS auth_sessions;

ALTER TABLE users
    DROP INDEX uq_users_email_normalized,
    DROP INDEX uq_users_account_name_normalized,
    DROP COLUMN terms_version,
    DROP COLUMN adult_confirmed_at,
    DROP COLUMN recovery_code_rotated_at,
    DROP COLUMN recovery_code_digest,
    DROP COLUMN email_verified_at,
    DROP COLUMN email_ciphertext,
    DROP COLUMN email_normalized,
    DROP COLUMN password_hash,
    DROP COLUMN account_name_normalized;
