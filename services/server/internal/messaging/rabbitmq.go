package messaging

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	EventExchange = "novelstudio.events"
	DeadExchange  = "novelstudio.dlx"
	TurnQueue     = "novelstudio.turn_jobs"
	TurnDeadQueue = "novelstudio.turn_jobs.dlq"
)

type Broker struct{ connection *amqp.Connection }

func OpenBroker(url, connectionName string) (*Broker, error) {
	if url == "" {
		return nil, fmt.Errorf("RABBITMQ_URL is required")
	}
	connection, err := amqp.DialConfig(url, amqp.Config{Properties: amqp.Table{"connection_name": connectionName}, Heartbeat: 10 * time.Second, Locale: "en_US"})
	if err != nil {
		return nil, fmt.Errorf("connect rabbitmq: %w", err)
	}
	broker := &Broker{connection: connection}
	if err := broker.declareTopology(); err != nil {
		connection.Close()
		return nil, err
	}
	return broker, nil
}

func (b *Broker) Close() error { return b.connection.Close() }

func (b *Broker) declareTopology() error {
	channel, err := b.connection.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()
	if err := channel.ExchangeDeclare(EventExchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare event exchange: %w", err)
	}
	if err := channel.ExchangeDeclare(DeadExchange, "topic", true, false, false, false, nil); err != nil {
		return fmt.Errorf("declare dead exchange: %w", err)
	}
	queue, err := channel.QueueDeclare(TurnQueue, true, false, false, false, amqp.Table{"x-dead-letter-exchange": DeadExchange, "x-dead-letter-routing-key": "turn_job.dead"})
	if err != nil {
		return fmt.Errorf("declare turn queue: %w", err)
	}
	if err := channel.QueueBind(queue.Name, "turn_job.queued", EventExchange, false, nil); err != nil {
		return fmt.Errorf("bind turn queue: %w", err)
	}
	dead, err := channel.QueueDeclare(TurnDeadQueue, true, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("declare turn dead queue: %w", err)
	}
	if err := channel.QueueBind(dead.Name, "turn_job.dead", DeadExchange, false, nil); err != nil {
		return fmt.Errorf("bind turn dead queue: %w", err)
	}
	return nil
}

type RabbitPublisher struct {
	channel       *amqp.Channel
	confirmations <-chan amqp.Confirmation
	returns       <-chan amqp.Return
	mu            sync.Mutex
}

func (b *Broker) NewPublisher() (*RabbitPublisher, error) {
	channel, err := b.connection.Channel()
	if err != nil {
		return nil, err
	}
	if err := channel.Confirm(false); err != nil {
		channel.Close()
		return nil, fmt.Errorf("enable publisher confirms: %w", err)
	}
	return &RabbitPublisher{channel: channel, confirmations: channel.NotifyPublish(make(chan amqp.Confirmation, 1)), returns: channel.NotifyReturn(make(chan amqp.Return, 1))}, nil
}

func (p *RabbitPublisher) Close() error { return p.channel.Close() }

func (p *RabbitPublisher) Publish(ctx context.Context, event Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// Only queued turn jobs have a consumer. Terminal and informational events
	// are still durable outbox records, but must not fail the pump when no queue
	// is bound to their routing key.
	mandatory := event.EventType == "turn_job.queued"
	err := p.channel.PublishWithContext(ctx, EventExchange, event.EventType, mandatory, false, amqp.Publishing{ContentType: "application/json", DeliveryMode: amqp.Persistent, MessageId: event.ID, Timestamp: event.CreatedAt, Body: event.Payload, Headers: amqp.Table{"aggregate_id": event.AggregateID, "attempt": event.Attempts + 1}})
	if err != nil {
		return fmt.Errorf("publish %s: %w", event.ID, err)
	}
	for {
		select {
		case returned, ok := <-p.returns:
			if !ok {
				return errors.New("rabbitmq return channel closed")
			}
			return fmt.Errorf("message %s unroutable: %s", event.ID, returned.ReplyText)
		case confirmation, ok := <-p.confirmations:
			if !ok {
				return errors.New("rabbitmq confirmation channel closed")
			}
			if !confirmation.Ack {
				return fmt.Errorf("message %s was not confirmed", event.ID)
			}
			select {
			case returned := <-p.returns:
				return fmt.Errorf("message %s unroutable: %s", event.ID, returned.ReplyText)
			default:
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

type Disposition int

const (
	Acknowledge Disposition = iota
	RetryDelivery
	RejectDelivery
)

type DeliveryHandler func(context.Context, []byte, bool) (Disposition, error)

func (b *Broker) ConsumeTurnJobs(ctx context.Context, consumerName string, handler DeliveryHandler) error {
	channel, err := b.connection.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()
	if err := channel.Qos(1, 0, false); err != nil {
		return fmt.Errorf("set consumer qos: %w", err)
	}
	deliveries, err := channel.ConsumeWithContext(ctx, TurnQueue, consumerName, false, false, false, false, nil)
	if err != nil {
		return fmt.Errorf("consume turn jobs: %w", err)
	}
	for delivery := range deliveries {
		disposition, handleErr := handler(ctx, delivery.Body, delivery.Redelivered)
		switch disposition {
		case Acknowledge:
			err = delivery.Ack(false)
		case RetryDelivery:
			err = delivery.Nack(false, true)
		case RejectDelivery:
			err = delivery.Reject(false)
		default:
			err = delivery.Reject(false)
		}
		if err != nil {
			return fmt.Errorf("settle delivery: %w", err)
		}
		_ = handleErr
	}
	return ctx.Err()
}
