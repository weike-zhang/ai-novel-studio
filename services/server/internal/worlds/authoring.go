package worlds

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var (
	ErrVersionConflict           = errors.New("world version conflict")
	ErrInvalidTransition         = errors.New("invalid world state transition")
	ErrHumanConfirmationRequired = errors.New("AI draft requires human field confirmation")
	ErrOwnerSuspended            = errors.New("world owner is suspended")
)

type DraftSource string

const (
	DraftSourceManual DraftSource = "manual"
	DraftSourceAI     DraftSource = "ai"
)

const maxSettingBytes = 128 << 10

var requiredPublishFields = []string{
	"title",
	"summary",
	"setting.worldBackground",
	"setting.playerIdentity",
	"setting.characters",
	"setting.opening",
	"setting.contentRating",
}

// RequiredPublishFieldNames returns the complete field set an author must
// explicitly confirm before an AI-authored draft can be published.
func RequiredPublishFieldNames() []string {
	return append([]string(nil), requiredPublishFields...)
}

type ValidationIssue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationError struct {
	Issues []ValidationIssue `json:"issues"`
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("world validation failed with %d issue(s)", len(e.Issues))
}

type CreateDraftInput struct {
	OwnerID string
	Title   string
	Summary string
	Setting json.RawMessage
	Source  DraftSource
}

type UpdateDraftInput struct {
	OwnerID         string
	WorldID         string
	ExpectedVersion int64
	Title           string
	Summary         string
	Setting         json.RawMessage
}

type ConfirmAIFieldsInput struct {
	OwnerID         string
	WorldID         string
	ExpectedVersion int64
	Fields          []string
}

type ChangePublicationInput struct {
	OwnerID         string
	WorldID         string
	ExpectedVersion int64
}

type AuthoringWorld struct {
	ID                    string          `json:"id"`
	OwnerID               string          `json:"ownerId"`
	Title                 string          `json:"title"`
	Summary               string          `json:"summary"`
	Setting               json.RawMessage `json:"setting"`
	Visibility            string          `json:"visibility"`
	Status                string          `json:"status"`
	Version               int64           `json:"version"`
	DraftSource           DraftSource     `json:"draftSource"`
	HumanConfirmedVersion *int64          `json:"humanConfirmedVersion,omitempty"`
	HumanConfirmedFields  []string        `json:"humanConfirmedFields,omitempty"`
	HumanConfirmedAt      *time.Time      `json:"humanConfirmedAt,omitempty"`
	PublishedAt           *time.Time      `json:"publishedAt,omitempty"`
	UnpublishedAt         *time.Time      `json:"unpublishedAt,omitempty"`
	CreatedAt             time.Time       `json:"createdAt"`
	UpdatedAt             time.Time       `json:"updatedAt"`
}

func (s *Store) CreateDraft(ctx context.Context, input CreateDraftInput) (AuthoringWorld, error) {
	if !isUUID(input.OwnerID) {
		return AuthoringWorld{}, ErrNotFound
	}
	input.Source = normalizeDraftSource(input.Source)
	if input.Source == "" {
		return AuthoringWorld{}, validationError("source", "invalid", "起草来源必须是 manual 或 ai")
	}
	title, summary, setting, err := validateSavableDraft(input.Title, input.Summary, input.Setting)
	if err != nil {
		return AuthoringWorld{}, err
	}
	id, err := newUUID()
	if err != nil {
		return AuthoringWorld{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO worlds (id,owner_id,title,summary,setting_json,draft_source,visibility,status,version) SELECT UUID_TO_BIN(?,1),id,?,?,?,?,'private','draft',1 FROM users WHERE id=UUID_TO_BIN(?,1)`, id, title, summary, setting, input.Source, input.OwnerID)
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("create world draft: %w", err)
	}
	world, err := s.loadAuthoringWorld(ctx, nil, input.OwnerID, id, false)
	if err != nil {
		return AuthoringWorld{}, err
	}
	return world, nil
}

// MyWorldSummary is a lightweight entry for the "my creations" list. It
// carries the fields the account page needs without the full setting payload.
type MyWorldSummary struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Summary         string    `json:"summary"`
	Visibility      string    `json:"visibility"`
	Status          string    `json:"status"`
	Version         int64     `json:"version"`
	SimulationCount int64     `json:"simulationCount"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// SetCover stores the generated cover URL into the world setting. It is only
// allowed for the owner and updates the world version so public readers see the
// refreshed cover.
func (s *Store) SetCover(ctx context.Context, ownerID, worldID, coverURL string) error {
	if !validAuthoringIdentity(ownerID, worldID) {
		return ErrNotFound
	}
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET setting_json=JSON_SET(setting_json,'$.cover',?),version=version+1,human_confirmed_version=NULL,human_confirmed_fields=NULL,human_confirmed_at=NULL WHERE id=UUID_TO_BIN(?,1) AND owner_id=UUID_TO_BIN(?,1) AND status='draft' AND visibility='private'`, coverURL, worldID, ownerID)
	if err != nil {
		return fmt.Errorf("set world cover: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set world cover rows: %w", err)
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListMyWorlds(ctx context.Context, ownerID string) ([]MyWorldSummary, error) {
	if !isUUID(ownerID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id,1),title,summary,visibility,status,version,(SELECT COUNT(*) FROM simulations sim WHERE sim.world_id=w.id),updated_at FROM worlds w WHERE owner_id=UUID_TO_BIN(?,1) ORDER BY updated_at DESC`, ownerID)
	if err != nil {
		return nil, fmt.Errorf("list my worlds: %w", err)
	}
	defer rows.Close()
	worlds := make([]MyWorldSummary, 0)
	for rows.Next() {
		var world MyWorldSummary
		if err := rows.Scan(&world.ID, &world.Title, &world.Summary, &world.Visibility, &world.Status, &world.Version, &world.SimulationCount, &world.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan my world: %w", err)
		}
		worlds = append(worlds, world)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate my worlds: %w", err)
	}
	return worlds, nil
}

func (s *Store) UpdateDraft(ctx context.Context, input UpdateDraftInput) (AuthoringWorld, error) {
	if !validAuthoringIdentity(input.OwnerID, input.WorldID) {
		return AuthoringWorld{}, ErrNotFound
	}
	if input.ExpectedVersion <= 0 {
		return AuthoringWorld{}, ErrVersionConflict
	}
	title, summary, setting, err := validateSavableDraft(input.Title, input.Summary, input.Setting)
	if err != nil {
		return AuthoringWorld{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET title=?,summary=?,setting_json=?,version=version+1,human_confirmed_version=NULL,human_confirmed_fields=NULL,human_confirmed_at=NULL WHERE id=UUID_TO_BIN(?,1) AND owner_id=UUID_TO_BIN(?,1) AND status='draft' AND visibility='private' AND version=?`, title, summary, setting, input.WorldID, input.OwnerID, input.ExpectedVersion)
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("update world draft: %w", err)
	}
	if err := s.requireAffected(ctx, result, input.OwnerID, input.WorldID, input.ExpectedVersion, "draft"); err != nil {
		return AuthoringWorld{}, err
	}
	return s.loadAuthoringWorld(ctx, nil, input.OwnerID, input.WorldID, false)
}

func (s *Store) ConfirmAIFields(ctx context.Context, input ConfirmAIFieldsInput) (AuthoringWorld, error) {
	if !validAuthoringIdentity(input.OwnerID, input.WorldID) {
		return AuthoringWorld{}, ErrNotFound
	}
	if input.ExpectedVersion <= 0 {
		return AuthoringWorld{}, ErrVersionConflict
	}
	fields, err := normalizeConfirmedFields(input.Fields)
	if err != nil {
		return AuthoringWorld{}, err
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("encode confirmed fields: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET human_confirmed_version=version,human_confirmed_fields=?,human_confirmed_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1) AND owner_id=UUID_TO_BIN(?,1) AND draft_source='ai' AND status='draft' AND visibility='private' AND version=?`, payload, input.WorldID, input.OwnerID, input.ExpectedVersion)
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("confirm AI world fields: %w", err)
	}
	if err := s.requireAffected(ctx, result, input.OwnerID, input.WorldID, input.ExpectedVersion, "draft"); err != nil {
		return AuthoringWorld{}, err
	}
	return s.loadAuthoringWorld(ctx, nil, input.OwnerID, input.WorldID, false)
}

func (s *Store) Publish(ctx context.Context, input ChangePublicationInput) (AuthoringWorld, error) {
	if !validAuthoringIdentity(input.OwnerID, input.WorldID) {
		return AuthoringWorld{}, ErrNotFound
	}
	if input.ExpectedVersion <= 0 {
		return AuthoringWorld{}, ErrVersionConflict
	}
	preview, err := s.loadAuthoringWorld(ctx, nil, input.OwnerID, input.WorldID, false)
	if err != nil {
		return AuthoringWorld{}, err
	}
	if preview.Status != "draft" || preview.Visibility != "private" {
		return AuthoringWorld{}, ErrInvalidTransition
	}
	if preview.Version != input.ExpectedVersion {
		return AuthoringWorld{}, ErrVersionConflict
	}
	if err := ValidateForPublication(preview.Title, preview.Summary, preview.Setting); err != nil {
		return AuthoringWorld{}, err
	}
	if preview.DraftSource == DraftSourceAI && !hasCurrentHumanConfirmation(preview) {
		return AuthoringWorld{}, ErrHumanConfirmationRequired
	}
	decision, err := s.moderator.ReviewPublication(ctx, PublicationContent{
		WorldID: preview.ID, OwnerID: preview.OwnerID, Version: preview.Version,
		Title: preview.Title, Summary: preview.Summary, Setting: append([]byte(nil), preview.Setting...),
	})
	if err != nil || validateModerationDecision(decision) != nil {
		return AuthoringWorld{}, ErrModerationUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("begin world publication: %w", err)
	}
	defer tx.Rollback()

	world, ownerStatus, err := s.loadAuthoringWorldForUpdate(ctx, tx, input.OwnerID, input.WorldID)
	if err != nil {
		return AuthoringWorld{}, err
	}
	if world.Status != "draft" || world.Visibility != "private" {
		return AuthoringWorld{}, ErrInvalidTransition
	}
	if world.Version != input.ExpectedVersion {
		return AuthoringWorld{}, ErrVersionConflict
	}
	if ownerStatus != "active" {
		return AuthoringWorld{}, ErrOwnerSuspended
	}
	if err := ValidateForPublication(world.Title, world.Summary, world.Setting); err != nil {
		return AuthoringWorld{}, err
	}
	if world.DraftSource == DraftSourceAI && !hasCurrentHumanConfirmation(world) {
		return AuthoringWorld{}, ErrHumanConfirmationRequired
	}
	decisionID, err := newUUID()
	if err != nil {
		return AuthoringWorld{}, err
	}
	contentHash := sha256.Sum256(append(append([]byte(world.Title+"\n"+world.Summary+"\n"), world.Setting...), byte(world.Version)))
	_, err = tx.ExecContext(ctx, `INSERT INTO moderation_decisions (id,content_type,content_id,content_version,category,action,reason_code,rule_version,source,content_hash) VALUES (UUID_TO_BIN(?,1),'world_publication',UUID_TO_BIN(?,1),?,?,?,?,?,?,?)`, decisionID, world.ID, world.Version, decision.Category, decision.Action, decision.ReasonCode, decision.RuleVersion, decision.Source, contentHash[:])
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("record publication moderation: %w", err)
	}
	if decision.Action == ModerationRevise || decision.Action == ModerationBlock {
		if err := tx.Commit(); err != nil {
			return AuthoringWorld{}, fmt.Errorf("commit publication moderation: %w", err)
		}
		if decision.Action == ModerationRevise {
			return AuthoringWorld{}, ErrModerationRevise
		}
		return AuthoringWorld{}, ErrModerationBlocked
	}
	visibility, status := "public", "published"
	if decision.Action == ModerationManualReview {
		visibility, status = "private", "pending_review"
	}
	result, err := tx.ExecContext(ctx, `UPDATE worlds SET visibility=?,status=?,version=version+1,published_at=IF(?='published',CURRENT_TIMESTAMP(6),NULL),unpublished_at=NULL WHERE id=UUID_TO_BIN(?,1) AND owner_id=UUID_TO_BIN(?,1) AND version=? AND status='draft' AND visibility='private'`, visibility, status, status, input.WorldID, input.OwnerID, input.ExpectedVersion)
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("publish world: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return AuthoringWorld{}, ErrVersionConflict
	}
	if err := tx.Commit(); err != nil {
		return AuthoringWorld{}, fmt.Errorf("commit world publication: %w", err)
	}
	return s.loadAuthoringWorld(ctx, nil, input.OwnerID, input.WorldID, false)
}

func (s *Store) Unpublish(ctx context.Context, input ChangePublicationInput) (AuthoringWorld, error) {
	if !validAuthoringIdentity(input.OwnerID, input.WorldID) {
		return AuthoringWorld{}, ErrNotFound
	}
	if input.ExpectedVersion <= 0 {
		return AuthoringWorld{}, ErrVersionConflict
	}
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET unpublished_at=IF(status='published',CURRENT_TIMESTAMP(6),unpublished_at),visibility='private',status='draft',version=version+1,human_confirmed_version=NULL,human_confirmed_fields=NULL,human_confirmed_at=NULL WHERE id=UUID_TO_BIN(?,1) AND owner_id=UUID_TO_BIN(?,1) AND status IN ('published','pending_review') AND version=?`, input.WorldID, input.OwnerID, input.ExpectedVersion)
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("unpublish world: %w", err)
	}
	if err := s.requireAffected(ctx, result, input.OwnerID, input.WorldID, input.ExpectedVersion, "published_or_pending"); err != nil {
		return AuthoringWorld{}, err
	}
	return s.loadAuthoringWorld(ctx, nil, input.OwnerID, input.WorldID, false)
}

// SuspendByModerator takes a published public world down. It is intended for
// platform moderators: the world stays visible to its owner but disappears from
// the public catalog until RestoreByModerator returns it to published.
func (s *Store) SuspendByModerator(ctx context.Context, worldID string) (AuthoringWorld, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET visibility='private',status='suspended',version=version+1,unpublished_at=CURRENT_TIMESTAMP(6) WHERE id=UUID_TO_BIN(?,1) AND visibility='public' AND status='published'`, worldID)
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("suspend world: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return AuthoringWorld{}, ErrNotFound
	}
	return s.loadAuthoringWorldForOwner(ctx, worldID)
}

// RestoreByModerator returns a suspended world to the public catalog.
func (s *Store) RestoreByModerator(ctx context.Context, worldID string) (AuthoringWorld, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET visibility='public',status='published',version=version+1,published_at=CURRENT_TIMESTAMP(6),unpublished_at=NULL WHERE id=UUID_TO_BIN(?,1) AND status='suspended'`, worldID)
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("restore world: %w", err)
	}
	affected, _ := result.RowsAffected()
	if affected != 1 {
		return AuthoringWorld{}, ErrNotFound
	}
	return s.loadAuthoringWorldForOwner(ctx, worldID)
}

// SetCoverByModerator replaces the cover of any world on behalf of a platform
// moderator. Unlike SetCover it does not require the caller to be the owner and
// works on published public worlds, so editorial content can get a fresh cover
// without charging the author. The world version is bumped so public readers
// see the refreshed cover.
func (s *Store) SetCoverByModerator(ctx context.Context, worldID, coverURL string) error {
	if !isUUID(worldID) {
		return ErrNotFound
	}
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET setting_json=JSON_SET(setting_json,'$.cover',?),version=version+1,human_confirmed_version=NULL,human_confirmed_fields=NULL,human_confirmed_at=NULL WHERE id=UUID_TO_BIN(?,1)`, coverURL, worldID)
	if err != nil {
		return fmt.Errorf("set world cover by moderator: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set world cover by moderator rows: %w", err)
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}

// ListCoverlessWorlds returns the IDs of published public worlds that have no
// stored cover. Used by the moderator "generate all missing covers" action.
func (s *Store) ListCoverlessWorlds(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT BIN_TO_UUID(id,1) FROM worlds WHERE visibility='public' AND status='published' AND (JSON_UNQUOTE(JSON_EXTRACT(setting_json,'$.cover')) IS NULL OR JSON_UNQUOTE(JSON_EXTRACT(setting_json,'$.cover'))='')`)
	if err != nil {
		return nil, fmt.Errorf("list coverless worlds: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan coverless world: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate coverless worlds: %w", err)
	}
	return ids, nil
}

// SetFeaturedByModerator marks a world as editor-recommended, assigning it the
// next available rank so the recommendation shelf ordering stays stable.
func (s *Store) SetFeaturedByModerator(ctx context.Context, worldID string) (int, error) {
	if !isUUID(worldID) {
		return 0, ErrNotFound
	}
	var maxRank sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(CAST(JSON_UNQUOTE(JSON_EXTRACT(setting_json,'$.discovery.featuredRank')) AS UNSIGNED)) FROM worlds`).Scan(&maxRank); err != nil {
		return 0, fmt.Errorf("read max featured rank: %w", err)
	}
	rank := int(maxRank.Int64) + 1
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET setting_json=JSON_SET(setting_json,'$.discovery.featuredRank',?),version=version+1,human_confirmed_version=NULL,human_confirmed_fields=NULL,human_confirmed_at=NULL WHERE id=UUID_TO_BIN(?,1)`, rank, worldID)
	if err != nil {
		return 0, fmt.Errorf("set world featured: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("set world featured rows: %w", err)
	}
	if affected != 1 {
		return 0, ErrNotFound
	}
	return rank, nil
}

// UnsetFeaturedByModerator removes a world from the editor recommendation
// shelf by clearing its featured rank.
func (s *Store) UnsetFeaturedByModerator(ctx context.Context, worldID string) error {
	if !isUUID(worldID) {
		return ErrNotFound
	}
	result, err := s.db.ExecContext(ctx, `UPDATE worlds SET setting_json=JSON_SET(setting_json,'$.discovery.featuredRank',0),version=version+1,human_confirmed_version=NULL,human_confirmed_fields=NULL,human_confirmed_at=NULL WHERE id=UUID_TO_BIN(?,1)`, worldID)
	if err != nil {
		return fmt.Errorf("unset world featured: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("unset world featured rows: %w", err)
	}
	if affected != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) loadAuthoringWorldForOwner(ctx context.Context, worldID string) (AuthoringWorld, error) {
	row := s.db.QueryRowContext(ctx, `SELECT BIN_TO_UUID(w.id,1),BIN_TO_UUID(w.owner_id,1),w.title,w.summary,w.setting_json,w.visibility,w.status,w.version,w.draft_source,w.created_at,w.updated_at FROM worlds w WHERE w.id=UUID_TO_BIN(?,1)`, worldID)
	var world AuthoringWorld
	var setting []byte
	if err := row.Scan(&world.ID, &world.OwnerID, &world.Title, &world.Summary, &setting, &world.Visibility, &world.Status, &world.Version, &world.DraftSource, &world.CreatedAt, &world.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthoringWorld{}, ErrNotFound
		}
		return AuthoringWorld{}, fmt.Errorf("load world: %w", err)
	}
	world.Setting = json.RawMessage(setting)
	return world, nil
}

func ValidateForPublication(title, summary string, setting json.RawMessage) error {
	issues := validateDraftValues(title, summary, setting, true)
	if len(issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: issues}
}

func validateSavableDraft(title, summary string, setting json.RawMessage) (string, string, json.RawMessage, error) {
	title = strings.TrimSpace(title)
	summary = strings.TrimSpace(summary)
	if len(setting) == 0 {
		setting = json.RawMessage(`{}`)
	}
	issues := validateDraftValues(title, summary, setting, false)
	if len(issues) > 0 {
		return "", "", nil, &ValidationError{Issues: issues}
	}
	var value map[string]any
	if err := json.Unmarshal(setting, &value); err != nil {
		return "", "", nil, validationError("setting", "invalid_json", "世界设定必须是 JSON 对象")
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return "", "", nil, fmt.Errorf("normalize world setting: %w", err)
	}
	return title, summary, normalized, nil
}

func validateDraftValues(title, summary string, setting json.RawMessage, publishing bool) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	if strings.TrimSpace(title) == "" {
		issues = append(issues, issue("title", "required", "标题不能为空"))
	} else if runeLength(title) > 80 {
		issues = append(issues, issue("title", "too_long", "标题不能超过 80 个字"))
	}
	if runeLength(summary) > 500 {
		issues = append(issues, issue("summary", "too_long", "简介不能超过 500 个字"))
	} else if publishing && strings.TrimSpace(summary) == "" {
		issues = append(issues, issue("summary", "required", "发布前必须填写简介"))
	}
	if len(setting) > maxSettingBytes {
		issues = append(issues, issue("setting", "too_large", "世界设定不能超过 128 KiB"))
		return issues
	}
	var value map[string]any
	if err := json.Unmarshal(setting, &value); err != nil {
		issues = append(issues, issue("setting", "invalid_json", "世界设定必须是 JSON 对象"))
		return issues
	}
	issues = append(issues, validateJSONShape(value, "setting", 0)...)
	if !publishing {
		return issues
	}
	if !hasWorldBackground(value) {
		issues = append(issues, issue("setting.worldBackground", "required", "发布前必须填写世界背景"))
	}
	if !hasPlayerIdentity(value) {
		issues = append(issues, issue("setting.playerIdentity", "required", "发布前必须填写玩家身份"))
	}
	if !hasCharacters(value) {
		issues = append(issues, issue("setting.characters", "required", "发布前至少需要一名有名称的角色"))
	}
	if !hasOpening(value) {
		issues = append(issues, issue("setting.opening", "required", "发布前必须填写开场"))
	}
	if !hasContentRating(value) {
		issues = append(issues, issue("setting.contentRating", "required", "发布前必须选择内容分级"))
	}
	return issues
}

func validateJSONShape(value any, path string, depth int) []ValidationIssue {
	if depth > 12 {
		return []ValidationIssue{issue(path, "too_deep", "世界设定嵌套不能超过 12 层")}
	}
	issues := make([]ValidationIssue, 0)
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) > 200 {
			issues = append(issues, issue(path, "too_many_fields", "单个对象不能超过 200 个字段"))
		}
		for key, child := range typed {
			childPath := path + "." + key
			if runeLength(key) > 80 {
				issues = append(issues, issue(childPath, "key_too_long", "字段名不能超过 80 个字"))
			}
			if isForbiddenAuthorKey(key) {
				issues = append(issues, issue(childPath, "forbidden", "创作者不能覆盖平台系统或安全规则"))
			}
			issues = append(issues, validateJSONShape(child, childPath, depth+1)...)
		}
	case []any:
		if len(typed) > 100 {
			issues = append(issues, issue(path, "too_many_items", "单个列表不能超过 100 项"))
		}
		for index, child := range typed {
			issues = append(issues, validateJSONShape(child, fmt.Sprintf("%s[%d]", path, index), depth+1)...)
		}
	case string:
		if runeLength(typed) > 4000 {
			issues = append(issues, issue(path, "too_long", "单个设定字段不能超过 4000 个字"))
		}
	}
	return issues
}

func normalizeConfirmedFields(fields []string) ([]string, error) {
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			seen[field] = struct{}{}
		}
	}
	missing := make([]string, 0)
	for _, required := range requiredPublishFields {
		if _, ok := seen[required]; !ok {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return nil, &ValidationError{Issues: []ValidationIssue{issue("confirmedFields", "incomplete", "请逐项确认全部发布必填字段: "+strings.Join(missing, ", "))}}
	}
	result := RequiredPublishFieldNames()
	sort.Strings(result)
	return result, nil
}

func hasCurrentHumanConfirmation(world AuthoringWorld) bool {
	if world.HumanConfirmedVersion == nil || *world.HumanConfirmedVersion != world.Version {
		return false
	}
	_, err := normalizeConfirmedFields(world.HumanConfirmedFields)
	return err == nil
}

func (s *Store) loadAuthoringWorldForUpdate(ctx context.Context, tx *sql.Tx, ownerID, worldID string) (AuthoringWorld, string, error) {
	world, ownerStatus, err := scanAuthoringWorld(tx.QueryRowContext(ctx, authoringWorldSelect+` WHERE w.id=UUID_TO_BIN(?,1) AND w.owner_id=UUID_TO_BIN(?,1) FOR UPDATE`, worldID, ownerID))
	if errors.Is(err, sql.ErrNoRows) {
		return AuthoringWorld{}, "", ErrNotFound
	}
	if err != nil {
		return AuthoringWorld{}, "", fmt.Errorf("load world for publication: %w", err)
	}
	return world, ownerStatus, nil
}

func (s *Store) loadAuthoringWorld(ctx context.Context, tx *sql.Tx, ownerID, worldID string, forUpdate bool) (AuthoringWorld, error) {
	query := authoringWorldSelect + ` WHERE w.id=UUID_TO_BIN(?,1) AND w.owner_id=UUID_TO_BIN(?,1)`
	if forUpdate {
		query += " FOR UPDATE"
	}
	var row *sql.Row
	if tx != nil {
		row = tx.QueryRowContext(ctx, query, worldID, ownerID)
	} else {
		row = s.db.QueryRowContext(ctx, query, worldID, ownerID)
	}
	world, _, err := scanAuthoringWorld(row)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthoringWorld{}, ErrNotFound
	}
	if err != nil {
		return AuthoringWorld{}, fmt.Errorf("load authored world: %w", err)
	}
	return world, nil
}

const authoringWorldSelect = `SELECT BIN_TO_UUID(w.id,1),BIN_TO_UUID(w.owner_id,1),w.title,w.summary,w.setting_json,w.visibility,w.status,w.version,w.draft_source,w.human_confirmed_version,w.human_confirmed_fields,w.human_confirmed_at,w.published_at,w.unpublished_at,w.created_at,w.updated_at,u.status FROM worlds w JOIN users u ON u.id=w.owner_id`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAuthoringWorld(row rowScanner) (AuthoringWorld, string, error) {
	var world AuthoringWorld
	var setting []byte
	var confirmedVersion sql.NullInt64
	var confirmedFields []byte
	var confirmedAt, publishedAt, unpublishedAt sql.NullTime
	var ownerStatus string
	if err := row.Scan(&world.ID, &world.OwnerID, &world.Title, &world.Summary, &setting, &world.Visibility, &world.Status, &world.Version, &world.DraftSource, &confirmedVersion, &confirmedFields, &confirmedAt, &publishedAt, &unpublishedAt, &world.CreatedAt, &world.UpdatedAt, &ownerStatus); err != nil {
		return AuthoringWorld{}, "", err
	}
	world.Setting = append(json.RawMessage(nil), setting...)
	if confirmedVersion.Valid {
		value := confirmedVersion.Int64
		world.HumanConfirmedVersion = &value
	}
	if len(confirmedFields) > 0 {
		if err := json.Unmarshal(confirmedFields, &world.HumanConfirmedFields); err != nil {
			return AuthoringWorld{}, "", fmt.Errorf("decode confirmed world fields: %w", err)
		}
	}
	world.HumanConfirmedAt = nullableTime(confirmedAt)
	world.PublishedAt = nullableTime(publishedAt)
	world.UnpublishedAt = nullableTime(unpublishedAt)
	return world, ownerStatus, nil
}

func (s *Store) requireAffected(ctx context.Context, result sql.Result, ownerID, worldID string, expectedVersion int64, expectedStatus string) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read affected world rows: %w", err)
	}
	if affected == 1 {
		return nil
	}
	var version int64
	var status, visibility, source string
	err = s.db.QueryRowContext(ctx, `SELECT version,status,visibility,draft_source FROM worlds WHERE id=UUID_TO_BIN(?,1) AND owner_id=UUID_TO_BIN(?,1)`, worldID, ownerID).Scan(&version, &status, &visibility, &source)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("inspect world update conflict: %w", err)
	}
	if version != expectedVersion {
		return ErrVersionConflict
	}
	if expectedStatus == "draft" && source != string(DraftSourceAI) {
		return ErrInvalidTransition
	}
	if expectedStatus == "published_or_pending" {
		if (status == "published" && visibility == "public") || (status == "pending_review" && visibility == "private") {
			return ErrVersionConflict
		}
		return ErrInvalidTransition
	}
	if status != expectedStatus || (expectedStatus == "draft" && visibility != "private") || (expectedStatus == "published" && visibility != "public") {
		return ErrInvalidTransition
	}
	return ErrInvalidTransition
}

func hasWorldBackground(setting map[string]any) bool {
	return nonEmptyValue(setting["worldBackground"]) || nonEmptyValue(setting["premise"]) || nonEmptyValue(setting["eraPlace"])
}

func hasPlayerIdentity(setting map[string]any) bool {
	return nonEmptyValue(setting["playerIdentity"]) || nonEmptyValue(setting["playerTemplate"]) || nonEmptyValue(setting["player"])
}

func hasCharacters(setting map[string]any) bool {
	characters, ok := setting["characters"].([]any)
	if !ok || len(characters) == 0 {
		return false
	}
	for _, character := range characters {
		entry, ok := character.(map[string]any)
		if ok && nonEmptyValue(entry["name"]) {
			return true
		}
	}
	return false
}

func hasOpening(setting map[string]any) bool {
	return nonEmptyValue(setting["opening"])
}

func hasContentRating(setting map[string]any) bool {
	if nonEmptyValue(setting["contentRating"]) || nonEmptyValue(setting["rating"]) {
		return true
	}
	boundaries, ok := setting["contentBoundaries"].(map[string]any)
	return ok && nonEmptyValue(boundaries["rating"])
}

func nonEmptyValue(value any) bool {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case map[string]any:
		for _, child := range typed {
			if nonEmptyValue(child) {
				return true
			}
		}
		return false
	case []any:
		return len(typed) > 0
	default:
		return value != nil
	}
}

func isForbiddenAuthorKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	switch normalized {
	case "systemprompt", "developerprompt", "safetyoverride", "platformsafetyrules", "ignoreplatformrules", "bypassmoderation":
		return true
	default:
		return false
	}
}

func normalizeDraftSource(source DraftSource) DraftSource {
	if source == "" {
		return DraftSourceManual
	}
	if source == DraftSourceManual || source == DraftSourceAI {
		return source
	}
	return ""
}

func validAuthoringIdentity(ownerID, worldID string) bool {
	return isUUID(ownerID) && isUUID(worldID)
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}

func runeLength(value string) int {
	return len([]rune(value))
}

func validationError(field, code, message string) error {
	return &ValidationError{Issues: []ValidationIssue{issue(field, code, message)}}
}

func issue(field, code, message string) ValidationIssue {
	return ValidationIssue{Field: field, Code: code, Message: message}
}
