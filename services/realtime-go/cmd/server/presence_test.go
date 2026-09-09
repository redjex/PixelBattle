package main

import (
	"testing"
	"time"
)

func TestAppPresenceCountsUniqueActiveUsers(t *testing.T) {
	clock := time.Unix(1_700_000_000, 0)
	presence := newAppPresence(10 * time.Second)
	presence.now = func() time.Time { return clock }

	if got := presence.Touch(101); got != 1 {
		t.Fatalf("first user count = %d, want 1", got)
	}
	if got := presence.Touch(101); got != 1 {
		t.Fatalf("duplicate user count = %d, want 1", got)
	}
	if got := presence.Touch(202); got != 2 {
		t.Fatalf("second user count = %d, want 2", got)
	}

	clock = clock.Add(8 * time.Second)
	presence.Touch(101)
	clock = clock.Add(3 * time.Second)
	if got := presence.Count(); got != 1 {
		t.Fatalf("expired user was not removed: count = %d, want 1", got)
	}
}
