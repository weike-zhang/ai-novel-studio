package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/media"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/openaiutil"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlddrafts"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlds"
)

func main() {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		log.Fatal("MYSQL_DSN required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	mc, err := media.Open(media.Config{
		Endpoint:  os.Getenv("MINIO_ENDPOINT"),
		PublicURL: os.Getenv("MINIO_PUBLIC_URL"),
		AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("MINIO_SECRET_KEY"),
		Bucket:    os.Getenv("MINIO_BUCKET"),
	})
	if err != nil {
		log.Fatal("media:", err)
	}
	if err := mc.EnsureBucket(context.Background()); err != nil {
		log.Fatal("bucket:", err)
	}
	model := os.Getenv("COVER_MODEL")
	if model == "" {
		model = "gpt-image-2"
	}
	gen, err := worlddrafts.NewOpenAICompatibleCoverGeneratorWithClient(model, openaiutil.NewFailoverClient(
		&openaiutil.Endpoint{BaseURL: os.Getenv("COVER_BASE_URL"), APIKey: os.Getenv("COVER_API_KEY"), Model: model},
		nil, 300*time.Second,
	))
	if err != nil {
		log.Fatal(err)
	}
	store := worlds.NewStore(db)

	ids := []string{
		"0198b8a0-1002-7000-8000-000000000001",
		"0198b8a0-1002-7000-8000-000000000002",
		"0198b8a0-1002-7000-8000-000000000003",
		"0198b8a0-1002-7000-8000-000000000004",
		"0198b8a0-1002-7000-8000-000000000006",
		"0198b8a0-1002-7000-8000-000000000007",
		"0198b8a0-1002-7000-8000-000000000008",
		"0198b8a0-1002-7000-8000-000000000009",
		"0198b8a0-1002-7000-8000-000000000010",
		"0198b8a0-1002-7000-8000-000000000011",
		"0198b8a0-1002-7000-8000-000000000012",
		"0198b8a0-1002-7000-8000-000000000013",
		"0198b8a0-1002-7000-8000-000000000014",
		"0198b8a0-1002-7000-8000-000000000015",
		"0198b8a0-1002-7000-8000-000000000016",
		"0198b8a0-1002-7000-8000-000000000017",
	}

	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		sem <- struct{}{}
		go func(id string) {
			defer wg.Done()
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			world, err := store.Get(ctx, id, "")
			if err != nil {
				log.Printf("FAIL %s get: %v", id, err)
				return
			}
			var setting map[string]any
			if err := json.Unmarshal(world.Setting, &setting); err != nil {
				log.Printf("FAIL %s setting: %v", id, err)
				return
			}
			genre := ""
			if value, ok := setting["type"].(string); ok {
				genre = value
			}
			start := time.Now()
			data, err := gen.Generate(ctx, world.Title, world.Summary, genre)
			if err != nil {
				log.Printf("FAIL %s %q generate: %v", id, world.Title, err)
				return
			}
			url, err := worlddrafts.SaveCover(ctx, mc, id, data)
			if err != nil {
				log.Printf("FAIL %s %q save: %v", id, world.Title, err)
				return
			}
			if err := store.SetCoverByModerator(ctx, id, url); err != nil {
				log.Printf("FAIL %s %q setcover: %v", id, world.Title, err)
				return
			}
			log.Printf("OK %s %q cover=%s (%ds)", id, world.Title, url, int(time.Since(start).Round(time.Second)/time.Second))
		}(id)
	}
	wg.Wait()
	log.Println("ALL DONE")
}
