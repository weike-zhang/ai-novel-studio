-- Synthetic development data for the first MySQL vertical slice.
-- MySQL 8.4 / InnoDB. Safe to run repeatedly after 00001_story_slice.sql is up.
-- Every identity and story detail below is fictional; this file contains no secrets.

SET NAMES utf8mb4;

SET @seed_author_id = '0198b8a0-0001-7000-8000-000000000001';
SET @seed_traveler_id = '0198b8a0-0001-7000-8000-000000000002';
SET @seed_world_id = '0198b8a0-0002-7000-8000-000000000001';
SET @seed_simulation_id = '0198b8a0-0003-7000-8000-000000000001';
SET @seed_author_grant_id = '0198b8a0-0004-7000-8000-000000000001';
SET @seed_traveler_grant_id = '0198b8a0-0004-7000-8000-000000000002';
SET @seed_created_at = '2026-08-14 00:00:00.000000';
SET @seed_initial_light = 120;

START TRANSACTION;

-- Parents first: both accounts are synthetic and intentionally contain no auth data.
INSERT INTO users (
    id,
    account_name,
    nickname,
    status,
    created_at
) VALUES
    (UUID_TO_BIN(@seed_author_id, 1), 'seed_worldsmith', '合成造境者', 'active', @seed_created_at),
    (UUID_TO_BIN(@seed_traveler_id, 1), 'seed_wayfarer', '合成旅人', 'active', @seed_created_at)
AS new
ON DUPLICATE KEY UPDATE
    account_name = new.account_name,
    nickname = new.nickname,
    status = new.status,
    created_at = new.created_at;

-- One reviewed public world for discovery and journey creation.
INSERT INTO worlds (
    id,
    owner_id,
    title,
    summary,
    setting_json,
    visibility,
    status,
    version,
    created_at,
    updated_at
) VALUES (
    UUID_TO_BIN(@seed_world_id, 1),
    UUID_TO_BIN(@seed_author_id, 1),
    '雾灯港：第七码头',
    '最后一班渡船离港后，雾里响起一口没有指针的潮钟。你必须在黎明前找回它，否则这座港城会忘掉一个人。',
    JSON_OBJECT(
        'schemaVersion', 1,
        'premise', '雾灯港会把居民遗忘的往事凝成可见的潮雾；每次潮退，城市都会失去一段公共记忆。',
        'genre', JSON_ARRAY('奇幻', '悬疑', '探索'),
        'tone', JSON_ARRAY('克制', '温暖', '微悬疑'),
        'discovery', JSON_OBJECT(
            'featuredRank', 2,
            'label', '编辑部主打',
            'hook', '一座会遗忘人的港城，今晚轮到谁消失？',
            'promise', '追踪潮雾里的旧记忆，在相信守信人和追问真相之间作出选择。',
            'playTime', '首幕约 3 分钟',
            'format', '短回合悬疑'
        ),
        'time', JSON_OBJECT(
            'calendar', '潮历',
            'current', '潮历 312 年，回声月第七码头夜',
            'rule', '时间只能向前推进，长明灯在黎明前熄灭'
        ),
        'geography', JSON_OBJECT(
            'region', '雾灯群岛',
            'city', '雾灯港',
            'locations', JSON_ARRAY(
                JSON_OBJECT('id', 'seventh-pier', 'name', '第七码头', 'kind', 'harbor', 'public', TRUE),
                JSON_OBJECT('id', 'tide-archive', 'name', '潮汐档案馆', 'kind', 'archive', 'public', TRUE),
                JSON_OBJECT('id', 'north-beacon', 'name', '北岬长明灯', 'kind', 'lighthouse', 'public', FALSE)
            )
        ),
        'rules', JSON_ARRAY(
            JSON_OBJECT('id', 'memory-fog', 'text', '潮雾只能显现真实发生过、但被人遗忘的片段。', 'mutable', FALSE),
            JSON_OBJECT('id', 'bell-pass', 'text', '潮汐钟声是进入北岬长明灯的唯一通行凭证。', 'mutable', FALSE),
            JSON_OBJECT('id', 'no-resurrection', 'text', '逝者不能复生，回声也不能替代本人。', 'mutable', FALSE)
        ),
        'narrativeStyle', JSON_OBJECT(
            'person', '第二人称',
            'tense', '现在时',
            'pacing', '探索优先，每幕推进一个可验证事实',
            'language', '简体中文'
        ),
        'contentBoundaries', JSON_OBJECT(
            'rating', '18+ mature, non-explicit',
            'allowed', JSON_ARRAY('悬疑', '轻度危险', '悲伤主题'),
            'excluded', JSON_ARRAY('露骨色情', '未成年人性内容', '现实伤害指导', '仇恨煽动')
        ),
        'characters', JSON_ARRAY(
            JSON_OBJECT(
                'id', 'wayfarer-shen',
                'name', '沈砚',
                'role', '持空白航图的外来旅人',
                'pronouns', 'ta',
                'origin', '南方内海',
                'immutableTraits', JSON_ARRAY('成年人', '能够听见潮雾中的回声')
            ),
            JSON_OBJECT(
                'id', 'keeper-lin',
                'name', '林岚',
                'role', '第七码头守信人',
                'pronouns', '她',
                'origin', '雾灯港',
                'immutableTraits', JSON_ARRAY('成年人', '从不离开港区', '保管铜制潮匙')
            )
        ),
        'playerTemplate', JSON_OBJECT(
            'characterId', 'wayfarer-shen',
            'agency', '玩家决定沈砚的行动、态度与承诺；模型不得替玩家作出关键选择。'
        ),
        'opening', JSON_OBJECT(
            'locationId', 'seventh-pier',
            'situation', '最后一班渡船离港后，一只没有指针的潮钟在雾中响了一次。',
            'objective', '在黎明前确认钟声来源，并取得前往北岬长明灯的路径。'
        )
    ),
    'public',
    'published',
    1,
    @seed_created_at,
    @seed_created_at
)
AS new
ON DUPLICATE KEY UPDATE
    owner_id = new.owner_id,
    title = new.title,
    summary = new.summary,
    setting_json = new.setting_json,
    visibility = new.visibility,
    status = new.status,
    version = new.version,
    created_at = new.created_at,
    updated_at = new.updated_at;

-- A private journey owned by the traveler. The snapshot is self-contained and
-- remains valid even if the public world is edited after this point.
INSERT INTO simulations (
    id,
    user_id,
    world_id,
    world_version,
    world_snapshot_json,
    state_json,
    summary,
    status,
    version,
    last_turn_seq,
    created_at,
    updated_at
) VALUES (
    UUID_TO_BIN(@seed_simulation_id, 1),
    UUID_TO_BIN(@seed_traveler_id, 1),
    UUID_TO_BIN(@seed_world_id, 1),
    1,
    JSON_OBJECT(
        'schemaVersion', 1,
        'capturedAt', '2026-08-14T00:00:00.000000Z',
        'world', JSON_OBJECT(
            'id', @seed_world_id,
            'version', 1,
            'title', '雾灯港：第七码头',
            'summary', '最后一班渡船离港后，雾里响起一口没有指针的潮钟。你必须在黎明前找回它，否则这座港城会忘掉一个人。',
            'visibilityAtCapture', 'public',
            'statusAtCapture', 'published'
        ),
        'setting', JSON_OBJECT(
            'premise', '雾灯港会把居民遗忘的往事凝成可见的潮雾；每次潮退，城市都会失去一段公共记忆。',
            'genre', JSON_ARRAY('奇幻', '悬疑', '探索'),
            'tone', JSON_ARRAY('克制', '温暖', '微悬疑'),
            'time', JSON_OBJECT(
                'calendar', '潮历',
                'current', '潮历 312 年，回声月第七码头夜',
                'rule', '时间只能向前推进，长明灯在黎明前熄灭'
            ),
            'geography', JSON_OBJECT(
                'region', '雾灯群岛',
                'city', '雾灯港',
                'locations', JSON_ARRAY(
                    JSON_OBJECT('id', 'seventh-pier', 'name', '第七码头', 'kind', 'harbor', 'public', TRUE),
                    JSON_OBJECT('id', 'tide-archive', 'name', '潮汐档案馆', 'kind', 'archive', 'public', TRUE),
                    JSON_OBJECT('id', 'north-beacon', 'name', '北岬长明灯', 'kind', 'lighthouse', 'public', FALSE)
                )
            ),
            'rules', JSON_ARRAY(
                JSON_OBJECT('id', 'memory-fog', 'text', '潮雾只能显现真实发生过、但被人遗忘的片段。', 'mutable', FALSE),
                JSON_OBJECT('id', 'bell-pass', 'text', '潮汐钟声是进入北岬长明灯的唯一通行凭证。', 'mutable', FALSE),
                JSON_OBJECT('id', 'no-resurrection', 'text', '逝者不能复生，回声也不能替代本人。', 'mutable', FALSE)
            )
        ),
        'narrativeStyle', JSON_OBJECT(
            'person', '第二人称',
            'tense', '现在时',
            'pacing', '探索优先，每幕推进一个可验证事实',
            'language', '简体中文'
        ),
        'contentBoundaries', JSON_OBJECT(
            'rating', '18+ mature, non-explicit',
            'allowed', JSON_ARRAY('悬疑', '轻度危险', '悲伤主题'),
            'excluded', JSON_ARRAY('露骨色情', '未成年人性内容', '现实伤害指导', '仇恨煽动')
        ),
        'characters', JSON_ARRAY(
            JSON_OBJECT(
                'id', 'wayfarer-shen',
                'name', '沈砚',
                'role', '持空白航图的外来旅人',
                'pronouns', 'ta',
                'origin', '南方内海',
                'immutableTraits', JSON_ARRAY('成年人', '能够听见潮雾中的回声')
            ),
            JSON_OBJECT(
                'id', 'keeper-lin',
                'name', '林岚',
                'role', '第七码头守信人',
                'pronouns', '她',
                'origin', '雾灯港',
                'immutableTraits', JSON_ARRAY('成年人', '从不离开港区', '保管铜制潮匙')
            )
        ),
        'player', JSON_OBJECT(
            'characterId', 'wayfarer-shen',
            'agency', '玩家决定沈砚的行动、态度与承诺；模型不得替玩家作出关键选择。'
        ),
        'opening', JSON_OBJECT(
            'locationId', 'seventh-pier',
            'situation', '最后一班渡船离港后，一只没有指针的潮钟在雾中响了一次。',
            'objective', '在黎明前确认钟声来源，并取得前往北岬长明灯的路径。'
        )
    ),
    JSON_OBJECT(
        'schemaVersion', 1,
        'timeline', JSON_OBJECT('current', '潮历 312 年，回声月第七码头夜', 'elapsedMinutes', 0),
        'currentLocationId', 'seventh-pier',
        'playerCharacterId', 'wayfarer-shen',
        'inventory', JSON_ARRAY('空白航图', '未点燃的引潮灯'),
        'knownFacts', JSON_ARRAY('潮汐钟声已在第七码头响过一次'),
        'flags', JSON_OBJECT('heardFirstBell', TRUE, 'northBeaconAccessible', FALSE),
        'characterVersions', JSON_OBJECT('wayfarer-shen', 1, 'keeper-lin', 1),
        'threadStatuses', JSON_OBJECT('find-tide-bell', 'open')
    ),
    '旅程刚在第七码头开始；尚无已提交回合。沈砚听见第一声潮汐钟，林岚正在雾中守候。',
    'active',
    1,
    0,
    @seed_created_at,
    @seed_created_at
)
AS new
ON DUPLICATE KEY UPDATE
    id = new.id;

-- Current projections are children of the private journey.
INSERT INTO character_states (
    simulation_id,
    character_id,
    name,
    identity_json,
    current_state_json,
    version,
    source_turn_id
) VALUES
    (
        UUID_TO_BIN(@seed_simulation_id, 1),
        'wayfarer-shen',
        '沈砚',
        JSON_OBJECT(
            'name', '沈砚',
            'role', '持空白航图的外来旅人',
            'pronouns', 'ta',
            'origin', '南方内海',
            'adult', TRUE,
            'immutableTraits', JSON_ARRAY('能够听见潮雾中的回声')
        ),
        JSON_OBJECT(
            'alive', TRUE,
            'location', 'seventh-pier',
            'condition', '安然',
            'mood', '警觉',
            'inventory', JSON_ARRAY('空白航图', '未点燃的引潮灯'),
            'knowledge', JSON_ARRAY('潮汐钟声已在第七码头响过一次'),
            'goals', JSON_ARRAY('确认钟声来源')
        ),
        1,
        NULL
    ),
    (
        UUID_TO_BIN(@seed_simulation_id, 1),
        'keeper-lin',
        '林岚',
        JSON_OBJECT(
            'name', '林岚',
            'role', '第七码头守信人',
            'pronouns', '她',
            'origin', '雾灯港',
            'adult', TRUE,
            'immutableTraits', JSON_ARRAY('从不离开港区', '保管铜制潮匙')
        ),
        JSON_OBJECT(
            'alive', TRUE,
            'location', 'seventh-pier',
            'condition', '安然',
            'mood', '克制',
            'inventory', JSON_ARRAY('铜制潮匙'),
            'knowledge', JSON_ARRAY('钟声来自一段被城里人共同遗忘的往事'),
            'goals', JSON_ARRAY('判断沈砚是否值得信任')
        ),
        1,
        NULL
    )
AS new
ON DUPLICATE KEY UPDATE
    simulation_id = new.simulation_id;

INSERT INTO story_threads (
    simulation_id,
    thread_key,
    title,
    status,
    priority,
    opened_turn_id,
    resolved_turn_id
) VALUES (
    UUID_TO_BIN(@seed_simulation_id, 1),
    'find-tide-bell',
    '找回失落的潮汐钟声',
    'open',
    10,
    NULL,
    NULL
)
AS new
ON DUPLICATE KEY UPDATE
    simulation_id = new.simulation_id;

-- Initial Light is synthetic development credit, not a production price decision.
INSERT INTO wallets (
    user_id,
    available_balance,
    reserved_balance,
    version
) VALUES
    (UUID_TO_BIN(@seed_author_id, 1), @seed_initial_light, 0, 1),
    (UUID_TO_BIN(@seed_traveler_id, 1), @seed_initial_light, 0, 1)
AS new
ON DUPLICATE KEY UPDATE
    user_id = new.user_id;

INSERT INTO wallet_entries (
    id,
    user_id,
    entry_type,
    available_delta,
    reserved_delta,
    available_after,
    reserved_after,
    biz_type,
    biz_id,
    idempotency_key,
    created_at
) VALUES
    (
        UUID_TO_BIN(@seed_author_grant_id, 1),
        UUID_TO_BIN(@seed_author_id, 1),
        'grant',
        @seed_initial_light,
        0,
        @seed_initial_light,
        0,
        'initial_light',
        UUID_TO_BIN(@seed_author_id, 1),
        'development:initial-light:seed-worldsmith:v1',
        @seed_created_at
    ),
    (
        UUID_TO_BIN(@seed_traveler_grant_id, 1),
        UUID_TO_BIN(@seed_traveler_id, 1),
        'grant',
        @seed_initial_light,
        0,
        @seed_initial_light,
        0,
        'initial_light',
        UUID_TO_BIN(@seed_traveler_id, 1),
        'development:initial-light:seed-wayfarer:v1',
        @seed_created_at
    )
AS new
ON DUPLICATE KEY UPDATE
    id = new.id;

COMMIT;
