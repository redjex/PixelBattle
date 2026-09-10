package persistence

import (
	"testing"
	"time"
)

func TestTrophyDropChanceUsesOnlineAndLocalTime(t *testing.T) {
	localTime := func(hour int) time.Time {
		return time.Date(2026, time.September, 11, hour, 0, 0, 0, trophyLocation)
	}

	night := TrophyDropChance(localTime(2), 12)
	morning := TrophyDropChance(localTime(8), 12)
	day := TrophyDropChance(localTime(14), 12)
	evening := TrophyDropChance(localTime(20), 12)
	if !(night < morning && morning < day && day < evening) {
		t.Fatalf("unexpected time multipliers: night=%f morning=%f day=%f evening=%f", night, morning, day, evening)
	}

	quiet := TrophyDropChance(localTime(20), 1)
	active := TrophyDropChance(localTime(20), 12)
	busy := TrophyDropChance(localTime(20), 60)
	if !(quiet < active && active < busy) {
		t.Fatalf("unexpected online multipliers: quiet=%f active=%f busy=%f", quiet, active, busy)
	}
}

func TestTrophyRarityAndSupplyGrid(t *testing.T) {
	if len(trophyDefinitions) != 9 {
		t.Fatalf("expected 9 trophy definitions, got %d", len(trophyDefinitions))
	}
	for _, definition := range trophyDefinitions {
		switch definition.ID {
		case "stickers", "yng-explrz", "besigned":
			if definition.Weight != 100 || definition.Cap != 50 {
				t.Fatalf("invalid promo settings for %s: weight=%d cap=%d", definition.ID, definition.Weight, definition.Cap)
			}
		case "bear":
			if definition.Weight != 30 || definition.Cap != 5 {
				t.Fatalf("invalid bear settings: weight=%d cap=%d", definition.Weight, definition.Cap)
			}
		default:
			if definition.Weight != 4 || definition.Cap != 1 || definition.Total != 4 {
				t.Fatalf("invalid NFT settings for %s: weight=%d cap=%d parts=%d", definition.ID, definition.Weight, definition.Cap, definition.Total)
			}
		}
	}
}
