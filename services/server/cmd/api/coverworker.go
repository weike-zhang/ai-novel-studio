package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/media"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlddrafts"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlds"
)

// coverWorker generates and replaces world covers asynchronously inside the API
// process. Cover generation is slow (an image model round trip can take a
// minute or more), so every trigger — a moderator single world, a batch for all
// coverless worlds, or auto-generation after publish — is queued and processed
// in the background with bounded concurrency. Requests never block on the model.
type coverWorker struct {
	store     *worlds.Store
	generator worlddrafts.CoverGenerator
	media     *media.Client

	sem      chan struct{}
	inflight sync.Map // worldID -> struct{}
}

func newCoverWorker(store *worlds.Store, generator worlddrafts.CoverGenerator, mediaClient *media.Client) *coverWorker {
	return &coverWorker{
		store:     store,
		generator: generator,
		media:     mediaClient,
		sem:       make(chan struct{}, 2),
	}
}

// Generate queues a cover generation for one world and returns immediately.
func (w *coverWorker) Generate(worldID string) {
	if worldID == "" {
		return
	}
	if _, loaded := w.inflight.LoadOrStore(worldID, struct{}{}); loaded {
		return
	}
	go func() {
		defer w.inflight.Delete(worldID)
		w.sem <- struct{}{}
		defer func() { <-w.sem }()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := w.run(ctx, worldID); err != nil {
			log.Printf("cover generate world %s: %v", worldID, err)
		}
	}()
}

// GenerateMissing queues a cover for every published public world that has none
// and returns the number queued.
func (w *coverWorker) GenerateMissing(ctx context.Context) (int, error) {
	ids, err := w.store.ListCoverlessWorlds(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		w.Generate(id)
	}
	return len(ids), nil
}

func (w *coverWorker) run(ctx context.Context, worldID string) error {
	world, err := w.store.Get(ctx, worldID, "")
	if err != nil {
		return err
	}
	var setting map[string]any
	if err := json.Unmarshal(world.Setting, &setting); err != nil {
		return err
	}
	genre := ""
	if value, ok := setting["type"].(string); ok {
		genre = value
	}
	data, err := w.generator.Generate(ctx, world.Title, world.Summary, genre)
	if err != nil {
		return err
	}
	if w.media == nil {
		return nil
	}
	if err := w.media.EnsureBucket(ctx); err != nil {
		return err
	}
	url, err := worlddrafts.SaveCover(ctx, w.media, world.ID, data)
	if err != nil {
		return err
	}
	return w.store.SetCoverByModerator(ctx, world.ID, url)
}
