package turnjobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/storyengine"
)

// buildInitialProjections converts the authored characters and first objective
// in a frozen world snapshot into the normalized state used by the turn engine.
// Catalog worlds may use the compact authoring shape and do not have database
// projections until their first journey is loaded.
func buildInitialProjections(snapshot json.RawMessage) ([]storyengine.CharacterState, []storyengine.StoryThread, error) {
	var document any
	if err := json.Unmarshal(snapshot, &document); err != nil {
		return nil, nil, fmt.Errorf("decode world snapshot for initial projections: %w", err)
	}
	root := projectionObject(document)
	setting := projectionObject(root["setting"])
	background := projectionObject(setting["worldBackground"])
	player := projectionObject(setting["playerIdentity"])
	location := projectionText(background["eraPlace"])
	if location == "" {
		location = "旅程起点"
	}

	characters := make([]storyengine.CharacterState, 0)
	usedIDs := make(map[string]int)
	for _, value := range projectionArray(setting["characters"]) {
		character := projectionObject(value)
		name := projectionText(character["name"])
		if name == "" {
			continue
		}
		characterID := projectionText(character["id"])
		if characterID == "" {
			characterID = name
		}
		if count := usedIDs[characterID]; count > 0 {
			usedIDs[characterID] = count + 1
			characterID += "-" + strconv.Itoa(count+1)
		} else {
			usedIDs[characterID] = 1
		}
		character["id"] = characterID
		character["name"] = name

		goals := []string{}
		if goal := projectionText(character["goal"]); goal != "" {
			goals = append(goals, goal)
		}
		characters = append(characters, storyengine.CharacterState{
			ID:       characterID,
			Name:     name,
			Identity: character,
			Current: map[string]any{
				"alive":     true,
				"location":  location,
				"condition": "安然",
				"mood":      "未明",
				"knowledge": []string{},
				"goals":     goals,
			},
			Version: 1,
		})
	}

	objective := projectionText(player["goal"])
	if objective == "" {
		objective = projectionText(background["conflict"])
	}
	threads := make([]storyengine.StoryThread, 0, 1)
	if objective != "" {
		threads = append(threads, storyengine.StoryThread{Key: "main-objective", Title: objective, Status: "open", Priority: 10})
	}
	return characters, threads, nil
}

func projectionObject(value any) map[string]any {
	if object, ok := value.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

func projectionArray(value any) []any {
	if array, ok := value.([]any); ok {
		return array
	}
	return nil
}

func projectionText(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func (s *Store) ensureInitialProjections(ctx context.Context, simulation *storyengine.Simulation) error {
	characters, threads, err := buildInitialProjections(simulation.WorldSnapshot)
	if err != nil {
		return err
	}
	if len(characters) == 0 && len(threads) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin initial projection: %w", err)
	}
	defer tx.Rollback()
	for _, character := range characters {
		identity, err := json.Marshal(character.Identity)
		if err != nil {
			return fmt.Errorf("encode initial character identity: %w", err)
		}
		current, err := json.Marshal(character.Current)
		if err != nil {
			return fmt.Errorf("encode initial character state: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO character_states (simulation_id,character_id,name,identity_json,current_state_json,version) VALUES (UUID_TO_BIN(?,1),?,?,?,?,?)`, simulation.ID, character.ID, character.Name, identity, current, character.Version); err != nil {
			return fmt.Errorf("insert initial character state: %w", err)
		}
	}
	if len(simulation.Threads) == 0 {
		for _, thread := range threads {
			if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO story_threads (simulation_id,thread_key,title,status,priority) VALUES (UUID_TO_BIN(?,1),?,?,?,?)`, simulation.ID, thread.Key, thread.Title, thread.Status, thread.Priority); err != nil {
				return fmt.Errorf("insert initial story thread: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit initial projection: %w", err)
	}

	if simulation.Characters == nil {
		simulation.Characters = make(map[string]storyengine.CharacterState)
	}
	for _, character := range characters {
		if _, exists := simulation.Characters[character.ID]; !exists {
			simulation.Characters[character.ID] = character
		}
	}
	if len(simulation.Threads) == 0 && len(threads) > 0 {
		if simulation.Threads == nil {
			simulation.Threads = make(map[string]storyengine.StoryThread)
		}
		for _, thread := range threads {
			simulation.Threads[thread.Key] = thread
		}
	}
	return nil
}
