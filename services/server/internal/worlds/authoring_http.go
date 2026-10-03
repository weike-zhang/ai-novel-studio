package worlds

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

type AuthoringRepository interface {
	CreateDraft(context.Context, CreateDraftInput) (AuthoringWorld, error)
	UpdateDraft(context.Context, UpdateDraftInput) (AuthoringWorld, error)
	ConfirmAIFields(context.Context, ConfirmAIFieldsInput) (AuthoringWorld, error)
	Publish(context.Context, ChangePublicationInput) (AuthoringWorld, error)
	Unpublish(context.Context, ChangePublicationInput) (AuthoringWorld, error)
}

type AuthoringHandler struct{ store AuthoringRepository }

func NewAuthoringHandler(store AuthoringRepository) http.Handler {
	return &AuthoringHandler{store: store}
}

func (h *AuthoringHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1"), "/")
	if path == "worlds" && r.Method == http.MethodPost {
		h.create(w, r, userID)
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] != "worlds" {
		writeError(w, http.StatusNotFound, "not_found", "资源不存在")
		return
	}
	worldID := parts[1]
	switch {
	case len(parts) == 2 && r.Method == http.MethodPatch:
		h.update(w, r, userID, worldID)
	case len(parts) == 3 && parts[2] == "confirm-ai-fields" && r.Method == http.MethodPost:
		h.confirm(w, r, userID, worldID)
	case len(parts) == 3 && parts[2] == "publish" && r.Method == http.MethodPost:
		h.publication(w, r, userID, worldID, true)
	case len(parts) == 3 && parts[2] == "unpublish" && r.Method == http.MethodPost:
		h.publication(w, r, userID, worldID, false)
	default:
		writeError(w, http.StatusNotFound, "not_found", "资源不存在")
	}
}

func (h *AuthoringHandler) create(w http.ResponseWriter, r *http.Request, ownerID string) {
	var body struct {
		Title   string          `json:"title"`
		Summary string          `json:"summary"`
		Setting json.RawMessage `json:"setting"`
		Source  DraftSource     `json:"source"`
	}
	if !decodeAuthoringJSON(w, r, &body) {
		return
	}
	world, err := h.store.CreateDraft(r.Context(), CreateDraftInput{OwnerID: ownerID, Title: body.Title, Summary: body.Summary, Setting: body.Setting, Source: body.Source})
	if err != nil {
		writeAuthoringError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, world)
}

func (h *AuthoringHandler) update(w http.ResponseWriter, r *http.Request, ownerID, worldID string) {
	var body struct {
		ExpectedVersion int64           `json:"expectedVersion"`
		Title           string          `json:"title"`
		Summary         string          `json:"summary"`
		Setting         json.RawMessage `json:"setting"`
	}
	if !decodeAuthoringJSON(w, r, &body) {
		return
	}
	world, err := h.store.UpdateDraft(r.Context(), UpdateDraftInput{OwnerID: ownerID, WorldID: worldID, ExpectedVersion: body.ExpectedVersion, Title: body.Title, Summary: body.Summary, Setting: body.Setting})
	if err != nil {
		writeAuthoringError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, world)
}

func (h *AuthoringHandler) confirm(w http.ResponseWriter, r *http.Request, ownerID, worldID string) {
	var body struct {
		ExpectedVersion int64    `json:"expectedVersion"`
		Fields          []string `json:"fields"`
	}
	if !decodeAuthoringJSON(w, r, &body) {
		return
	}
	world, err := h.store.ConfirmAIFields(r.Context(), ConfirmAIFieldsInput{OwnerID: ownerID, WorldID: worldID, ExpectedVersion: body.ExpectedVersion, Fields: body.Fields})
	if err != nil {
		writeAuthoringError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, world)
}

func (h *AuthoringHandler) publication(w http.ResponseWriter, r *http.Request, ownerID, worldID string, publish bool) {
	var body struct {
		ExpectedVersion int64 `json:"expectedVersion"`
	}
	if !decodeAuthoringJSON(w, r, &body) {
		return
	}
	input := ChangePublicationInput{OwnerID: ownerID, WorldID: worldID, ExpectedVersion: body.ExpectedVersion}
	var world AuthoringWorld
	var err error
	if publish {
		world, err = h.store.Publish(r.Context(), input)
	} else {
		world, err = h.store.Unpublish(r.Context(), input)
	}
	if err != nil {
		writeAuthoringError(w, err)
		return
	}
	status := http.StatusOK
	if publish && world.Status == "pending_review" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, world)
}

func decodeAuthoringJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 160<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || ensureJSONEnd(decoder) != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求体格式不正确")
		return false
	}
	return true
}

func writeAuthoringError(w http.ResponseWriter, err error) {
	var validation *ValidationError
	switch {
	case errors.As(err, &validation):
		writeJSON(w, http.StatusUnprocessableEntity, struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Issues  []ValidationIssue `json:"issues"`
		}{"world_validation_failed", "世界草稿还有需要处理的字段", validation.Issues})
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "世界不存在")
	case errors.Is(err, ErrVersionConflict):
		writeError(w, http.StatusConflict, "version_conflict", "草稿已在其他位置更新，请刷新后重试")
	case errors.Is(err, ErrHumanConfirmationRequired):
		writeError(w, http.StatusConflict, "human_confirmation_required", "AI 草稿需要逐项人工确认后才能发布")
	case errors.Is(err, ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_world_state", "当前世界状态不能执行此操作")
	case errors.Is(err, ErrOwnerSuspended):
		writeError(w, http.StatusForbidden, "account_suspended", "当前账户暂时不能发布")
	case errors.Is(err, ErrModerationRevise):
		writeError(w, http.StatusUnprocessableEntity, "publication_requires_revision", "公开内容需要修改后才能再次提交审核")
	case errors.Is(err, ErrModerationBlocked):
		writeError(w, http.StatusForbidden, "publication_blocked", "这个世界不能公开传播；请调整内容后再试")
	case errors.Is(err, ErrModerationUnavailable):
		writeError(w, http.StatusServiceUnavailable, "moderation_unavailable", "发布审核暂时不可用，世界仍保持私密")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}
