package turnjobs

import (
	"context"
	"testing"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

func TestValidationFailureCodeKeepsTheFirstIssue(t *testing.T) {
	err := storyengine.ValidationError{Issues: []storyengine.ValidationIssue{{Code: "unknown_character"}}}
	if got := validationFailureCode("plan_rejected", err); got != "plan_rejected_unknown_character" {
		t.Fatalf("validationFailureCode = %q", got)
	}
	if got := validationFailureCode("plan_rejected", context.Canceled); got != "plan_rejected" {
		t.Fatalf("generic validationFailureCode = %q", got)
	}
}
