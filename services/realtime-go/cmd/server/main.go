package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"pixelbattle/realtime/internal/access"
	"pixelbattle/realtime/internal/auth"
	"pixelbattle/realtime/internal/domain"
	"pixelbattle/realtime/internal/persistence"
	"pixelbattle/realtime/internal/queue"
	"pixelbattle/realtime/internal/realtime"
	"pixelbattle/realtime/internal/state"
)

var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
var itemPalette = []string{
	"#FF8080", "#FFCA73", "#FBFFA5", "#7CFF80", "#7EFFF2", "#84D0FF", "#8290FF", "#CD81FF", "#FF80D0", "#FDFDFD",
	"#FF0000", "#FF9D00", "#F2FF00", "#00FF07", "#00FFE6", "#009DFF", "#001EFF", "#9900FF", "#FF00A1", "#8A8A8A",
	"#870000", "#8D4E00", "#B6A700", "#009904", "#009687", "#00568C", "#001194", "#53008A", "#8E005A", "#000000",
}

const defaultBoardSize = 150
const placementCooldown = 5 * time.Second

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := realtime.NewHub()
	presence := newAppPresence(10 * time.Second)
	cooldown := realtime.NewCooldown()
	memoryQueue := queue.NewMemory()
	var eventQueue queue.EventQueue = memoryQueue
	var writer *persistence.Writer
	var redisQueue *queue.RedisQueue
	boardStore := state.NewBoardStore()
	boardWidth := atomic.Int64{}
	boardHeight := atomic.Int64{}
	boardWidth.Store(defaultBoardSize)
	boardHeight.Store(defaultBoardSize)
	boardCache := &boardSnapshotCache{}
	inlineImageCache := struct {
		sync.Mutex
		data      []byte
		expiresAt time.Time
	}{}
	adminAPIToken := env("ADMIN_API_TOKEN", "")
	requestLimits := &rateLimiter{}
	socketLimits := &connectionLimits{}
	var boardMu sync.Mutex
	var stopping atomic.Bool
	var version atomic.Int64
	startupCtx, startupCancel := context.WithTimeout(ctx, 60*time.Second)
	defer startupCancel()
	adminIDs, err := access.ParseAdminIDs(os.Getenv("TELEGRAM_ADMIN_IDS"))
	if err != nil {
		log.Fatal(err)
	}
	trustedProxies, err := parseTrustedProxies(os.Getenv("TRUSTED_PROXY_CIDRS"))
	if err != nil {
		log.Fatal(err)
	}
	devMemory := os.Getenv("GO_DEV_IN_MEMORY") == "true"
	redisURL := env("REDIS_URL", "redis://localhost:6379/0")
	accessURL := redisURL
	if devMemory {
		accessURL = ""
		log.Print("WARNING: explicit development in-memory mode; placements are not durable")
	}
	accessStore, err := access.New(startupCtx, accessURL, adminIDs)
	if err != nil {
		log.Fatalf("access store startup failed: %v", err)
	}
	defer accessStore.Close()
	go accessStore.Run(ctx)

	if !devMemory {
		writer, err = persistence.NewWriter(startupCtx, env("POSTGRES_DSN", "postgres://pixelbattle:pixelbattle@localhost:5432/pixelbattle?sslmode=disable"))
		if err != nil {
			log.Fatalf("postgres startup failed: %v", err)
		}
		defer writer.Close()
		if err := writer.AcquireLease(startupCtx); err != nil {
			log.Fatal(err)
		}
		if err := writer.Migrate(startupCtx); err != nil {
			log.Fatalf("migration failed: %v", err)
		}
		redisQueue, err = queue.NewRedis(redisURL)
		if err != nil {
			log.Fatalf("redis startup failed: %v", err)
		}
		defer redisQueue.Close()
		if err := redisQueue.Ready(startupCtx); err != nil {
			log.Fatalf("redis startup failed: %v", err)
		}
		if err := redisQueue.Recover(startupCtx, writer.WriteBatch); err != nil {
			log.Fatalf("queue recovery failed: %v", err)
		}
		eventQueue = redisQueue
		size, err := writer.LoadBoardSize(startupCtx, "main")
		if err != nil && err != pgx.ErrNoRows {
			log.Fatalf("board size load failed: %v", err)
		}
		if err == nil {
			if size.Width < 16 || size.Height < 16 || size.Width > 500 || size.Height > 500 {
				log.Fatal("invalid persisted board size")
			}
			boardWidth.Store(int64(size.Width))
			boardHeight.Store(int64(size.Height))
		}
		pixels, maximum, err := writer.RestoreBoard(startupCtx, "main")
		if err != nil {
			log.Fatalf("board restoration failed: %v", err)
		}
		for _, p := range pixels {
			if p.X < 0 || p.Y < 0 || p.X >= int(boardWidth.Load()) || p.Y >= int(boardHeight.Load()) || !colorPattern.MatchString(p.Color) {
				log.Fatal("invalid persisted board pixel; reconciliation required")
			}
		}
		boardStore.Restore("main", pixels)
		version.Store(maximum)
		go func() {
			if err := writer.MonitorLease(ctx); err != nil {
				log.Fatal(err)
			}
		}()
	}
	go persistence.RunMemoryBatcher(ctx, memoryQueue.Events, writer)
	if redisQueue != nil && writer != nil {
		go redisQueue.Consume(ctx, writer.WriteBatch)
	}
	if writer != nil {
		go snapshotLoop(ctx, &boardMu, func(ctx context.Context) error {
			return writer.WriteSnapshot(ctx, "main", boardStore.Snapshot("main"), version.Load(), persistence.BoardSize{Width: int(boardWidth.Load()), Height: int(boardHeight.Load())})
		})
	}

	checkpoint := func(ctx context.Context) error {
		return writer.WriteSnapshot(ctx, "main", boardStore.Snapshot("main"), version.Load(), persistence.BoardSize{Width: int(boardWidth.Load()), Height: int(boardHeight.Load())})
	}
	awardDueTrophy := func(ctx context.Context, userID string, author domain.PixelAuthor) *trophyAwardEvent {
		if writer == nil {
			return nil
		}
		prize, err := writer.ClaimDueTrophyPart(ctx, userID, time.Now().UTC())
		if err != nil {
			log.Printf("trophy drop failed for user=%s: %v", userID, err)
			return nil
		}
		if prize != nil {
			log.Printf("trophy part awarded: user=%s prize=%s part=%d/%d", userID, prize.ID, prize.CollectedParts, prize.Total)
			nickname := author.Username
			if nickname == "" {
				nickname = author.DisplayName
			}
			notification := &trophyAwardEvent{Type: "trophy_awarded", EventID: id(), UserID: userID, Nickname: nickname}
			payload, marshalErr := json.Marshal(notification)
			if marshalErr != nil {
				log.Printf("trophy notification encoding failed for user=%s: %v", userID, marshalErr)
				return nil
			}
			hub.Broadcast(payload)
			return notification
		}
		return nil
	}
	allowedOrigin := env("GO_ALLOWED_ORIGIN", "http://localhost:5173")
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == allowedOrigin || origin == "https://pixelbattle.redjex.bond"
	}}
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "service": "realtime-go"})
	})
	http.HandleFunc("/api/boards/main", func(w http.ResponseWriter, r *http.Request) {
		boardMu.Lock()
		defer boardMu.Unlock()
		// Board snapshots are protected just like WebSocket connections.
		if _, err := telegramUserFromRequest(r); err != nil {
			http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Cache-Control", "private, no-cache")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("compact") == "1" {
			raw, compressed, err := boardCache.Payload(boardStore, boardWidth.Load(), boardHeight.Load())
			if err != nil {
				http.Error(w, "failed to build board snapshot", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Vary", "Accept-Encoding")
			if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				_, _ = w.Write(compressed)
				return
			}
			_, _ = w.Write(raw)
			return
		}
		writeJSON(w, map[string]any{"id": "main", "width": boardWidth.Load(), "height": boardHeight.Load(), "pixels": publicSnapshot(boardStore.Snapshot("main"))})
	})
	http.HandleFunc("/api/boards/session", func(w http.ResponseWriter, r *http.Request) {
		telegramUser, err := telegramUserFromRequest(r)
		if err != nil {
			http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
			return
		}
		testMode := accessStore.IsTestMode()
		isAdmin := accessStore.IsAdmin(telegramUser.ID)
		if testMode && !isAdmin {
			writeJSON(w, map[string]any{"testMode": true, "isAdmin": false, "accessAllowed": false})
			return
		}
		online := presence.Touch(telegramUser.ID)
		if writer != nil {
			if err := writer.UpsertProfile(r.Context(), profileFromTelegram(telegramUser)); err != nil {
				log.Printf("profile upsert failed for user=%d: %v", telegramUser.ID, err)
			}
		}
		userCooldown := accessStore.CooldownFor(telegramUser.ID, placementCooldown)
		inventory := persistence.Inventory{}
		prizes := json.RawMessage("[]")
		if writer != nil {
			identity := strconv.FormatInt(telegramUser.ID, 10)
			inventory, _ = writer.Inventory(r.Context(), identity)
			if storedPrizes, err := writer.Prizes(r.Context(), identity); err == nil {
				prizes = storedPrizes
			}
		}
		writeJSON(w, map[string]any{"cooldownBypassed": userCooldown == 0, "cooldownMs": userCooldown.Milliseconds(), "paused": accessStore.IsPaused(), "inventory": inventory, "prizes": prizes, "online": online, "testMode": testMode, "isAdmin": isAdmin, "accessAllowed": true})
	})
	http.HandleFunc("/api/boards/main/rewards", func(w http.ResponseWriter, r *http.Request) {
		telegramUser, err := telegramUserFromRequest(r)
		if err != nil {
			http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
			return
		}
		if writer == nil {
			http.Error(w, "rewards unavailable", http.StatusServiceUnavailable)
			return
		}
		identity := strconv.FormatInt(telegramUser.ID, 10)
		stats, err := writer.UserStats(r.Context(), identity)
		if err != nil {
			http.Error(w, "failed to load player level", http.StatusServiceUnavailable)
			return
		}
		currentLevel := playerLevel(stats.PlacedPixels)
		if r.Method == http.MethodPost {
			var request struct {
				Level int `json:"level"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Level < 1 || request.Level > 100 {
				http.Error(w, "invalid reward level", http.StatusBadRequest)
				return
			}
			if request.Level > currentLevel {
				http.Error(w, "reward is locked", http.StatusConflict)
				return
			}
			item, amount := levelReward(request.Level)
			inventory, claimed, err := writer.ClaimLevelReward(r.Context(), identity, request.Level, item, amount)
			if err != nil {
				http.Error(w, "failed to claim reward", http.StatusServiceUnavailable)
				return
			}
			claimedLevels, _ := writer.ClaimedLevelRewards(r.Context(), identity)
			writeJSON(w, map[string]any{"currentLevel": currentLevel, "claimed": claimed, "claimedLevels": claimedLevels, "inventory": inventory})
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		claimedLevels, err := writer.ClaimedLevelRewards(r.Context(), identity)
		if err != nil {
			http.Error(w, "failed to load rewards", http.StatusServiceUnavailable)
			return
		}
		inventory, err := writer.Inventory(r.Context(), identity)
		if err != nil {
			http.Error(w, "failed to load inventory", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]any{"currentLevel": currentLevel, "claimedLevels": claimedLevels, "inventory": inventory})
	})
	http.HandleFunc("/api/boards/items/ice/activate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		telegramUser, err := telegramUserFromRequest(r)
		if err != nil {
			http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
			return
		}
		if writer == nil {
			http.Error(w, "inventory unavailable", http.StatusServiceUnavailable)
			return
		}
		inventory, activated, err := writer.ActivateIce(r.Context(), strconv.FormatInt(telegramUser.ID, 10))
		if err != nil {
			http.Error(w, "failed to activate ice", http.StatusServiceUnavailable)
			return
		}
		if !activated {
			w.WriteHeader(http.StatusConflict)
		}
		writeJSON(w, map[string]any{"activated": activated, "inventory": inventory})
	})
	http.HandleFunc("/api/boards/main/pixels", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		telegramUser, err := telegramUserFromRequest(r)
		if err != nil {
			log.Printf("http placement rejected: invalid Telegram initData")
			http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
			return
		}
		if writer != nil {
			if err := writer.UpsertProfile(r.Context(), profileFromTelegram(telegramUser)); err != nil {
				log.Printf("profile upsert failed for user=%d: %v", telegramUser.ID, err)
			}
		}
		if accessStore.IsPaused() {
			http.Error(w, "game paused", http.StatusLocked)
			return
		}
		var request domain.PlacementRequest
		boardMu.Lock()
		defer boardMu.Unlock()
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !operationIDPattern.MatchString(request.OperationID) || request.Type != "place_pixel" || request.BoardID != "main" || request.X < 0 || request.Y < 0 || request.X >= int(boardWidth.Load()) || request.Y >= int(boardHeight.Load()) || !colorPattern.MatchString(request.Color) {
			log.Printf("http placement rejected: decode=%v type=%q board=%q x=%d y=%d color=%q limits=%dx%d", err, request.Type, request.BoardID, request.X, request.Y, request.Color, boardWidth.Load(), boardHeight.Load())
			http.Error(w, "invalid pixel placement", http.StatusBadRequest)
			return
		}
		identity := strconv.FormatInt(telegramUser.ID, 10)
		now := time.Now().UTC()
		var existingFreeze *time.Time
		if current, ok := boardStore.Pixel(request.BoardID, request.X, request.Y); ok && current.FrozenUntil != nil && now.Before(*current.FrozenUntil) {
			if current.Author.ID != identity {
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusLocked)
				writeJSON(w, map[string]any{"code": "pixel_frozen", "frozenUntil": current.FrozenUntil})
				return
			}
			existingFreeze = current.FrozenUntil
		}
		if userCooldown := accessStore.CooldownFor(telegramUser.ID, placementCooldown); userCooldown > 0 {
			if allowed, retryAfter := cooldown.Allow(identity, request.BoardID, userCooldown, time.Now()); !allowed {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()))))
				http.Error(w, "placement cooldown", http.StatusTooManyRequests)
				return
			}
		}
		author := profileFromTelegram(telegramUser)
		frozenUntil := existingFreeze
		freezeChargeUsed := false
		if writer != nil && request.UseIce {
			freezeChargeUsed, err = writer.ConsumeFreezeCharge(r.Context(), identity)
			if err != nil {
				http.Error(w, "inventory unavailable", http.StatusServiceUnavailable)
				return
			}
			if freezeChargeUsed {
				expires := now.Add(10 * time.Minute)
				frozenUntil = &expires
			}
		}
		event := domain.PixelEvent{Type: "pixel_placed", EventID: id(), BoardID: request.BoardID, X: request.X, Y: request.Y, Color: request.Color, OperationID: "server:" + id(), UserID: identity, Author: author, Version: version.Add(1), CreatedAt: now, FrozenUntil: frozenUntil}
		if err := eventQueue.Append(r.Context(), event); err != nil {
			if freezeChargeUsed && writer != nil {
				_ = writer.RefundFreezeCharge(r.Context(), identity)
			}
			log.Printf("http placement queue failed: %v", err)
			http.Error(w, "queue unavailable", http.StatusServiceUnavailable)
			return
		}
		boardStore.Apply(event)
		trophyAward := awardDueTrophy(r.Context(), identity, author)
		if freezeChargeUsed && writer != nil {
			_ = checkpoint(r.Context())
		}
		log.Printf("http placement accepted: user=%d x=%d y=%d version=%d", telegramUser.ID, event.X, event.Y, event.Version)
		publicEvent := eventForClient(event)
		payload, _ := json.Marshal(publicEvent)
		hub.Broadcast(payload)
		publicEvent.TrophyAward = trophyAward
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, publicEvent)
	})
	http.HandleFunc("/api/boards/items/bomb/use", func(w http.ResponseWriter, r *http.Request) {
		boardMu.Lock()
		defer boardMu.Unlock()
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		telegramUser, err := telegramUserFromRequest(r)
		if err != nil {
			http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
			return
		}
		if writer == nil {
			http.Error(w, "inventory unavailable", http.StatusServiceUnavailable)
			return
		}
		if accessStore.IsPaused() {
			http.Error(w, "game paused", http.StatusLocked)
			return
		}
		var request struct {
			X           int    `json:"x"`
			Y           int    `json:"y"`
			Color       string `json:"color"`
			OperationID string `json:"operationId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.X < 0 || request.Y < 0 || request.X >= int(boardWidth.Load()) || request.Y >= int(boardHeight.Load()) || !colorPattern.MatchString(request.Color) || !operationIDPattern.MatchString(request.OperationID) {
			http.Error(w, "invalid bomb target", http.StatusBadRequest)
			return
		}
		identity := strconv.FormatInt(telegramUser.ID, 10)
		inventory, consumed, err := writer.ConsumeBomb(r.Context(), identity)
		if err != nil {
			http.Error(w, "inventory unavailable", http.StatusServiceUnavailable)
			return
		}
		if !consumed {
			w.WriteHeader(http.StatusConflict)
			writeJSON(w, map[string]any{"code": "no_bombs", "inventory": inventory})
			return
		}
		author := profileFromTelegram(telegramUser)
		now := time.Now().UTC()
		colors := bombPalette(request.Color)
		events := make([]domain.PixelEvent, 0, 21)
		for dy := -2; dy <= 2; dy++ {
			for dx := -2; dx <= 2; dx++ {
				x, y := request.X+dx, request.Y+dy
				if x < 0 || y < 0 || x >= int(boardWidth.Load()) || y >= int(boardHeight.Load()) || !bombIncludes(dx, dy) {
					continue
				}
				var frozenUntil *time.Time
				if current, ok := boardStore.Pixel("main", x, y); ok && current.FrozenUntil != nil && now.Before(*current.FrozenUntil) {
					if current.Author.ID != identity {
						continue
					}
					frozenUntil = current.FrozenUntil
				}
				shade := bombColor(colors, dx, dy)
				events = append(events, domain.PixelEvent{Type: "pixel_placed", EventID: id(), BoardID: "main", X: x, Y: y, Color: shade, OperationID: "server:" + id(), UserID: identity, Author: author, Version: version.Add(1), CreatedAt: now, FrozenUntil: frozenUntil})
			}
		}
		if len(events) == 0 {
			_ = writer.RefundBomb(r.Context(), identity)
			http.Error(w, "bomb has no available pixels", http.StatusLocked)
			return
		}
		for index, event := range events {
			if err := eventQueue.Append(r.Context(), event); err != nil {
				if index == 0 {
					_ = writer.RefundBomb(r.Context(), identity)
				}
				http.Error(w, "queue unavailable", http.StatusServiceUnavailable)
				return
			}
			boardStore.Apply(event)
			payload, _ := json.Marshal(eventForClient(event))
			hub.Broadcast(payload)
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, map[string]any{"placed": len(events), "inventory": inventory})
	})
	statsHandler := func(w http.ResponseWriter, r *http.Request) {
		telegramUser, err := telegramUserFromRequest(r)
		if err != nil {
			http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
			return
		}
		if writer == nil {
			writeJSON(w, map[string]int64{
				"placedPixels": 0, "repaintedPixels": 0, "currentPixels": 0,
				"dailyPlacedPixels": 0, "dailyRepaintedPixels": 0,
				"dailyColorsUsed": 0, "dailyUniqueCells": 0,
			})
			return
		}
		userID := strconv.FormatInt(telegramUser.ID, 10)
		stats, err := writer.UserStats(r.Context(), userID)
		if err != nil {
			log.Printf("statistics query failed for user=%d: %v", telegramUser.ID, err)
			http.Error(w, "Failed to load profile statistics", http.StatusInternalServerError)
			return
		}
		// The in-memory board is authoritative and updates before the asynchronous
		// PostgreSQL writer, so this value is both current and correct after clears/resizes.
		stats.CurrentPixels = boardStore.CountByAuthor("main", userID)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, stats)
	}
	http.HandleFunc("/api/boards/main/stats", statsHandler)
	http.HandleFunc("/api/profiles/me", statsHandler)
	profileHandler := func(w http.ResponseWriter, r *http.Request) {
		if _, err := telegramUserFromRequest(r); err != nil {
			http.Error(w, "Telegram Mini App authentication required", http.StatusUnauthorized)
			return
		}
		telegramID := strings.TrimPrefix(r.URL.Path, "/api/profiles/")
		telegramID = strings.TrimPrefix(telegramID, "/api/boards/profiles/")
		if parsed, err := strconv.ParseInt(telegramID, 10, 64); err != nil || parsed <= 0 || strconv.FormatInt(parsed, 10) != telegramID {
			http.Error(w, "profile not found", http.StatusNotFound)
			return
		}
		if telegramID == "" || writer == nil {
			http.Error(w, "profile not found", http.StatusNotFound)
			return
		}
		profile, err := writer.Profile(r.Context(), telegramID)
		if err != nil {
			http.Error(w, "profile not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, profile)
	}
	http.HandleFunc("/api/profiles/", profileHandler)
	http.HandleFunc("/api/boards/profiles/", profileHandler)
	http.HandleFunc("/api/boards/main/image", func(w http.ResponseWriter, r *http.Request) {
		boardMu.Lock()
		defer boardMu.Unlock()
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		canvas := renderMapCanvas(int(boardWidth.Load()), int(boardHeight.Load()), boardStore.Snapshot("main"))
		var output bytes.Buffer
		if err := png.Encode(&output, canvas); err != nil {
			http.Error(w, "failed to render map", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(output.Bytes())
	})
	http.HandleFunc("/inline-map.jpg", func(w http.ResponseWriter, _ *http.Request) {
		inlineImageCache.Lock()
		defer inlineImageCache.Unlock()
		if time.Now().After(inlineImageCache.expiresAt) || len(inlineImageCache.data) == 0 {
			boardMu.Lock()
			canvas := renderMapCanvas(int(boardWidth.Load()), int(boardHeight.Load()), boardStore.Snapshot("main"))
			boardMu.Unlock()
			var output bytes.Buffer
			if err := jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 96}); err != nil {
				http.Error(w, "failed to render map", http.StatusInternalServerError)
				return
			}
			inlineImageCache.data = output.Bytes()
			inlineImageCache.expiresAt = time.Now().Add(2 * time.Second)
		}
		w.Header().Set("Cache-Control", "public, max-age=2")
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", strconv.Itoa(len(inlineImageCache.data)))
		_, _ = w.Write(inlineImageCache.data)
	})
	http.HandleFunc("/api/admin/boards/main/size", func(w http.ResponseWriter, r *http.Request) {
		boardMu.Lock()
		defer boardMu.Unlock()
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, map[string]int64{"width": boardWidth.Load(), "height": boardHeight.Load()})
			return
		}
		var request struct {
			Width  int `json:"width"`
			Height int `json:"height"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Width < 16 || request.Height < 16 || request.Width > 500 || request.Height > 500 {
			http.Error(w, "invalid board size", http.StatusBadRequest)
			return
		}
		previousPixels := boardStore.Snapshot("main")
		previousWidth, previousHeight := boardWidth.Load(), boardHeight.Load()
		boardStore.Resize("main", request.Width, request.Height)
		boardWidth.Store(int64(request.Width))
		boardHeight.Store(int64(request.Height))
		if writer != nil {
			if err := checkpoint(r.Context()); err != nil {
				boardStore.Restore("main", previousPixels)
				boardWidth.Store(previousWidth)
				boardHeight.Store(previousHeight)
				http.Error(w, "failed to persist board size", http.StatusServiceUnavailable)
				return
			}
		}
		writeJSON(w, map[string]any{"width": request.Width, "height": request.Height})
	})
	http.HandleFunc("/api/admin/boards/main/fill", func(w http.ResponseWriter, r *http.Request) {
		boardMu.Lock()
		defer boardMu.Unlock()
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || writer == nil {
			http.Error(w, "fill unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			X1      int    `json:"x1"`
			Y1      int    `json:"y1"`
			X2      int    `json:"x2"`
			Y2      int    `json:"y2"`
			Color   string `json:"color"`
			AdminID int64  `json:"adminId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || !colorPattern.MatchString(request.Color) || request.AdminID <= 0 {
			http.Error(w, "invalid fill request", http.StatusBadRequest)
			return
		}
		x1, x2 := min(request.X1, request.X2), max(request.X1, request.X2)
		y1, y2 := min(request.Y1, request.Y2), max(request.Y1, request.Y2)
		width, height := int(boardWidth.Load()), int(boardHeight.Load())
		if x1 < 0 || y1 < 0 || x2 >= width || y2 >= height {
			http.Error(w, "fill coordinates outside board", http.StatusBadRequest)
			return
		}
		adminID := strconv.FormatInt(request.AdminID, 10)
		author := domain.PixelAuthor{ID: "admin:" + adminID, DisplayName: "Администрация"}
		pixels := make([]domain.BoardPixel, 0, (x2-x1+1)*(y2-y1+1))
		for y := y1; y <= y2; y++ {
			for x := x1; x <= x2; x++ {
				pixels = append(pixels, domain.BoardPixel{X: x, Y: y, Color: request.Color, Version: version.Add(1), Author: author})
			}
		}
		previousPixels := boardStore.Snapshot("main")
		boardStore.ApplyPixels("main", pixels)
		if err := checkpoint(r.Context()); err != nil {
			boardStore.Restore("main", previousPixels)
			http.Error(w, "failed to persist fill", http.StatusServiceUnavailable)
			return
		}
		hub.Broadcast([]byte(`{"type":"board_reload"}`))
		writeJSON(w, map[string]any{"filled": len(pixels), "x1": x1, "y1": y1, "x2": x2, "y2": y2, "color": request.Color})
	})
	http.HandleFunc("/api/admin/boards/main/image", func(w http.ResponseWriter, r *http.Request) {
		boardMu.Lock()
		defer boardMu.Unlock()
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || writer == nil {
			http.Error(w, "image import unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			X       int   `json:"x"`
			Y       int   `json:"y"`
			AdminID int64 `json:"adminId"`
			Pixels  []struct {
				X     int    `json:"x"`
				Y     int    `json:"y"`
				Color string `json:"color"`
			} `json:"pixels"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&request); err != nil || request.AdminID <= 0 || len(request.Pixels) == 0 || len(request.Pixels) > 250000 {
			http.Error(w, "invalid image request", http.StatusBadRequest)
			return
		}
		width, height := int(boardWidth.Load()), int(boardHeight.Load())
		adminID := strconv.FormatInt(request.AdminID, 10)
		author := domain.PixelAuthor{ID: "admin:" + adminID, DisplayName: "Администрация"}
		pixels := make([]domain.BoardPixel, 0, len(request.Pixels))
		for _, pixel := range request.Pixels {
			x, y := request.X+pixel.X, request.Y+pixel.Y
			if x < 0 || y < 0 || x >= width || y >= height || !colorPattern.MatchString(pixel.Color) {
				http.Error(w, "image pixels outside board", http.StatusBadRequest)
				return
			}
			pixels = append(pixels, domain.BoardPixel{X: x, Y: y, Color: pixel.Color, Version: version.Add(1), Author: author})
		}
		previousPixels := boardStore.Snapshot("main")
		boardStore.ApplyPixels("main", pixels)
		if err := checkpoint(r.Context()); err != nil {
			boardStore.Restore("main", previousPixels)
			http.Error(w, "failed to persist image", http.StatusServiceUnavailable)
			return
		}
		hub.Broadcast([]byte(`{"type":"board_reload"}`))
		writeJSON(w, map[string]any{"placed": len(pixels)})
	})
	http.HandleFunc("/api/admin/boards/main/clear", func(w http.ResponseWriter, r *http.Request) {
		boardMu.Lock()
		defer boardMu.Unlock()
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if writer == nil {
			http.Error(w, "persistent backup storage unavailable", http.StatusServiceUnavailable)
			return
		}
		pixels := boardStore.Snapshot("main")
		backupID := id()
		size := persistence.BoardSize{Width: int(boardWidth.Load()), Height: int(boardHeight.Load())}
		if err := writer.SaveBoardBackup(r.Context(), backupID, "main", size, pixels); err != nil {
			http.Error(w, "failed to back up board", http.StatusServiceUnavailable)
			return
		}
		boardStore.Clear("main")
		if err := checkpoint(r.Context()); err != nil {
			boardStore.Restore("main", pixels)
			http.Error(w, "failed to clear board", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"cleared": true, "backupId": backupID})
	})
	http.HandleFunc("/api/admin/boards/main/restore", func(w http.ResponseWriter, r *http.Request) {
		boardMu.Lock()
		defer boardMu.Unlock()
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || writer == nil {
			http.Error(w, "restore unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			BackupID string `json:"backupId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.BackupID == "" {
			http.Error(w, "invalid backup", http.StatusBadRequest)
			return
		}
		backup, err := writer.LoadBoardBackup(r.Context(), request.BackupID, "main")
		if err != nil {
			http.Error(w, "backup not found", http.StatusNotFound)
			return
		}
		previousPixels := boardStore.Snapshot("main")
		previousWidth, previousHeight := boardWidth.Load(), boardHeight.Load()
		for i := range backup.Pixels {
			backup.Pixels[i].Version = version.Add(1)
		}
		boardStore.Restore("main", backup.Pixels)
		boardWidth.Store(int64(backup.Width))
		boardHeight.Store(int64(backup.Height))
		if err := checkpoint(r.Context()); err != nil {
			boardStore.Restore("main", previousPixels)
			boardWidth.Store(previousWidth)
			boardHeight.Store(previousHeight)
			http.Error(w, "failed to restore board", http.StatusServiceUnavailable)
			return
		}
		_ = writer.MarkBoardBackupRestored(r.Context(), request.BackupID)
		writeJSON(w, map[string]any{"restored": true, "width": backup.Width, "height": backup.Height})
	})
	http.HandleFunc("/api/admin/stats", func(w http.ResponseWriter, r *http.Request) {
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		current := presence.Count()
		peak := accessStore.RecordOnlinePeak(r.Context(), current)
		writeJSON(w, map[string]int64{"currentOnline": current, "peakOnline": peak})
	})
	http.HandleFunc("/api/admin/quests/reset", func(w http.ResponseWriter, r *http.Request) {
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if writer == nil {
			http.Error(w, "persistence unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			UserID string `json:"userId"`
			All    bool   `json:"all"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || (!request.All && request.UserID == "") || (request.All && request.UserID != "") {
			http.Error(w, "specify one userId or all", http.StatusBadRequest)
			return
		}
		if request.UserID != "" {
			if _, err := strconv.ParseInt(request.UserID, 10, 64); err != nil {
				http.Error(w, "invalid userId", http.StatusBadRequest)
				return
			}
		}
		if err := writer.ResetDailyQuests(r.Context(), request.UserID); err != nil {
			log.Printf("daily quests reset failed: %v", err)
			http.Error(w, "failed to reset daily quests", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"reset": true, "all": request.All, "userId": request.UserID})
	})
	http.HandleFunc("/api/admin/items/grant", func(w http.ResponseWriter, r *http.Request) {
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || writer == nil {
			http.Error(w, "item grants unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			UserID string `json:"userId"`
			Item   string `json:"item"`
			Amount int64  `json:"amount"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.UserID == "" || request.Amount <= 0 || request.Amount > 100000 || (request.Item != "bomb" && request.Item != "ice") {
			http.Error(w, "invalid item grant", http.StatusBadRequest)
			return
		}
		if _, err := strconv.ParseInt(request.UserID, 10, 64); err != nil {
			http.Error(w, "invalid userId", http.StatusBadRequest)
			return
		}
		inventory, err := writer.GrantItem(r.Context(), request.UserID, request.Item, request.Amount)
		if err != nil {
			http.Error(w, "failed to grant items", http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{"granted": true, "userId": request.UserID, "item": request.Item, "amount": request.Amount, "inventory": inventory})
	})
	http.HandleFunc("/api/admin/trophies/drop", func(w http.ResponseWriter, r *http.Request) {
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if writer == nil {
			http.Error(w, "persistence unavailable", http.StatusServiceUnavailable)
			return
		}
		now := time.Now().UTC()
		var nextDrop time.Time
		var err error
		switch r.Method {
		case http.MethodGet:
			nextDrop, err = writer.TrophyDropTime(r.Context())
		case http.MethodPost:
			nextDrop, err = writer.MakeTrophyDropDue(r.Context(), now)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err != nil {
			log.Printf("trophy drop admin action failed: %v", err)
			http.Error(w, "trophy drop state unavailable", http.StatusServiceUnavailable)
			return
		}
		remainingSeconds := int64(nextDrop.Sub(now).Seconds())
		if remainingSeconds < 0 {
			remainingSeconds = 0
		} else if nextDrop.After(now) && remainingSeconds == 0 {
			remainingSeconds = 1
		}
		writeJSON(w, map[string]any{
			"ready":            !nextDrop.After(now),
			"remainingSeconds": remainingSeconds,
		})
	})
	http.HandleFunc("/api/admin/game/pause", func(w http.ResponseWriter, r *http.Request) {
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, map[string]bool{"paused": accessStore.IsPaused()})
			return
		}
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Paused bool `json:"paused"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid pause state", http.StatusBadRequest)
			return
		}
		if err := accessStore.SetPaused(r.Context(), request.Paused); err != nil {
			http.Error(w, "failed to save pause state", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]bool{"paused": request.Paused})
	})
	http.HandleFunc("/api/admin/game/test-mode", func(w http.ResponseWriter, r *http.Request) {
		if !adminAuthorized(r, adminAPIToken) {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodGet {
			writeJSON(w, map[string]bool{"enabled": accessStore.IsTestMode()})
			return
		}
		if r.Method != http.MethodPut {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var request struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, "invalid test mode state", http.StatusBadRequest)
			return
		}
		if err := accessStore.SetTestMode(r.Context(), request.Enabled); err != nil {
			http.Error(w, "failed to save test mode state", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]bool{"enabled": request.Enabled})
	})
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if !requestLimits.allow("ws:"+peerIP(r), 30, time.Now()) {
			http.Error(w, "connection rate limit", http.StatusTooManyRequests)
			return
		}
		if !socketLimits.acquire(peerIP(r)) {
			http.Error(w, "connection limit", http.StatusServiceUnavailable)
			return
		}
		defer socketLimits.release(peerIP(r))
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(16 << 10)
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		var authentication struct {
			Type     string `json:"type"`
			InitData string `json:"initData"`
		}
		if err := conn.ReadJSON(&authentication); err != nil || authentication.Type != "authenticate" {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4401, "authentication required"), time.Now().Add(time.Second))
			_ = conn.Close()
			return
		}
		telegramUser, err := auth.ValidateTelegramInitData(authentication.InitData)
		if err != nil {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4401, "authentication failed"), time.Now().Add(time.Second))
			_ = conn.Close()
			return
		}
		if accessStore.IsTestMode() && !accessStore.IsAdmin(telegramUser.ID) {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4403, "test mode enabled"), time.Now().Add(time.Second))
			_ = conn.Close()
			return
		}
		expiryTimer := time.AfterFunc(time.Until(telegramUser.ExpiresAt), func() {
			_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(4401, "authentication expired"), time.Now().Add(time.Second))
			_ = conn.Close()
		})
		defer expiryTimer.Stop()
		if writer != nil {
			if err := writer.UpsertProfile(r.Context(), profileFromTelegram(telegramUser)); err != nil {
				log.Printf("profile upsert failed for user=%d: %v", telegramUser.ID, err)
			}
		}
		accessStore.RegisterUser(r.Context(), telegramUser.ID, telegramUser.Username)
		identity := strconv.FormatInt(telegramUser.ID, 10)
		client := hub.Add(conn, identity)
		if client == nil {
			return
		}
		accessStore.RecordOnlinePeak(ctx, presence.Count())
		defer hub.Remove(client)
		conn.SetReadLimit(1024)
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
		for {
			var request domain.PlacementRequest
			if err := client.Conn.ReadJSON(&request); err != nil {
				return
			}
			if !time.Now().Before(telegramUser.ExpiresAt) || !requestLimits.allow("user:"+identity, 240, time.Now()) || (accessStore.IsTestMode() && !accessStore.IsAdmin(telegramUser.ID)) {
				return
			}
			func() {
				boardMu.Lock()
				defer boardMu.Unlock()
				if stopping.Load() {
					return
				}
				if accessStore.IsPaused() {
					_ = client.SendJSON(map[string]any{"type": "error", "code": "game_paused", "message": "Game is paused"})
					return
				}
				if !operationIDPattern.MatchString(request.OperationID) || request.Type != "place_pixel" || request.BoardID != "main" || request.X < 0 || request.Y < 0 || request.X >= int(boardWidth.Load()) || request.Y >= int(boardHeight.Load()) || !colorPattern.MatchString(request.Color) {
					_ = client.SendJSON(map[string]any{"type": "error", "code": "invalid_placement", "message": "Invalid pixel placement"})
					return
				}
				now := time.Now().UTC()
				var existingFreeze *time.Time
				if current, ok := boardStore.Pixel(request.BoardID, request.X, request.Y); ok && current.FrozenUntil != nil && now.Before(*current.FrozenUntil) {
					if current.Author.ID != identity {
						_ = client.SendJSON(map[string]any{"type": "error", "code": "pixel_frozen", "frozenUntil": current.FrozenUntil})
						return
					}
					existingFreeze = current.FrozenUntil
				}
				if userCooldown := accessStore.CooldownFor(telegramUser.ID, placementCooldown); userCooldown > 0 {
					if allowed, retryAfter := cooldown.Allow(identity, request.BoardID, userCooldown, time.Now()); !allowed {
						_ = client.SendJSON(map[string]any{"type": "error", "code": "placement_cooldown", "message": fmt.Sprintf("Place one pixel every %s", userCooldown), "retryAfterMs": retryAfter.Milliseconds()})
						return
					}
				}
				author := profileFromTelegram(telegramUser)
				frozenUntil := existingFreeze
				freezeChargeUsed := false
				if writer != nil && request.UseIce {
					freezeChargeUsed, err = writer.ConsumeFreezeCharge(r.Context(), identity)
					if err != nil {
						_ = client.SendJSON(map[string]any{"type": "error", "code": "inventory_unavailable"})
						return
					}
					if freezeChargeUsed {
						expires := now.Add(10 * time.Minute)
						frozenUntil = &expires
					}
				}
				event := domain.PixelEvent{Type: "pixel_placed", EventID: id(), BoardID: request.BoardID, X: request.X, Y: request.Y, Color: request.Color, OperationID: "server:" + id(), UserID: identity, Author: author, Version: version.Add(1), CreatedAt: now, FrozenUntil: frozenUntil}
				if err := eventQueue.Append(r.Context(), event); err != nil {
					if freezeChargeUsed && writer != nil {
						_ = writer.RefundFreezeCharge(r.Context(), identity)
					}
					_ = client.SendJSON(map[string]any{"type": "error", "code": "queue_unavailable"})
					return
				}
				boardStore.Apply(event)
				awardDueTrophy(r.Context(), identity, author)
				if freezeChargeUsed && writer != nil {
					_ = checkpoint(r.Context())
				}
				payload, _ := json.Marshal(eventForClient(event))
				hub.Broadcast(payload)
			}()
		}
	})

	addr := env("GO_HTTP_ADDR", ":8080")
	server := &http.Server{Addr: addr, Handler: withTrustedProxies(secureHandler(http.DefaultServeMux, adminAPIToken, requestLimits, accessStore), trustedProxies), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 8 << 10}
	stop := make(chan os.Signal, 1)
	shutdownDone := make(chan struct{})
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		stopping.Store(true)
		hub.Close()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = server.Shutdown(shutdownCtx)
		shutdownCancel()
		if writer != nil {
			boardMu.Lock()
			snapshotCtx, snapshotCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := checkpoint(snapshotCtx); err != nil {
				log.Printf("shutdown snapshot failed: %v", err)
			} else {
				log.Printf("shutdown snapshot saved")
			}
			snapshotCancel()
			boardMu.Unlock()
		}
		cancel()
		close(shutdownDone)
	}()
	log.Printf("realtime server listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-shutdownDone
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func id() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("secure random source unavailable")
	}
	return hex.EncodeToString(b)
}
func randomByte() byte { b := []byte{0}; _, _ = rand.Read(b); return b[0] }
func bombIncludes(dx, dy int) bool {
	distance := dx*dx + dy*dy
	if distance <= 2 {
		return true
	}
	if distance <= 5 {
		return randomByte() < 210
	}
	return distance == 8 && randomByte() < 45
}

func playerLevel(placedPixels int64) int {
	if placedPixels < 0 {
		placedPixels = 0
	}
	level := 1
	for level < 100 && placedPixels >= pixelsRequiredForLevel(level+1) {
		level++
	}
	return level
}

func pixelsRequiredForLevel(level int) int64 {
	if level < 1 {
		level = 1
	} else if level > 100 {
		level = 100
	}
	return int64(5 * (level - 1) * level)
}

func levelReward(level int) (string, int64) {
	item := "bomb"
	if level%2 == 0 {
		item = "ice"
	}
	amount := int64(((level-1)/20 + 1) * 5)
	return item, amount
}
func parseHex(value string) (int, int, int) {
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return int(parsed >> 16), int((parsed >> 8) & 255), int(parsed & 255)
}
func bombPalette(selected string) []string {
	selected = strings.ToUpper(selected)
	anchor := 0
	bestDistance := int(^uint(0) >> 1)
	r, g, b := parseHex(selected)
	for index, value := range itemPalette {
		if value == selected {
			anchor = index
			bestDistance = 0
			break
		}
		cr, cg, cb := parseHex(value)
		dr, dg, db := r-cr, g-cg, b-cb
		distance := dr*dr + dg*dg + db*db
		if distance < bestDistance {
			anchor = index
			bestDistance = distance
		}
	}
	column := anchor % 10
	result := []string{selected}
	for row := 0; row < 3; row++ {
		value := itemPalette[row*10+column]
		if value != selected {
			result = append(result, value)
		}
	}
	return result
}
func bombColor(colors []string, dx, dy int) string {
	distance := dx*dx + dy*dy
	roll := int(randomByte())
	if distance <= 1 || len(colors) == 1 || roll < 132 {
		return colors[0]
	}
	index := 1 + int(randomByte())%(len(colors)-1)
	if distance <= 2 && roll < 210 {
		index = 1
	}
	return colors[index]
}
func profileFromTelegram(user auth.TelegramUser) domain.PixelAuthor {
	displayName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if displayName == "" {
		displayName = user.Username
	}
	if displayName == "" {
		displayName = strconv.FormatInt(user.ID, 10)
	}
	return domain.PixelAuthor{ID: strconv.FormatInt(user.ID, 10), DisplayName: displayName, Username: user.Username, PhotoURL: user.PhotoURL}
}

func renderMapCanvas(width, height int, pixels []domain.BoardPixel) *image.RGBA {
	const outputSize = 1200
	canvas := image.NewRGBA(image.Rect(0, 0, outputSize, outputSize))
	for y := 0; y < outputSize; y++ {
		for x := 0; x < outputSize; x++ {
			canvas.Set(x, y, color.White)
		}
	}
	cellW := outputSize / width
	cellH := outputSize / height
	if cellW < 1 {
		cellW = 1
	}
	if cellH < 1 {
		cellH = 1
	}
	for _, pixel := range pixels {
		if pixel.X < 0 || pixel.Y < 0 || pixel.X >= width || pixel.Y >= height {
			continue
		}
		parsed := color.RGBA{A: 255}
		if _, err := fmt.Sscanf(pixel.Color, "#%02x%02x%02x", &parsed.R, &parsed.G, &parsed.B); err != nil {
			continue
		}
		for y := pixel.Y * cellH; y < (pixel.Y+1)*cellH && y < outputSize; y++ {
			for x := pixel.X * cellW; x < (pixel.X+1)*cellW && x < outputSize; x++ {
				canvas.Set(x, y, parsed)
			}
		}
	}
	return canvas
}

func telegramUserFromRequest(r *http.Request) (auth.TelegramUser, error) {
	if user, ok := r.Context().Value(telegramUserKey{}).(auth.TelegramUser); ok {
		return user, nil
	}
	if len(r.Header.Values("X-Telegram-Init-Data")) != 1 {
		return auth.TelegramUser{}, auth.ErrInvalidTelegramData
	}
	return auth.ValidateTelegramInitData(r.Header.Get("X-Telegram-Init-Data"))
}

type publicPixelAuthor struct {
	ID string `json:"id"`
}
type compactBoardPixel struct {
	X int        `json:"x"`
	Y int        `json:"y"`
	C string     `json:"c"`
	A string     `json:"a,omitempty"`
	F *time.Time `json:"f,omitempty"`
}
type compactBoardSnapshot struct {
	ID     string              `json:"id"`
	Width  int64               `json:"width"`
	Height int64               `json:"height"`
	Pixels []compactBoardPixel `json:"pixels"`
}
type boardSnapshotCache struct {
	mu         sync.Mutex
	revision   uint64
	width      int64
	height     int64
	raw        []byte
	compressed []byte
}

func (c *boardSnapshotCache) Payload(store *state.BoardStore, width, height int64) ([]byte, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	revision := store.Revision("main")
	if c.raw != nil && c.revision == revision && c.width == width && c.height == height {
		return c.raw, c.compressed, nil
	}
	var pixels []domain.BoardPixel
	for {
		revision = store.Revision("main")
		pixels = store.Snapshot("main")
		if revision == store.Revision("main") {
			break
		}
	}
	compact := make([]compactBoardPixel, 0, len(pixels))
	for _, pixel := range pixels {
		compact = append(compact, compactBoardPixel{X: pixel.X, Y: pixel.Y, C: pixel.Color, A: pixel.Author.ID, F: pixel.FrozenUntil})
	}
	raw, err := json.Marshal(compactBoardSnapshot{ID: "main", Width: width, Height: height, Pixels: compact})
	if err != nil {
		return nil, nil, err
	}
	var output bytes.Buffer
	compressor, err := gzip.NewWriterLevel(&output, gzip.BestSpeed)
	if err != nil {
		return nil, nil, err
	}
	if _, err = compressor.Write(raw); err != nil {
		return nil, nil, err
	}
	if err = compressor.Close(); err != nil {
		return nil, nil, err
	}
	c.revision, c.width, c.height = revision, width, height
	c.raw, c.compressed = raw, output.Bytes()
	return c.raw, c.compressed, nil
}

type publicBoardPixel struct {
	X           int               `json:"x"`
	Y           int               `json:"y"`
	Color       string            `json:"color"`
	Version     int64             `json:"version"`
	Author      publicPixelAuthor `json:"author"`
	FrozenUntil *time.Time        `json:"frozenUntil,omitempty"`
}
type publicPixelEvent struct {
	Type        string            `json:"type"`
	X           int               `json:"x"`
	Y           int               `json:"y"`
	Color       string            `json:"color"`
	Version     int64             `json:"version"`
	Author      publicPixelAuthor `json:"author"`
	FrozenUntil *time.Time        `json:"frozenUntil,omitempty"`
	TrophyAward *trophyAwardEvent `json:"trophyAward,omitempty"`
}

type trophyAwardEvent struct {
	Type     string `json:"type"`
	EventID  string `json:"eventId"`
	UserID   string `json:"userId"`
	Nickname string `json:"nickname"`
}

func publicSnapshot(pixels []domain.BoardPixel) []publicBoardPixel {
	result := make([]publicBoardPixel, 0, len(pixels))
	for _, pixel := range pixels {
		result = append(result, publicBoardPixel{X: pixel.X, Y: pixel.Y, Color: pixel.Color, Version: pixel.Version, Author: publicPixelAuthor{ID: pixel.Author.ID}, FrozenUntil: pixel.FrozenUntil})
	}
	return result
}
func eventForClient(event domain.PixelEvent) publicPixelEvent {
	return publicPixelEvent{Type: event.Type, X: event.X, Y: event.Y, Color: event.Color, Version: event.Version, Author: publicPixelAuthor{ID: event.Author.ID}, FrozenUntil: event.FrozenUntil}
}
func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func snapshotLoop(ctx context.Context, mu *sync.Mutex, checkpoint func(context.Context) error) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			mu.Lock()
			if err := checkpoint(ctx); err != nil {
				log.Printf("snapshot: %v", err)
			}
			mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}
