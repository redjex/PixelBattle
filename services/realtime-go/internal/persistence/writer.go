package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"log"
	"math/rand/v2"
	"pixelbattle/realtime/internal/domain"
	"time"
)

type Writer struct {
	pool  *pgxpool.Pool
	lease *pgxpool.Conn
}

type UserStats struct {
	PlacedPixels         int64 `json:"placedPixels"`
	BonusExperience      int64 `json:"bonusExperience"`
	RepaintedPixels      int64 `json:"repaintedPixels"`
	CurrentPixels        int64 `json:"currentPixels"`
	DailyPlacedPixels    int64 `json:"dailyPlacedPixels"`
	DailyRepaintedPixels int64 `json:"dailyRepaintedPixels"`
	DailyColorsUsed      int64 `json:"dailyColorsUsed"`
	DailyUniqueCells     int64 `json:"dailyUniqueCells"`
}

type Inventory struct {
	Bombs           int64 `json:"bombs"`
	Ice             int64 `json:"ice"`
	Experience      int64 `json:"experience"`
	FreezeRemaining int64 `json:"freezeRemaining"`
}

type BoardSize struct {
	Width  int
	Height int
}

type BoardBackup struct {
	ID     string
	Width  int
	Height int
	Pixels []domain.BoardPixel
}

func NewWriter(ctx context.Context, dsn string) (*Writer, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Writer{pool: pool}, nil
}

func (w *Writer) Close() {
	if w.lease != nil {
		_ = w.lease.Conn().Close(context.Background())
		w.lease.Release()
	}
	w.pool.Close()
}

// In-memory versions and board state require exactly one active server.
func (w *Writer) AcquireLease(ctx context.Context) error {
	conn, err := w.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	var locked bool
	if err = conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(706978, 1)`).Scan(&locked); err != nil || !locked {
		conn.Release()
		if err != nil {
			return err
		}
		return fmt.Errorf("another realtime server holds the board lease")
	}
	w.lease = conn
	return nil
}

func (w *Writer) MonitorLease(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Second):
			check, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := w.lease.Conn().Ping(check)
			cancel()
			if err != nil {
				return fmt.Errorf("board lease connection lost: %w", err)
			}
		}
	}
}

func (w *Writer) Migrate(ctx context.Context) error {
	_, err := w.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS pixel_events (
 event_id text PRIMARY KEY, operation_id text UNIQUE NOT NULL, board_id text NOT NULL,
 x integer NOT NULL, y integer NOT NULL, color text NOT NULL, user_id text NOT NULL,
 version bigint NOT NULL, created_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS board_pixels (
 board_id text NOT NULL, x integer NOT NULL, y integer NOT NULL, color text NOT NULL,
 version bigint NOT NULL, updated_by text NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY (board_id, x, y)
);
CREATE TABLE IF NOT EXISTS board_snapshots (
 board_id text PRIMARY KEY, version bigint NOT NULL DEFAULT 0,
 pixels jsonb NOT NULL, updated_at timestamptz NOT NULL
);
ALTER TABLE board_snapshots ADD COLUMN IF NOT EXISTS event_version bigint;
ALTER TABLE board_pixels ADD COLUMN IF NOT EXISTS frozen_until timestamptz;
CREATE TABLE IF NOT EXISTS board_settings (
 board_id text PRIMARY KEY, width integer NOT NULL, height integer NOT NULL,
 updated_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS profiles (
 telegram_id text PRIMARY KEY,
 display_name text NOT NULL,
 username text NOT NULL DEFAULT '',
 photo_url text NOT NULL DEFAULT '',
 prizes jsonb NOT NULL DEFAULT '[]'::jsonb,
 first_seen_at timestamptz NOT NULL DEFAULT NOW(),
 updated_at timestamptz NOT NULL DEFAULT NOW()
);
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS prizes jsonb NOT NULL DEFAULT '[]'::jsonb;
CREATE TABLE IF NOT EXISTS board_clear_backups (
 backup_id text PRIMARY KEY,
 board_id text NOT NULL,
 width integer NOT NULL,
 height integer NOT NULL,
 pixels jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT NOW(),
 restored_at timestamptz
);
CREATE TABLE IF NOT EXISTS daily_quest_resets (
 scope text PRIMARY KEY,
 reset_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS player_items (
 user_id text PRIMARY KEY,
 bombs bigint NOT NULL DEFAULT 0 CHECK (bombs >= 0),
 ice bigint NOT NULL DEFAULT 0 CHECK (ice >= 0),
 experience bigint NOT NULL DEFAULT 0 CHECK (experience >= 0),
 freeze_remaining integer NOT NULL DEFAULT 0 CHECK (freeze_remaining >= 0),
 updated_at timestamptz NOT NULL DEFAULT NOW()
);
ALTER TABLE player_items ADD COLUMN IF NOT EXISTS experience bigint NOT NULL DEFAULT 0 CHECK (experience >= 0);
CREATE TABLE IF NOT EXISTS level_reward_claims (
 user_id text NOT NULL,
 level integer NOT NULL CHECK (level BETWEEN 1 AND 100),
 item text NOT NULL CHECK (item IN ('bomb','ice')),
 amount bigint NOT NULL CHECK (amount > 0),
 claimed_at timestamptz NOT NULL DEFAULT NOW(),
 PRIMARY KEY (user_id,level)
);
CREATE TABLE IF NOT EXISTS trophy_drop_state (
 id smallint PRIMARY KEY CHECK (id=1),
 next_drop_at timestamptz NOT NULL,
 force_next boolean NOT NULL DEFAULT false
);
ALTER TABLE trophy_drop_state ADD COLUMN IF NOT EXISTS force_next boolean NOT NULL DEFAULT false;
INSERT INTO trophy_drop_state(id,next_drop_at)
VALUES(1,NOW()+((600+floor(random()*301))::text||' seconds')::interval)
ON CONFLICT(id) DO NOTHING;
CREATE TABLE IF NOT EXISTS trophy_nft_campaign (
 id smallint PRIMARY KEY CHECK (id=1),
 starts_at timestamptz NOT NULL,
 ends_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS trophy_nft_plan (
	sequence integer PRIMARY KEY,
	trophy_id text NOT NULL,
	release_at timestamptz NOT NULL,
	claimed_at timestamptz,
	winner_user_id text
);
ALTER TABLE trophy_nft_plan DROP CONSTRAINT IF EXISTS trophy_nft_plan_sequence_check;
ALTER TABLE trophy_nft_plan ADD CONSTRAINT trophy_nft_plan_sequence_check CHECK (sequence BETWEEN 1 AND 100);
CREATE TABLE IF NOT EXISTS trophy_nft_winners (
 trophy_id text PRIMARY KEY,
 user_id text NOT NULL,
	assigned_at timestamptz NOT NULL DEFAULT NOW()
);
DELETE FROM trophy_nft_winners AS winner
WHERE NOT EXISTS (
 SELECT 1
 FROM profiles
 CROSS JOIN LATERAL jsonb_array_elements(prizes) AS prize
 WHERE profiles.telegram_id=winner.user_id
   AND prize->>'id'=winner.trophy_id
   AND jsonb_typeof(prize->'collectedParts')='number'
   AND jsonb_typeof(prize->'total')='number'
   AND (prize->>'collectedParts')::int >= (prize->>'total')::int
);
CREATE INDEX IF NOT EXISTS pixel_events_user_id_idx ON pixel_events(user_id);
CREATE INDEX IF NOT EXISTS pixel_events_user_created_at_idx ON pixel_events(user_id,created_at);
CREATE INDEX IF NOT EXISTS pixel_events_cell_version_idx ON pixel_events(board_id,x,y,version);
CREATE INDEX IF NOT EXISTS board_pixels_updated_by_idx ON board_pixels(updated_by);

WITH upgraded_rewards AS (
 UPDATE level_reward_claims
 SET amount=amount*5
 WHERE amount=((level-1)/20)+1
 RETURNING user_id,item,amount-(amount/5) AS delta
), reward_totals AS (
 SELECT user_id,
  COALESCE(SUM(delta) FILTER (WHERE item='bomb'),0) AS bombs,
  COALESCE(SUM(delta) FILTER (WHERE item='ice'),0) AS ice
 FROM upgraded_rewards
 GROUP BY user_id
)
INSERT INTO player_items(user_id,bombs,ice,updated_at)
SELECT user_id,bombs,ice,NOW() FROM reward_totals
ON CONFLICT(user_id) DO UPDATE SET
 bombs=player_items.bombs+EXCLUDED.bombs,
 ice=player_items.ice+EXCLUDED.ice,
 updated_at=NOW();`)
	return err
}

func (w *Writer) Inventory(ctx context.Context, userID string) (Inventory, error) {
	if _, err := w.pool.Exec(ctx, `INSERT INTO player_items(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return Inventory{}, err
	}
	var inventory Inventory
	err := w.pool.QueryRow(ctx, `SELECT bombs,ice,experience,freeze_remaining FROM player_items WHERE user_id=$1`, userID).
		Scan(&inventory.Bombs, &inventory.Ice, &inventory.Experience, &inventory.FreezeRemaining)
	return inventory, err
}

func (w *Writer) GrantItem(ctx context.Context, userID, item string, amount int64) (Inventory, error) {
	if amount <= 0 {
		return Inventory{}, fmt.Errorf("amount must be positive")
	}
	column := ""
	switch item {
	case "bomb":
		column = "bombs"
	case "ice":
		column = "ice"
	default:
		return Inventory{}, fmt.Errorf("unknown item")
	}
	query := fmt.Sprintf(`INSERT INTO player_items(user_id,%s,updated_at) VALUES($1,$2,NOW())
ON CONFLICT(user_id) DO UPDATE SET %s=player_items.%s+EXCLUDED.%s,updated_at=NOW()
RETURNING bombs,ice,experience,freeze_remaining`, column, column, column, column)
	var inventory Inventory
	err := w.pool.QueryRow(ctx, query, userID, amount).Scan(&inventory.Bombs, &inventory.Ice, &inventory.Experience, &inventory.FreezeRemaining)
	return inventory, err
}

func (w *Writer) ClaimedLevelRewards(ctx context.Context, userID string) ([]int, error) {
	rows, err := w.pool.Query(ctx, `SELECT level FROM level_reward_claims WHERE user_id=$1 ORDER BY level`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	levels := make([]int, 0)
	for rows.Next() {
		var level int
		if err := rows.Scan(&level); err != nil {
			return nil, err
		}
		levels = append(levels, level)
	}
	return levels, rows.Err()
}

func (w *Writer) ClaimLevelReward(ctx context.Context, userID string, level int, item string, amount int64) (Inventory, bool, error) {
	column := ""
	switch item {
	case "bomb":
		column = "bombs"
	case "ice":
		column = "ice"
	default:
		return Inventory{}, false, fmt.Errorf("unknown item")
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Inventory{}, false, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `INSERT INTO player_items(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return Inventory{}, false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO level_reward_claims(user_id,level,item,amount) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, userID, level, item, amount)
	if err != nil {
		return Inventory{}, false, err
	}
	claimed := tag.RowsAffected() == 1
	if claimed {
		query := fmt.Sprintf(`UPDATE player_items SET %s=%s+$2,updated_at=NOW() WHERE user_id=$1`, column, column)
		if _, err = tx.Exec(ctx, query, userID, amount); err != nil {
			return Inventory{}, false, err
		}
	}
	var inventory Inventory
	if err = tx.QueryRow(ctx, `SELECT bombs,ice,experience,freeze_remaining FROM player_items WHERE user_id=$1`, userID).
		Scan(&inventory.Bombs, &inventory.Ice, &inventory.Experience, &inventory.FreezeRemaining); err != nil {
		return Inventory{}, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return Inventory{}, false, err
	}
	return inventory, claimed, nil
}

func (w *Writer) ActivateIce(ctx context.Context, userID string) (Inventory, bool, error) {
	if _, err := w.pool.Exec(ctx, `INSERT INTO player_items(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return Inventory{}, false, err
	}
	var inventory Inventory
	err := w.pool.QueryRow(ctx, `UPDATE player_items SET ice=ice-1,freeze_remaining=freeze_remaining+1,updated_at=NOW()
WHERE user_id=$1 AND ice>0 AND freeze_remaining=0 RETURNING bombs,ice,experience,freeze_remaining`, userID).
		Scan(&inventory.Bombs, &inventory.Ice, &inventory.Experience, &inventory.FreezeRemaining)
	if err == pgx.ErrNoRows {
		current, loadErr := w.Inventory(ctx, userID)
		return current, false, loadErr
	}
	return inventory, err == nil, err
}

func (w *Writer) ConsumeBomb(ctx context.Context, userID string) (Inventory, bool, error) {
	if _, err := w.pool.Exec(ctx, `INSERT INTO player_items(user_id) VALUES($1) ON CONFLICT DO NOTHING`, userID); err != nil {
		return Inventory{}, false, err
	}
	var inventory Inventory
	err := w.pool.QueryRow(ctx, `UPDATE player_items SET bombs=bombs-1,updated_at=NOW()
WHERE user_id=$1 AND bombs>0 RETURNING bombs,ice,experience,freeze_remaining`, userID).
		Scan(&inventory.Bombs, &inventory.Ice, &inventory.Experience, &inventory.FreezeRemaining)
	if err == pgx.ErrNoRows {
		current, loadErr := w.Inventory(ctx, userID)
		return current, false, loadErr
	}
	return inventory, err == nil, err
}

func (w *Writer) RefundBomb(ctx context.Context, userID string) error {
	_, err := w.pool.Exec(ctx, `UPDATE player_items SET bombs=bombs+1,updated_at=NOW() WHERE user_id=$1`, userID)
	return err
}

func (w *Writer) ConsumeFreezeCharge(ctx context.Context, userID string) (bool, error) {
	command, err := w.pool.Exec(ctx, `UPDATE player_items SET freeze_remaining=freeze_remaining-1,updated_at=NOW()
WHERE user_id=$1 AND freeze_remaining>0`, userID)
	return err == nil && command.RowsAffected() == 1, err
}

func (w *Writer) RefundFreezeCharge(ctx context.Context, userID string) error {
	_, err := w.pool.Exec(ctx, `UPDATE player_items SET freeze_remaining=freeze_remaining+1,updated_at=NOW() WHERE user_id=$1`, userID)
	return err
}

func (w *Writer) LoadBoardSize(ctx context.Context, boardID string) (BoardSize, error) {
	var size BoardSize
	err := w.pool.QueryRow(ctx, `SELECT width, height FROM board_settings WHERE board_id=$1`, boardID).Scan(&size.Width, &size.Height)
	return size, err
}

func (w *Writer) SaveBoardSize(ctx context.Context, boardID string, size BoardSize) error {
	_, err := w.pool.Exec(ctx, `INSERT INTO board_settings(board_id,width,height,updated_at)
VALUES($1,$2,$3,NOW()) ON CONFLICT(board_id) DO UPDATE SET width=EXCLUDED.width,height=EXCLUDED.height,updated_at=EXCLUDED.updated_at`, boardID, size.Width, size.Height)
	return err
}

func (w *Writer) SaveBoardBackup(ctx context.Context, backupID, boardID string, size BoardSize, pixels []domain.BoardPixel) error {
	raw, err := json.Marshal(pixels)
	if err != nil {
		return err
	}
	_, err = w.pool.Exec(ctx, `INSERT INTO board_clear_backups(backup_id,board_id,width,height,pixels,created_at)
VALUES($1,$2,$3,$4,$5,NOW())`, backupID, boardID, size.Width, size.Height, raw)
	return err
}

func (w *Writer) LoadBoardBackup(ctx context.Context, backupID, boardID string) (BoardBackup, error) {
	var backup BoardBackup
	var raw []byte
	backup.ID = backupID
	err := w.pool.QueryRow(ctx, `SELECT width,height,pixels FROM board_clear_backups WHERE backup_id=$1 AND board_id=$2`, backupID, boardID).
		Scan(&backup.Width, &backup.Height, &raw)
	if err != nil {
		return BoardBackup{}, err
	}
	if err := json.Unmarshal(raw, &backup.Pixels); err != nil {
		return BoardBackup{}, err
	}
	return backup, nil
}

func (w *Writer) MarkBoardBackupRestored(ctx context.Context, backupID string) error {
	_, err := w.pool.Exec(ctx, `UPDATE board_clear_backups SET restored_at=NOW() WHERE backup_id=$1`, backupID)
	return err
}

func (w *Writer) UpsertProfile(ctx context.Context, profile domain.PixelAuthor) error {
	if profile.ID == "" {
		return nil
	}
	_, err := w.pool.Exec(ctx, `INSERT INTO profiles(telegram_id,display_name,username,photo_url,first_seen_at,updated_at)
VALUES($1,$2,$3,$4,NOW(),NOW())
ON CONFLICT(telegram_id) DO UPDATE SET
 display_name=EXCLUDED.display_name,
 username=EXCLUDED.username,
 photo_url=EXCLUDED.photo_url,
 updated_at=NOW()`, profile.ID, profile.DisplayName, profile.Username, profile.PhotoURL)
	return err
}

func (w *Writer) Profile(ctx context.Context, telegramID string) (domain.PixelAuthor, error) {
	var profile domain.PixelAuthor
	err := w.pool.QueryRow(ctx, `SELECT telegram_id,display_name,username,photo_url FROM profiles WHERE telegram_id=$1`, telegramID).
		Scan(&profile.ID, &profile.DisplayName, &profile.Username, &profile.PhotoURL)
	return profile, err
}

func (w *Writer) Prizes(ctx context.Context, telegramID string) (json.RawMessage, error) {
	var prizes []byte
	err := w.pool.QueryRow(ctx, `SELECT prizes FROM profiles WHERE telegram_id=$1`, telegramID).Scan(&prizes)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(prizes), nil
}

func (w *Writer) ResetTrophies(ctx context.Context, telegramID string) (bool, error) {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var stateID int
	if err := tx.QueryRow(ctx, `SELECT id FROM trophy_drop_state WHERE id=1 FOR UPDATE`).Scan(&stateID); err != nil {
		return false, err
	}
	rows, err := tx.Query(ctx, `SELECT trophy_id FROM trophy_nft_winners WHERE user_id=$1`, telegramID)
	if err != nil {
		return false, err
	}
	var plannedTrophies []string
	for rows.Next() {
		var trophyID string
		if err := rows.Scan(&trophyID); err != nil {
			rows.Close()
			return false, err
		}
		plannedTrophies = append(plannedTrophies, trophyID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	rows.Close()
	tag, err := tx.Exec(ctx, `UPDATE profiles SET prizes='[]'::jsonb,updated_at=NOW() WHERE telegram_id=$1`, telegramID)
	if err != nil || tag.RowsAffected() != 1 {
		return false, err
	}
	for _, trophyID := range plannedTrophies {
		if _, err := tx.Exec(ctx, `UPDATE trophy_nft_plan SET claimed_at=NULL,winner_user_id=NULL WHERE trophy_id=$1`, trophyID); err != nil {
			return false, err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM trophy_nft_winners WHERE trophy_id=$1`, trophyID); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

type TrophyPrize struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	CollectedParts int    `json:"collectedParts"`
	Total          int    `json:"total"`
}

type TrophyClaim struct {
	TrophyPrize
	UserID string
}

type NFTPlanStatus struct {
	StartsAt time.Time
	EndsAt   time.Time
	Total    int
	Claimed  int
}

type trophyDefinition struct {
	TrophyPrize
	Weight       int
	Cap          int64
	RewardItem   string
	RewardAmount int64
}

var trophyDefinitions = []trophyDefinition{
	{TrophyPrize: TrophyPrize{ID: "experience", Name: "Опыт", Total: 1}, Weight: 100, Cap: 50, RewardItem: "experience", RewardAmount: 25},
	{TrophyPrize: TrophyPrize{ID: "bomb", Name: "Бомба", Total: 1}, Weight: 100, Cap: 50, RewardItem: "bomb", RewardAmount: 1},
	{TrophyPrize: TrophyPrize{ID: "ice", Name: "Заморозка", Total: 1}, Weight: 100, Cap: 50, RewardItem: "ice", RewardAmount: 1},
	{TrophyPrize: TrophyPrize{ID: "stickers", Name: "Стикеры", Total: 2}, Weight: 100, Cap: 50},
	{TrophyPrize: TrophyPrize{ID: "yng-explrz", Name: "YNG EXPLRZ", Total: 2}, Weight: 100, Cap: 50},
	{TrophyPrize: TrophyPrize{ID: "besigned", Name: "BeSigned", Total: 2}, Weight: 100, Cap: 50},
	{TrophyPrize: TrophyPrize{ID: "bear", Name: "Мишка", Total: 2}, Weight: 30, Cap: 5},
	{TrophyPrize: TrophyPrize{ID: "liberty-figure-252202", Name: "LibertyFigure #252202", Total: 4}, Weight: 4, Cap: 1},
	{TrophyPrize: TrophyPrize{ID: "candy-cane-162605", Name: "CandyCane #162605", Total: 4}, Weight: 4, Cap: 1},
	{TrophyPrize: TrophyPrize{ID: "vice-cream-227533", Name: "ViceCream #227533", Total: 4}, Weight: 4, Cap: 1},
	{TrophyPrize: TrophyPrize{ID: "vice-cream-428029", Name: "ViceCream #428029", Total: 4}, Weight: 4, Cap: 1},
	{TrophyPrize: TrophyPrize{ID: "chill-flame-303522", Name: "ChillFlame #303522", Total: 4}, Weight: 4, Cap: 1},
}

var trophyLocation = time.FixedZone("Asia/Yekaterinburg", 5*60*60)

const (
	nftPartsPerCampaign = 20
	nftPersonalCooldown = 45 * time.Minute
)

func nftInventoryDropChance(counts map[string]int) float64 {
	total := 0
	for _, definition := range trophyDefinitions {
		if definition.Cap == 1 {
			total += counts[definition.ID]
		}
	}
	switch {
	case total == 0:
		return 1
	case total == 1:
		return 0.55
	case total == 2:
		return 0.35
	case total == 3:
		return 0.22
	case total <= 5:
		return 0.12
	default:
		return 0.05
	}
}

func shuffledNFTOutcomes(partsPerNFT int) []string {
	outcomes := make([]string, 0, 5*partsPerNFT)
	for _, definition := range trophyDefinitions {
		if definition.Cap != 1 {
			continue
		}
		for range partsPerNFT {
			outcomes = append(outcomes, definition.ID)
		}
	}
	rand.Shuffle(len(outcomes), func(i, j int) { outcomes[i], outcomes[j] = outcomes[j], outcomes[i] })
	return outcomes
}

// TrophyDropChance returns the chance for one successful placement. A quiet
// board and off-peak hours deliberately make solo farming inefficient.
func TrophyDropChance(now time.Time, online int64) float64 {
	onlineMultiplier := 1.0
	switch {
	case online <= 1:
		onlineMultiplier = 0.15
	case online <= 4:
		onlineMultiplier = 0.35
	case online <= 9:
		onlineMultiplier = 0.65
	case online <= 24:
		onlineMultiplier = 1
	case online <= 49:
		onlineMultiplier = 1.25
	default:
		onlineMultiplier = 1.5
	}

	hour := now.In(trophyLocation).Hour()
	timeMultiplier := 0.35
	switch {
	case hour >= 6 && hour < 11:
		timeMultiplier = 0.55
	case hour >= 11 && hour < 18:
		timeMultiplier = 1.15
	case hour >= 18 && hour < 23:
		timeMultiplier = 1.35
	}
	return 0.008 * onlineMultiplier * timeMultiplier
}

func (w *Writer) TrophyDropForced(ctx context.Context) (bool, error) {
	var forced bool
	err := w.pool.QueryRow(ctx, `SELECT force_next FROM trophy_drop_state WHERE id=1`).Scan(&forced)
	return forced, err
}

// ForceNextTrophyDrop makes the next eligible pixel placement bypass the
// probability roll once, while rarity and supply limits remain in force.
func (w *Writer) ForceNextTrophyDrop(ctx context.Context) (bool, error) {
	var forced bool
	err := w.pool.QueryRow(ctx, `UPDATE trophy_drop_state SET force_next=true WHERE id=1 RETURNING force_next`).Scan(&forced)
	return forced, err
}

// EnsureNFTDropPlan records a full seven-day NFT competition before any new
// parts are awarded. Each NFT gets enough opportunities for several players
// to compete; its final four opportunities form a completion runway.
func (w *Writer) EnsureNFTDropPlan(ctx context.Context, now time.Time) error {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	startsAt := now.UTC()
	endsAt := startsAt.Add(7 * 24 * time.Hour)
	if _, err := tx.Exec(ctx, `INSERT INTO trophy_nft_campaign(id,starts_at,ends_at) VALUES(1,$1,$2) ON CONFLICT(id) DO NOTHING`, startsAt, endsAt); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT starts_at,ends_at FROM trophy_nft_campaign WHERE id=1 FOR UPDATE`).Scan(&startsAt, &endsAt); err != nil {
		return err
	}
	var existing int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM trophy_nft_plan`).Scan(&existing); err != nil {
		return err
	}
	targetCount := 0
	for _, definition := range trophyDefinitions {
		if definition.Cap == 1 {
			targetCount += nftPartsPerCampaign
		}
	}
	if existing >= targetCount {
		return tx.Commit(ctx)
	}

	counts := make(map[string]int)
	rows, err := tx.Query(ctx, `SELECT trophy_id,COUNT(*) FROM trophy_nft_plan GROUP BY trophy_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var trophyID string
		var count int
		if err := rows.Scan(&trophyID, &count); err != nil {
			rows.Close()
			return err
		}
		counts[trophyID] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	outcomes := make([]string, 0, targetCount-existing)
	for _, definition := range trophyDefinitions {
		if definition.Cap != 1 {
			continue
		}
		for count := counts[definition.ID]; count < nftPartsPerCampaign; count++ {
			outcomes = append(outcomes, definition.ID)
		}
	}
	rand.Shuffle(len(outcomes), func(i, j int) { outcomes[i], outcomes[j] = outcomes[j], outcomes[i] })

	scheduleStart := startsAt
	if now.After(scheduleStart) {
		scheduleStart = now.UTC()
	}
	duration := endsAt.Sub(scheduleStart)
	if duration <= time.Hour {
		duration = time.Hour
		endsAt = scheduleStart.Add(duration)
		if _, err := tx.Exec(ctx, `UPDATE trophy_nft_campaign SET ends_at=$1 WHERE id=1`, endsAt); err != nil {
			return err
		}
	}
	for index, trophyID := range outcomes {
		slotStart := duration * time.Duration(index) / time.Duration(len(outcomes))
		slotEnd := duration * time.Duration(index+1) / time.Duration(len(outcomes))
		releaseAt := scheduleStart.Add(slotStart + time.Duration(rand.Float64()*float64(slotEnd-slotStart)))
		if index == len(outcomes)-1 {
			releaseAt = endsAt.Add(-time.Hour)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO trophy_nft_plan(sequence,trophy_id,release_at) VALUES($1,$2,$3)`, existing+index+1, trophyID, releaseAt); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (w *Writer) NFTDropPlanStatus(ctx context.Context) (NFTPlanStatus, error) {
	var status NFTPlanStatus
	err := w.pool.QueryRow(ctx, `
SELECT campaign.starts_at,campaign.ends_at,COUNT(plan.sequence),COUNT(plan.claimed_at)
FROM trophy_nft_campaign AS campaign
LEFT JOIN trophy_nft_plan AS plan ON true
WHERE campaign.id=1
GROUP BY campaign.starts_at,campaign.ends_at`).Scan(&status.StartsAt, &status.EndsAt, &status.Total, &status.Claimed)
	return status, err
}

func (w *Writer) ClaimTrophyPart(ctx context.Context, userID string, now time.Time, online int64) (*TrophyClaim, error) {
	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var forced bool
	if err := tx.QueryRow(ctx, `SELECT force_next FROM trophy_drop_state WHERE id=1 FOR UPDATE`).Scan(&forced); err != nil {
		return nil, err
	}
	var plannedSequence int
	var plannedID string
	plannedErr := tx.QueryRow(ctx, `
SELECT sequence,trophy_id FROM trophy_nft_plan
WHERE claimed_at IS NULL AND release_at <= $1
ORDER BY release_at,sequence LIMIT 1 FOR UPDATE`, now).Scan(&plannedSequence, &plannedID)
	planned := plannedErr == nil
	if plannedErr != nil && plannedErr != pgx.ErrNoRows {
		return nil, plannedErr
	}
	if !planned && !forced && rand.Float64() >= TrophyDropChance(now, online) {
		return nil, tx.Commit(ctx)
	}

	recipientID := userID
	plannedRemaining := 0
	if planned {
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM trophy_nft_plan WHERE trophy_id=$1 AND claimed_at IS NULL`, plannedID).Scan(&plannedRemaining); err != nil {
			return nil, err
		}
		if plannedRemaining <= 4 {
			var leaderID string
			err := tx.QueryRow(ctx, `
SELECT telegram_id
FROM profiles
CROSS JOIN LATERAL jsonb_array_elements(prizes) AS prize
WHERE prize->>'id'=$1
  AND jsonb_typeof(prize->'collectedParts')='number'
  AND (prize->>'collectedParts')::int > 0
  AND (prize->>'collectedParts')::int < $2
ORDER BY (prize->>'collectedParts')::int DESC,updated_at ASC
LIMIT 1`, plannedID, 4).Scan(&leaderID)
			if err == nil {
				recipientID = leaderID
			} else if err != pgx.ErrNoRows {
				return nil, err
			}
		}
	}

	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT prizes FROM profiles WHERE telegram_id=$1 FOR UPDATE`, recipientID).Scan(&raw); err != nil {
		return nil, err
	}
	var prizes []map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &prizes); err != nil {
			return nil, fmt.Errorf("decode trophy prizes: %w", err)
		}
	}
	counts := make(map[string]int, len(trophyDefinitions))
	indexes := make(map[string]int, len(trophyDefinitions))
	for index, prize := range prizes {
		id, _ := prize["id"].(string)
		value, _ := prize["collectedParts"].(float64)
		counts[id] = int(value)
		indexes[id] = index
	}
	if planned {
		var latestNFTDrop time.Time
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(claimed_at),to_timestamp(0)) FROM trophy_nft_plan WHERE winner_user_id=$1`, recipientID).Scan(&latestNFTDrop); err != nil {
			return nil, err
		}
		if now.Sub(latestNFTDrop) < nftPersonalCooldown {
			return nil, tx.Commit(ctx)
		}
		if plannedRemaining > 4 && rand.Float64() >= nftInventoryDropChance(counts) {
			return nil, tx.Commit(ctx)
		}
	}
	completed := make(map[string]int64, len(trophyDefinitions))
	rows, err := tx.Query(ctx, `
SELECT prize->>'id',COUNT(*)
FROM profiles
CROSS JOIN LATERAL jsonb_array_elements(prizes) AS prize
WHERE jsonb_typeof(prize->'collectedParts')='number'
  AND jsonb_typeof(prize->'total')='number'
  AND (prize->>'collectedParts')::int >= (prize->>'total')::int
GROUP BY prize->>'id'`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var count int64
		if err := rows.Scan(&id, &count); err != nil {
			rows.Close()
			return nil, err
		}
		completed[id] = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	candidates := make([]trophyDefinition, 0, len(trophyDefinitions))
	for _, definition := range trophyDefinitions {
		if planned && definition.ID != plannedID {
			continue
		}
		if !planned && definition.Cap == 1 {
			continue
		}
		if counts[definition.ID] < definition.Total && completed[definition.ID] < definition.Cap {
			candidates = append(candidates, definition)
		}
	}
	if len(candidates) == 0 {
		if planned {
			if _, err := tx.Exec(ctx, `
UPDATE trophy_nft_plan
SET claimed_at=$2,
    winner_user_id=COALESCE((SELECT user_id FROM trophy_nft_winners WHERE trophy_id=$1),winner_user_id)
WHERE trophy_id=$1 AND claimed_at IS NULL`, plannedID, now); err != nil {
				return nil, err
			}
		}
		if forced {
			if _, err := tx.Exec(ctx, `UPDATE trophy_drop_state SET force_next=false WHERE id=1`); err != nil {
				return nil, err
			}
		}
		return nil, tx.Commit(ctx)
	}
	totalWeight := 0
	for _, candidate := range candidates {
		totalWeight += candidate.Weight
	}
	draw := rand.IntN(totalWeight)
	won := candidates[0]
	for _, candidate := range candidates {
		if draw < candidate.Weight {
			won = candidate
			break
		}
		draw -= candidate.Weight
	}
	won.CollectedParts = counts[won.ID] + 1
	if index, ok := indexes[won.ID]; ok {
		prizes[index]["name"] = won.Name
		prizes[index]["collectedParts"] = won.CollectedParts
		prizes[index]["total"] = won.Total
	} else {
		prizes = append(prizes, map[string]any{"id": won.ID, "name": won.Name, "collectedParts": won.CollectedParts, "total": won.Total})
	}
	updated, err := json.Marshal(prizes)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE profiles SET prizes=$2,updated_at=NOW() WHERE telegram_id=$1`, recipientID, updated); err != nil {
		return nil, err
	}
	if won.RewardItem != "" && won.CollectedParts >= won.Total {
		column := won.RewardItem
		if column != "bomb" && column != "ice" && column != "experience" {
			return nil, fmt.Errorf("unknown trophy reward item %q", column)
		}
		query := fmt.Sprintf(`INSERT INTO player_items(user_id,%s,updated_at) VALUES($1,$2,NOW())
ON CONFLICT(user_id) DO UPDATE SET %s=player_items.%s+EXCLUDED.%s,updated_at=NOW()`, column, column, column, column)
		if _, err := tx.Exec(ctx, query, recipientID, won.RewardAmount); err != nil {
			return nil, err
		}
	}
	if planned && won.Cap == 1 && won.CollectedParts >= won.Total {
		if _, err := tx.Exec(ctx, `INSERT INTO trophy_nft_winners(trophy_id,user_id,assigned_at) VALUES($1,$2,$3) ON CONFLICT(trophy_id) DO NOTHING`, won.ID, recipientID, now); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE trophy_nft_plan SET claimed_at=$2,winner_user_id=$3 WHERE trophy_id=$1 AND claimed_at IS NULL`, won.ID, now, recipientID); err != nil {
			return nil, err
		}
	} else if planned {
		if _, err := tx.Exec(ctx, `UPDATE trophy_nft_plan SET claimed_at=$2,winner_user_id=$3 WHERE sequence=$1`, plannedSequence, now, recipientID); err != nil {
			return nil, err
		}
	}
	if forced {
		if _, err := tx.Exec(ctx, `UPDATE trophy_drop_state SET force_next=false WHERE id=1`); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &TrophyClaim{TrophyPrize: won.TrophyPrize, UserID: recipientID}, nil
}

func (w *Writer) LoadSnapshot(ctx context.Context, boardID string) ([]domain.BoardPixel, error) {
	var raw []byte
	err := w.pool.QueryRow(ctx, `SELECT pixels FROM board_snapshots WHERE board_id=$1`, boardID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var pixels []domain.BoardPixel
	if err := json.Unmarshal(raw, &pixels); err != nil {
		return nil, err
	}
	return pixels, nil
}

// A snapshot is a complete checkpoint (including clears and admin paint).
// Overlay only events beyond its explicit watermark, never old board rows.
func (w *Writer) RestoreBoard(ctx context.Context, boardID string) ([]domain.BoardPixel, int64, error) {
	var raw []byte
	var watermark *int64
	err := w.pool.QueryRow(ctx, `SELECT pixels,event_version FROM board_snapshots WHERE board_id=$1`, boardID).Scan(&raw, &watermark)
	if err != nil && err != pgx.ErrNoRows {
		return nil, 0, err
	}
	var pixels []domain.BoardPixel
	if err == nil {
		if err := json.Unmarshal(raw, &pixels); err != nil {
			return nil, 0, err
		}
		if watermark == nil {
			// Legacy snapshots are authoritative as before. Overlaying rows without
			// a checkpoint watermark could resurrect cleared or restored pixels.
			log.Printf("WARNING: board %q uses a legacy snapshot without an event watermark; restoring snapshot only, later persisted placements may be absent", boardID)
			var maximum int64
			if err := w.pool.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM pixel_events WHERE board_id=$1`, boardID).Scan(&maximum); err != nil {
				return nil, 0, err
			}
			for _, p := range pixels {
				maximum = max(maximum, p.Version)
			}
			return pixels, maximum, nil
		}
	}
	var floor int64
	if watermark != nil {
		floor = *watermark
	}
	cells := make(map[[2]int]domain.BoardPixel, len(pixels))
	for _, pixel := range pixels {
		cells[[2]int{pixel.X, pixel.Y}] = pixel
	}
	rows, err := w.pool.Query(ctx, `SELECT b.x,b.y,b.color,b.version,b.updated_by,COALESCE(p.display_name,''),COALESCE(p.username,''),COALESCE(p.photo_url,''),b.frozen_until FROM board_pixels b LEFT JOIN profiles p ON p.telegram_id=b.updated_by WHERE b.board_id=$1 AND b.version>$2`, boardID, floor)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var p domain.BoardPixel
		if err := rows.Scan(&p.X, &p.Y, &p.Color, &p.Version, &p.Author.ID, &p.Author.DisplayName, &p.Author.Username, &p.Author.PhotoURL, &p.FrozenUntil); err != nil {
			return nil, 0, err
		}
		cells[[2]int{p.X, p.Y}] = p
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	rows.Close()
	var maximum int64
	if err := w.pool.QueryRow(ctx, `SELECT COALESCE(MAX(version),0) FROM pixel_events WHERE board_id=$1`, boardID).Scan(&maximum); err != nil {
		return nil, 0, err
	}
	maximum = max(maximum, floor)
	pixels = make([]domain.BoardPixel, 0, len(cells))
	for _, p := range cells {
		pixels = append(pixels, p)
		maximum = max(maximum, p.Version)
	}
	return pixels, maximum, nil
}

func (w *Writer) UserStats(ctx context.Context, userID string) (UserStats, error) {
	var stats UserStats
	err := w.pool.QueryRow(ctx, `
WITH daily_cutoff AS (
 SELECT GREATEST(
   date_trunc('day',NOW() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
   COALESCE((SELECT reset_at FROM daily_quest_resets WHERE scope='all'), '-infinity'::timestamptz),
   COALESCE((SELECT reset_at FROM daily_quest_resets WHERE scope='user:' || $1), '-infinity'::timestamptz)
 ) AS value
)
SELECT
  (SELECT COUNT(*) FROM pixel_events WHERE user_id=$1),
  COALESCE((SELECT experience FROM player_items WHERE user_id=$1),0),
  (SELECT COUNT(*) FROM pixel_events current_event
   WHERE current_event.user_id=$1 AND EXISTS (
     SELECT 1 FROM pixel_events previous_event
     WHERE previous_event.board_id=current_event.board_id
       AND previous_event.x=current_event.x
       AND previous_event.y=current_event.y
       AND previous_event.version<current_event.version
   )),
  (SELECT COUNT(*) FROM board_pixels WHERE updated_by=$1),
  (SELECT COUNT(*) FROM pixel_events WHERE user_id=$1 AND created_at>=(SELECT value FROM daily_cutoff)),
  (SELECT COUNT(*) FROM pixel_events current_event
   WHERE current_event.user_id=$1
     AND current_event.created_at>=(SELECT value FROM daily_cutoff)
     AND EXISTS (
       SELECT 1 FROM pixel_events previous_event
       WHERE previous_event.board_id=current_event.board_id
         AND previous_event.x=current_event.x
         AND previous_event.y=current_event.y
         AND previous_event.version<current_event.version
     )),
  (SELECT COUNT(DISTINCT color) FROM pixel_events WHERE user_id=$1 AND created_at>=(SELECT value FROM daily_cutoff)),
  (SELECT COUNT(DISTINCT (board_id,x,y)) FROM pixel_events WHERE user_id=$1 AND created_at>=(SELECT value FROM daily_cutoff))`, userID).Scan(
		&stats.PlacedPixels,
		&stats.BonusExperience,
		&stats.RepaintedPixels,
		&stats.CurrentPixels,
		&stats.DailyPlacedPixels,
		&stats.DailyRepaintedPixels,
		&stats.DailyColorsUsed,
		&stats.DailyUniqueCells,
	)
	return stats, err
}

func (w *Writer) ResetDailyQuests(ctx context.Context, userID string) error {
	scope := "all"
	if userID != "" {
		scope = "user:" + userID
	}
	_, err := w.pool.Exec(ctx, `INSERT INTO daily_quest_resets(scope,reset_at) VALUES($1,NOW())
ON CONFLICT(scope) DO UPDATE SET reset_at=EXCLUDED.reset_at`, scope)
	return err
}

func (w *Writer) WriteSnapshot(ctx context.Context, boardID string, pixels []domain.BoardPixel, watermark int64, size BoardSize) error {
	raw, err := json.Marshal(pixels)
	if err != nil {
		return err
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	_, err = tx.Exec(ctx, `INSERT INTO board_snapshots(board_id, version, pixels, updated_at,event_version)
VALUES($1,1,$2,NOW(),$3)
ON CONFLICT(board_id) DO UPDATE SET version=board_snapshots.version+1,pixels=EXCLUDED.pixels,updated_at=NOW(),event_version=EXCLUDED.event_version`, boardID, raw, watermark)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO board_settings(board_id,width,height,updated_at) VALUES($1,$2,$3,NOW()) ON CONFLICT(board_id) DO UPDATE SET width=EXCLUDED.width,height=EXCLUDED.height,updated_at=NOW()`, boardID, size.Width, size.Height)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (w *Writer) WriteBatch(ctx context.Context, events []domain.PixelEvent) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, event := range events {
		tag, err := tx.Exec(ctx, `INSERT INTO pixel_events(event_id,operation_id,board_id,x,y,color,user_id,version,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, event.EventID, event.OperationID, event.BoardID, event.X, event.Y, event.Color, event.UserID, event.Version, event.CreatedAt)
		if err != nil {
			return err
		}
		// A duplicate must not mutate profiles or the materialized board either.
		if tag.RowsAffected() == 0 {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO profiles(telegram_id,display_name,username,photo_url,first_seen_at,updated_at)
VALUES($1,$2,$3,$4,NOW(),NOW())
ON CONFLICT(telegram_id) DO UPDATE SET display_name=EXCLUDED.display_name,username=EXCLUDED.username,photo_url=EXCLUDED.photo_url,updated_at=NOW()`, event.Author.ID, event.Author.DisplayName, event.Author.Username, event.Author.PhotoURL)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO board_pixels(board_id,x,y,color,version,updated_by,updated_at,frozen_until)
VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(board_id,x,y) DO UPDATE SET color=excluded.color,version=excluded.version,updated_by=excluded.updated_by,updated_at=excluded.updated_at,frozen_until=excluded.frozen_until WHERE board_pixels.version < excluded.version`, event.BoardID, event.X, event.Y, event.Color, event.Version, event.UserID, event.CreatedAt, event.FrozenUntil)
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func RunMemoryBatcher(ctx context.Context, input <-chan domain.PixelEvent, writer *Writer) {
	var write func(context.Context, []domain.PixelEvent) error
	if writer != nil {
		write = writer.WriteBatch
	}
	runMemoryBatcher(ctx, input, write)
}

func runMemoryBatcher(ctx context.Context, input <-chan domain.PixelEvent, write func(context.Context, []domain.PixelEvent) error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	batch := make([]domain.PixelEvent, 0, 500)
	flush := func(flushCtx context.Context) bool {
		if len(batch) > 0 {
			if write != nil {
				for {
					if err := write(flushCtx, batch); err == nil {
						break
					} else {
						log.Printf("memory batch write failed; retaining %d events: %v", len(batch), err)
					}
					select {
					case <-flushCtx.Done():
						return false
					case <-time.After(time.Second):
					}
				}
			}
			batch = batch[:0]
		}
		return true
	}
	for {
		select {
		case event, ok := <-input:
			if !ok {
				flush(ctx)
				return
			}
			batch = append(batch, event)
			if len(batch) >= 500 {
				flush(ctx)
			}
		case <-ticker.C:
			flush(ctx)
		case <-ctx.Done():
			finalCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if !flush(finalCtx) {
				log.Printf("shutdown: %d in-memory events remain unpersisted", len(batch))
			}
			cancel()
			return
		}
	}
}
