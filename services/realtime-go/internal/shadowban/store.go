package shadowban

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/redis/go-redis/v9"
	"pixelbattle/realtime/internal/domain"
)

const bannedKey = "pixelbattle:shadow:banned"
const boardKeyPrefix = "pixelbattle:shadow:board:"
const clientPrizesKey = "pixelbattle:shadow:client-prizes"

// Store keeps each shadow-banned player's private overlay. The public board is
// never modified by entries in this store.
type Store struct {
	client *redis.Client
	mu     sync.RWMutex
	banned map[string]struct{}
	boards map[string]map[[2]int]domain.BoardPixel
}

func New(ctx context.Context, rawURL string) (*Store, error) {
	s := &Store{banned: make(map[string]struct{}), boards: make(map[string]map[[2]int]domain.BoardPixel)}
	if rawURL == "" {
		return s, nil
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, err
	}
	s.client = redis.NewClient(options)
	if err := s.client.Ping(ctx).Err(); err != nil {
		_ = s.client.Close()
		return nil, err
	}
	if err := s.load(ctx); err != nil {
		_ = s.client.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() {
	if s.client != nil {
		_ = s.client.Close()
	}
}

func (s *Store) IsBanned(userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.banned[userID]
	return ok
}

func (s *Store) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, 0, len(s.banned))
	for id := range s.banned {
		result = append(result, id)
	}
	sort.Slice(result, func(i, j int) bool {
		a, ea := strconv.ParseInt(result[i], 10, 64)
		b, eb := strconv.ParseInt(result[j], 10, 64)
		if ea == nil && eb == nil {
			return a < b
		}
		return result[i] < result[j]
	})
	return result
}

func (s *Store) Set(ctx context.Context, userID string, banned bool) error {
	if userID == "" {
		return fmt.Errorf("empty user id")
	}
	if s.client != nil {
		var err error
		if banned {
			err = s.client.SAdd(ctx, bannedKey, userID).Err()
		} else {
			pipe := s.client.TxPipeline()
			pipe.SRem(ctx, bannedKey, userID)
			pipe.Del(ctx, boardKeyPrefix+userID)
			_, err = pipe.Exec(ctx)
		}
		if err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if banned {
		s.banned[userID] = struct{}{}
		if s.boards[userID] == nil {
			s.boards[userID] = make(map[[2]int]domain.BoardPixel)
		}
	} else {
		delete(s.banned, userID)
		delete(s.boards, userID)
	}
	return nil
}

func (s *Store) Apply(ctx context.Context, userID string, event domain.PixelEvent) error {
	pixel := domain.BoardPixel{X: event.X, Y: event.Y, Color: event.Color, Version: event.Version, Author: event.Author, FrozenUntil: event.FrozenUntil, UpdatedAt: event.CreatedAt}
	if s.client != nil {
		raw, err := json.Marshal(pixel)
		if err != nil {
			return err
		}
		if err := s.client.HSet(ctx, boardKeyPrefix+userID, coordinate(event.X, event.Y), raw).Err(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	board := s.boards[userID]
	if board == nil {
		board = make(map[[2]int]domain.BoardPixel)
		s.boards[userID] = board
	}
	board[[2]int{event.X, event.Y}] = pixel
	return nil
}

func (s *Store) Pixel(userID string, x, y int) (domain.BoardPixel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pixel, ok := s.boards[userID][[2]int{x, y}]
	return pixel, ok
}

func (s *Store) Snapshot(userID string) []domain.BoardPixel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	board := s.boards[userID]
	result := make([]domain.BoardPixel, 0, len(board))
	for _, pixel := range board {
		result = append(result, pixel)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Y == result[j].Y {
			return result[i].X < result[j].X
		}
		return result[i].Y < result[j].Y
	})
	return result
}

func (s *Store) UsersWithPixel(x, y int) map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]struct{})
	for userID := range s.banned {
		if _, ok := s.boards[userID][[2]int{x, y}]; ok {
			result[userID] = struct{}{}
		}
	}
	return result
}

func (s *Store) MaxVersion() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var maximum int64
	for _, board := range s.boards {
		for _, pixel := range board {
			if pixel.Version > maximum {
				maximum = pixel.Version
			}
		}
	}
	return maximum
}

func (s *Store) ClearOverlays(ctx context.Context) error {
	ids := s.List()
	if s.client != nil && len(ids) > 0 {
		pipe := s.client.TxPipeline()
		for _, id := range ids {
			pipe.Del(ctx, boardKeyPrefix+id)
		}
		if _, err := pipe.Exec(ctx); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.banned {
		s.boards[id] = make(map[[2]int]domain.BoardPixel)
	}
	return nil
}

// ClientPrizes returns a presentation-only trophy snapshot. It is never used
// by drop eligibility, supply accounting, rewards, or winner selection.
func (s *Store) ClientPrizes(ctx context.Context, userID string) (json.RawMessage, bool, error) {
	if s.client == nil {
		return nil, false, nil
	}
	raw, err := s.client.HGet(ctx, clientPrizesKey, userID).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !json.Valid(raw) {
		return nil, false, fmt.Errorf("invalid client prize snapshot")
	}
	return json.RawMessage(raw), true, nil
}

func (s *Store) load(ctx context.Context) error {
	ids, err := s.client.SMembers(ctx, bannedKey).Result()
	if err != nil {
		return err
	}
	for _, id := range ids {
		rawPixels, err := s.client.HGetAll(ctx, boardKeyPrefix+id).Result()
		if err != nil {
			return err
		}
		board := make(map[[2]int]domain.BoardPixel, len(rawPixels))
		for _, raw := range rawPixels {
			var pixel domain.BoardPixel
			if json.Unmarshal([]byte(raw), &pixel) == nil {
				board[[2]int{pixel.X, pixel.Y}] = pixel
			}
		}
		s.banned[id] = struct{}{}
		s.boards[id] = board
	}
	return nil
}

func coordinate(x, y int) string { return strconv.Itoa(x) + ":" + strconv.Itoa(y) }
