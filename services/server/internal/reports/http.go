package reports

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlds"
)

type Handler struct{ store *Store }

func NewHandler(store *Store) http.Handler { return &Handler{store: store} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := worlds.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1"), "/")
	parts := strings.Split(path, "/")
	if len(parts) == 3 && parts[0] == "worlds" && parts[2] == "reports" && r.Method == http.MethodPost {
		h.create(w, r, userID, parts[1])
		return
	}
	writeError(w, http.StatusNotFound, "not_found", "资源不存在")
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, reporterID, worldID string) {
	var body struct {
		Reason string `json:"reason"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	report, err := h.store.Create(r.Context(), CreateInput{
		WorldID: worldID, ReporterID: reporterID, Reason: body.Reason, Note: body.Note,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Location", "/v1/worlds/"+worldID+"/reports/"+report.ID)
	writeJSON(w, http.StatusCreated, report)
}

func writeStoreError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "举报提交失败"
	switch {
	case errors.Is(err, ErrForbidden), errors.Is(err, ErrInvalidReason), errors.Is(err, ErrInvalidNote):
		status, code, message = http.StatusUnprocessableEntity, "invalid_report", err.Error()
	case errors.Is(err, ErrDuplicateReport):
		status, code, message = http.StatusConflict, "report_already_exists", "你已举报过这个世界，正在处理中"
	case errors.Is(err, ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "世界不存在"
	}
	writeError(w, status, code, message)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
