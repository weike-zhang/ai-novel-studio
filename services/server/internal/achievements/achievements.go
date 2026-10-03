package achievements

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/benefits"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

// Scope labels stored in achievement_claims.
const (
	ScopeGlobal   = "global"
	ScopeWorld    = "world"
	ScopeEmergent = "emergent"
)

// Metric is the counter a global achievement is measured against.
type Metric string

const (
	MetricTurns    Metric = "turns"
	MetricContinue Metric = "continue"
	MetricAct      Metric = "act"
	MetricForced   Metric = "forced"
	MetricWorlds   Metric = "worlds"
)

// Definition is a built-in global achievement. Fewer, harder, one-time rewards.
type Definition struct {
	Code        string
	Title       string
	Description string
	Metric      Metric
	Threshold   int
	Reward      int64
}

// Global holds the platform-wide achievement set.
var Global = []Definition{
	{Code: "first-act", Title: "初见世界", Description: "完成第一幕，踏入这个世界。", Metric: MetricTurns, Threshold: 1, Reward: 3},
	{Code: "deep-diver", Title: "深潜者", Description: "累计完成 30 幕。", Metric: MetricTurns, Threshold: 30, Reward: 15},
	{Code: "voyager", Title: "远行", Description: "累计完成 100 幕。", Metric: MetricTurns, Threshold: 100, Reward: 30},
	{Code: "wanderer", Title: "漫游者", Description: "累计完成 300 幕。", Metric: MetricTurns, Threshold: 300, Reward: 60},
	{Code: "observer", Title: "一路旁观", Description: "累计 20 次选择「继续」，让世界沿自己的轨道推进。", Metric: MetricContinue, Threshold: 20, Reward: 15},
	{Code: "fate-witness", Title: "命运的旁观者", Description: "累计经历 10 次世界不可干预的回合。", Metric: MetricForced, Threshold: 10, Reward: 12},
	{Code: "scribe", Title: "笔走龙蛇", Description: "累计自由输入行动 30 次。", Metric: MetricAct, Threshold: 30, Reward: 15},
	{Code: "many-worlds", Title: "千帆过尽", Description: "在 5 个不同的世界完成第一幕。", Metric: MetricWorlds, Threshold: 5, Reward: 20},
}

// Claim is a persisted, newly earned achievement. Zero reward claims are still
// surfaced so the reader sees the surprise even without a credit grant.
type Claim struct {
	Scope       string    `json:"scope"`
	WorldID     string    `json:"worldId,omitempty"`
	Code        string    `json:"code"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Reward      int64     `json:"reward"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Store persists achievement claims and grants rewards through the wallet.
type Store struct {
	db       *sql.DB
	benefits *benefits.Store
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db, benefits: benefits.NewStore(db)}
}

// worldRewardCap limits how many credits a single world can pay a single user
// through world + emergent achievements, so creators cannot farm their own
// catalog and players cannot grind one world for credits.
const worldRewardCap int64 = 30

type catalogEntry struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Hint   string `json:"hint"`
	Reward int64  `json:"reward"`
}

// Evaluate inspects a freshly committed turn and returns newly unlocked claims.
// Global milestones are derived from persisted counters; world achievements are
// judged by the model against the frozen world catalogue, and improvised
// emergent achievements are recorded without a credit reward. Evaluation is
// best-effort: a failure here never fails the turn itself.
func (s *Store) Evaluate(ctx context.Context, userID, simulationID string, worldSnapshot json.RawMessage, proposal storyengine.TurnProposal, now time.Time) ([]Claim, error) {
	worldID, catalog := parseWorldContext(worldSnapshot)
	var claims []Claim

	for _, definition := range Global {
		met, err := s.metricReached(ctx, userID, definition.Metric, definition.Threshold)
		if err != nil {
			return nil, err
		}
		if !met {
			continue
		}
		claim, err := s.claim(ctx, userID, ScopeGlobal, "", simulationID, definition.Code, definition.Title, definition.Description, definition.Reward, now)
		if err != nil {
			return nil, err
		}
		if claim != nil {
			claims = append(claims, *claim)
		}
	}

	if worldID != "" {
		used, err := s.worldRewardUsed(ctx, userID, worldID)
		if err != nil {
			return nil, err
		}
		for _, proposed := range proposal.Achievements {
			entry, found := findCatalogEntry(catalog, proposed.Key)
			var scope, code, title, description string
			var reward int64
			switch {
			case found:
				scope, code, title, description, reward = ScopeWorld, entry.Key, entry.Title, entry.Hint, entry.Reward
			case strings.TrimSpace(proposed.Title) != "":
				scope, code, title, description = ScopeEmergent, proposed.Key, proposed.Title, proposed.Description
			default:
				continue
			}
			if used < worldRewardCap {
				available := worldRewardCap - used
				if reward > available {
					reward = available
				}
				used += reward
			} else {
				reward = 0
			}
			claim, err := s.claim(ctx, userID, scope, worldID, simulationID, code, title, description, reward, now)
			if err != nil {
				return nil, err
			}
			if claim != nil {
				claims = append(claims, *claim)
			}
		}
	}

	return claims, nil
}

// ClaimRow is a persisted achievement for the account wall and world detail page.
type ClaimRow struct {
	Scope       string    `json:"scope"`
	WorldID     string    `json:"worldId,omitempty"`
	WorldTitle  string    `json:"worldTitle,omitempty"`
	Code        string    `json:"code"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Reward      int64     `json:"reward"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ListClaims returns the user's earned achievements, newest first.
func (s *Store) ListClaims(ctx context.Context, userID string, limit int) ([]ClaimRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.scope,COALESCE(BIN_TO_UUID(c.world_id,1),''),COALESCE(w.title,''),c.code,c.title,c.description,c.reward,c.created_at FROM achievement_claims c LEFT JOIN worlds w ON w.id=c.world_id WHERE c.user_id=UUID_TO_BIN(?,1) ORDER BY c.created_at DESC LIMIT ?`,
		userID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list achievement claims: %w", err)
	}
	defer rows.Close()
	claims := make([]ClaimRow, 0, limit)
	for rows.Next() {
		var claim ClaimRow
		if err := rows.Scan(&claim.Scope, &claim.WorldID, &claim.WorldTitle, &claim.Code, &claim.Title, &claim.Description, &claim.Reward, &claim.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan achievement claim: %w", err)
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate achievement claims: %w", err)
	}
	return claims, nil
}

func (s *Store) metricReached(ctx context.Context, userID string, metric Metric, threshold int) (bool, error) {
	var value int
	query := `SELECT COUNT(*) FROM turns t JOIN simulations sim ON t.simulation_id=sim.id WHERE sim.user_id=UUID_TO_BIN(?,1)`
	switch metric {
	case MetricTurns:
		// default query counts all committed turns
	case MetricContinue:
		query += ` AND t.interaction='continue'`
	case MetricAct:
		query += ` AND t.interaction='act'`
	case MetricForced:
		query += ` AND JSON_LENGTH(t.choices_json)=0`
	case MetricWorlds:
		query = `SELECT COUNT(DISTINCT sim.world_id) FROM turns t JOIN simulations sim ON t.simulation_id=sim.id WHERE sim.user_id=UUID_TO_BIN(?,1)`
	default:
		return false, nil
	}
	if err := s.db.QueryRowContext(ctx, query, userID).Scan(&value); err != nil {
		return false, fmt.Errorf("measure achievement metric %s: %w", metric, err)
	}
	return value >= threshold, nil
}

func (s *Store) worldRewardUsed(ctx context.Context, userID, worldID string) (int64, error) {
	var used int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(reward),0) FROM achievement_claims WHERE user_id=UUID_TO_BIN(?,1) AND world_id=UUID_TO_BIN(?,1) AND scope IN ('world','emergent')`, userID, worldID).Scan(&used)
	if err != nil {
		return 0, fmt.Errorf("measure world reward: %w", err)
	}
	return used, nil
}

func (s *Store) claim(ctx context.Context, userID, scope, worldID, simulationID, code, title, description string, reward int64, now time.Time) (*Claim, error) {
	dedupeKey := userID + ":" + scope + ":" + worldID + ":" + code
	claimID, err := randomID()
	if err != nil {
		return nil, err
	}
	var worldColumn, worldArg string
	if worldID == "" {
		worldColumn, worldArg = "NULL", ""
	} else {
		worldColumn, worldArg = "UUID_TO_BIN(?,1)", worldID
	}
	args := []any{claimID, userID}
	if worldArg != "" {
		args = append(args, worldArg)
	}
	args = append(args, simulationID, scope, code, title, description, reward, dedupeKey, now.UTC())
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO achievement_claims (id,user_id,world_id,simulation_id,scope,code,title,description,reward,dedupe_key,created_at) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),`+worldColumn+`,UUID_TO_BIN(?,1),?,?,?,?,?,?,?)`,
		args...,
	)
	if err != nil {
		if isDuplicate(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("record achievement claim: %w", err)
	}
	if reward > 0 {
		if err := s.benefits.AchievementGrant(ctx, userID, dedupeKey+":wallet", reward, now); err != nil {
			return nil, err
		}
	}
	return &Claim{Scope: scope, WorldID: worldID, Code: code, Title: title, Description: description, Reward: reward, CreatedAt: now.UTC()}, nil
}

func parseWorldContext(worldSnapshot json.RawMessage) (string, []catalogEntry) {
	var frozen struct {
		World struct {
			ID string `json:"id"`
		} `json:"world"`
		Setting struct {
			Achievements []catalogEntry `json:"achievements"`
		} `json:"setting"`
	}
	if err := json.Unmarshal(worldSnapshot, &frozen); err != nil {
		return "", nil
	}
	catalog := make([]catalogEntry, 0, len(frozen.Setting.Achievements))
	for _, entry := range frozen.Setting.Achievements {
		if strings.TrimSpace(entry.Key) != "" {
			catalog = append(catalog, entry)
		}
	}
	return frozen.World.ID, catalog
}

func findCatalogEntry(catalog []catalogEntry, key string) (catalogEntry, bool) {
	for _, entry := range catalog {
		if entry.Key == strings.TrimSpace(key) {
			return entry, true
		}
	}
	return catalogEntry{}, false
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func isDuplicate(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate entry") || strings.Contains(text, "unique constraint")
}
