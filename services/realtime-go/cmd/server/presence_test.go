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

func TestPresenceReportsIndividualOnlineState(t *testing.T) {
	clock := time.Unix(100, 0)
	presence := newAppPresence(10 * time.Second)
	presence.now = func() time.Time { return clock }
	presence.Touch(101)
	if !presence.IsOnline(101) || presence.IsOnline(202) {
		t.Fatal("unexpected online state")
	}
	clock = clock.Add(11 * time.Second)
	if presence.IsOnline(101) {
		t.Fatal("expired user remained online")
	}
}

func TestPresenceListsActiveUsers(t *testing.T) {
	clock := time.Unix(100, 0)
	presence := newAppPresence(10 * time.Second)
	presence.now = func() time.Time { return clock }
	presence.Touch(202)
	presence.Touch(101)
	users := presence.Users()
	if len(users) != 2 || users[0] != 101 || users[1] != 202 {
		t.Fatalf("unexpected active users: %v", users)
	}
	clock = clock.Add(11 * time.Second)
	if users := presence.Users(); len(users) != 0 {
		t.Fatalf("expired users were listed: %v", users)
	}
}

func TestPresenceCountsOnlyMatchingUsers(t *testing.T) {
	presence := newAppPresence(10 * time.Second)
	presence.Touch(101)
	presence.Touch(202)
	if got := presence.CountMatching(func(userID int64) bool { return userID != 202 }); got != 1 {
		t.Fatalf("filtered count = %d, want 1", got)
	}
}
