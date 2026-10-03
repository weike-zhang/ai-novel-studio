package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/turnjobs"
)

type TurnJobHandler struct {
	store     *turnjobs.Store
	processor *turnjobs.Processor
	workerID  string
}

func NewTurnJobHandler(store *turnjobs.Store, processor *turnjobs.Processor, workerID string) *TurnJobHandler {
	return &TurnJobHandler{store: store, processor: processor, workerID: workerID}
}

func (h *TurnJobHandler) Handle(parent context.Context, payload []byte, redelivered bool) (Disposition, error) {
	var message struct {
		JobID string `json:"jobId"`
	}
	if err := json.Unmarshal(payload, &message); err != nil || message.JobID == "" {
		return RejectDelivery, fmt.Errorf("invalid turn job message")
	}
	job, err := h.store.GetForProcessing(parent, message.JobID)
	if errors.Is(err, turnjobs.ErrNotFound) {
		return RejectDelivery, err
	}
	if err != nil {
		return RetryDelivery, err
	}
	switch job.Status {
	case turnjobs.StatusCompleted, turnjobs.StatusFailed, turnjobs.StatusCancelled:
		return Acknowledge, nil
	case turnjobs.StatusCommitting:
		return RetryDelivery, turnjobs.ErrConcurrentState
	case turnjobs.StatusPlanning, turnjobs.StatusDrafting, turnjobs.StatusValidating:
		if !redelivered {
			return Acknowledge, nil
		}
		recovered, err := h.store.RecoverForRedelivery(parent, job.ID)
		if err != nil {
			return RetryDelivery, err
		}
		if !recovered {
			return Acknowledge, nil
		}
	case turnjobs.StatusQueued:
	default:
		return RejectDelivery, fmt.Errorf("unknown job status %q", job.Status)
	}
	ctx, cancel := context.WithTimeout(parent, 125*time.Second)
	defer cancel()
	if _, err := h.processor.Process(ctx, job.ID, h.workerID); err != nil {
		log.Printf("turn job %s processing failed (redelivered=%t): %v", job.ID, redelivered, err)
		current, getErr := h.store.GetForProcessing(parent, job.ID)
		if getErr == nil && (current.Status == turnjobs.StatusFailed || current.Status == turnjobs.StatusCancelled) {
			return Acknowledge, err
		}
		return RetryDelivery, err
	}
	return Acknowledge, nil
}
