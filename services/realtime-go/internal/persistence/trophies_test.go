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
	if len(trophyDefinitions) != 12 {
		t.Fatalf("expected 12 trophy definitions, got %d", len(trophyDefinitions))
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
		case "experience", "bomb", "ice":
			if definition.Weight != 100 || definition.Total != 1 || definition.RewardAmount <= 0 {
				t.Fatalf("invalid common reward settings for %s: weight=%d cap=%d parts=%d amount=%d", definition.ID, definition.Weight, definition.Cap, definition.Total, definition.RewardAmount)
			}
			if definition.ID == "experience" && (definition.Cap != 50 || definition.Repeatable || definition.RewardAmount != 100) {
				t.Fatalf("invalid experience settings: %+v", definition)
			}
			if definition.ID == "bomb" && (definition.Cap != 500 || !definition.Repeatable || definition.RewardAmount != 5) {
				t.Fatalf("invalid bomb settings: %+v", definition)
			}
			if definition.ID == "ice" && (definition.Cap != 500 || !definition.Repeatable || definition.RewardAmount != 1) {
				t.Fatalf("invalid ice settings: %+v", definition)
			}
		default:
			if definition.Weight != 4 || definition.Cap != 1 || definition.Total != 4 {
				t.Fatalf("invalid NFT settings for %s: weight=%d cap=%d parts=%d", definition.ID, definition.Weight, definition.Cap, definition.Total)
			}
		}
	}
}

func TestNFTOutcomePlanContainsEveryRequiredPart(t *testing.T) {
	outcomes := shuffledNFTOutcomes(nftPartsPerCampaign)
	if len(outcomes) != 100 {
		t.Fatalf("expected 100 NFT opportunities, got %d", len(outcomes))
	}
	counts := make(map[string]int)
	for _, trophyID := range outcomes {
		counts[trophyID]++
	}
	for _, definition := range trophyDefinitions {
		if definition.Cap == 1 && counts[definition.ID] != nftPartsPerCampaign {
			t.Fatalf("NFT %s has %d planned opportunities, want %d", definition.ID, counts[definition.ID], nftPartsPerCampaign)
		}
	}
}

func TestNFTInventoryDropChanceFallsAsInventoryGrows(t *testing.T) {
	previous := 2.0
	for parts := 0; parts <= 8; parts++ {
		counts := map[string]int{"liberty-figure-252202": parts}
		chance := nftInventoryDropChance(counts)
		if chance > previous {
			t.Fatalf("chance grew from %.2f to %.2f at %d parts", previous, chance, parts)
		}
		previous = chance
	}
	if chance := nftInventoryDropChance(map[string]int{}); chance != 1 {
		t.Fatalf("empty NFT inventory chance = %.2f, want 1", chance)
	}
	if chance := nftInventoryDropChance(map[string]int{"liberty-figure-252202": 6}); chance != 0.05 {
		t.Fatalf("large NFT inventory chance = %.2f, want 0.05", chance)
	}
}
