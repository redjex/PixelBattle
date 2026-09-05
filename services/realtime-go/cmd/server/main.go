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
	"image/png"
	"log"
	"math/bits"
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

var builtinAdminIDs = []int64{743086174, 6997207264}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := realtime.NewHub()
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
	adminAPIToken := env("ADMIN_API_TOKEN", "")
	accessStore := access.New(ctx, env("REDIS_URL", "redis://localhost:6379/0"), builtinAdminIDs)
	defer accessStore.Close()
	go accessStore.Run(ctx)

	if os.Getenv("GO_DEV_IN_MEMORY") != "true" {
		candidate, err := queue.NewRedis(env("REDIS_URL", "redis://localhost:6379/0"))
		if err != nil {
			log.Printf("redis disabled: %v", err)
		} else if err = candidate.Ready(ctx); err != nil {
			log.Printf("redis disabled: %v", err)
		} else {
			redisQueue = candidate
			eventQueue = candidate
		}
		if pgWriter, err := persistence.NewWriter(ctx, env("POSTGRES_DSN", "postgres://pixelbattle:pixelbattle@localhost:5432/pixelbattle?sslmode=disable")); err == nil {
			writer = pgWriter
			defer writer.Close()
			if err := writer.Migrate(ctx); err != nil {
				log.Printf("migration: %v", err)
			} else {
				if size, sizeErr := writer.LoadBoardSize(ctx, "main"); sizeErr == nil && size.Width > 0 && size.Height > 0 {
					boardWidth.Store(int64(size.Width))
					boardHeight.Store(int64(size.Height))
				}
				if pixels, loadErr := writer.LoadSnapshot(ctx, "main"); loadErr == nil {
					boardStore.Restore("main", pixels)
					log.Printf("restored %d pixels from snapshot", len(pixels))
				}
			}
		} else {
			log.Printf("postgres disabled: %v", err)
		}
	}
	go persistence.RunMemoryBatcher(ctx, memoryQueue.Events, writer)
	if redisQueue != nil && writer != nil {
		go redisQueue.Consume(ctx, writer.WriteBatch)
	}
	if writer != nil {
		go snapshotLoop(ctx, writer, boardStore)
	}

	var version atomic.Int64
	version.Store(boardStore.MaxVersion("main"))
	allowedOrigin := env("GO_ALLOWED_ORIGIN", "http://localhost:5173")
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == allowedOrigin || origin == "https://pixelbattle.redjex.bond"
	}}
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"status": "ok", "service": "realtime-go"})
	})
	http.HandleFunc("/api/boards/main", func(w http.ResponseWriter, r *http.Request) {
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
		if writer != nil {
			if err := writer.UpsertProfile(r.Context(), profileFromTelegram(telegramUser)); err != nil {
				log.Printf("profile upsert failed for user=%d: %v", telegramUser.ID, err)
			}
		}
		userCooldown := accessStore.CooldownFor(telegramUser.ID, placementCooldown)
		inventory := persistence.Inventory{}
		if writer != nil {
			inventory, _ = writer.Inventory(r.Context(), strconv.FormatInt(telegramUser.ID, 10))
		}
		writeJSON(w, map[string]any{"cooldownBypassed": userCooldown == 0, "cooldownMs": userCooldown.Milliseconds(), "paused": accessStore.IsPaused(), "inventory": inventory})
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
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Type != "place_pixel" || request.BoardID != "main" || request.X < 0 || request.Y < 0 || request.X >= int(boardWidth.Load()) || request.Y >= int(boardHeight.Load()) || !colorPattern.MatchString(request.Color) {
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
		event := domain.PixelEvent{Type: "pixel_placed", EventID: id(), BoardID: request.BoardID, X: request.X, Y: request.Y, Color: request.Color, OperationID: request.OperationID, UserID: identity, Author: author, Version: version.Add(1), CreatedAt: now, FrozenUntil: frozenUntil}
		if err := eventQueue.Append(r.Context(), event); err != nil {
			if freezeChargeUsed && writer != nil {
				_ = writer.RefundFreezeCharge(r.Context(), identity)
			}
			log.Printf("http placement queue failed: %v", err)
			http.Error(w, "queue unavailable", http.StatusServiceUnavailable)
			return
		}
		boardStore.Apply(event)
		if freezeChargeUsed && writer != nil {
			_ = writer.WriteSnapshot(r.Context(), "main", boardStore.Snapshot("main"))
		}
		log.Printf("http placement accepted: user=%d x=%d y=%d version=%d", telegramUser.ID, event.X, event.Y, event.Version)
		publicEvent := eventForClient(event)
		payload, _ := json.Marshal(publicEvent)
		hub.Broadcast(payload)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, publicEvent)
	})
	http.HandleFunc("/api/boards/items/bomb/use", func(w http.ResponseWriter, r *http.Request) {
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
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.X < 0 || request.Y < 0 || request.X >= int(boardWidth.Load()) || request.Y >= int(boardHeight.Load()) || !colorPattern.MatchString(request.Color) || request.OperationID == "" {
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
				events = append(events, domain.PixelEvent{Type: "pixel_placed", EventID: id(), BoardID: "main", X: x, Y: y, Color: shade, OperationID: fmt.Sprintf("%s:%d:%d", request.OperationID, dx+2, dy+2), UserID: identity, Author: author, Version: version.Add(1), CreatedAt: now, FrozenUntil: frozenUntil})
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
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		width, height := int(boardWidth.Load()), int(boardHeight.Load())
		const outputSize = 600
		canvas := image.NewRGBA(image.Rect(0, 0, outputSize, outputSize))
		for y := 0; y < outputSize; y++ {
			for x := 0; x < outputSize; x++ {
				canvas.Set(x, y, color.White)
			}
		}
		for _, pixel := range boardStore.Snapshot("main") {
			if pixel.X < 0 || pixel.Y < 0 || pixel.X >= width || pixel.Y >= height {
				continue
			}
			cellW := outputSize / width
			cellH := outputSize / height
			if cellW < 1 {
				cellW = 1
			}
			if cellH < 1 {
				cellH = 1
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
		var output bytes.Buffer
		if err := png.Encode(&output, canvas); err != nil {
			http.Error(w, "failed to render map", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(output.Bytes())
	})
	http.HandleFunc("/api/admin/boards/main/size", func(w http.ResponseWriter, r *http.Request) {
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
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
		boardStore.Resize("main", request.Width, request.Height)
		boardWidth.Store(int64(request.Width))
		boardHeight.Store(int64(request.Height))
		if writer != nil {
			_ = writer.SaveBoardSize(r.Context(), "main", persistence.BoardSize{Width: request.Width, Height: request.Height})
			_ = writer.WriteSnapshot(r.Context(), "main", boardStore.Snapshot("main"))
		}
		writeJSON(w, map[string]any{"width": request.Width, "height": request.Height})
	})
	http.HandleFunc("/api/admin/boards/main/fill", func(w http.ResponseWriter, r *http.Request) {
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
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
		if err := writer.WriteSnapshot(r.Context(), "main", boardStore.Snapshot("main")); err != nil {
			boardStore.Restore("main", previousPixels)
			http.Error(w, "failed to persist fill", http.StatusServiceUnavailable)
			return
		}
		hub.Broadcast([]byte(`{"type":"board_reload"}`))
		writeJSON(w, map[string]any{"filled": len(pixels), "x1": x1, "y1": y1, "x2": x2, "y2": y2, "color": request.Color})
	})
	http.HandleFunc("/api/admin/boards/main/image", func(w http.ResponseWriter, r *http.Request) {
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
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
		if err := writer.WriteSnapshot(r.Context(), "main", boardStore.Snapshot("main")); err != nil {
			boardStore.Restore("main", previousPixels)
			http.Error(w, "failed to persist image", http.StatusServiceUnavailable)
			return
		}
		hub.Broadcast([]byte(`{"type":"board_reload"}`))
		writeJSON(w, map[string]any{"placed": len(pixels)})
	})
	http.HandleFunc("/api/admin/boards/main/clear", func(w http.ResponseWriter, r *http.Request) {
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
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
		if err := writer.WriteSnapshot(r.Context(), "main", boardStore.Snapshot("main")); err != nil {
			boardStore.Restore("main", pixels)
			http.Error(w, "failed to clear board", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, map[string]any{"cleared": true, "backupId": backupID})
	})
	http.HandleFunc("/api/admin/boards/main/restore", func(w http.ResponseWriter, r *http.Request) {
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
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
		boardStore.Restore("main", backup.Pixels)
		boardWidth.Store(int64(backup.Width))
		boardHeight.Store(int64(backup.Height))
		if err := writer.SaveBoardSize(r.Context(), "main", persistence.BoardSize{Width: backup.Width, Height: backup.Height}); err != nil {
			http.Error(w, "failed to restore board size", http.StatusServiceUnavailable)
			return
		}
		if err := writer.WriteSnapshot(r.Context(), "main", backup.Pixels); err != nil {
			http.Error(w, "failed to restore board", http.StatusServiceUnavailable)
			return
		}
		_ = writer.MarkBoardBackupRestored(r.Context(), request.BackupID)
		writeJSON(w, map[string]any{"restored": true, "width": backup.Width, "height": backup.Height})
	})
	http.HandleFunc("/api/admin/stats", func(w http.ResponseWriter, r *http.Request) {
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
			http.Error(w, "admin access required", http.StatusUnauthorized)
			return
		}
		current := hub.OnlineCount()
		peak := accessStore.RecordOnlinePeak(r.Context(), current)
		writeJSON(w, map[string]int64{"currentOnline": current, "peakOnline": peak})
	})
	http.HandleFunc("/api/admin/quests/reset", func(w http.ResponseWriter, r *http.Request) {
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
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
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
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
	http.HandleFunc("/api/admin/game/pause", func(w http.ResponseWriter, r *http.Request) {
		if adminAPIToken == "" || r.URL.Query().Get("adminToken") != adminAPIToken {
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
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn.SetReadLimit(16 << 10)
		_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		var authentication struct {
			Type     string `json:"type"`
			InitData string `json:"initData"`
		}
		if err := conn.ReadJSON(&authentication); err != nil || authentication.Type != "authenticate" {
			_ = conn.Close()
			return
		}
		telegramUser, err := auth.ValidateTelegramInitData(authentication.InitData)
		if err != nil {
			_ = conn.Close()
			return
		}
		if writer != nil {
			if err := writer.UpsertProfile(r.Context(), profileFromTelegram(telegramUser)); err != nil {
				log.Printf("profile upsert failed for user=%d: %v", telegramUser.ID, err)
			}
		}
		accessStore.RegisterUser(r.Context(), telegramUser.ID, telegramUser.Username)
		identity := strconv.FormatInt(telegramUser.ID, 10)
		client := hub.Add(conn, identity)
		accessStore.RecordOnlinePeak(ctx, hub.OnlineCount())
		defer hub.Remove(client)
		conn.SetReadLimit(1024)
		_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
		for {
			var request domain.PlacementRequest
			if err := client.Conn.ReadJSON(&request); err != nil {
				return
			}
			if accessStore.IsPaused() {
				_ = client.SendJSON(map[string]any{"type": "error", "code": "game_paused", "message": "Game is paused"})
				continue
			}
			if request.Type != "place_pixel" || request.BoardID != "main" || request.X < 0 || request.Y < 0 || request.X >= int(boardWidth.Load()) || request.Y >= int(boardHeight.Load()) || !colorPattern.MatchString(request.Color) {
				_ = client.SendJSON(map[string]any{"type": "error", "code": "invalid_placement", "message": "Invalid pixel placement"})
				continue
			}
			now := time.Now().UTC()
			var existingFreeze *time.Time
			if current, ok := boardStore.Pixel(request.BoardID, request.X, request.Y); ok && current.FrozenUntil != nil && now.Before(*current.FrozenUntil) {
				if current.Author.ID != identity {
					_ = client.SendJSON(map[string]any{"type": "error", "code": "pixel_frozen", "frozenUntil": current.FrozenUntil})
					continue
				}
				existingFreeze = current.FrozenUntil
			}
			if userCooldown := accessStore.CooldownFor(telegramUser.ID, placementCooldown); userCooldown > 0 {
				if allowed, retryAfter := cooldown.Allow(identity, request.BoardID, userCooldown, time.Now()); !allowed {
					_ = client.SendJSON(map[string]any{"type": "error", "code": "placement_cooldown", "message": fmt.Sprintf("Place one pixel every %s", userCooldown), "retryAfterMs": retryAfter.Milliseconds()})
					continue
				}
			}
			author := profileFromTelegram(telegramUser)
			frozenUntil := existingFreeze
			freezeChargeUsed := false
			if writer != nil && request.UseIce {
				freezeChargeUsed, err = writer.ConsumeFreezeCharge(r.Context(), identity)
				if err != nil {
					_ = client.SendJSON(map[string]any{"type": "error", "code": "inventory_unavailable"})
					continue
				}
				if freezeChargeUsed {
					expires := now.Add(10 * time.Minute)
					frozenUntil = &expires
				}
			}
			event := domain.PixelEvent{Type: "pixel_placed", EventID: id(), BoardID: request.BoardID, X: request.X, Y: request.Y, Color: request.Color, OperationID: request.OperationID, UserID: identity, Author: author, Version: version.Add(1), CreatedAt: now, FrozenUntil: frozenUntil}
			if err := eventQueue.Append(r.Context(), event); err != nil {
				if freezeChargeUsed && writer != nil {
					_ = writer.RefundFreezeCharge(r.Context(), identity)
				}
				_ = client.SendJSON(map[string]any{"type": "error", "code": "queue_unavailable"})
				continue
			}
			boardStore.Apply(event)
			if freezeChargeUsed && writer != nil {
				_ = writer.WriteSnapshot(r.Context(), "main", boardStore.Snapshot("main"))
			}
			payload, _ := json.Marshal(eventForClient(event))
			hub.Broadcast(payload)
		}
	})

	addr := env("GO_HTTP_ADDR", ":8080")
	server := &http.Server{Addr: addr, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 120 * time.Second, MaxHeaderBytes: 8 << 10}
	stop := make(chan os.Signal, 1)
	shutdownDone := make(chan struct{})
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
		_ = server.Shutdown(shutdownCtx)
		shutdownCancel()
		if writer != nil {
			snapshotCtx, snapshotCancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := writer.WriteSnapshot(snapshotCtx, "main", boardStore.Snapshot("main")); err != nil {
				log.Printf("shutdown snapshot failed: %v", err)
			} else {
				log.Printf("shutdown snapshot saved")
			}
			snapshotCancel()
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
func id() string       { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }
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
	quests := placedPixels / 10
	level := bits.Len64(uint64(quests + 1))
	if level < 1 {
		return 1
	}
	if level > 100 {
		return 100
	}
	return level
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
func telegramUserFromRequest(r *http.Request) (auth.TelegramUser, error) {
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

func snapshotLoop(ctx context.Context, writer *persistence.Writer, boards *state.BoardStore) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := writer.WriteSnapshot(ctx, "main", boards.Snapshot("main")); err != nil {
				log.Printf("snapshot: %v", err)
			}
		case <-ctx.Done():
			return
		}
	}
}
