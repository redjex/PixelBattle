package main

import (
	"reflect"
	"testing"
)

func TestBombPaletteUsesOnlyPaletteColumn(t *testing.T) {
	tests := []struct {
		selected string
		want     []string
	}{
		{selected: "#000000", want: []string{"#000000", "#FDFDFD", "#8A8A8A"}},
		{selected: "#001EFF", want: []string{"#001EFF", "#8290FF", "#001194"}},
		{selected: "#FF0000", want: []string{"#FF0000", "#FF8080", "#870000"}},
	}
	for _, test := range tests {
		if got := bombPalette(test.selected); !reflect.DeepEqual(got, test.want) {
			t.Errorf("bombPalette(%q) = %v, want %v", test.selected, got, test.want)
		}
	}
}

func TestPlayerLevelMatchesFrontendProgression(t *testing.T) {
	tests := []struct {
		placed int64
		level  int
	}{{0, 1}, {9, 1}, {10, 2}, {29, 2}, {30, 3}, {59, 3}, {60, 4}, {99, 4}, {100, 5}, {3906, 28}, {49499, 99}, {49500, 100}}
	for _, test := range tests {
		if got := playerLevel(test.placed); got != test.level {
			t.Errorf("playerLevel(%d) = %d, want %d", test.placed, got, test.level)
		}
	}
}

func TestLevelRewardsAlternateAndScale(t *testing.T) {
	tests := []struct {
		level  int
		item   string
		amount int64
	}{{1, "bomb", 5}, {2, "ice", 5}, {21, "bomb", 10}, {40, "ice", 10}, {100, "ice", 25}}
	for _, test := range tests {
		item, amount := levelReward(test.level)
		if item != test.item || amount != test.amount {
			t.Errorf("levelReward(%d) = (%q,%d), want (%q,%d)", test.level, item, amount, test.item, test.amount)
		}
	}
}
