-- +goose Up
-- 互动类型：act=自由输入行动、branch=选择岔路、continue=沿明显路径继续。
ALTER TABLE ai_jobs
    ADD COLUMN interaction ENUM('act', 'branch', 'continue') NOT NULL DEFAULT 'act';

ALTER TABLE turns
    ADD COLUMN interaction ENUM('act', 'branch', 'continue') NOT NULL DEFAULT 'act';

-- +goose Down
ALTER TABLE turns
    DROP COLUMN interaction;
ALTER TABLE ai_jobs
    DROP COLUMN interaction;