package queue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"pixelbattle/realtime/internal/domain"
)

type EventQueue interface {
	Append(context.Context, domain.PixelEvent) error
}

type RedisQueue struct {
	client *redis.Client
	stream string
	group  string
	name   string
}

func NewRedis(rawURL string) (*RedisQueue, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	name := os.Getenv("REDIS_CONSUMER_NAME")
	if name == "" {
		name = "writer-1"
	}
	return &RedisQueue{client: redis.NewClient(options), stream: "pixel-events", group: "postgres-writers", name: name}, nil
}

func (q *RedisQueue) Ready(ctx context.Context) error {
	if err := q.client.Ping(ctx).Err(); err != nil {
		return err
	}
	err := q.client.XGroupCreateMkStream(ctx, q.stream, q.group, "0").Err()
	if err != nil && !strings.HasPrefix(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

func (q *RedisQueue) Append(ctx context.Context, event domain.PixelEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return q.client.XAdd(ctx, &redis.XAddArgs{Stream: q.stream, MaxLen: 1000000, Approx: true, Values: map[string]any{"payload": payload}}).Err()
}

func (q *RedisQueue) Consume(ctx context.Context, write func(context.Context, []domain.PixelEvent) error) {
	for ctx.Err() == nil {
		events, ids, err := q.read(ctx, 500, 200*time.Millisecond)
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			time.Sleep(time.Second)
			continue
		}
		deadline := time.Now().Add(200 * time.Millisecond)
		for len(events) < 500 && time.Now().Before(deadline) {
			moreEvents, moreIDs, readErr := q.read(ctx, int64(500-len(events)), time.Until(deadline))
			if errors.Is(readErr, redis.Nil) {
				break
			}
			if readErr != nil {
				err = readErr
				break
			}
			events = append(events, moreEvents...)
			ids = append(ids, moreIDs...)
		}
		if len(events) == 0 {
			continue
		}
		if err := write(ctx, events); err != nil {
			time.Sleep(time.Second)
			continue
		}
		_ = q.client.XAck(ctx, q.stream, q.group, ids...).Err()
	}
}

func (q *RedisQueue) read(ctx context.Context, count int64, block time.Duration) ([]domain.PixelEvent, []string, error) {
	streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group: q.group, Consumer: q.name, Streams: []string{q.stream, ">"}, Count: count, Block: block,
	}).Result()
	if err != nil {
		return nil, nil, err
	}
	events := make([]domain.PixelEvent, 0, count)
	ids := make([]string, 0, count)
	for _, stream := range streams {
		for _, message := range stream.Messages {
			raw, ok := message.Values["payload"].(string)
			if !ok {
				continue
			}
			var event domain.PixelEvent
			if json.Unmarshal([]byte(raw), &event) != nil {
				continue
			}
			events = append(events, event)
			ids = append(ids, message.ID)
		}
	}
	return events, ids, nil
}

type MemoryQueue struct{ Events chan domain.PixelEvent }

func NewMemory() *MemoryQueue { return &MemoryQueue{Events: make(chan domain.PixelEvent, 100000)} }
func (q *MemoryQueue) Append(ctx context.Context, event domain.PixelEvent) error {
	select {
	case q.Events <- event:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
