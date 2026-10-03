package worlddrafts

import "testing"

func TestAssistFieldGuidesCoverCommonFields(t *testing.T) {
	for _, field := range []string{"title", "summary", "opening", "eraPlace", "rules", "playerBackground"} {
		guide := fieldGuide(field)
		if guide == "" {
			t.Fatalf("fieldGuide(%q) is empty", field)
		}
	}
	// Unknown fields fall back to a generic guide instead of panicking.
	if guide := fieldGuide("conflict"); guide == "" {
		t.Fatal("fieldGuide for unknown field should have a fallback")
	}
}

func TestAssistResultHoldsText(t *testing.T) {
	result := AssistResult{Text: "一座被潮汐改变的海港。"}
	if result.Text == "" {
		t.Fatal("assist result should carry text")
	}
}
