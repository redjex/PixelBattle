package shadowban

import (
	"context"
	"testing"
	"time"

	"pixelbattle/realtime/internal/domain"
)

func TestPrivateOverlayLifecycle(t *testing.T) {
	store, err := New(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(context.Background(), "42", true); err != nil {
		t.Fatal(err)
	}
	if !store.IsBanned("42") {
		t.Fatal("user was not marked banned")
	}
	event := domain.PixelEvent{X: 3, Y: 4, Color: "#112233", Version: 7, CreatedAt: time.Now(), Author: domain.PixelAuthor{ID: "42"}}
	if err := store.Apply(context.Background(), "42", event); err != nil {
		t.Fatal(err)
	}
	pixel, ok := store.Pixel("42", 3, 4)
	if !ok || pixel.Color != event.Color || pixel.Author.ID != "42" {
		t.Fatalf("unexpected private pixel: %#v, %v", pixel, ok)
	}
	if _, ok := store.UsersWithPixel(3, 4)["42"]; !ok {
		t.Fatal("private owner was not excluded from public fan-out")
	}
	if store.MaxVersion() != 7 {
		t.Fatalf("max version = %d, want 7", store.MaxVersion())
	}
	if err := store.ClearOverlays(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.IsBanned("42") || len(store.Snapshot("42")) != 0 {
		t.Fatal("clearing overlays changed the ban or kept pixels")
	}
	if err := store.Set(context.Background(), "42", false); err != nil {
		t.Fatal(err)
	}
	if store.IsBanned("42") || len(store.Snapshot("42")) != 0 {
		t.Fatal("unban did not remove private overlay")
	}
}
