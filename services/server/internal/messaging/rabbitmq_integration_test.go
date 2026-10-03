package messaging

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestRabbitMQPublishAndConsume(t *testing.T) {
	url := os.Getenv("RABBITMQ_TEST_URL")
	if url == "" {
		t.Skip("RABBITMQ_TEST_URL is not set")
	}

	broker, err := OpenBroker(url, "integration-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	publisher, err := broker.NewPublisher()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = publisher.Close() })

	messageID := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	payload := []byte(fmt.Sprintf(`{"jobId":%q}`, messageID))
	if err := publisher.Publish(context.Background(), Event{
		ID: messageID, EventType: "turn_job.queued", AggregateID: messageID,
		Payload: payload, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	received := make(chan []byte, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- broker.ConsumeTurnJobs(ctx, "integration-consumer", func(_ context.Context, body []byte, _ bool) (Disposition, error) {
			received <- append([]byte(nil), body...)
			return Acknowledge, nil
		})
	}()

	select {
	case body := <-received:
		if string(body) != string(payload) {
			t.Fatalf("payload=%s", body)
		}
		cancel()
	case err := <-errCh:
		t.Fatalf("consume before delivery: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
