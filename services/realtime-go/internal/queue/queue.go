package queue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

func (q *RedisQueue) Close() { _ = q.client.Close() }

func (q *RedisQueue) Append(ctx context.Context, event domain.PixelEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	// Never trim unacknowledged events. Successful writes delete their entries.
	return q.client.XAdd(ctx, &redis.XAddArgs{Stream: q.stream, Values: map[string]any{"payload": payload}}).Err()
}

func (q *RedisQueue) Consume(ctx context.Context, write func(context.Context, []domain.PixelEvent) error) {
	for ctx.Err() == nil {
		if err := q.Recover(ctx, write); err != nil && ctx.Err() == nil {
			log.Printf("queue recovery/write failed: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// Recover drains pending entries (including dead consumers) before new entries.
// The server holds a PostgreSQL singleton lock before calling this: MinIdle=0
// is deliberate, so restart recovery need not wait for abandoned consumers.
func (q *RedisQueue) Recover(ctx context.Context, write func(context.Context, []domain.PixelEvent) error) error {
	for ctx.Err() == nil {
		messages, _, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: q.stream, Group: q.group, Consumer: q.name, MinIdle: 0, Start: "0-0", Count: 500}).Result()
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: q.group, Consumer: q.name, Streams: []string{q.stream, ">"}, Count: 500, Block: -1}).Result()
			if errors.Is(err, redis.Nil) {
				return nil
			}
			if err != nil {
				return err
			}
			for _, stream := range streams {
				messages = append(messages, stream.Messages...)
			}
		}
		if len(messages) == 0 {
			return nil
		}
		events, ids, err := decodeMessages(messages)
		if err != nil {
			return err
		}
		if err := write(ctx, events); err != nil {
			return err
		}
		// Commit first, then atomically acknowledge/delete. Retrying after any
		// ambiguous failure is safe because WriteBatch is idempotent.
		_, err = q.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.XAck(ctx, q.stream, q.group, ids...)
			pipe.XDel(ctx, q.stream, ids...)
			return nil
		})
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}

func decodeMessages(messages []redis.XMessage) ([]domain.PixelEvent, []string, error) {
	events := make([]domain.PixelEvent, 0, len(messages))
	ids := make([]string, 0, len(messages))
	for _, message := range messages {
		raw, ok := message.Values["payload"].(string)
		var event domain.PixelEvent
		if !ok || json.Unmarshal([]byte(raw), &event) != nil || event.EventID == "" || event.OperationID == "" || event.BoardID != "main" || event.Version <= 0 {
			return nil, nil, fmt.Errorf("invalid queue entry %s; operator repair required", message.ID)
		}
		events = append(events, event)
		ids = append(ids, message.ID)
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
