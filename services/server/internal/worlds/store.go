package worlds

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Store struct {
	db        *sql.DB
	moderator PublicationModerator
}

func NewStore(db *sql.DB) *Store {
	return NewStoreWithPublicationModerator(db, UnavailablePublicationModerator{})
}

func NewStoreWithPublicationModerator(db *sql.DB, moderator PublicationModerator) *Store {
	if moderator == nil {
		moderator = UnavailablePublicationModerator{}
	}
	return &Store{db: db, moderator: moderator}
}

func (s *Store) ListPublic(ctx context.Context, input PublicWorldQuery) (PublicWorldPage, error) {
	query, args, limit, err := buildPublicWorldQuery(input)
	if err != nil {
		return PublicWorldPage{}, err
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return PublicWorldPage{}, fmt.Errorf("list public worlds: %w", err)
	}
	defer rows.Close()

	page := PublicWorldPage{Worlds: make([]WorldCard, 0, limit)}
	popularities := make([]int64, 0, limit+1)
	for rows.Next() {
		var card WorldCard
		var tags []byte
		var popularity int64
		var featuredRank int
		var rawID []byte
		if err := rows.Scan(&card.ID, &card.OwnerID, &card.AuthorNickname, &card.Title, &card.Summary, &card.Hook, &card.Promise, &card.PlayTime, &card.Format, &featuredRank, &card.Cover, &card.Type, &tags, &card.SimulationCount, &popularity, &card.Version, &card.UpdatedAt, &rawID); err != nil {
			return PublicWorldPage{}, fmt.Errorf("scan public world: %w", err)
		}
		card.FeaturedRank = featuredRank
		if err := json.Unmarshal(tags, &card.Tags); err != nil {
			return PublicWorldPage{}, fmt.Errorf("decode world tags: %w", err)
		}
		page.Worlds = append(page.Worlds, card)
		popularities = append(popularities, popularity)
	}
	if err := rows.Err(); err != nil {
		return PublicWorldPage{}, fmt.Errorf("iterate public worlds: %w", err)
	}
	if len(page.Worlds) > limit {
		page.Worlds = page.Worlds[:limit]
		last := page.Worlds[limit-1]
		sort := input.Sort
		if sort == "" {
			sort = SortNewest
		}
		popularity := popularities[limit-1]
		page.NextCursor, err = encodeCursor(pageCursor{Sort: sort, Popularity: popularity, UpdatedAt: last.UpdatedAt, ID: last.ID})
		if err != nil {
			return PublicWorldPage{}, fmt.Errorf("encode next cursor: %w", err)
		}
	}
	return page, nil
}

func (s *Store) Get(ctx context.Context, worldID, viewerID string) (World, error) {
	if !isUUID(worldID) {
		return World{}, ErrNotFound
	}
	if viewerID != "" && !isUUID(viewerID) {
		viewerID = ""
	}
	var world World
	var setting, tags []byte
	err := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(w.id,1),BIN_TO_UUID(w.owner_id,1),u.nickname,w.title,w.summary,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.cover')),''),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.type')),JSON_UNQUOTE(JSON_EXTRACT(w.setting_json,'$.genre[0]')),''),COALESCE(JSON_EXTRACT(w.setting_json,'$.tags'),JSON_EXTRACT(w.setting_json,'$.genre'),JSON_ARRAY()),(SELECT COUNT(*) FROM simulations s WHERE s.world_id=w.id),w.version,w.updated_at,w.visibility,w.status,w.setting_json FROM worlds w JOIN users u ON u.id=w.owner_id WHERE w.id=UUID_TO_BIN(?,1) AND (w.owner_id=UUID_TO_BIN(NULLIF(?,''),1) OR (w.visibility='public' AND w.status='published'))`, worldID, viewerID).Scan(
		&world.ID, &world.OwnerID, &world.AuthorNickname, &world.Title, &world.Summary, &world.Cover, &world.Type, &tags, &world.SimulationCount, &world.Version, &world.UpdatedAt, &world.Visibility, &world.Status, &setting,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return World{}, ErrNotFound
	}
	if err != nil {
		return World{}, fmt.Errorf("get world: %w", err)
	}
	if err := json.Unmarshal(tags, &world.Tags); err != nil {
		return World{}, fmt.Errorf("decode world tags: %w", err)
	}
	world.Setting = append(json.RawMessage(nil), setting...)
	return world, nil
}

func (s *Store) CreateJourney(ctx context.Context, input CreateJourneyInput) (Journey, error) {
	if !isUUID(input.UserID) {
		return Journey{}, fmt.Errorf("invalid user id")
	}
	if !isUUID(input.WorldID) {
		return Journey{}, ErrNotFound
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return Journey{}, fmt.Errorf("begin journey: %w", err)
	}
	defer tx.Rollback()

	var ownerID, title, summary, visibility, status string
	var version int64
	var setting []byte
	// Lock the world row so concurrent "start journey" requests serialize
	// before checking for an existing active journey.
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(owner_id,1),title,summary,setting_json,visibility,status,version FROM worlds WHERE id=UUID_TO_BIN(?,1) AND (owner_id=UUID_TO_BIN(?,1) OR (visibility='public' AND status='published')) FOR UPDATE`, input.WorldID, input.UserID).Scan(&ownerID, &title, &summary, &setting, &visibility, &status, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return Journey{}, ErrNotFound
	}
	if err != nil {
		return Journey{}, fmt.Errorf("load journey world: %w", err)
	}

	capturedAt := time.Now().UTC()
	snapshot, err := buildSnapshot(input.WorldID, ownerID, title, summary, visibility, status, version, setting, capturedAt)
	if err != nil {
		return Journey{}, err
	}

	// Reuse an existing active journey for this world instead of creating a
	// duplicate when the player taps "start" again.
	var existingID string
	var existingVersion int64
	err = tx.QueryRowContext(ctx, `SELECT BIN_TO_UUID(id,1),version FROM simulations WHERE user_id=UUID_TO_BIN(?,1) AND world_id=UUID_TO_BIN(?,1) AND status='active' ORDER BY last_turn_seq DESC,updated_at DESC,created_at ASC LIMIT 1 FOR UPDATE`, input.UserID, input.WorldID).Scan(&existingID, &existingVersion)
	if err == nil {
		var existingWorldVersion int64
		var existingSnapshot []byte
		if err := tx.QueryRowContext(ctx, `SELECT world_version,world_snapshot_json FROM simulations WHERE id=UUID_TO_BIN(?,1)`, existingID).Scan(&existingWorldVersion, &existingSnapshot); err != nil {
			return Journey{}, fmt.Errorf("load existing journey: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return Journey{}, fmt.Errorf("commit journey: %w", err)
		}
		return Journey{ID: existingID, UserID: input.UserID, WorldID: input.WorldID, WorldVersion: existingWorldVersion, WorldSnapshot: existingSnapshot, Status: "active", Version: existingVersion, CreatedAt: capturedAt}, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Journey{}, fmt.Errorf("check existing journey: %w", err)
	}

	journeyID, err := newUUID()
	if err != nil {
		return Journey{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO simulations (id,user_id,world_id,world_version,world_snapshot_json,state_json,summary,status,version,last_turn_seq) VALUES (UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),UUID_TO_BIN(?,1),?,?,JSON_OBJECT(),'','active',1,0)`, journeyID, input.UserID, input.WorldID, version, snapshot)
	if err != nil {
		return Journey{}, fmt.Errorf("insert journey: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Journey{}, fmt.Errorf("commit journey: %w", err)
	}
	return Journey{ID: journeyID, UserID: input.UserID, WorldID: input.WorldID, WorldVersion: version, WorldSnapshot: snapshot, Status: "active", Version: 1, CreatedAt: capturedAt}, nil
}

func buildSnapshot(worldID, ownerID, title, summary, visibility, status string, version int64, setting []byte, capturedAt time.Time) (json.RawMessage, error) {
	var settingValue any
	if err := json.Unmarshal(setting, &settingValue); err != nil {
		return nil, fmt.Errorf("decode world setting: %w", err)
	}
	snapshot := map[string]any{
		"schemaVersion": 1,
		"capturedAt":    capturedAt.UTC().Format(time.RFC3339Nano),
		"world": map[string]any{
			"id": worldID, "ownerId": ownerID, "version": version, "title": title, "summary": summary,
			"visibilityAtCapture": visibility, "statusAtCapture": status,
		},
		"setting": settingValue,
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return nil, fmt.Errorf("encode world snapshot: %w", err)
	}
	return payload, nil
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func isUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	compact := value[0:8] + value[9:13] + value[14:18] + value[19:23] + value[24:36]
	_, err := hex.DecodeString(compact)
	return err == nil
}
