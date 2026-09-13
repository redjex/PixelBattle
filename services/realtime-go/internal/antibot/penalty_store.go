package antibot

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

const penaltyRedisKey = "pixelbattle:captcha:penalty_strikes"

type PenaltyStore struct {
	client *redis.Client
}

func NewPenaltyStore(ctx context.Context, redisURL string) (*PenaltyStore, error) {
	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse captcha penalty redis URL: %w", err)
	}
	store := &PenaltyStore{client: redis.NewClient(options)}
	if err := store.client.Ping(ctx).Err(); err != nil {
		_ = store.client.Close()
		return nil, fmt.Errorf("connect captcha penalty store: %w", err)
	}
	return store, nil
}

func (s *PenaltyStore) Load(ctx context.Context) (map[string]int, error) {
	values, err := s.client.HGetAll(ctx, penaltyRedisKey).Result()
	if err != nil {
		return nil, err
	}
	penalties := make(map[string]int, len(values))
	for identity, raw := range values {
		strikes, err := strconv.Atoi(raw)
		if err == nil && identity != "" && strikes > 0 {
			penalties[identity] = strikes
		}
	}
	return penalties, nil
}

func (s *PenaltyStore) Increment(ctx context.Context, identity string) (int, error) {
	strikes, err := s.client.HIncrBy(ctx, penaltyRedisKey, identity, 1).Result()
	return int(strikes), err
}

func (s *PenaltyStore) Close() error {
	return s.client.Close()
}
