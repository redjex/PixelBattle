package domain

import "time"

type PlacementRequest struct {
	Type        string `json:"type"`
	BoardID     string `json:"boardId"`
	X           int    `json:"x"`
	Y           int    `json:"y"`
	Color       string `json:"color"`
	OperationID string `json:"operationId"`
	UseIce      bool   `json:"useIce,omitempty"`
}

type PixelEvent struct {
	Type        string      `json:"type"`
	EventID     string      `json:"eventId"`
	BoardID     string      `json:"boardId"`
	X           int         `json:"x"`
	Y           int         `json:"y"`
	Color       string      `json:"color"`
	OperationID string      `json:"operationId"`
	UserID      string      `json:"userId"`
	Author      PixelAuthor `json:"author"`
	Version     int64       `json:"version"`
	CreatedAt   time.Time   `json:"createdAt"`
	FrozenUntil *time.Time  `json:"frozenUntil,omitempty"`
}

type PixelAuthor struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Username    string `json:"username,omitempty"`
	PhotoURL    string `json:"photoUrl,omitempty"`
}

type BoardPixel struct {
	X           int         `json:"x"`
	Y           int         `json:"y"`
	Color       string      `json:"color"`
	Version     int64       `json:"version"`
	Author      PixelAuthor `json:"author"`
	FrozenUntil *time.Time  `json:"frozenUntil,omitempty"`
}
