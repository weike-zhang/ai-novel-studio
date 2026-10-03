package worlds

import (
	"context"
	"errors"
)

type ModerationAction string

const (
	ModerationAllow          ModerationAction = "allow"
	ModerationAllowWithLabel ModerationAction = "allow_with_label"
	ModerationRevise         ModerationAction = "revise"
	ModerationManualReview   ModerationAction = "manual_review"
	ModerationBlock          ModerationAction = "block"
)

var (
	ErrModerationUnavailable = errors.New("publication moderation unavailable")
	ErrModerationRevise      = errors.New("publication requires revision")
	ErrModerationBlocked     = errors.New("publication blocked")
)

type PublicationContent struct {
	WorldID string
	OwnerID string
	Version int64
	Title   string
	Summary string
	Setting []byte
}

type ModerationDecision struct {
	Action      ModerationAction
	Category    string
	ReasonCode  string
	RuleVersion string
	Source      string
}

type PublicationModerator interface {
	ReviewPublication(context.Context, PublicationContent) (ModerationDecision, error)
}

type UnavailablePublicationModerator struct{}

func (UnavailablePublicationModerator) ReviewPublication(context.Context, PublicationContent) (ModerationDecision, error) {
	return ModerationDecision{}, ErrModerationUnavailable
}

// SyntheticAllowPublicationModerator exists only for local development and
// deterministic contract tests. Production startup must never select it.
type SyntheticAllowPublicationModerator struct{}

func (SyntheticAllowPublicationModerator) ReviewPublication(context.Context, PublicationContent) (ModerationDecision, error) {
	return ModerationDecision{Action: ModerationAllow, Category: "synthetic", ReasonCode: "development_allow", RuleVersion: "dev-v1", Source: "synthetic"}, nil
}

func validateModerationDecision(decision ModerationDecision) error {
	switch decision.Action {
	case ModerationAllow, ModerationAllowWithLabel, ModerationRevise, ModerationManualReview, ModerationBlock:
	default:
		return ErrModerationUnavailable
	}
	if decision.Category == "" || decision.ReasonCode == "" || decision.RuleVersion == "" || decision.Source == "" {
		return ErrModerationUnavailable
	}
	return nil
}
