package persistence

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"pixelbattle/realtime/internal/domain"
)

func TestWriteBatchDuplicateDoesNotUpdateBoardOrProfile(t *testing.T) {
	dsn := os.Getenv("GO_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set GO_TEST_POSTGRES_DSN for isolated-schema PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("security_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	w := &Writer{pool: pool}
	defer w.Close()
	if err := w.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	event := domain.PixelEvent{EventID: "event", OperationID: "operation", BoardID: "main", Color: "#FFFFFF", UserID: "123", Author: domain.PixelAuthor{ID: "123", DisplayName: "Original"}, Version: 1, CreatedAt: time.Now()}
	if err := w.WriteBatch(ctx, []domain.PixelEvent{event}); err != nil {
		t.Fatal(err)
	}
	duplicate := event
	duplicate.EventID = "other-event"
	duplicate.Version = 2
	duplicate.Color = "#000000"
	duplicate.Author.DisplayName = "Changed"
	if err := w.WriteBatch(ctx, []domain.PixelEvent{duplicate, event}); err != nil {
		t.Fatal(err)
	}
	var color, name string
	var count int
	if err := pool.QueryRow(ctx, `SELECT color FROM board_pixels WHERE board_id='main' AND x=0 AND y=0`).Scan(&color); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT display_name FROM profiles WHERE telegram_id='123'`).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pixel_events`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if color != "#FFFFFF" || name != "Original" || count != 1 {
		t.Fatalf("duplicate mutated state: color=%s name=%s count=%d", color, name, count)
	}
	pixels, maximum, err := w.RestoreBoard(ctx, "main")
	if err != nil || len(pixels) != 1 || maximum != 1 {
		t.Fatalf("durable board recovery: %v %d %v", pixels, maximum, err)
	}
	if err := w.WriteSnapshot(ctx, "main", nil, 1, BoardSize{Width: 150, Height: 150}); err != nil {
		t.Fatal(err)
	}
	pixels, _, err = w.RestoreBoard(ctx, "main")
	if err != nil || len(pixels) != 0 {
		t.Fatal("clear resurrected old pixels", pixels, err)
	}
	newEvent := event
	newEvent.EventID = "next"
	newEvent.OperationID = "next"
	newEvent.Version = 2
	expiry := time.Now().UTC().Truncate(time.Microsecond).Add(time.Minute)
	newEvent.FrozenUntil = &expiry
	if err := w.WriteBatch(ctx, []domain.PixelEvent{newEvent}); err != nil {
		t.Fatal(err)
	}
	pixels, maximum, err = w.RestoreBoard(ctx, "main")
	if err != nil || len(pixels) != 1 || maximum != 2 || pixels[0].FrozenUntil == nil || !pixels[0].FrozenUntil.Equal(expiry) {
		t.Fatal("post-checkpoint recovery failed", pixels, maximum, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE board_snapshots SET event_version=NULL`); err != nil {
		t.Fatal(err)
	}
	pixels, maximum, err = w.RestoreBoard(ctx, "main")
	if err != nil || len(pixels) != 0 || maximum != 2 {
		t.Fatal("legacy empty snapshot must not resurrect rows", pixels, maximum, err)
	}
	legacy := []domain.BoardPixel{{X: 3, Y: 4, Color: "#123456", Version: 1, Author: event.Author}}
	if err := w.WriteSnapshot(ctx, "main", legacy, 2, BoardSize{Width: 150, Height: 150}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE board_snapshots SET event_version=NULL`); err != nil {
		t.Fatal(err)
	}
	pixels, maximum, err = w.RestoreBoard(ctx, "main")
	if err != nil || len(pixels) != 1 || pixels[0].X != 3 || pixels[0].Color != "#123456" || maximum != 2 {
		t.Fatal("legacy snapshot changed", pixels, maximum, err)
	}
}

func TestMemoryBatchRetainsFailedWrites(t *testing.T) {
	input := make(chan domain.PixelEvent, 1)
	input <- domain.PixelEvent{EventID: "retained"}
	close(input)
	calls := 0
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runMemoryBatcher(ctx, input, func(_ context.Context, events []domain.PixelEvent) error {
		calls++
		if len(events) != 1 || events[0].EventID != "retained" {
			t.Fatal("batch lost", events)
		}
		if calls == 1 {
			return errors.New("temporary failure")
		}
		return nil
	})
	if calls != 2 {
		t.Fatal("batch not retried", calls)
	}
}

func TestWriterStartupFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if w, err := NewWriter(ctx, "postgres://localhost:1/unavailable?connect_timeout=1"); err == nil {
		w.Close()
		t.Fatal("unavailable database accepted")
	}
}
