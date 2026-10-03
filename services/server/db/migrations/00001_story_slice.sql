-- +goose Up
CREATE TABLE users (
    id BINARY(16) PRIMARY KEY,
    account_name VARCHAR(20) NOT NULL,
    nickname VARCHAR(40) NOT NULL,
    status ENUM('active', 'suspended') NOT NULL DEFAULT 'active',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    UNIQUE KEY uq_users_account_name (account_name)
) ENGINE=InnoDB;

CREATE TABLE worlds (
    id BINARY(16) PRIMARY KEY,
    owner_id BINARY(16) NOT NULL,
    title VARCHAR(80) NOT NULL,
    summary VARCHAR(500) NOT NULL,
    setting_json JSON NOT NULL,
    visibility ENUM('private', 'public') NOT NULL DEFAULT 'private',
    status ENUM('draft', 'pending_review', 'published', 'suspended', 'archived') NOT NULL DEFAULT 'draft',
    version BIGINT NOT NULL DEFAULT 1,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_worlds_owner FOREIGN KEY (owner_id) REFERENCES users(id),
    KEY idx_worlds_discovery (visibility, status, updated_at),
    FULLTEXT KEY ft_worlds_title_summary (title, summary)
) ENGINE=InnoDB;

CREATE TABLE simulations (
    id BINARY(16) PRIMARY KEY,
    user_id BINARY(16) NOT NULL,
    world_id BINARY(16) NOT NULL,
    world_version BIGINT NOT NULL,
    world_snapshot_json JSON NOT NULL,
    state_json JSON NOT NULL,
    summary TEXT NOT NULL,
    status ENUM('active', 'archived') NOT NULL DEFAULT 'active',
    version BIGINT NOT NULL DEFAULT 1,
    last_turn_seq INT NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_simulations_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_simulations_world FOREIGN KEY (world_id) REFERENCES worlds(id),
    KEY idx_simulations_owner_updated (user_id, status, updated_at)
) ENGINE=InnoDB;

CREATE TABLE character_states (
    simulation_id BINARY(16) NOT NULL,
    character_id VARCHAR(80) NOT NULL,
    name VARCHAR(80) NOT NULL,
    identity_json JSON NOT NULL,
    current_state_json JSON NOT NULL,
    version BIGINT NOT NULL DEFAULT 1,
    source_turn_id BINARY(16) NULL,
    PRIMARY KEY (simulation_id, character_id),
    CONSTRAINT fk_character_states_simulation FOREIGN KEY (simulation_id) REFERENCES simulations(id)
) ENGINE=InnoDB;

CREATE TABLE story_threads (
    simulation_id BINARY(16) NOT NULL,
    thread_key VARCHAR(120) NOT NULL,
    title VARCHAR(200) NOT NULL,
    status ENUM('open', 'resolved') NOT NULL,
    priority TINYINT NOT NULL DEFAULT 0,
    opened_turn_id BINARY(16) NULL,
    resolved_turn_id BINARY(16) NULL,
    PRIMARY KEY (simulation_id, thread_key),
    CONSTRAINT fk_story_threads_simulation FOREIGN KEY (simulation_id) REFERENCES simulations(id)
) ENGINE=InnoDB;

CREATE TABLE ai_jobs (
    id BINARY(16) PRIMARY KEY,
    user_id BINARY(16) NOT NULL,
    simulation_id BINARY(16) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    user_action TEXT NOT NULL,
    status ENUM('queued', 'planning', 'drafting', 'validating', 'committing', 'completed', 'failed', 'cancelled') NOT NULL,
    reserved_credits BIGINT NOT NULL,
    input_version BIGINT NOT NULL,
    cancel_requested_at DATETIME(6) NULL,
    error_code VARCHAR(80) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_ai_jobs_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_ai_jobs_simulation FOREIGN KEY (simulation_id) REFERENCES simulations(id),
    UNIQUE KEY uq_ai_jobs_user_idempotency (user_id, idempotency_key),
    KEY idx_ai_jobs_simulation_status (simulation_id, status),
    KEY idx_ai_jobs_user_status (user_id, status)
) ENGINE=InnoDB;

CREATE TABLE ai_attempts (
    id BINARY(16) PRIMARY KEY,
    job_id BINARY(16) NOT NULL,
    attempt_no TINYINT NOT NULL,
    provider VARCHAR(40) NOT NULL,
    model VARCHAR(100) NOT NULL,
    status ENUM('started', 'completed', 'failed', 'cancelled') NOT NULL,
    usage_json JSON NULL,
    error_code VARCHAR(80) NULL,
    started_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    finished_at DATETIME(6) NULL,
    CONSTRAINT fk_ai_attempts_job FOREIGN KEY (job_id) REFERENCES ai_jobs(id),
    UNIQUE KEY uq_ai_attempts_job_no (job_id, attempt_no)
) ENGINE=InnoDB;

CREATE TABLE turns (
    id BINARY(16) PRIMARY KEY,
    simulation_id BINARY(16) NOT NULL,
    job_id BINARY(16) NOT NULL,
    seq INT NOT NULL,
    user_action TEXT NOT NULL,
    proposal_json JSON NOT NULL,
    narrative MEDIUMTEXT NOT NULL,
    choices_json JSON NOT NULL,
    status ENUM('committed') NOT NULL DEFAULT 'committed',
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_turns_simulation FOREIGN KEY (simulation_id) REFERENCES simulations(id),
    CONSTRAINT fk_turns_job FOREIGN KEY (job_id) REFERENCES ai_jobs(id),
    UNIQUE KEY uq_turns_simulation_seq (simulation_id, seq),
    UNIQUE KEY uq_turns_job (job_id)
) ENGINE=InnoDB;

ALTER TABLE character_states
    ADD CONSTRAINT fk_character_states_source_turn FOREIGN KEY (source_turn_id) REFERENCES turns(id);

CREATE TABLE story_events (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    simulation_id BINARY(16) NOT NULL,
    turn_id BINARY(16) NOT NULL,
    seq INT NOT NULL,
    event_type VARCHAR(80) NOT NULL,
    subject_ref VARCHAR(120) NOT NULL,
    object_ref VARCHAR(120) NULL,
    payload_json JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_story_events_simulation FOREIGN KEY (simulation_id) REFERENCES simulations(id),
    CONSTRAINT fk_story_events_turn FOREIGN KEY (turn_id) REFERENCES turns(id),
    KEY idx_story_events_context (simulation_id, subject_ref, seq)
) ENGINE=InnoDB;

CREATE TABLE wallets (
    user_id BINARY(16) PRIMARY KEY,
    available_balance BIGINT NOT NULL DEFAULT 0,
    reserved_balance BIGINT NOT NULL DEFAULT 0,
    version BIGINT NOT NULL DEFAULT 1,
    CONSTRAINT fk_wallets_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT chk_wallets_available_nonnegative CHECK (available_balance >= 0),
    CONSTRAINT chk_wallets_reserved_nonnegative CHECK (reserved_balance >= 0)
) ENGINE=InnoDB;

CREATE TABLE wallet_entries (
    id BINARY(16) PRIMARY KEY,
    user_id BINARY(16) NOT NULL,
    entry_type ENUM('grant', 'reserve', 'settle', 'release', 'purchase', 'adjustment', 'refund') NOT NULL,
    available_delta BIGINT NOT NULL,
    reserved_delta BIGINT NOT NULL,
    available_after BIGINT NOT NULL,
    reserved_after BIGINT NOT NULL,
    biz_type VARCHAR(40) NOT NULL,
    biz_id BINARY(16) NULL,
    idempotency_key VARCHAR(160) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_wallet_entries_user FOREIGN KEY (user_id) REFERENCES users(id),
    UNIQUE KEY uq_wallet_entries_idempotency (user_id, idempotency_key),
    KEY idx_wallet_entries_created (user_id, created_at)
) ENGINE=InnoDB;

CREATE TABLE outbox_events (
    id BINARY(16) PRIMARY KEY,
    aggregate_type VARCHAR(40) NOT NULL,
    aggregate_id BINARY(16) NOT NULL,
    event_type VARCHAR(80) NOT NULL,
    payload_json JSON NOT NULL,
    published_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    KEY idx_outbox_unpublished (published_at, created_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE IF EXISTS outbox_events;
DROP TABLE IF EXISTS wallet_entries;
DROP TABLE IF EXISTS wallets;
DROP TABLE IF EXISTS story_events;
ALTER TABLE character_states DROP FOREIGN KEY fk_character_states_source_turn;
DROP TABLE IF EXISTS turns;
DROP TABLE IF EXISTS ai_attempts;
DROP TABLE IF EXISTS ai_jobs;
DROP TABLE IF EXISTS story_threads;
DROP TABLE IF EXISTS character_states;
DROP TABLE IF EXISTS simulations;
DROP TABLE IF EXISTS worlds;
DROP TABLE IF EXISTS users;
