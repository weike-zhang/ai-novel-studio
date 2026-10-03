package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/weike-zhang/ai-novel-studio/services/server/internal/achievements"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/messaging"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/openaiutil"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/turnjobs"
	"github.com/weike-zhang/ai-novel-studio/services/server/internal/worlddrafts"
)

func modelEndpoint(suffix string) *openaiutil.Endpoint {
	baseURL := strings.TrimSpace(os.Getenv("TEST_MODEL_BASE_URL" + suffix))
	apiKey := strings.TrimSpace(os.Getenv("TEST_MODEL_API_KEY" + suffix))
	model := strings.TrimSpace(os.Getenv("TEST_MODEL_NAME" + suffix))
	if baseURL == "" || apiKey == "" {
		return nil
	}
	if model == "" {
		model = "gpt-5-6-instant"
	}
	return &openaiutil.Endpoint{BaseURL: baseURL, APIKey: apiKey, Model: model}
}

func main() {
	provider := os.Getenv("MODEL_PROVIDER")
	if provider == "" {
		provider = "fake"
	}
	worldDraftProvider, err := worldDraftProviderFor(provider)
	if err != nil {
		panic(err)
	}
	turnProvider, err := turnProviderFor(provider)
	if err != nil {
		panic(err)
	}
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		panic("MYSQL_DSN is required")
	}
	db, err := turnjobs.Open(dsn)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	store := turnjobs.NewStore(db)
	achievementStore := achievements.NewStore(db)
	processor := turnjobs.NewProcessor(store, turnProvider, achievementStore)
	worldDraftStore := worlddrafts.NewStore(db)
	worldDraftProcessor := worlddrafts.NewProcessor(worldDraftStore, worldDraftProvider)
	workerID := workerIdentity()
	if jobID := os.Getenv("PROCESS_JOB_ID"); jobID != "" {
		if _, err := processor.Process(context.Background(), jobID, workerID); err != nil {
			panic(err)
		}
		return
	}
	if draftJobID := os.Getenv("PROCESS_WORLD_DRAFT_ID"); draftJobID != "" {
		if _, err := worldDraftProcessor.Process(context.Background(), draftJobID, workerID); err != nil {
			panic(err)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rabbitURL := os.Getenv("RABBITMQ_URL")
	if rabbitURL == "" {
		if os.Getenv("APP_ENV") != "development" {
			panic("RABBITMQ_URL is required outside development")
		}
		log.Println("AI 小说创作台开发 Worker 使用 MySQL durable queue 轮询；设置 RABBITMQ_URL 可验证正式消息链路")
		runDevelopmentPoller(ctx, store, processor, worldDraftStore, worldDraftProcessor, workerID)
		return
	}
	broker, err := messaging.OpenBroker(rabbitURL, workerID)
	if err != nil {
		panic(err)
	}
	defer broker.Close()
	publisher, err := broker.NewPublisher()
	if err != nil {
		panic(err)
	}
	defer publisher.Close()
	pump := messaging.NewPump(messaging.NewSQLForwarder(db), publisher, workerID, 25)
	go runOutboxPump(ctx, pump)
	handler := messaging.NewTurnJobHandler(store, processor, workerID)
	log.Printf("AI 小说创作台 Worker 已连接 RabbitMQ，consumer=%s", workerID)
	if err := broker.ConsumeTurnJobs(ctx, workerID, handler.Handle); err != nil && ctx.Err() == nil {
		panic(err)
	}
}

func worldDraftProviderFor(provider string) (worlddrafts.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "fake":
		return worlddrafts.FakeProvider{}, nil
	case "test_openai_compatible", "openai_compatible":
		return worlddrafts.NewOpenAICompatibleProviderWithClient(
			"test-openai-compatible",
			testModelName(),
			openaiutil.NewFailoverClient(modelEndpoint(""), modelEndpoint("FALLBACK"), 90*time.Second),
		)
	default:
		return nil, fmt.Errorf("unsupported MODEL_PROVIDER=%q", provider)
	}
}

func turnProviderFor(provider string) (turnjobs.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "fake":
		return turnjobs.FakeProvider{}, nil
	case "test_openai_compatible", "openai_compatible":
		return turnjobs.NewOpenAICompatibleProviderWithClient(
			"test-openai-compatible",
			testModelName(),
			openaiutil.NewFailoverClient(modelEndpoint(""), modelEndpoint("FALLBACK"), 120*time.Second),
		)
	default:
		return nil, fmt.Errorf("unsupported MODEL_PROVIDER=%q", provider)
	}
}

func testModelName() string {
	if name := strings.TrimSpace(os.Getenv("TEST_MODEL_NAME")); name != "" {
		return name
	}
	return "gpt-5-6-instant"
}

func runOutboxPump(ctx context.Context, pump *messaging.Pump) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_, err := pump.RunOnce(publishCtx)
			cancel()
			if err != nil {
				log.Printf("publish outbox: %v", err)
			}
		}
	}
}

func runDevelopmentPoller(ctx context.Context, store *turnjobs.Store, processor *turnjobs.Processor, worldDraftStore *worlddrafts.Store, worldDraftProcessor *worlddrafts.Processor, workerID string) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			listCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			ids, err := store.ListQueuedIDs(listCtx, 10)
			cancel()
			if err != nil {
				log.Printf("list queued jobs: %v", err)
			}
			for _, id := range ids {
				processCtx, cancel := context.WithTimeout(ctx, 125*time.Second)
				_, err := processor.Process(processCtx, id, workerID)
				cancel()
				if err != nil {
					log.Printf("process job %s: %v", id, err)
				}
			}
			draftListCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			draftIDs, err := worldDraftStore.ListProcessableIDs(draftListCtx, 10)
			cancel()
			if err != nil {
				log.Printf("list queued world drafts: %v", err)
			}
			for _, id := range draftIDs {
				processCtx, cancel := context.WithTimeout(ctx, 125*time.Second)
				_, err := worldDraftProcessor.Process(processCtx, id, workerID)
				cancel()
				if err != nil {
					log.Printf("process world draft %s: %v", id, err)
				}
			}
		}
	}
}

func workerIdentity() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-host"
	}
	return fmt.Sprintf("%s-%d", hostname, os.Getpid())
}
