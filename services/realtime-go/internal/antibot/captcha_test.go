package antibot

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func TestPerfectTimingRequiresCaptcha(t *testing.T) {
	guard := New()
	now := time.Unix(100, 0)
	for index := 0; index < perfectIntervalsNeeded; index++ {
		if guard.RecordPlacement("player", now, 5*time.Second) {
			t.Fatal("captcha triggered too early")
		}
		now = now.Add(5*time.Second + 50*time.Millisecond)
	}
	if !guard.RecordPlacement("player", now, 5*time.Second) {
		t.Fatal("captcha did not trigger after a perfect timing streak")
	}
	if !guard.Required("player", now) {
		t.Fatal("required state was not retained")
	}
	statuses := guard.ReviewStatuses(now)
	if len(statuses) != 1 || statuses[0].Status != "suspicious" {
		t.Fatalf("automatically challenged player was not suspicious: %#v", statuses)
	}
}

func TestImperfectTimingResetsStreak(t *testing.T) {
	guard := New()
	now := time.Unix(100, 0)
	for index := 0; index < perfectIntervalsNeeded; index++ {
		if guard.RecordPlacement("player", now, 5*time.Second) {
			t.Fatal("captcha triggered unexpectedly")
		}
		now = now.Add(5*time.Second + 50*time.Millisecond)
	}
	now = now.Add(time.Second)
	if guard.RecordPlacement("player", now, 5*time.Second) {
		t.Fatal("an imperfect interval did not reset the streak")
	}
}

func TestChallengeCanBeSolved(t *testing.T) {
	guard := New()
	guard.random = strings.NewReader(strings.Repeat("A", 2000))
	now := time.Unix(100, 0)
	guard.Force("player", now)
	challenge, required, err := guard.Challenge("player", now)
	if err != nil || !required || challenge.ID == "" || !strings.HasPrefix(challenge.Image, "data:image/png;base64,") {
		t.Fatalf("invalid challenge: %#v required=%v err=%v", challenge, required, err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(challenge.Image, "data:image/png;base64,"))
	if err != nil || len(raw) < 8 || string(raw[1:4]) != "PNG" {
		t.Fatal("challenge image is not a PNG")
	}
	if !guard.Verify("player", challenge.ID, "33333", now.Add(time.Second)) {
		t.Fatal("valid answer was rejected")
	}
	if guard.Required("player", now.Add(time.Second)) {
		t.Fatal("solved player stayed blocked")
	}
	statuses := guard.ReviewStatuses(now.Add(time.Second))
	if len(statuses) != 1 || statuses[0].UserID != "player" || statuses[0].Status != "clean" {
		t.Fatalf("solved player was not marked clean: %#v", statuses)
	}
}

func TestWrongAnswerRotatesChallenge(t *testing.T) {
	guard := New()
	guard.random = strings.NewReader(strings.Repeat("B", 4000))
	now := time.Unix(100, 0)
	guard.Force("player", now)
	challenge, _, err := guard.Challenge("player", now)
	if err != nil {
		t.Fatal(err)
	}
	if guard.Verify("player", challenge.ID, "WRONG", now.Add(time.Second)) {
		t.Fatal("wrong answer was accepted")
	}
	statuses := guard.ReviewStatuses(now.Add(time.Second))
	if len(statuses) != 1 || statuses[0].Status != "suspicious" {
		t.Fatalf("player with a failed CAPTCHA was not suspicious: %#v", statuses)
	}
	if guard.players["player"].challengeID != "" {
		t.Fatal("wrong answer did not invalidate the challenge")
	}
	next, _, err := guard.Challenge("player", now.Add(2*time.Second))
	if err != nil || next.ID == "" {
		t.Fatal("replacement challenge was not generated")
	}
}

func TestForceRequiresCaptcha(t *testing.T) {
	guard := New()
	now := time.Unix(100, 0)
	if !guard.Force("player", now) || !guard.Required("player", now) {
		t.Fatal("forced captcha was not retained")
	}
	challenge, required, err := guard.Challenge("player", now)
	if err != nil || !required || challenge.ID == "" {
		t.Fatal("forced captcha did not create a challenge")
	}
	statuses := guard.ReviewStatuses(now)
	if len(statuses) != 1 || statuses[0].UserID != "player" || statuses[0].Status != "suspicious" {
		t.Fatalf("challenged player was not marked suspicious: %#v", statuses)
	}
}

func TestReviewStatusesOnlyIncludeChallengedPlayers(t *testing.T) {
	guard := New()
	now := time.Unix(100, 0)
	guard.RecordPlacement("ordinary", now, 5*time.Second)
	guard.Force("older", now)
	guard.Force("newer", now.Add(time.Second))

	statuses := guard.ReviewStatuses(now.Add(time.Second))
	if len(statuses) != 2 || statuses[0].UserID != "newer" || statuses[1].UserID != "older" {
		t.Fatalf("unexpected review statuses: %#v", statuses)
	}
}
