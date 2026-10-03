-- +goose Up
-- 成就领取记录：全局成就（scope=global）、世界成就（scope=world）、
-- AI 即兴涌现成就（scope=emergent，不发微光）。dedupe_key 保证幂等防刷。
CREATE TABLE achievement_claims (
    id BINARY(16) PRIMARY KEY,
    user_id BINARY(16) NOT NULL,
    world_id BINARY(16) NULL,
    simulation_id BINARY(16) NULL,
    scope ENUM('global', 'world', 'emergent') NOT NULL,
    code VARCHAR(80) NOT NULL,
    title VARCHAR(120) NOT NULL,
    description VARCHAR(300) NOT NULL DEFAULT '',
    reward INT NOT NULL DEFAULT 0,
    dedupe_key VARCHAR(220) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_achievement_claims_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_achievement_claims_world FOREIGN KEY (world_id) REFERENCES worlds(id),
    CONSTRAINT fk_achievement_claims_simulation FOREIGN KEY (simulation_id) REFERENCES simulations(id),
    UNIQUE KEY uq_achievement_claims_dedupe (dedupe_key),
    KEY idx_achievement_claims_user (user_id, created_at),
    KEY idx_achievement_claims_world (world_id)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE IF EXISTS achievement_claims;