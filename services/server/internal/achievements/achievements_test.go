package achievements

import (
	"encoding/json"
	"testing"
)

func TestGlobalDefinitionsAreReasonable(t *testing.T) {
	seen := make(map[string]bool, len(Global))
	var total int64
	for _, definition := range Global {
		if definition.Code == "" || definition.Title == "" || definition.Threshold < 1 || definition.Reward < 0 {
			t.Fatalf("invalid definition %#v", definition)
		}
		if seen[definition.Code] {
			t.Fatalf("duplicate definition code %q", definition.Code)
		}
		seen[definition.Code] = true
		total += definition.Reward
	}
	if total > 300 {
		t.Fatalf("global achievement reward budget too high: %d", total)
	}
}

func TestParseWorldContextExtractsCatalog(t *testing.T) {
	raw := []byte(`{
		"world": {"id": "world-1"},
		"setting": {
			"achievements": [
				{"key": "answer-bell", "title": "只有钟声记得", "hint": "没有回答第二声钟", "reward": 6},
				{"key": "", "title": "空键应被过滤", "hint": "", "reward": 3}
			]
		}
	}`)
	worldID, catalog := parseWorldContext(json.RawMessage(raw))
	if worldID != "world-1" || len(catalog) != 1 {
		t.Fatalf("worldID=%q catalog=%#v", worldID, catalog)
	}
	if catalog[0].Key != "answer-bell" || catalog[0].Reward != 6 {
		t.Fatalf("catalog entry = %#v", catalog[0])
	}
	if _, found := findCatalogEntry(catalog, "answer-bell"); !found {
		t.Fatal("answer-bell should be found")
	}
	if _, found := findCatalogEntry(catalog, "missing"); found {
		t.Fatal("missing key should not be found")
	}
}

func TestParseWorldContextToleratesGarbage(t *testing.T) {
	if worldID, catalog := parseWorldContext(json.RawMessage(`not json`)); worldID != "" || len(catalog) != 0 {
		t.Fatalf("worldID=%q catalog=%#v", worldID, catalog)
	}
	if worldID, catalog := parseWorldContext(nil); worldID != "" || len(catalog) != 0 {
		t.Fatalf("worldID=%q catalog=%#v", worldID, catalog)
	}
}
