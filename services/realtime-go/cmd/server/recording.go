package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"pixelbattle/realtime/internal/domain"
)

const (
	recordingFrameInterval = time.Second
	maxRecordingFrames     = 18000
)

type recordingPixel struct {
	Event domain.PixelEvent `json:"event"`
	At    time.Time         `json:"at"`
}
type gameRecorder struct {
	mu                sync.Mutex
	active, exporting bool
	startedAt         time.Time
	width, height     int
	initial           []domain.BoardPixel
	events            []recordingPixel
	statePath         string
	lastVideoPath     string
}
type recordingState struct {
	Active        bool      `json:"active"`
	StartedAt     time.Time `json:"startedAt"`
	Width, Height int
	Initial       []domain.BoardPixel `json:"initial"`
	Events        []recordingPixel    `json:"events"`
}
type recordingStatus struct {
	Active    bool      `json:"active"`
	StartedAt time.Time `json:"startedAt,omitempty"`
	Width     int       `json:"width,omitempty"`
	Height    int       `json:"height,omitempty"`
	Events    int       `json:"events"`
}

func newGameRecorder(statePath string) *gameRecorder {
	r := &gameRecorder{statePath: statePath}
	if statePath != "" {
		r.lastVideoPath = filepath.Join(filepath.Dir(statePath), "last-recording.mp4")
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		return r
	}
	var s recordingState
	if json.Unmarshal(data, &s) == nil && s.Active && !s.StartedAt.IsZero() {
		r.active, r.startedAt, r.width, r.height, r.initial, r.events = true, s.StartedAt, s.Width, s.Height, s.Initial, s.Events
	}
	return r
}
func (r *gameRecorder) persistLocked() error {
	if r.statePath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(r.statePath), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(recordingState{r.active, r.startedAt, r.width, r.height, r.initial, r.events})
	if err != nil {
		return err
	}
	tmp := r.statePath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, r.statePath)
}
func (r *gameRecorder) Start(width, height int, pixels []domain.BoardPixel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active {
		return fmt.Errorf("recording is already active")
	}
	r.active, r.exporting, r.startedAt, r.width, r.height = true, false, time.Now().UTC(), width, height
	r.initial = append([]domain.BoardPixel(nil), pixels...)
	r.events = nil
	if err := r.persistLocked(); err != nil {
		r.active = false
		return fmt.Errorf("persist recording: %w", err)
	}
	return nil
}
func (r *gameRecorder) Record(event domain.PixelEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.active || r.exporting || len(r.events) >= 500000 {
		return
	}
	r.events = append(r.events, recordingPixel{event, event.CreatedAt})
	if len(r.events)%25 == 0 {
		_ = r.persistLocked()
	}
}
func (r *gameRecorder) Status() recordingStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return recordingStatus{r.active, r.startedAt, r.width, r.height, len(r.events)}
}
func (r *gameRecorder) Stop(ctx context.Context) ([]byte, error) {
	r.mu.Lock()
	if !r.active {
		r.mu.Unlock()
		if r.lastVideoPath != "" {
			if video, err := os.ReadFile(r.lastVideoPath); err == nil && len(video) > 0 {
				return video, nil
			}
		}
		return nil, fmt.Errorf("recording is not active")
	}
	if r.exporting {
		r.mu.Unlock()
		return nil, fmt.Errorf("recording export is already running")
	}
	r.exporting = true
	startedAt, width, height := r.startedAt, r.width, r.height
	initial := append([]domain.BoardPixel(nil), r.initial...)
	events := append([]recordingPixel(nil), r.events...)
	r.mu.Unlock()
	video, err := exportRecording(ctx, startedAt, width, height, initial, events)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.exporting = false
	if err != nil {
		return nil, err
	}
	if r.lastVideoPath != "" {
		_ = os.WriteFile(r.lastVideoPath, video, 0o640)
	}
	r.active, r.startedAt, r.initial, r.events = false, time.Time{}, nil, nil
	_ = r.persistLocked()
	return video, nil
}
func exportRecording(ctx context.Context, startedAt time.Time, width, height int, initial []domain.BoardPixel, events []recordingPixel) ([]byte, error) {
	endedAt := time.Now().UTC()
	if len(events) > 0 && events[len(events)-1].At.After(endedAt) {
		endedAt = events[len(events)-1].At
	}
	duration := endedAt.Sub(startedAt)
	if duration < recordingFrameInterval {
		duration = recordingFrameInterval
	}
	directory, err := os.MkdirTemp("", "pixelbattle-recording-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(directory)
	state := make(map[[2]int]domain.BoardPixel, len(initial))
	for _, p := range initial {
		state[[2]int{p.X, p.Y}] = p
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	frameNumber := 0
	writeFrame := func() error {
		frameNumber++
		pixels := make([]domain.BoardPixel, 0, len(state))
		for _, p := range state {
			pixels = append(pixels, p)
		}
		file, e := os.Create(filepath.Join(directory, fmt.Sprintf("frame-%06d.png", frameNumber)))
		if e != nil {
			return e
		}
		encErr := png.Encode(file, renderMapCanvas(width, height, pixels))
		closeErr := file.Close()
		if encErr != nil {
			return encErr
		}
		return closeErr
	}
	if len(events) > maxRecordingFrames-1 {
		events = events[:maxRecordingFrames-1]
	}
	if len(events) == 0 {
		frameCount := int(duration.Seconds() * 5)
		if frameCount < 1 {
			frameCount = 1
		}
		if frameCount > maxRecordingFrames {
			frameCount = maxRecordingFrames
		}
		for i := 0; i < frameCount; i++ {
			if err := writeFrame(); err != nil {
				return nil, err
			}
		}
	} else {
		if err := writeFrame(); err != nil {
			return nil, err
		}
		for _, recorded := range events {
			e := recorded.Event
			state[[2]int{e.X, e.Y}] = domain.BoardPixel{X: e.X, Y: e.Y, Color: e.Color, Version: e.Version, Author: e.Author, FrozenUntil: e.FrozenUntil}
			if err := writeFrame(); err != nil {
				return nil, err
			}
		}
	}
	outputPath := filepath.Join(directory, "pixelbattle-recording.mp4")
	rate := "5"
	input := filepath.Join(directory, "frame-%06d.png")
	command := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-framerate", rate, "-i", input, "-vf", "setpts=PTS/3", "-c:v", "libx264", "-preset", "veryfast", "-crf", "20", "-pix_fmt", "yuv420p", "-movflags", "+faststart", outputPath)
	if output, ffmpegErr := command.CombinedOutput(); ffmpegErr != nil {
		fallback := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "error", "-y", "-framerate", rate, "-i", input, "-vf", "setpts=PTS/3", "-c:v", "mpeg4", "-q:v", "4", "-pix_fmt", "yuv420p", outputPath)
		if fallbackOutput, fallbackErr := fallback.CombinedOutput(); fallbackErr != nil {
			return nil, fmt.Errorf("ffmpeg failed: %w (%s; fallback: %s)", ffmpegErr, bytes.TrimSpace(output), bytes.TrimSpace(fallbackOutput))
		}
	}
	return os.ReadFile(outputPath)
}
