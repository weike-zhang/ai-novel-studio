package worlds

import (
	"context"
	"errors"
	"testing"
)

func TestUnavailablePublicationModeratorFailsClosed(t *testing.T) {
	_, err := (UnavailablePublicationModerator{}).ReviewPublication(context.Background(), PublicationContent{})
	if !errors.Is(err, ErrModerationUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func TestModerationDecisionRequiresCompleteAuditableResult(t *testing.T) {
	if err := validateModerationDecision(ModerationDecision{Action: ModerationAllow}); !errors.Is(err, ErrModerationUnavailable) {
		t.Fatalf("incomplete decision error = %v", err)
	}
	if err := validateModerationDecision(ModerationDecision{Action: ModerationManualReview, Category: "high_risk", ReasonCode: "review_required", RuleVersion: "test-v1", Source: "fake"}); err != nil {
		t.Fatal(err)
	}
}
