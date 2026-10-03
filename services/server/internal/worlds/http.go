package worlds

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type Repository interface {
	ListPublic(context.Context, PublicWorldQuery) (PublicWorldPage, error)
	Get(context.Context, string, string) (World, error)
	CreateJourney(context.Context, CreateJourneyInput) (Journey, error)
}

type Handler struct {
	store Repository
}

func NewHandler(store Repository) http.Handler {
	return &Handler{store: store}
}

type userIDContextKey struct{}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, strings.TrimSpace(userID))
}

func UserIDFromContext(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(userIDContextKey{}).(string)
	return userID, ok && userID != ""
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "" {
		path = "/"
	}
	if path == "/v1" {
		path = "/"
	} else {
		path = strings.TrimPrefix(path, "/v1")
	}

	switch {
	case path == "/worlds":
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.listWorlds(w, r)
	case strings.HasPrefix(path, "/worlds/") && !strings.Contains(strings.TrimPrefix(path, "/worlds/"), "/"):
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		h.getWorld(w, r, strings.TrimPrefix(path, "/worlds/"))
	case path == "/simulations":
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w, http.MethodPost)
			return
		}
		h.createSimulation(w, r)
	default:
		writeError(w, http.StatusNotFound, "not_found", "资源不存在")
	}
}

func (h *Handler) listWorlds(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit := 0
	if rawLimit := strings.TrimSpace(query.Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit 必须是整数")
			return
		}
		limit = parsed
	}

	input := PublicWorldQuery{
		Keyword: query.Get("keyword"),
		Type:    query.Get("type"),
		Tags:    parseTags(query["tags"]),
		Sort:    Sort(query.Get("sort")),
		Cursor:  query.Get("cursor"),
		Limit:   limit,
	}
	if _, _, _, err := buildPublicWorldQuery(input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	page, err := h.store.ListPublic(r.Context(), input)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *Handler) getWorld(w http.ResponseWriter, r *http.Request, worldID string) {
	viewerID, _ := UserIDFromContext(r.Context())
	world, err := h.store.Get(r.Context(), worldID, viewerID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, world)
}

func (h *Handler) createSimulation(w http.ResponseWriter, r *http.Request) {
	userID, authenticated := UserIDFromContext(r.Context())
	if !authenticated || !isUUID(userID) {
		writeError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}

	var body struct {
		WorldID string `json:"worldId"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求体必须包含 worldId")
		return
	}
	if err := ensureJSONEnd(decoder); err != nil || !isUUID(body.WorldID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "worldId 必须是 UUID")
		return
	}

	journey, err := h.store.CreateJourney(r.Context(), CreateJourneyInput{UserID: userID, WorldID: body.WorldID})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, journey)
}

func parseTags(values []string) []string {
	var tags []string
	for _, value := range values {
		for _, tag := range strings.Split(value, ",") {
			if tag = strings.TrimSpace(tag); tag != "" {
				tags = append(tags, tag)
			}
		}
	}
	return tags
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return errors.New("multiple JSON values")
	} else if errors.Is(err, io.EOF) {
		return nil
	} else {
		return err
	}
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "世界不存在")
		return
	}
	writeError(w, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
}

func writeMethodNotAllowed(w http.ResponseWriter, allowed string) {
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不受支持")
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
