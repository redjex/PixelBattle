package realtime

import (
	"sync"
	"time"
)

type Cooldown struct {
	mu        sync.Mutex
	players   map[string]cooldownState
	nextSweep time.Time
}

type cooldownState struct {
	nextAllowed   time.Time
	lastPlacement time.Time
}

const stateLifetime = 2 * time.Hour

func NewCooldown() *Cooldown { return &Cooldown{players: make(map[string]cooldownState)} }

// Allow atomically reserves the next placement slot for an identity and board.
// The configured delay is applied exactly and never grows after inactivity.
func (c *Cooldown) Allow(identity, board string, delay time.Duration, now time.Time) (bool, time.Duration, time.Duration) {
	key := identity + ":" + board
	c.mu.Lock()
	defer c.mu.Unlock()
	if !now.Before(c.nextSweep) {
		for key, state := range c.players {
			if now.Sub(state.lastPlacement) > stateLifetime {
				delete(c.players, key)
			}
		}
		c.nextSweep = now.Add(time.Minute)
	}
	state, exists := c.players[key]
	if exists && state.nextAllowed.After(now) {
		return false, state.nextAllowed.Sub(now), state.nextAllowed.Sub(state.lastPlacement)
	}
	if !exists && len(c.players) >= 10000 {
		return false, time.Minute, delay
	}
	c.players[key] = cooldownState{nextAllowed: now.Add(delay), lastPlacement: now}
	return true, 0, delay
}
