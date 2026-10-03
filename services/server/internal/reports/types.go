package reports

import (
	"errors"
	"strings"
	"time"
)

var ErrNotFound = errors.New("world or report not found")
var ErrForbidden = errors.New("report not permitted")
var ErrInvalidReason = errors.New("invalid report reason")
var ErrDuplicateReport = errors.New("an open report already exists for this world")
var ErrInvalidNote = errors.New("report note too long")

const maxNoteLength = 300

var validReasons = map[string]bool{
	"违法违规": true,
	"色情低俗": true,
	"暴力仇恨": true,
	"侵权冒用": true,
	"其他":   true,
}

func IsValidReason(reason string) bool { return validReasons[reason] }

type Report struct {
	ID         string    `json:"id"`
	WorldID    string    `json:"worldId"`
	ReporterID string    `json:"reporterId"`
	Reason     string    `json:"reason"`
	Note       string    `json:"note,omitempty"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type CreateInput struct {
	WorldID    string
	ReporterID string
	Reason     string
	Note       string
}

func (in CreateInput) Normalized() (CreateInput, error) {
	in.WorldID = strings.TrimSpace(in.WorldID)
	in.ReporterID = strings.TrimSpace(in.ReporterID)
	in.Reason = strings.TrimSpace(in.Reason)
	in.Note = strings.TrimSpace(in.Note)
	if in.WorldID == "" || in.ReporterID == "" {
		return CreateInput{}, ErrForbidden
	}
	if !IsValidReason(in.Reason) {
		return CreateInput{}, ErrInvalidReason
	}
	if len(in.Note) > maxNoteLength {
		return CreateInput{}, ErrInvalidNote
	}
	return in, nil
}
