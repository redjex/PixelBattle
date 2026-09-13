package antibot

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	perfectTimingTolerance = 120 * time.Millisecond
	perfectIntervalsNeeded = 5
	timingWindowSize       = 12
	timingClusterNeeded    = 10
	timingClusterTolerance = 300 * time.Millisecond
	maximumTimingOverrun   = 2 * time.Second
	challengeLifetime      = 2 * time.Minute
	minimumSolveTime       = 1500 * time.Millisecond
	maximumAnswerAttempts  = 3
	verificationGrace      = 15 * time.Minute
	penaltyPerStrike       = 5 * time.Second
	cleanStatusLifetime    = 24 * time.Hour
	maxTrackedPlayers      = 10000
)

const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

type Challenge struct {
	ID    string `json:"challengeId"`
	Image string `json:"image"`
}

type ReviewStatus struct {
	UserID         string    `json:"userId"`
	Status         string    `json:"status"`
	UpdatedAt      time.Time `json:"updatedAt"`
	StrikeCount    int       `json:"strikeCount"`
	PenaltySeconds int64     `json:"penaltySeconds"`
	Online         bool      `json:"online"`
}

type playerState struct {
	lastPlacement   time.Time
	lastSeen        time.Time
	perfectStreak   int
	recentIntervals [timingWindowSize]time.Duration
	intervalCount   int
	intervalIndex   int
	required        bool
	verified        bool
	reviewed        bool
	statusChangedAt time.Time
	challengeID     string
	challengeImage  string
	challengeAnswer [32]byte
	challengeIssued time.Time
	challengeTries  int
	challengeExpiry time.Time
	trustedUntil    time.Time
	penaltyStrikes  int
	penalizedAt     time.Time
}

// Guard tracks suspicious placement timing and keeps CAPTCHA answers server-side.
type Guard struct {
	mu               sync.Mutex
	players          map[string]*playerState
	penaltyStrikes   map[string]int
	incrementPenalty func(string) (int, error)
	nextSweep        time.Time
	random           io.Reader
}

func New() *Guard {
	return NewPersistent(nil, nil)
}

// NewPersistent restores cooldown strikes and persists every new strike
// synchronously. CAPTCHA challenges remain ephemeral, but punishment survives
// process and server restarts.
func NewPersistent(penalties map[string]int, increment func(string) (int, error)) *Guard {
	restored := make(map[string]int, len(penalties))
	for identity, strikes := range penalties {
		if identity != "" && strikes > 0 {
			restored[identity] = strikes
		}
	}
	return &Guard{
		players:          make(map[string]*playerState),
		penaltyStrikes:   restored,
		incrementPenalty: increment,
		random:           rand.Reader,
	}
}

// RecordPlacement returns true once a player has repeatedly placed immediately
// after the cooldown. Only accepted placements should be recorded.
func (g *Guard) RecordPlacement(identity string, now time.Time, cooldown time.Duration) bool {
	if identity == "" || cooldown <= 0 {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	state := g.players[identity]
	if state == nil {
		if len(g.players) >= maxTrackedPlayers {
			return false
		}
		state = &playerState{}
		g.players[identity] = state
	}
	state.lastSeen = now
	if state.required {
		return true
	}
	if !state.lastPlacement.IsZero() {
		delta := now.Sub(state.lastPlacement)
		if delta >= cooldown && delta <= cooldown+perfectTimingTolerance {
			state.perfectStreak++
		} else {
			state.perfectStreak = 0
		}
		if delta >= cooldown && delta <= cooldown+maximumTimingOverrun {
			state.recentIntervals[state.intervalIndex] = delta
			state.intervalIndex = (state.intervalIndex + 1) % timingWindowSize
			if state.intervalCount < timingWindowSize {
				state.intervalCount++
			}
		} else {
			state.intervalCount = 0
			state.intervalIndex = 0
		}
	}
	state.lastPlacement = now
	// The 15-minute grace suppresses ordinary checks, but not strong machine
	// timing. Otherwise OCR-assisted bots can solve once and automate freely.
	suspiciousTiming := state.perfectStreak >= perfectIntervalsNeeded || clusteredMachineTiming(state)
	if suspiciousTiming {
		g.require(state, now)
	}
	return state.required
}

func (g *Guard) Required(identity string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	state, ok := g.players[identity]
	if !ok {
		return false
	}
	state.lastSeen = now
	return state.required
}

// Force requires a CAPTCHA independently of placement timing. It is intended
// for the authenticated administration API.
func (g *Guard) Force(identity string, now time.Time) bool {
	if identity == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	state := g.players[identity]
	if state == nil {
		if len(g.players) >= maxTrackedPlayers {
			return false
		}
		state = &playerState{}
		g.players[identity] = state
	}
	state.lastSeen = now
	g.require(state, now)
	return true
}

// Flag requires a CAPTCHA unless the player solved one during the grace
// period. Automatic detectors use this method; Force remains an admin override.
func (g *Guard) Flag(identity string, now time.Time) bool {
	if identity == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	state := g.players[identity]
	if state == nil {
		if len(g.players) >= maxTrackedPlayers {
			return false
		}
		state = &playerState{}
		g.players[identity] = state
	}
	state.lastSeen = now
	if now.Before(state.trustedUntil) {
		return false
	}
	g.require(state, now)
	return true
}

func (g *Guard) require(state *playerState, now time.Time) {
	state.required = true
	state.verified = false
	state.trustedUntil = time.Time{}
	state.reviewed = true
	state.statusChangedAt = now
	state.challengeID = ""
	state.challengeImage = ""
	state.challengeAnswer = [32]byte{}
	state.challengeIssued = time.Time{}
	state.challengeTries = 0
	state.challengeExpiry = time.Time{}
}

func (g *Guard) Challenge(identity string, now time.Time) (Challenge, bool, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	state, ok := g.players[identity]
	if !ok || !state.required {
		return Challenge{}, false, nil
	}
	state.lastSeen = now
	if state.challengeID != "" && now.Before(state.challengeExpiry) {
		return Challenge{ID: state.challengeID, Image: state.challengeImage}, true, nil
	}
	answer, err := randomText(g.random, 5)
	if err != nil {
		return Challenge{}, true, err
	}
	id, err := randomText(g.random, 24)
	if err != nil {
		return Challenge{}, true, err
	}
	imageData, err := renderChallenge(answer, g.random)
	if err != nil {
		return Challenge{}, true, err
	}
	state.challengeID = id
	state.challengeImage = imageData
	state.challengeAnswer = sha256.Sum256([]byte(answer))
	state.challengeIssued = now
	state.challengeTries = 0
	state.challengeExpiry = now.Add(challengeLifetime)
	return Challenge{ID: id, Image: imageData}, true, nil
}

func (g *Guard) Verify(identity, challengeID, answer string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	state, ok := g.players[identity]
	if !ok || !state.required || state.challengeID == "" || challengeID != state.challengeID || !now.Before(state.challengeExpiry) {
		return false
	}
	if now.Sub(state.challengeIssued) < minimumSolveTime {
		return false
	}
	supplied := sha256.Sum256([]byte(strings.ToUpper(strings.TrimSpace(answer))))
	if subtle.ConstantTimeCompare(supplied[:], state.challengeAnswer[:]) != 1 {
		state.challengeTries++
		if state.challengeTries >= maximumAnswerAttempts {
			state.challengeID = ""
			state.challengeImage = ""
			state.challengeAnswer = [32]byte{}
			state.challengeIssued = time.Time{}
			state.challengeTries = 0
			state.challengeExpiry = time.Time{}
		}
		return false
	}
	state.lastSeen = now
	state.lastPlacement = time.Time{}
	state.perfectStreak = 0
	state.intervalCount = 0
	state.intervalIndex = 0
	state.recentIntervals = [timingWindowSize]time.Duration{}
	state.required = false
	state.verified = true
	state.trustedUntil = now.Add(verificationGrace)
	state.reviewed = true
	state.statusChangedAt = now
	state.challengeID = ""
	state.challengeImage = ""
	state.challengeAnswer = [32]byte{}
	state.challengeIssued = time.Time{}
	state.challengeTries = 0
	state.challengeExpiry = time.Time{}
	return true
}

// Penalize adds one persistent cooldown strike for the current unanswered
// CAPTCHA. Repeated calls for the same challenge are idempotent.
func (g *Guard) Penalize(identity string, now time.Time) (time.Duration, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	state, ok := g.players[identity]
	if !ok || !state.required || state.statusChangedAt.IsZero() {
		return 0, 0, false
	}
	if !state.penalizedAt.Equal(state.statusChangedAt) {
		strikes := g.penaltyStrikes[identity] + 1
		if g.incrementPenalty != nil {
			persisted, err := g.incrementPenalty(identity)
			if err != nil || persisted <= 0 {
				return 0, g.penaltyStrikes[identity], false
			}
			strikes = persisted
		}
		g.penaltyStrikes[identity] = strikes
		state.penaltyStrikes = strikes
		state.penalizedAt = state.statusChangedAt
	}
	strikes := g.penaltyStrikes[identity]
	return time.Duration(strikes) * penaltyPerStrike, strikes, true
}

func (g *Guard) Penalty(identity string, now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	return time.Duration(g.penaltyStrikes[identity]) * penaltyPerStrike
}

func clusteredMachineTiming(state *playerState) bool {
	if state.intervalCount < timingWindowSize {
		return false
	}
	intervals := append([]time.Duration(nil), state.recentIntervals[:]...)
	sort.Slice(intervals, func(i, j int) bool { return intervals[i] < intervals[j] })
	median := intervals[len(intervals)/2]
	clustered := 0
	for _, interval := range intervals {
		difference := interval - median
		if difference < 0 {
			difference = -difference
		}
		if difference <= timingClusterTolerance {
			clustered++
		}
	}
	return clustered >= timingClusterNeeded
}

// ReviewStatuses returns players who have been challenged, ordered by the most
// recent status change. A pending or failed CAPTCHA remains suspicious until it
// is solved successfully.
func (g *Guard) ReviewStatuses(now time.Time) []ReviewStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.sweep(now)
	statuses := make([]ReviewStatus, 0)
	for identity, state := range g.players {
		if !state.reviewed {
			continue
		}
		status := "suspicious"
		if state.verified && !state.required {
			status = "clean"
		}
		statuses = append(statuses, ReviewStatus{
			UserID: identity, Status: status, UpdatedAt: state.statusChangedAt,
			StrikeCount: g.penaltyStrikes[identity], PenaltySeconds: int64(g.penaltyStrikes[identity]) * int64(penaltyPerStrike/time.Second),
		})
	}
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].UpdatedAt.After(statuses[j].UpdatedAt)
	})
	return statuses
}

func (g *Guard) player(identity string, now time.Time) *playerState {
	state := g.players[identity]
	if state == nil {
		state = &playerState{}
		g.players[identity] = state
	}
	state.lastSeen = now
	return state
}

func (g *Guard) sweep(now time.Time) {
	if now.Before(g.nextSweep) {
		return
	}
	for identity, state := range g.players {
		retention := time.Hour
		if state.reviewed {
			retention = cleanStatusLifetime
		}
		if !state.required && now.Sub(state.lastSeen) > retention {
			delete(g.players, identity)
		}
	}
	g.nextSweep = now.Add(time.Minute)
}

func randomText(source io.Reader, length int) (string, error) {
	raw := make([]byte, length)
	if _, err := io.ReadFull(source, raw); err != nil {
		return "", err
	}
	for index := range raw {
		raw[index] = alphabet[int(raw[index])%len(alphabet)]
	}
	return string(raw), nil
}

var glyphs = map[byte][7]string{
	'2': {"11110", "00001", "00001", "11110", "10000", "10000", "11111"},
	'3': {"11110", "00001", "00001", "01110", "00001", "00001", "11110"},
	'4': {"10010", "10010", "10010", "11111", "00010", "00010", "00010"},
	'5': {"11111", "10000", "10000", "11110", "00001", "00001", "11110"},
	'6': {"01111", "10000", "10000", "11110", "10001", "10001", "01110"},
	'7': {"11111", "00001", "00010", "00100", "01000", "01000", "01000"},
	'8': {"01110", "10001", "10001", "01110", "10001", "10001", "01110"},
	'9': {"01110", "10001", "10001", "01111", "00001", "00001", "11110"},
	'A': {"01110", "10001", "10001", "11111", "10001", "10001", "10001"},
	'B': {"11110", "10001", "10001", "11110", "10001", "10001", "11110"},
	'C': {"01111", "10000", "10000", "10000", "10000", "10000", "01111"},
	'D': {"11110", "10001", "10001", "10001", "10001", "10001", "11110"},
	'E': {"11111", "10000", "10000", "11110", "10000", "10000", "11111"},
	'F': {"11111", "10000", "10000", "11110", "10000", "10000", "10000"},
	'G': {"01111", "10000", "10000", "10111", "10001", "10001", "01110"},
	'H': {"10001", "10001", "10001", "11111", "10001", "10001", "10001"},
	'J': {"00111", "00010", "00010", "00010", "00010", "10010", "01100"},
	'K': {"10001", "10010", "10100", "11000", "10100", "10010", "10001"},
	'L': {"10000", "10000", "10000", "10000", "10000", "10000", "11111"},
	'M': {"10001", "11011", "10101", "10101", "10001", "10001", "10001"},
	'N': {"10001", "11001", "10101", "10011", "10001", "10001", "10001"},
	'P': {"11110", "10001", "10001", "11110", "10000", "10000", "10000"},
	'Q': {"01110", "10001", "10001", "10001", "10101", "10010", "01101"},
	'R': {"11110", "10001", "10001", "11110", "10100", "10010", "10001"},
	'S': {"01111", "10000", "10000", "01110", "00001", "00001", "11110"},
	'T': {"11111", "00100", "00100", "00100", "00100", "00100", "00100"},
	'U': {"10001", "10001", "10001", "10001", "10001", "10001", "01110"},
	'V': {"10001", "10001", "10001", "10001", "10001", "01010", "00100"},
	'W': {"10001", "10001", "10001", "10101", "10101", "10101", "01010"},
	'X': {"10001", "10001", "01010", "00100", "01010", "10001", "10001"},
	'Y': {"10001", "10001", "01010", "00100", "00100", "00100", "00100"},
	'Z': {"11111", "00001", "00010", "00100", "01000", "10000", "11111"},
}

func renderChallenge(answer string, source io.Reader) (string, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, 300, 110))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: color.RGBA{245, 245, 245, 255}}, image.Point{}, draw.Src)
	randomBytes := make([]byte, 1800)
	if _, err := io.ReadFull(source, randomBytes); err != nil {
		return "", err
	}
	for index := 0; index < 350; index++ {
		x := int(randomBytes[index*2]) % canvas.Bounds().Dx()
		y := int(randomBytes[index*2+1]) % canvas.Bounds().Dy()
		shade := uint8(155 + randomBytes[700+index]%85)
		canvas.Set(x, y, color.RGBA{shade, shade, shade, 255})
	}
	for index := 0; index < 14; index++ {
		offset := 1050 + index*4
		x1 := int(randomBytes[offset]) % canvas.Bounds().Dx()
		y1 := int(randomBytes[offset+1]) % canvas.Bounds().Dy()
		x2 := int(randomBytes[offset+2]) % canvas.Bounds().Dx()
		y2 := int(randomBytes[offset+3]) % canvas.Bounds().Dy()
		drawLine(canvas, x1, y1, x2, y2, color.RGBA{110, uint8(105 + index*5), 145, 180})
	}
	for index := range answer {
		glyph := glyphs[answer[index]]
		scaleX := 5 + int(randomBytes[1110+index]%2)
		scaleY := 7 + int(randomBytes[1120+index]%2)
		shear := int(randomBytes[1130+index]%5) - 2
		x := 28 + index*52 + int(randomBytes[1140+index]%7) - 3
		y := 25 + int(randomBytes[1150+index]%11) - 5
		ink := color.RGBA{uint8(10 + randomBytes[1160+index]%55), uint8(10 + randomBytes[1170+index]%45), uint8(25 + randomBytes[1180+index]%65), 255}
		for row, line := range glyph {
			for column := range line {
				if line[column] != '1' {
					continue
				}
				cellX := x + column*scaleX + (row-3)*shear
				cellY := y + row*scaleY + ((column+int(randomBytes[1190+index]))%3 - 1)
				draw.Draw(canvas, image.Rect(cellX, cellY, cellX+scaleX, cellY+scaleY), &image.Uniform{C: ink}, image.Point{}, draw.Src)
			}
		}
	}
	// Foreground interference crosses glyphs. Drawing it last prevents a bot
	// from removing every line by simply subtracting the background layer.
	for index := 0; index < 9; index++ {
		offset := 1220 + index*4
		x1 := int(randomBytes[offset]) % canvas.Bounds().Dx()
		y1 := 15 + int(randomBytes[offset+1])%80
		x2 := int(randomBytes[offset+2]) % canvas.Bounds().Dx()
		y2 := 15 + int(randomBytes[offset+3])%80
		ink := color.RGBA{uint8(35 + randomBytes[1280+index]%80), uint8(35 + randomBytes[1290+index]%70), uint8(55 + randomBytes[1300+index]%80), 210}
		drawLine(canvas, x1, y1, x2, y2, ink)
	}
	for index := 0; index < 45; index++ {
		offset := 1320 + index*4
		x := int(randomBytes[offset]) % canvas.Bounds().Dx()
		y := 10 + int(randomBytes[offset+1])%90
		width := 1 + int(randomBytes[offset+2]%4)
		height := 1 + int(randomBytes[offset+3]%4)
		shade := uint8(45 + randomBytes[1500+index]%100)
		draw.Draw(canvas, image.Rect(x, y, x+width, y+height), &image.Uniform{C: color.RGBA{shade, shade, uint8(min(200, int(shade)+25)), 230}}, image.Point{}, draw.Src)
	}
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(output.Bytes()), nil
}

func drawLine(canvas *image.RGBA, x0, y0, x1, y1 int, ink color.Color) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := -1, -1
	if x0 < x1 {
		sx = 1
	}
	if y0 < y1 {
		sy = 1
	}
	err := dx + dy
	for {
		canvas.Set(x0, y0, ink)
		if x0 == x1 && y0 == y1 {
			return
		}
		twice := 2 * err
		if twice >= dy {
			err += dy
			x0 += sx
		}
		if twice <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
