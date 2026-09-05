package state

import (
	"sort"
	"sync"

	"pixelbattle/realtime/internal/domain"
)

type BoardStore struct {
	mu        sync.RWMutex
	boards    map[string]map[[2]int]domain.BoardPixel
	revisions map[string]uint64
}

func NewBoardStore() *BoardStore {
	return &BoardStore{boards: make(map[string]map[[2]int]domain.BoardPixel), revisions: make(map[string]uint64)}
}

func (s *BoardStore) Apply(event domain.PixelEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board := s.boards[event.BoardID]
	if board == nil {
		board = make(map[[2]int]domain.BoardPixel)
		s.boards[event.BoardID] = board
	}
	key := [2]int{event.X, event.Y}
	if current, ok := board[key]; ok && current.Version >= event.Version {
		return
	}
	board[key] = domain.BoardPixel{X: event.X, Y: event.Y, Color: event.Color, Version: event.Version, Author: event.Author, FrozenUntil: event.FrozenUntil}
	s.revisions[event.BoardID]++
}

func (s *BoardStore) Pixel(boardID string, x, y int) (domain.BoardPixel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pixel, ok := s.boards[boardID][[2]int{x, y}]
	return pixel, ok
}

func (s *BoardStore) ApplyPixels(boardID string, pixels []domain.BoardPixel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board := s.boards[boardID]
	if board == nil {
		board = make(map[[2]int]domain.BoardPixel)
		s.boards[boardID] = board
	}
	for _, pixel := range pixels {
		key := [2]int{pixel.X, pixel.Y}
		if current, ok := board[key]; !ok || current.Version < pixel.Version {
			board[key] = pixel
		}
	}
	s.revisions[boardID]++
}

func (s *BoardStore) Restore(boardID string, pixels []domain.BoardPixel) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board := make(map[[2]int]domain.BoardPixel, len(pixels))
	for _, pixel := range pixels {
		board[[2]int{pixel.X, pixel.Y}] = pixel
	}
	s.boards[boardID] = board
	s.revisions[boardID]++
}

func (s *BoardStore) Resize(boardID string, width, height int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	board := s.boards[boardID]
	if board == nil {
		board = make(map[[2]int]domain.BoardPixel)
	}
	for key := range board {
		if key[0] >= width || key[1] >= height {
			delete(board, key)
		}
	}
	s.boards[boardID] = board
	s.revisions[boardID]++
}

func (s *BoardStore) Clear(boardID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.boards[boardID] = make(map[[2]int]domain.BoardPixel)
	s.revisions[boardID]++
}

func (s *BoardStore) Revision(boardID string) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revisions[boardID]
}

func (s *BoardStore) Snapshot(boardID string) []domain.BoardPixel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	board := s.boards[boardID]
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

func (s *BoardStore) MaxVersion(boardID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var maximum int64
	for _, pixel := range s.boards[boardID] {
		if pixel.Version > maximum {
			maximum = pixel.Version
		}
	}
	return maximum
}

func (s *BoardStore) CountByAuthor(boardID, userID string) int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var count int64
	for _, pixel := range s.boards[boardID] {
		if pixel.Author.ID == userID {
			count++
		}
	}
	return count
}
