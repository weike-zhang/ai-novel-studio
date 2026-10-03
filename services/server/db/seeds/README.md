# MySQL 开发种子数据

`development.sql` 为首个 MySQL 纵向切片提供可重复加载的纯合成数据：两名开发账号、一个已发布的公开世界、由另一账号拥有的私人旅程、完整且冻结的世界快照、两个角色当前投影、一条未完成线索，以及与初光流水一致的钱包余额。

所有 UUID、时间和内容均固定。脚本使用 `INSERT ... ON DUPLICATE KEY UPDATE`，重复执行不会增加行数。用户和公开世界的展示字段会同步到种子版本；私人旅程、角色投影、线索、钱包和流水若已存在则保留，避免重新加载种子时破坏已经产生的回合或账本一致性。请只在开发数据库使用；脚本不包含密码、邮箱、会话、真实用户资料、模型端点或密钥。`120` 初光仅是便于开发的合成额度，不代表产品定价决定。

## 加载

前提：MySQL 8.4 已启动，并且 `00001_story_slice.sql` 的 Up 迁移已经由 goose 应用。不要把整个迁移文件直接交给 MySQL 客户端，因为文件末尾还包含 goose 的 Down 段。

使用仓库 Docker Compose 中的开发数据库时，在仓库根目录执行：

```sh
docker compose -f deploy/compose/compose.yaml up -d mysql
docker compose -f deploy/compose/compose.yaml exec -T mysql \
  mysql -unovelstudio -pnovelstudio novelstudio \
  < services/server/db/seeds/development.sql
```

若使用本机 MySQL 客户端，请通过你自己的安全连接配置加载：

```sh
mysql --defaults-extra-file=/path/to/local-mysql.cnf novelstudio \
  < services/server/db/seeds/development.sql
```

在一个仅应用过首批迁移、尚未产生业务数据的开发库中，连续执行两次 `development.sql` 后再运行下列查询，即可同时验收可重复性、公开/私有边界、快照完整性、角色投影和初光账本。应用开始写入回合或消费微光后，余额、版本和状态自然会变化，不应再与初始预期值比较。

## 验收查询

### 1. 固定记录与归属

```sql
SELECT BIN_TO_UUID(w.id, 1) AS world_id,
       w.title,
       w.visibility,
       w.status,
       owner.account_name AS world_owner,
       BIN_TO_UUID(s.id, 1) AS simulation_id,
       traveler.account_name AS journey_owner,
       s.status AS journey_status,
       s.world_version,
       s.last_turn_seq
FROM worlds AS w
JOIN users AS owner ON owner.id = w.owner_id
JOIN simulations AS s ON s.world_id = w.id
JOIN users AS traveler ON traveler.id = s.user_id
WHERE w.id = UUID_TO_BIN('0198b8a0-0002-7000-8000-000000000001', 1);
```

预期恰好一行：公开且已发布的世界属于 `seed_worldsmith`，私人旅程属于 `seed_wayfarer`，旅程为 `active`、世界版本为 `1`、已提交回合数为 `0`。

### 2. 世界快照完整且与当前世界版本对应

```sql
SELECT BIN_TO_UUID(id, 1) AS simulation_id,
       world_version,
       JSON_UNQUOTE(JSON_EXTRACT(world_snapshot_json, '$.world.id')) AS snapshot_world_id,
       JSON_EXTRACT(world_snapshot_json, '$.world.version') AS snapshot_world_version,
       JSON_LENGTH(JSON_EXTRACT(world_snapshot_json, '$.setting.rules')) AS rule_count,
       JSON_LENGTH(JSON_EXTRACT(world_snapshot_json, '$.setting.geography.locations')) AS location_count,
       JSON_LENGTH(JSON_EXTRACT(world_snapshot_json, '$.characters')) AS character_count,
       JSON_CONTAINS_PATH(
           world_snapshot_json,
           'all',
           '$.world',
           '$.setting',
           '$.narrativeStyle',
           '$.contentBoundaries',
           '$.characters',
           '$.player',
           '$.opening'
       ) AS snapshot_complete
FROM simulations
WHERE id = UUID_TO_BIN('0198b8a0-0003-7000-8000-000000000001', 1);
```

预期 `snapshot_world_id` 为固定世界 UUID，两个版本均为 `1`，规则/地点分别为 `3`，角色为 `2`，`snapshot_complete=1`。

### 3. 角色投影和未完成线索

```sql
SELECT cs.character_id,
       cs.name,
       cs.version,
       JSON_UNQUOTE(JSON_EXTRACT(cs.current_state_json, '$.location')) AS location_id,
       cs.source_turn_id IS NULL AS is_initial_projection
FROM character_states AS cs
WHERE cs.simulation_id = UUID_TO_BIN('0198b8a0-0003-7000-8000-000000000001', 1)
ORDER BY cs.character_id;

SELECT thread_key, title, status, priority,
       opened_turn_id IS NULL AS exists_at_journey_start
FROM story_threads
WHERE simulation_id = UUID_TO_BIN('0198b8a0-0003-7000-8000-000000000001', 1);
```

预期两个角色都位于 `seventh-pier`、版本为 `1`、来源回合为空；线索 `find-tide-bell` 为 `open`。

### 4. 初光余额、不可变流水与幂等数量

```sql
SELECT u.account_name,
       w.available_balance,
       w.reserved_balance,
       COUNT(we.id) AS grant_entry_count,
       COALESCE(SUM(we.available_delta), 0) AS ledger_available,
       COALESCE(SUM(we.reserved_delta), 0) AS ledger_reserved
FROM users AS u
JOIN wallets AS w ON w.user_id = u.id
LEFT JOIN wallet_entries AS we ON we.user_id = u.id
WHERE u.account_name IN ('seed_worldsmith', 'seed_wayfarer')
GROUP BY u.id, u.account_name, w.available_balance, w.reserved_balance
ORDER BY u.account_name;
```

预期每名合成用户只有一条 grant 流水，钱包和流水均为 `available=120`、`reserved=0`。重复加载后结果不变。

### 5. 所有固定 UUID 与外键均可解析

```sql
SELECT
    (SELECT COUNT(*) FROM users
      WHERE id IN (
        UUID_TO_BIN('0198b8a0-0001-7000-8000-000000000001', 1),
        UUID_TO_BIN('0198b8a0-0001-7000-8000-000000000002', 1)
      )) AS users_count,
    (SELECT COUNT(*) FROM worlds
      WHERE id = UUID_TO_BIN('0198b8a0-0002-7000-8000-000000000001', 1)) AS worlds_count,
    (SELECT COUNT(*) FROM simulations
      WHERE id = UUID_TO_BIN('0198b8a0-0003-7000-8000-000000000001', 1)) AS simulations_count,
    (SELECT COUNT(*) FROM character_states
      WHERE simulation_id = UUID_TO_BIN('0198b8a0-0003-7000-8000-000000000001', 1)) AS character_states_count,
    (SELECT COUNT(*) FROM wallets
      WHERE user_id IN (
        UUID_TO_BIN('0198b8a0-0001-7000-8000-000000000001', 1),
        UUID_TO_BIN('0198b8a0-0001-7000-8000-000000000002', 1)
      )) AS wallets_count,
    (SELECT COUNT(*) FROM wallet_entries
      WHERE id IN (
        UUID_TO_BIN('0198b8a0-0004-7000-8000-000000000001', 1),
        UUID_TO_BIN('0198b8a0-0004-7000-8000-000000000002', 1)
      )) AS wallet_entries_count;
```

预期依次为 `2, 1, 1, 2, 2, 2`。脚本的插入顺序是 `users → worlds → simulations → character_states/story_threads → wallets → wallet_entries`，与首批迁移的外键依赖一致。
