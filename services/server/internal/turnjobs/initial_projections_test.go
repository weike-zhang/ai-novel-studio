package turnjobs

import (
	"encoding/json"
	"testing"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

func TestBuildInitialProjectionsFromWorldSnapshot(t *testing.T) {
	snapshot := json.RawMessage(`{
      "setting": {
        "worldBackground": {"eraPlace": "深空哨站"},
        "playerIdentity": {"goal": "活到救援抵达，并弄清频道里的声音"},
        "characters": [
          {"name": "VEGA", "role": "哨站主脑 AI", "personality": "冷静、精确", "goal": "维持哨站运转", "relation": "最可靠也最可疑的伙伴", "public": true},
          {"name": "林岚", "role": "维修工程师", "personality": "谨慎", "goal": "修复通讯模块", "relation": "旧同事", "public": true}
        ]
      }
    }`)

	characters, threads, err := buildInitialProjections(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(characters) != 2 {
		t.Fatalf("characters = %#v", characters)
	}
	if characters[0].ID != "VEGA" || characters[0].Version != 1 || characters[0].Current["location"] != "深空哨站" {
		t.Fatalf("first character = %#v", characters[0])
	}
	if len(characters[0].Current["goals"].([]string)) != 1 {
		t.Fatalf("first character goals = %#v", characters[0].Current["goals"])
	}
	if len(threads) != 1 || threads[0].Key != "main-objective" || threads[0].Title == "" {
		t.Fatalf("threads = %#v", threads)
	}

	charactersByID := make(map[string]storyengine.CharacterState, len(characters))
	for _, character := range characters {
		charactersByID[character.ID] = character
	}
	_, err = storyengine.AuthorizePlan(storyengine.ContinuityContext{Simulation: storyengine.Simulation{
		ID: "simulation-1", Version: 1, LastSeq: 0, Characters: charactersByID,
		Threads: map[string]storyengine.StoryThread{"main-objective": threads[0]},
	}}, storyengine.TurnProposal{
		InputVersion: 1, ExpectedNextSeq: 1,
		Events:           []storyengine.StoryEvent{{Type: "signal_received", SubjectRef: "VEGA"}},
		CharacterPatches: []storyengine.CharacterPatch{{CharacterID: "VEGA", ExpectedVersion: 1, Current: map[string]any{"mood": "警戒"}}},
	})
	if err != nil {
		t.Fatalf("catalog character plan rejected: %v", err)
	}
}

func TestBuildInitialProjectionsSkipsWorldsWithoutAuthoredCharacters(t *testing.T) {
	characters, threads, err := buildInitialProjections(json.RawMessage(`{"setting":{"type":"悬疑"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(characters) != 0 || len(threads) != 0 {
		t.Fatalf("empty projections = %#v / %#v", characters, threads)
	}
}
