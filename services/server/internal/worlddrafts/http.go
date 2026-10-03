package worlddrafts

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlds"
)

type Handler struct{ store *Store }

func NewHandler(store *Store) *Handler { return &Handler{store: store} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID, ok := worlds.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication_required", "请先登录")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1"), "/")
	parts := strings.Split(path, "/")
	switch {
	case len(parts) == 2 && parts[0] == "worlds" && parts[1] == "generate-draft" && r.Method == http.MethodPost:
		h.create(w, r, userID)
	case len(parts) == 2 && parts[0] == "jobs" && r.Method == http.MethodGet:
		h.get(w, r, userID, parts[1])
	case len(parts) == 3 && parts[0] == "jobs" && parts[2] == "cancel" && r.Method == http.MethodPost:
		h.cancel(w, r, userID, parts[1])
	case len(parts) == 3 && parts[0] == "jobs" && parts[2] == "retry" && r.Method == http.MethodPost:
		h.retry(w, r, userID, parts[1])
	case len(parts) == 3 && parts[0] == "jobs" && parts[2] == "events" && r.Method == http.MethodGet:
		h.stream(w, r, userID, parts[1])
	default:
		writeError(w, http.StatusNotFound, "not_found", "资源不存在")
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, userID string) {
	var body struct {
		Topic         string `json:"topic"`
		Type          string `json:"type"`
		ContentRating string `json:"contentRating"`
		Boundaries    string `json:"boundaries"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "请求体必须是 JSON")
		return
	}
	input := GenerateInput{Topic: body.Topic, Type: body.Type, ContentRating: body.ContentRating, Boundaries: body.Boundaries}
	job, fresh, err := h.store.Create(r.Context(), CreateInput{
		UserID: userID, IdempotencyKey: r.Header.Get("Idempotency-Key"), GenerateInput: input,
	})
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !fresh {
		w.Header().Set("Idempotent-Replayed", "true")
	}
	w.Header().Set("Location", "/v1/jobs/"+job.ID)
	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request, userID, jobID string) {
	job, err := h.store.Get(r.Context(), userID, jobID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *Handler) cancel(w http.ResponseWriter, r *http.Request, userID, jobID string) {
	job, err := h.store.Cancel(r.Context(), userID, jobID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) retry(w http.ResponseWriter, r *http.Request, userID, jobID string) {
	job, err := h.store.Retry(r.Context(), userID, jobID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, job)
}

func (h *Handler) stream(w http.ResponseWriter, r *http.Request, userID, jobID string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming_unsupported", "服务器不支持事件流")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	lastEvent := r.Header.Get("Last-Event-ID")
	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(125 * time.Second)
	defer timeout.Stop()
	for {
		job, err := h.store.Get(r.Context(), userID, jobID)
		if err != nil {
			writeSSE(w, "", "error", ginH{"code": "job_unavailable"})
			flusher.Flush()
			return
		}
		eventID := statusEventID(job)
		if eventID != lastEvent {
			writeSSE(w, eventID, "job.status", ginH{"status": job.Status, "errorCode": job.ErrorCode})
			lastEvent = eventID
			if job.Status == StatusCompleted && job.Result != nil {
				writeSSE(w, eventID+":world", "world_draft.completed", ginH{"jobId": job.ID, "worldId": job.Result.ID})
				flusher.Flush()
				return
			}
			if job.Status == StatusFailed || job.Status == StatusCancelled {
				flusher.Flush()
				return
			}
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-timeout.C:
			return
		case <-ticker.C:
		}
	}
}

func statusEventID(job Job) string {
	return job.ID + ":" + string(job.Status) + ":" + strconv.Itoa(job.AttemptNo)
}

type ginH map[string]any

func writeSSE(w http.ResponseWriter, id, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		payload = []byte(`{"code":"event_encoding_failed"}`)
	}
	if id != "" {
		_, _ = w.Write([]byte("id:" + id + "\n"))
	}
	_, _ = w.Write([]byte("event:" + event + "\ndata:" + string(payload) + "\n\n"))
}

func writeStoreError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "世界草稿任务处理失败"
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrForbidden):
		status, code, message = http.StatusNotFound, "not_found", "任务不存在"
	case errors.Is(err, ErrInvalidInput):
		status, code, message = http.StatusUnprocessableEntity, "invalid_input", err.Error()
	case errors.Is(err, ErrIdempotencyConflict):
		status, code, message = http.StatusConflict, "conflict", err.Error()
	case errors.Is(err, ErrConcurrentState), errors.Is(err, ErrAttemptsExhausted):
		status, code, message = http.StatusConflict, "invalid_job_state", err.Error()
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
