-- +goose Up
-- 一幕 = 叙事场景，不再等于生成次数。每个已提交回合归属一个场景编号；
-- 场景 1 固定为世界开场，之后由 AI 在规划阶段决定是否开启新场景。
ALTER TABLE simulations
    ADD COLUMN last_scene_no INT NOT NULL DEFAULT 1;

ALTER TABLE turns
    ADD COLUMN scene_no INT NOT NULL DEFAULT 1,
    ADD COLUMN scene_title VARCHAR(120) NOT NULL DEFAULT '',
    ADD COLUMN scene_closure VARCHAR(500) NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE turns
    DROP COLUMN scene_closure,
    DROP COLUMN scene_title,
    DROP COLUMN scene_no;
ALTER TABLE simulations
    DROP COLUMN last_scene_no;
