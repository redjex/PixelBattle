package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"pixelbattle/realtime/internal/domain"
)

func TestDecodeMessagesFailClosed(t *testing.T) {
	for _, value := range []any{123, `{}`, `not json`} {
		if _, _, err := decodeMessages([]redis.XMessage{{ID: "1-0", Values: map[string]any{"payload": value}}}); err == nil {
			t.Fatal("bad entry accepted")
		}
	}
}

func TestRedisPendingRecovery(t *testing.T) {
	raw := os.Getenv("GO_TEST_REDIS_URL")
	if raw == "" {
		t.Skip("set GO_TEST_REDIS_URL for isolated-stream Redis integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	q, err := NewRedis(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer q.Close()
	q.stream = fmt.Sprintf("security-test:%d", time.Now().UnixNano())
	defer q.client.Del(context.Background(), q.stream)
	if err := q.Ready(ctx); err != nil {
		t.Fatal(err)
	}
	event := domain.PixelEvent{EventID: "event", OperationID: "operation", BoardID: "main", Version: 1}
	if err := q.Append(ctx, event); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("database unavailable")
	if err := q.Recover(ctx, func(context.Context, []domain.PixelEvent) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	pending, err := q.client.XPending(ctx, q.stream, q.group).Result()
	if err != nil || pending.Count != 1 {
		t.Fatal(pending, err)
	}
	q.name = "replacement-consumer"
	calls := 0
	if err := q.Recover(ctx, func(_ context.Context, events []domain.PixelEvent) error {
		calls++
		if len(events) != 1 || events[0].EventID != "event" {
			t.Fatal(events)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	if count := q.client.XLen(ctx, q.stream).Val(); count != 0 {
		t.Fatal("acknowledged entry retained", count)
	}
	payload, _ := json.Marshal(event)
	q.client.XAdd(ctx, &redis.XAddArgs{Stream: q.stream, Values: map[string]any{"payload": string(payload)}})
	q.client.XAdd(ctx, &redis.XAddArgs{Stream: q.stream, Values: map[string]any{"payload": "bad"}})
	if err := q.Recover(ctx, func(context.Context, []domain.PixelEvent) error { t.Fatal("poison batch written"); return nil }); err == nil {
		t.Fatal("malformed message skipped")
	}
}
