package main

import (
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

func (p *appPresence) prune(now time.Time) {
	cutoff := now.Add(-p.ttl)
	for userID, seenAt := range p.users {
		if seenAt.Before(cutoff) {
			delete(p.users, userID)
		}
	}
}
