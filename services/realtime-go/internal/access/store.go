package access

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	bypassKey        = "pixelbattle:cooldown:bypass"
	cooldownKey      = "pixelbattle:cooldown:seconds"
	globalCooldownKey = "pixelbattle:cooldown:global_seconds"
	gamePausedKey      = "pixelbattle:game:paused"
	peakOnlineKey    = "pixelbattle:online:peak"
	usernameKey      = "pixelbattle:users:username"
	userIDKey        = "pixelbattle:users:id"
)

type Store struct {
	client   *redis.Client
	mu       sync.RWMutex
	bypassed map[int64]struct{}
	cooldowns map[int64]time.Duration
	globalCooldown time.Duration
	hasGlobalCooldown bool
	paused bool
	peakOnline int64
}

func New(ctx context.Context, rawURL string, builtinIDs []int64) *Store {
	store := &Store{bypassed: make(map[int64]struct{}, len(builtinIDs)), cooldowns: make(map[int64]time.Duration)}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		log.Printf("access redis disabled: %v", err)
		return store
	}
	client := redis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		log.Printf("access redis disabled: %v", err)
		_ = client.Close()
		return store
	}
	store.client = client
	store.refresh(ctx)
	return store
}

func (s *Store) Close() {
	if s.client != nil {
		_ = s.client.Close()
	}
}

func (s *Store) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.refresh(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (s *Store) RegisterUser(ctx context.Context, id int64, username string) {
	if s.client == nil || username == "" {
		return
	}
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	if normalized == "" {
		return
	}
	pipe := s.client.Pipeline()
	pipe.HSet(ctx, usernameKey, normalized, id)
	pipe.HSet(ctx, userIDKey, strconv.FormatInt(id, 10), normalized)
	_, _ = pipe.Exec(ctx)
}

func (s *Store) IsCooldownBypassed(id int64) bool {
	s.mu.RLock()
	_, ok := s.bypassed[id]
	s.mu.RUnlock()
	return ok
}

func (s *Store) CooldownFor(id int64, fallback time.Duration) time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.bypassed[id]; ok {
		return 0
	}
	if duration, ok := s.cooldowns[id]; ok {
		return duration
	}
	if s.hasGlobalCooldown {
		return s.globalCooldown
	}
	return fallback
}

func (s *Store) RecordOnlinePeak(ctx context.Context, current int64) int64 {
	if s.client != nil {
		const updatePeak = `local old=tonumber(redis.call('GET',KEYS[1]) or '0'); local value=tonumber(ARGV[1]); if value>old then redis.call('SET',KEYS[1],value); return value end; return old`
		if peak, err := s.client.Eval(ctx, updatePeak, []string{peakOnlineKey}, current).Int64(); err == nil {
			s.mu.Lock()
			s.peakOnline = peak
			s.mu.Unlock()
			return peak
		}
	}
	s.mu.Lock()
	if current > s.peakOnline {
		s.peakOnline = current
	}
	peak := s.peakOnline
	s.mu.Unlock()
	return peak
}

func (s *Store) PeakOnline() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.peakOnline
}

func (s *Store) IsPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.paused
}

func (s *Store) SetPaused(ctx context.Context, paused bool) error {
	if s.client != nil {
		var err error
		if paused {
			err = s.client.Set(ctx, gamePausedKey, "1", 0).Err()
		} else {
			err = s.client.Del(ctx, gamePausedKey).Err()
		}
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	s.paused = paused
	s.mu.Unlock()
	return nil
}

func (s *Store) refresh(ctx context.Context) {
	next := make(map[int64]struct{})
	nextCooldowns := make(map[int64]time.Duration)
	var globalCooldown time.Duration
	var hasGlobalCooldown bool
	var paused bool
	var peak int64
	if s.client != nil {
		values, err := s.client.SMembers(ctx, bypassKey).Result()
		if err != nil {
			return
		}
		for _, value := range values {
			if id, err := strconv.ParseInt(value, 10, 64); err == nil {
				next[id] = struct{}{}
			}
		}
		custom, err := s.client.HGetAll(ctx, cooldownKey).Result()
		if err != nil {
			return
		}
		for rawID, rawSeconds := range custom {
			id, idErr := strconv.ParseInt(rawID, 10, 64)
			seconds, secondsErr := strconv.Atoi(rawSeconds)
			if idErr == nil && secondsErr == nil && seconds >= 0 {
				nextCooldowns[id] = time.Duration(seconds) * time.Second
			}
		}
		if rawGlobal, err := s.client.Get(ctx, globalCooldownKey).Result(); err == nil {
			seconds, parseErr := strconv.Atoi(rawGlobal)
			if parseErr == nil && seconds >= 0 {
				globalCooldown = time.Duration(seconds) * time.Second
				hasGlobalCooldown = true
			}
		} else if err != redis.Nil {
			return
		}
		pausedValue, err := s.client.Get(ctx, gamePausedKey).Result()
		if err == nil {
			paused = pausedValue == "1" || strings.EqualFold(pausedValue, "true")
		} else if err != redis.Nil {
			return
		}
		peak, _ = s.client.Get(ctx, peakOnlineKey).Int64()
	}
	s.mu.Lock()
	s.bypassed = next
	s.cooldowns = nextCooldowns
	s.globalCooldown = globalCooldown
	s.hasGlobalCooldown = hasGlobalCooldown
	s.paused = paused
	if peak > s.peakOnline {
		s.peakOnline = peak
	}
	s.mu.Unlock()
}
