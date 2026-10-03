package messaging

import (
	"context"
	"errors"
	"testing"
)

type fakeOutbox struct {
	events    []Event
	published []string
	failed    []string
}

func (f *fakeOutbox) Claim(context.Context, string, int) ([]Event, error) { return f.events, nil }
func (f *fakeOutbox) MarkPublished(_ context.Context, _ string, id string) error {
	f.published = append(f.published, id)
	return nil
}
func (f *fakeOutbox) MarkFailed(_ context.Context, _ string, id, _ string) error {
	f.failed = append(f.failed, id)
	return nil
}

type fakePublisher struct{ failOn string }

func (f fakePublisher) Publish(_ context.Context, event Event) error {
	if event.ID == f.failOn {
		return errors.New("broker unavailable")
	}
	return nil
}

func TestPumpMarksOnlyConfirmedEventsPublished(t *testing.T) {
	outbox := &fakeOutbox{events: []Event{{ID: "one"}, {ID: "two"}, {ID: "three"}}}
	pump := NewPump(outbox, fakePublisher{failOn: "two"}, "worker-a", 10)
	count, err := pump.RunOnce(context.Background())
	if err == nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
	if len(outbox.published) != 1 || outbox.published[0] != "one" {
		t.Fatalf("published=%v", outbox.published)
	}
	if len(outbox.failed) != 1 || outbox.failed[0] != "two" {
		t.Fatalf("failed=%v", outbox.failed)
	}
}
