package main

import (
	"testing"

	"pixelbattle/realtime/internal/domain"
)

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

func TestRenderMapCanvasUsesFastHexColorPath(t *testing.T) {
	canvas := renderMapCanvas(2, 2, []domain.BoardPixel{
		{X: 1, Y: 1, Color: "#12aBef"},
		{X: 0, Y: 1, Color: "invalid"},
	})
	painted := canvas.RGBAAt(900, 900)
	if painted.R != 0x12 || painted.G != 0xab || painted.B != 0xef || painted.A != 0xff {
		t.Fatalf("painted pixel = %#v", painted)
	}
	background := canvas.RGBAAt(100, 900)
	if background.R != 0xff || background.G != 0xff || background.B != 0xff || background.A != 0xff {
		t.Fatalf("invalid color changed background: %#v", background)
	}
}
