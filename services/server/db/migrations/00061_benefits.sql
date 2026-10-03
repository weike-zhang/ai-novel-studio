-- +goose Up
CREATE TABLE user_benefits (
    user_id BINARY(16) PRIMARY KEY,
    cover_allowance INT NOT NULL DEFAULT 0,
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_user_benefits_user FOREIGN KEY (user_id) REFERENCES users(id)
) ENGINE=InnoDB;

CREATE TABLE checkins (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BINARY(16) NOT NULL,
    checkin_date DATE NOT NULL,
    streak_days INT NOT NULL DEFAULT 1,
    reward_credits INT NOT NULL DEFAULT 0,
    bonus_credits INT NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_checkins_user FOREIGN KEY (user_id) REFERENCES users(id),
    UNIQUE KEY uq_checkins_user_date (user_id, checkin_date),
    KEY idx_checkins_user_date (user_id, checkin_date)
) ENGINE=InnoDB;

CREATE TABLE benefit_grants (
    id BINARY(16) PRIMARY KEY,
    user_id BINARY(16) NOT NULL,
    grant_type VARCHAR(40) NOT NULL,
    credits INT NOT NULL DEFAULT 0,
    cover_allowance INT NOT NULL DEFAULT 0,
    idempotency_key VARCHAR(160) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_benefit_grants_user FOREIGN KEY (user_id) REFERENCES users(id),
    UNIQUE KEY uq_benefit_grants_idempotency (user_id, idempotency_key),
    KEY idx_benefit_grants_user (user_id, created_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE IF EXISTS benefit_grants;
DROP TABLE IF EXISTS checkins;
DROP TABLE IF EXISTS user_benefits;
