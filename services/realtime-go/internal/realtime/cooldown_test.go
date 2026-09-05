package realtime

import (
	"testing"
	"time"
)

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
