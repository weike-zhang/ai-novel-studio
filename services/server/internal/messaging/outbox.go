package messaging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Event struct {
	ID          string
	AggregateID string
	EventType   string
	Payload     []byte
	Attempts    int
	CreatedAt   time.Time
}

type Outbox interface {
	Claim(context.Context, string, int) ([]Event, error)
	MarkPublished(context.Context, string, string) error
	MarkFailed(context.Context, string, string, string) error
}

type EventPublisher interface {
	Publish(context.Context, Event) error
}

type SQLForwarder struct{ db *sql.DB }

func NewSQLForwarder(db *sql.DB) *SQLForwarder { return &SQLForwarder{db: db} }

func (s *SQLForwarder) Claim(ctx context.Context, owner string, limit int) ([]Event, error) {
	if owner == "" {
		return nil, fmt.Errorf("claim owner is required")
	}
	if limit < 1 || limit > 100 {
		limit = 25
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT BIN_TO_UUID(id,1),BIN_TO_UUID(aggregate_id,1),event_type,payload_json,publish_attempts,created_at FROM outbox_events WHERE published_at IS NULL AND (publish_claimed_at IS NULL OR publish_claimed_at<DATE_SUB(CURRENT_TIMESTAMP(6),INTERVAL 5 MINUTE)) ORDER BY created_at LIMIT ? FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, fmt.Errorf("claim outbox rows: %w", err)
	}
	events := make([]Event, 0, limit)
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.AggregateID, &event.EventType, &event.Payload, &event.Attempts, &event.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		events = append(events, event)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, event := range events {
		_, err = tx.ExecContext(ctx, `UPDATE outbox_events SET publish_claimed_by=?,publish_claimed_at=CURRENT_TIMESTAMP(6),publish_attempts=publish_attempts+1,last_error=NULL WHERE id=UUID_TO_BIN(?,1) AND published_at IS NULL`, owner, event.ID)
		if err != nil {
			return nil, fmt.Errorf("mark outbox claim: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return events, nil
}

func (s *SQLForwarder) MarkPublished(ctx context.Context, owner, eventID string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE outbox_events SET published_at=CURRENT_TIMESTAMP(6),publish_claimed_by=NULL,publish_claimed_at=NULL,last_error=NULL WHERE id=UUID_TO_BIN(?,1) AND published_at IS NULL AND publish_claimed_by=?`, eventID, owner)
	if err != nil {
		return fmt.Errorf("mark outbox published: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return fmt.Errorf("outbox claim lost")
	}
	return nil
}

func (s *SQLForwarder) MarkFailed(ctx context.Context, owner, eventID, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	result, err := s.db.ExecContext(ctx, `UPDATE outbox_events SET publish_claimed_by=NULL,publish_claimed_at=NULL,last_error=? WHERE id=UUID_TO_BIN(?,1) AND published_at IS NULL AND publish_claimed_by=?`, message, eventID, owner)
	if err != nil {
		return fmt.Errorf("release outbox claim: %w", err)
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return fmt.Errorf("outbox claim lost")
	}
	return nil
}

type Pump struct {
	outbox    Outbox
	publisher EventPublisher
	owner     string
	batchSize int
}

func NewPump(outbox Outbox, publisher EventPublisher, owner string, batchSize int) *Pump {
	return &Pump{outbox: outbox, publisher: publisher, owner: owner, batchSize: batchSize}
}

func (p *Pump) RunOnce(ctx context.Context) (int, error) {
	events, err := p.outbox.Claim(ctx, p.owner, p.batchSize)
	if err != nil {
		return 0, err
	}
	published := 0
	for _, event := range events {
		if err := p.publisher.Publish(ctx, event); err != nil {
			markErr := p.outbox.MarkFailed(ctx, p.owner, event.ID, err.Error())
			return published, errors.Join(err, markErr)
		}
		if err := p.outbox.MarkPublished(ctx, p.owner, event.ID); err != nil {
			return published, err
		}
		published++
	}
	return published, nil
}
