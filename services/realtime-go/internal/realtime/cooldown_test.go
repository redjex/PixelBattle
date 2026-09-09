package realtime

import (
	"fmt"
	"testing"
	"time"
)

func TestCooldownBounded(t *testing.T) {
	c := NewCooldown()
	now := time.Now()
	for i := 0; i < 10000; i++ {
		c.Allow(fmt.Sprint(i), "main", time.Minute, now)
	}
	if allowed, _ := c.Allow("overflow", "main", time.Minute, now); allowed || len(c.last) != 10000 {
		t.Fatal("cooldown table unbounded")
	}
	if allowed, _ := c.Allow("overflow", "main", time.Minute, now.Add(time.Minute)); !allowed || len(c.last) != 1 {
		t.Fatal("expired cooldowns not reclaimed")
	}
}

func TestCooldownAllowsOncePerBoardAndUser(t *testing.T) {
	cooldown := NewCooldown()
	now := time.Unix(100, 0)
	if allowed, _ := cooldown.Allow("user-1", "main", 10*time.Second, now); !allowed {
		t.Fatal("first placement should be allowed")
	}
	if allowed, retry := cooldown.Allow("user-1", "main", 10*time.Second, now.Add(time.Second)); allowed || retry != 9*time.Second {
		t.Fatalf("expected 9 second retry, got allowed=%v retry=%s", allowed, retry)
	}
	if allowed, _ := cooldown.Allow("user-1", "main", 10*time.Second, now.Add(10*time.Second)); !allowed {
		t.Fatal("placement should be allowed after cooldown")
	}
	if allowed, _ := cooldown.Allow("user-1", "other", 10*time.Second, now); !allowed {
		t.Fatal("cooldown should be scoped to a board")
	}
}
