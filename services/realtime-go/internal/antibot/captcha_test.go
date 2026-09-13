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

func TestClusteredTimingAroundCooldownRequiresCaptcha(t *testing.T) {
	guard := New()
	now := time.Unix(100, 0)
	guard.RecordPlacement("player", now, 5*time.Second)
	for index := 0; index < timingWindowSize-1; index++ {
		now = now.Add(5*time.Second + 650*time.Millisecond + time.Duration(index%3)*40*time.Millisecond)
		if guard.RecordPlacement("player", now, 5*time.Second) {
			t.Fatal("captcha triggered before the timing window was full")
		}
	}
	now = now.Add(5*time.Second + 690*time.Millisecond)
	if !guard.RecordPlacement("player", now, 5*time.Second) {
		t.Fatal("clustered machine timing did not require captcha")
	}
}

func TestVariedHumanTimingDoesNotTriggerClusterDetector(t *testing.T) {
	guard := New()
	now := time.Unix(100, 0)
	guard.RecordPlacement("player", now, 5*time.Second)
	intervals := []time.Duration{5*time.Second + 400*time.Millisecond, 6*time.Second + 100*time.Millisecond, 5*time.Second + 800*time.Millisecond, 6*time.Second + 700*time.Millisecond}
	for index := 0; index < timingWindowSize*2; index++ {
		now = now.Add(intervals[index%len(intervals)])
		if guard.RecordPlacement("player", now, 5*time.Second) {
			t.Fatal("varied human timing unexpectedly required captcha")
		}
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
	if !guard.Verify("player", challenge.ID, "33333", now.Add(2*time.Second)) {
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

func TestChallengeIsBoundToPlayerAndSingleUse(t *testing.T) {
	guard := New()
	guard.random = strings.NewReader(strings.Repeat("A", 3000))
	now := time.Unix(100, 0)
	guard.Force("player", now)
	challenge, _, err := guard.Challenge("player", now)
	if err != nil {
		t.Fatal(err)
	}
	if guard.Verify("other", challenge.ID, "33333", now.Add(2*time.Second)) {
		t.Fatal("captcha from one player was accepted for another")
	}
	if !guard.Verify("player", challenge.ID, "33333", now.Add(2*time.Second)) {
		t.Fatal("valid captcha was rejected")
	}
	if guard.Verify("player", challenge.ID, "33333", now.Add(3*time.Second)) {
		t.Fatal("captcha answer was accepted twice")
	}
}

func TestSolvedCaptchaSuppressesAutomaticChallengesForFifteenMinutes(t *testing.T) {
	guard := New()
	guard.random = strings.NewReader(strings.Repeat("A", 3000))
	now := time.Unix(100, 0)
	guard.Force("player", now)
	challenge, _, err := guard.Challenge("player", now)
	if err != nil || !guard.Verify("player", challenge.ID, "33333", now.Add(2*time.Second)) {
		t.Fatal("captcha setup failed", err)
	}
	solvedAt := now.Add(2 * time.Second)
	placedAt := solvedAt
	humanIntervals := []time.Duration{5400 * time.Millisecond, 6100 * time.Millisecond, 5800 * time.Millisecond, 6700 * time.Millisecond}
	for index := 1; index <= 20; index++ {
		placedAt = placedAt.Add(humanIntervals[index%len(humanIntervals)])
		if guard.RecordPlacement("player", placedAt, 5*time.Second) {
			t.Fatal("varied human timing interrupted the verification grace period")
		}
	}
	if guard.Flag("player", solvedAt.Add(verificationGrace-time.Second)) {
		t.Fatal("automatic flag interrupted the verification grace period")
	}
	if !guard.Flag("player", solvedAt.Add(verificationGrace)) {
		t.Fatal("automatic flag stayed suppressed after the grace period")
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
	for attempt := 0; attempt < maximumAnswerAttempts; attempt++ {
		if guard.Verify("player", challenge.ID, "WRONG", now.Add(time.Duration(attempt+2)*time.Second)) {
			t.Fatal("wrong answer was accepted")
		}
	}
	statuses := guard.ReviewStatuses(now.Add(5 * time.Second))
	if len(statuses) != 1 || statuses[0].Status != "suspicious" {
		t.Fatalf("player with a failed CAPTCHA was not suspicious: %#v", statuses)
	}
	if guard.players["player"].challengeID != "" {
		t.Fatal("wrong answer did not invalidate the challenge")
	}
	next, _, err := guard.Challenge("player", now.Add(6*time.Second))
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

func TestPendingCaptchaPenaltyAccumulatesOncePerChallenge(t *testing.T) {
	guard := New()
	now := time.Unix(100, 0)
	guard.Force("player", now)
	penalty, strikes, ok := guard.Penalize("player", now.Add(time.Minute))
	if !ok || penalty != 5*time.Second || strikes != 1 {
		t.Fatalf("unexpected first penalty: ok=%v penalty=%s strikes=%d", ok, penalty, strikes)
	}
	penalty, strikes, ok = guard.Penalize("player", now.Add(2*time.Minute))
	if !ok || penalty != 5*time.Second || strikes != 1 {
		t.Fatalf("same challenge was penalized twice: ok=%v penalty=%s strikes=%d", ok, penalty, strikes)
	}
	guard.Force("player", now.Add(3*time.Minute))
	penalty, strikes, ok = guard.Penalize("player", now.Add(4*time.Minute))
	if !ok || penalty != 10*time.Second || strikes != 2 {
		t.Fatalf("second challenge did not accumulate: ok=%v penalty=%s strikes=%d", ok, penalty, strikes)
	}
	statuses := guard.ReviewStatuses(now.Add(4 * time.Minute))
	if len(statuses) != 1 || statuses[0].StrikeCount != 2 || statuses[0].PenaltySeconds != 10 {
		t.Fatalf("penalty missing from review status: %#v", statuses)
	}
}

func TestCaptchaPenaltySurvivesGuardRestart(t *testing.T) {
	persisted := map[string]int{"player": 3}
	guard := NewPersistent(persisted, func(identity string) (int, error) {
		persisted[identity]++
		return persisted[identity], nil
	})
	if penalty := guard.Penalty("player", time.Now()); penalty != 15*time.Second {
		t.Fatalf("restored penalty = %s, want 15s", penalty)
	}
	now := time.Unix(100, 0)
	guard.Force("player", now)
	penalty, strikes, ok := guard.Penalize("player", now.Add(time.Minute))
	if !ok || strikes != 4 || penalty != 20*time.Second || persisted["player"] != 4 {
		t.Fatalf("persistent penalty not incremented: ok=%v strikes=%d penalty=%s persisted=%d", ok, strikes, penalty, persisted["player"])
	}
	restarted := NewPersistent(persisted, nil)
	if penalty := restarted.Penalty("player", time.Now()); penalty != 20*time.Second {
		t.Fatalf("penalty after restart = %s, want 20s", penalty)
	}
}

func TestCaptchaCannotBeSolvedImmediatelyByAutomation(t *testing.T) {
	guard := New()
	guard.random = strings.NewReader(strings.Repeat("A", 4000))
	now := time.Unix(100, 0)
	guard.Force("player", now)
	challenge, _, err := guard.Challenge("player", now)
	if err != nil {
		t.Fatal(err)
	}
	if guard.Verify("player", challenge.ID, "33333", now.Add(minimumSolveTime-time.Millisecond)) {
		t.Fatal("captcha was accepted before the minimum human solve time")
	}
	if !guard.Verify("player", challenge.ID, "33333", now.Add(minimumSolveTime)) {
		t.Fatal("captcha was not accepted after the minimum solve time")
	}
}

func TestMachineTimingRevokesSolvedCaptchaGrace(t *testing.T) {
	guard := New()
	guard.random = strings.NewReader(strings.Repeat("A", 4000))
	now := time.Unix(100, 0)
	guard.Force("player", now)
	challenge, _, err := guard.Challenge("player", now)
	if err != nil || !guard.Verify("player", challenge.ID, "33333", now.Add(2*time.Second)) {
		t.Fatal("captcha setup failed", err)
	}
	now = now.Add(2 * time.Second)
	guard.RecordPlacement("player", now, 5*time.Second)
	for index := 0; index < perfectIntervalsNeeded; index++ {
		now = now.Add(5*time.Second + 50*time.Millisecond)
		if required := guard.RecordPlacement("player", now, 5*time.Second); required != (index == perfectIntervalsNeeded-1) {
			t.Fatalf("unexpected captcha state at machine interval %d: %v", index, required)
		}
	}
}
