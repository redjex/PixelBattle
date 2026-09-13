package trophyrewards

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogReadsCodesAndSkipsComments(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "yng-explrz.txt"), []byte("# codes\nFIRST\n\nSECOND\nFIRST\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := New(directory).Entries("yng-explrz")
	if err != nil || len(entries) != 2 || entries[0] != "FIRST" || entries[1] != "SECOND" {
		t.Fatalf("unexpected entries: %#v err=%v", entries, err)
	}
}

func TestCatalogRejectsUnsafeURL(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "stickers.txt"), []byte("javascript:alert(1)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(directory).Entries("stickers"); err == nil {
		t.Fatal("unsafe URL was accepted")
	}
}

func TestRewardKindsCoverEveryPuzzlePrize(t *testing.T) {
	expected := map[string]Kind{
		"yng-explrz": KindCode, "stashvpn": KindCode,
		"besigned": KindURL, "stickers": KindURL, "stikidbot": KindURL,
		"bear": KindRequest, "bear-redjex": KindRequest,
		"liberty-figure-252202": KindURL, "candy-cane-162605": KindURL,
		"vice-cream-227533": KindURL, "vice-cream-428029": KindURL,
		"chill-flame-303522": KindURL,
		"vice-cream-10":      KindRequest,
	}
	if len(rewardKinds) != len(expected) {
		t.Fatalf("reward kind count = %d, want %d", len(rewardKinds), len(expected))
	}
	for trophyID, want := range expected {
		if got, ok := New("").Kind(trophyID); !ok || got != want {
			t.Fatalf("kind for %s = %q, %v; want %q", trophyID, got, ok, want)
		}
	}
}
