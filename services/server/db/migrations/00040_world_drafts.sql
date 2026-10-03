-- +goose Up
CREATE TABLE world_draft_jobs (
    id BINARY(16) PRIMARY KEY,
    user_id BINARY(16) NOT NULL,
    idempotency_key VARCHAR(128) NOT NULL,
    input_json JSON NOT NULL,
    input_hash BINARY(32) NOT NULL,
    status ENUM('queued', 'generating', 'validating', 'committing', 'completed', 'failed', 'cancelled') NOT NULL,
    attempt_count INT NOT NULL DEFAULT 0,
    max_attempts INT NOT NULL,
    available_at DATETIME(6) NOT NULL,
    lease_owner VARCHAR(96) NULL,
    lease_expires_at DATETIME(6) NULL,
    world_id BINARY(16) NULL,
    error_code VARCHAR(80) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    completed_at DATETIME(6) NULL,
    CONSTRAINT fk_world_draft_jobs_user FOREIGN KEY (user_id) REFERENCES users(id),
    CONSTRAINT fk_world_draft_jobs_world FOREIGN KEY (world_id) REFERENCES worlds(id),
    UNIQUE KEY uq_world_draft_jobs_user_key (user_id, idempotency_key),
    KEY idx_world_draft_jobs_processable (status, available_at, created_at),
    KEY idx_world_draft_jobs_lease (status, lease_expires_at)
) ENGINE=InnoDB;

CREATE TABLE world_draft_attempts (
    id BINARY(16) PRIMARY KEY,
    job_id BINARY(16) NOT NULL,
    attempt_no INT NOT NULL,
    provider VARCHAR(40) NOT NULL,
    model VARCHAR(100) NOT NULL,
    status ENUM('started', 'completed', 'failed') NOT NULL,
    usage_json JSON NULL,
    error_code VARCHAR(80) NULL,
    started_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    finished_at DATETIME(6) NULL,
    CONSTRAINT fk_world_draft_attempts_job FOREIGN KEY (job_id) REFERENCES world_draft_jobs(id),
    UNIQUE KEY uq_world_draft_attempts_job_no (job_id, attempt_no),
    KEY idx_world_draft_attempts_status (job_id, status)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE world_draft_attempts;
DROP TABLE world_draft_jobs;
