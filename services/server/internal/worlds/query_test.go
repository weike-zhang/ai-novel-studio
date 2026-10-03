package worlds

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildPublicWorldQueryKeepsDiscoveryPredicatesAndBindsFilters(t *testing.T) {
	query, args, limit, err := buildPublicWorldQuery(PublicWorldQuery{Keyword: "雾%港", Type: "奇幻", Tags: []string{"探索", "悬疑"}, Sort: SortPopular, Limit: 12})
	if err != nil {
		t.Fatal(err)
	}
	for _, predicate := range []string{"w.visibility='public'", "w.status='published'", "u.nickname LIKE", "JSON_CONTAINS", "discovery.hook", "recent.created_at>=UTC_TIMESTAMP(6)-INTERVAL 30 DAY", "ORDER BY popularity DESC"} {
		if !strings.Contains(query, predicate) {
			t.Fatalf("query missing %q:\n%s", predicate, query)
		}
	}
	if strings.Contains(query, "雾%港") {
		t.Fatal("keyword must not be interpolated into SQL")
	}
	if limit != 12 || args[len(args)-1] != 13 {
		t.Fatalf("limit=%d args=%#v", limit, args)
	}
	if args[0] != "%雾=%港%" {
		t.Fatalf("escaped keyword=%q", args[0])
	}
}

func TestCursorRoundTripAndSortBoundary(t *testing.T) {
	want := pageCursor{Sort: SortPopular, Popularity: 42, UpdatedAt: time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC), ID: "018f1000-0000-7000-8000-000000000001"}
	encoded, err := encodeCursor(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeCursor(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("cursor=%#v want=%#v", got, want)
	}
	if _, _, _, err := buildPublicWorldQuery(PublicWorldQuery{Sort: SortNewest, Cursor: encoded}); err == nil {
		t.Fatal("expected mismatched cursor sort to fail")
	}
}

func TestBuildSnapshotIsSelfContainedAndFreezesVersion(t *testing.T) {
	setting := []byte(`{"genre":["奇幻"],"rules":[{"id":"fixed","mutable":false}]}`)
	snapshot, err := buildSnapshot("world-1", "owner-1", "雾港", "简介", "private", "draft", 7, setting, time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		World struct {
			ID      string `json:"id"`
			OwnerID string `json:"ownerId"`
			Version int64  `json:"version"`
		} `json:"world"`
		Setting map[string]json.RawMessage `json:"setting"`
	}
	if err := json.Unmarshal(snapshot, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.World.ID != "world-1" || decoded.World.OwnerID != "owner-1" || decoded.World.Version != 7 {
		t.Fatalf("snapshot world=%#v", decoded.World)
	}
	if _, ok := decoded.Setting["rules"]; !ok {
		t.Fatalf("snapshot omitted setting: %s", snapshot)
	}
	setting[2] = 'X'
	if !json.Valid(snapshot) {
		t.Fatal("snapshot retained mutable input bytes")
	}
}

func TestBuildPublicWorldQueryRejectsInvalidLimitAndSort(t *testing.T) {
	if _, _, _, err := buildPublicWorldQuery(PublicWorldQuery{Limit: 101}); err == nil {
		t.Fatal("expected invalid limit error")
	}
	if _, _, _, err := buildPublicWorldQuery(PublicWorldQuery{Sort: "random"}); err == nil {
		t.Fatal("expected invalid sort error")
	}
}

func TestUUIDValidation(t *testing.T) {
	for _, valid := range []string{"018f1000-0000-7000-8000-000000000001", "FFFFFFFF-FFFF-4FFF-8FFF-FFFFFFFFFFFF"} {
		if !isUUID(valid) {
			t.Fatalf("isUUID(%q)=false", valid)
		}
	}
	for _, invalid := range []string{"", "world-1", "018f1000-0000-7000-8000-00000000000z"} {
		if isUUID(invalid) {
			t.Fatalf("isUUID(%q)=true", invalid)
		}
	}
}
