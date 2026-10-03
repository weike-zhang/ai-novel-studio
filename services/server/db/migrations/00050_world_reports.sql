-- +goose Up
CREATE TABLE world_reports (
    id BINARY(16) PRIMARY KEY,
    world_id BINARY(16) NOT NULL,
    reporter_id BINARY(16) NOT NULL,
    reason ENUM('违法违规', '色情低俗', '暴力仇恨', '侵权冒用', '其他') NOT NULL,
    note VARCHAR(300) NULL,
    status ENUM('pending', 'reviewing', 'resolved', 'dismissed') NOT NULL DEFAULT 'pending',
    open_key BINARY(32) GENERATED ALWAYS AS (IF(status IN ('pending', 'reviewing'), CONCAT(reporter_id, world_id), NULL)) STORED,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_world_reports_world FOREIGN KEY (world_id) REFERENCES worlds(id),
    CONSTRAINT fk_world_reports_reporter FOREIGN KEY (reporter_id) REFERENCES users(id),
    UNIQUE KEY uq_world_reports_open (open_key),
    KEY idx_world_reports_status_created (status, created_at),
    KEY idx_world_reports_world (world_id, created_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE world_reports;
