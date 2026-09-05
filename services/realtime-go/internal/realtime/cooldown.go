package realtime

import (
	"sync"
	"time"
)

type Cooldown struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func NewCooldown() *Cooldown { return &Cooldown{last: make(map[string]time.Time)} }

// Allow atomically reserves the next placement slot for an identity and board.
func (c *Cooldown) Allow(identity, board string, delay time.Duration, now time.Time) (bool, time.Duration) {
	key := identity + ":" + board
	c.mu.Lock()
	defer c.mu.Unlock()
	if next, ok := c.last[key]; ok && next.After(now) {
		return false, next.Sub(now)
	}
	c.last[key] = now.Add(delay)
	return true, 0
}
