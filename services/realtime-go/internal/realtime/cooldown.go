package realtime

import (
	"sync"
	"time"
)

type Cooldown struct {
	mu        sync.Mutex
	last      map[string]time.Time
	nextSweep time.Time
}

func NewCooldown() *Cooldown { return &Cooldown{last: make(map[string]time.Time)} }

// Allow atomically reserves the next placement slot for an identity and board.
func (c *Cooldown) Allow(identity, board string, delay time.Duration, now time.Time) (bool, time.Duration) {
	key := identity + ":" + board
	c.mu.Lock()
	defer c.mu.Unlock()
	if !now.Before(c.nextSweep) {
		for key, expiry := range c.last {
			if !expiry.After(now) {
				delete(c.last, key)
			}
		}
		c.nextSweep = now.Add(time.Minute)
	}
	if next, ok := c.last[key]; ok && next.After(now) {
		return false, next.Sub(now)
	}
	if _, exists := c.last[key]; !exists && len(c.last) >= 10000 {
		return false, time.Minute
	}
	c.last[key] = now.Add(delay)
	return true, 0
}
