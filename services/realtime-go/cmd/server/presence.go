package main

import (
	"sort"
	"sync"
	"time"
)

type appPresence struct {
	mu    sync.Mutex
	users map[int64]time.Time
	ttl   time.Duration
	now   func() time.Time
}

func newAppPresence(ttl time.Duration) *appPresence {
	return &appPresence{users: make(map[int64]time.Time), ttl: ttl, now: time.Now}
}

func (p *appPresence) Touch(userID int64) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.prune(now)
	p.users[userID] = now
	return int64(len(p.users))
}

func (p *appPresence) Count() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune(p.now())
	return int64(len(p.users))
}

func (p *appPresence) IsOnline(userID int64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.now()
	p.prune(now)
	_, ok := p.users[userID]
	return ok
}

func (p *appPresence) Users() []int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune(p.now())
	result := make([]int64, 0, len(p.users))
	for userID := range p.users {
		result = append(result, userID)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (p *appPresence) CountMatching(include func(int64) bool) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.prune(p.now())
	var count int64
	for userID := range p.users {
		if include == nil || include(userID) {
			count++
		}
	}
	return count
}

func (p *appPresence) prune(now time.Time) {
	cutoff := now.Add(-p.ttl)
	for userID, seenAt := range p.users {
		if seenAt.Before(cutoff) {
			delete(p.users, userID)
		}
	}
}
